package telephonist

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MaratBR/TelephonistAgent/logging"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	wsLogger = logging.ChildLogger("telephonist-ws")
)

const (
	restartTimeout = time.Second * 20
)

type ClientState uint8

func (s ClientState) String() string {
	switch s {
	case StateStopped:
		return "Stopped"
	case StateDisconnected:
		return "Disconnected"
	case StateConnecting:
		return "Connecting"
	case StatePreparing:
		return "Preparing"
	case StateConnected:
		return "Connected"
	default:
		return "Unknown"
	}
}

const (
	StateStopped      ClientState = iota // client stopped, not connected
	StateDisconnected                    // client started, but not connected
	StateConnecting                      // connecting to the server
	StatePreparing                       // connected, but did not complete introduction sequence
	StateConnected                       // connected and ready to go
)

type WSClientOptions struct {
	Timeout                time.Duration
	ConnectionRetryTimeout time.Duration
	CompatibilityKey       string
	InstanceID             uuid.UUID
	MachineID              string
	ConnectionID           uuid.UUID
	OnTask                 TaskAddedCallback
	OnTasks                TasksUpdatedCallback
	OnTaskRemoved          TaskRemovedCallback
	OnConnected            func()
}

type WSClient struct {
	opts             WSClientOptions
	client           *Client
	conn             *websocket.Conn
	sendMessages     chan oRawMessage
	state            ClientState
	userAgent        string
	ConnectionID     uuid.UUID
	eventChannels    map[string]map[uint32]eventsChannel
	eventChannelsSeq uint32
	lastError        error
	mut              sync.Mutex
}

func NewWSClient(client *Client, options WSClientOptions) *WSClient {
	if client == nil {
		panic("client is nil")
	}
	connectionID := options.ConnectionID
	if uuid.Nil == connectionID {
		connectionID = uuid.New()
	}
	return &WSClient{
		opts:          options,
		client:        client,
		eventChannels: make(map[string]map[uint32]eventsChannel),
		ConnectionID:  connectionID,
	}
}

// #region Websocket connection

// #region WS client

func (c *WSClient) IsStarted() bool {
	return c.state != StateStopped
}

func (c *WSClient) IsConnected() bool {
	return c.state == StateConnected
}

func (c *WSClient) GetLastError() error {
	return c.lastError
}

func (c *WSClient) StartAsync() {
	go c.runLoop()
}

func (c *WSClient) Start() error {
	c.runLoop()
	return c.lastError
}

func (c *WSClient) connect() error {
	c.transitionState(StateDisconnected, StateConnecting)

	// issue ticket
	ticket, errs := c.client.issueWSTicket()
	if errs != nil {
		c.transitionState(StateConnecting, StateDisconnected)
		if len(errs) == 1 {
			if _, ok := errs[0].(*UnexpectedStatusCode); ok {
				return errs[0]
			}
			return fmt.Errorf("failed to issue WS ticket: %s", errs[0].Error())
		} else {
			sb := strings.Builder{}
			for index, err := range errs {
				sb.WriteString("\n\t" + strconv.Itoa(index+1) + ") " + err.Error())
			}
			return fmt.Errorf("failed to issue WS ticket: %s", sb.String())
		}
	}

	return c.connectWithTicket(ticket)
}

func (c *WSClient) connectWithTicket(ticket string) error {
	c.requireState(StateConnecting)

	c.sendMessages = make(chan oRawMessage)

	var err error
	u := c.getWebsocketURL("report", ticket)
	wsLogger.Debug("connecting to the Telephonist server", zap.Stringer("url", u))

	c.conn, _, err = websocket.DefaultDialer.Dial(u.String(), http.Header{
		"User-Agent": []string{c.userAgent},
	})
	if err != nil {
		c.transitionState(StateConnecting, StateDisconnected)
		return err
	}
	c.transitionState(StateConnecting, StatePreparing)
	return nil
}

func (c *WSClient) runLoop() {
	c.transitionState(StateStopped, StateDisconnected)
	for c.state != StateStopped {
		c.lastError = c.connect()
		if c.lastError != nil {
			wsLogger.Error("failed to connect to the server", zap.Error(c.lastError))
		} else {
			c.requireState(StatePreparing)
			go c.readPump()
			c.writePump()
		}

		// TODO: make restarting optional
		wsLogger.Warn(fmt.Sprintf("restarting in %s", restartTimeout.String()))
		time.Sleep(restartTimeout)
	}
}

func (c *WSClient) Stop() {
	if c.state == StateStopped {
		return
	}

	// this will cause writePump to stop and then close WS connection
	// which in turn will stop readPump
	c.state = StateStopped
	close(c.sendMessages)

}

func (c *WSClient) SendWebsocketMessage(messageType string, data interface{}) error {
	if c.state != StateConnected {
		return ErrClientIsNotConnected
	}
	c.sendMessages <- oRawMessage{MessageType: messageType, Data: data}
	return nil
}

func (c *WSClient) SendTasksSync(tasks []*DefinedTask) error {
	return c.SendWebsocketMessage(MO_TASK_SYNC, tasks)
}

func (c *WSClient) SendLogs(logs *LogMessage) error {
	return c.SendWebsocketMessage(MO_SEND_LOG, logs)
}

func (c *WSClient) requireState(expectedState ...ClientState) {
	for _, expected := range expectedState {
		if c.state == expected {
			return
		}
	}
	wsLogger.Fatal("invalid client state encountered",
		zap.Array("expected", zapcore.ArrayMarshalerFunc(func(enc zapcore.ArrayEncoder) error {
			for _, expected := range expectedState {
				enc.AppendString(expected.String())
			}
			return nil
		})),
		zap.String("got", c.state.String()),
	)
}

func (c *WSClient) setState(state ClientState) {
	c.state = state
}

func (c *WSClient) transitionState(expectedState, newState ClientState) {
	c.requireState(expectedState)
	c.setState(newState)
}

func (c *WSClient) getWebsocketURL(prefix, ticket string) *url.URL {
	u := new(url.URL)
	*u = *c.client.opts.URL
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	if prefix != "" && prefix[0] == '/' {
		prefix = prefix[1:]
	}
	u.Path = "_ws/application/" + prefix
	u.RawQuery = "ticket=" + ticket
	return u
}

func (c *WSClient) writePump() {
	t := time.NewTicker(pingPeriod)
	wsLogger.Debug("writePump: starting")

	var msg oRawMessage
	looping := true

	for looping {
		select {
		case msg, looping = <-c.sendMessages:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !looping {
				wsLogger.Debug("sendMessages channel is closed")
				break
			}
			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				// if connection is gone, stop the loop
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					looping = false
				}
				continue
			}
			var mb []byte
			mb, err = json.Marshal(&msg)
			if err != nil {
				wsLogger.Error("failed to marshal message")
				continue
			}
			_, err = w.Write(mb)
			if err != nil {
				wsLogger.Error("failed to write to socket", zap.String("error", err.Error()))
				looping = false
			}
			w.Close()
		case <-t.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				looping = false
			}
		}
	}

	c.onDisconnect()
}

func (c *WSClient) readPump() {
	var rm iRawMesage
	wsLogger.Debug("readPump: starting")
	for {
		err := c.readMessage(&rm)
		if err != nil {
			if err == ErrInvalidWebsocketMessage {
				continue
			}
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				// we need to
				wsLogger.Error("unexpectedly closed the connection", zap.Error(err))
			}
			break
		}
		err = c.handleMessage(rm)
		if err != nil {
			wsLogger.Error("failed to handle the message",
				zap.String("msg_type", rm.MessageType),
				zap.Error(err),
			)
		}
	}
	c.onDisconnect()
}

func (c *WSClient) onDisconnect() {
	c.mut.Lock()
	defer c.mut.Unlock()
	if c.state == StateConnected || c.state == StatePreparing {
		c.conn.SetWriteDeadline(time.Now().Add(time.Millisecond * 100))
		c.conn.WriteMessage(websocket.CloseMessage, []byte{})
		c.conn.Close()
		wsLogger.Warn("connection closed")
		c.setState(StateDisconnected)
	}
}

func (c *WSClient) readMessage(rm *iRawMesage) error {
	messageType, message, err := c.conn.ReadMessage()
	if err != nil {
		return err
	}
	if messageType != websocket.TextMessage {
		return ErrInvalidWebsocketMessage
	}
	return json.Unmarshal(message, rm)
}

// #endregion

// #region Handling messages

func (c *WSClient) handleMessage(m iRawMesage) (err error) {
	wsLogger.Debug(fmt.Sprintf("handling message \"%s\"", m.MessageType), zap.String("msg_type", m.MessageType))

	switch m.MessageType {
	case MI_ERROR:
		var d ErrorData
		err = convertToStruct(m.Data, &d)
		if err == nil {
			c.onServerError(d)
		}
	case MI_NEW_EVENT:
		var d Event
		err = convertToStruct(m.Data, &d)
		if err == nil {
			c.onNewEvent(d)
		}
	case MI_TASKS:
		d := new(TasksIncomingMessage)
		err = convertToStruct(m.Data, d)
		if err == nil {
			c.onNewTasks(d)
		}
	case MI_TASK_UPDATED:
		d := new(DefinedTask)
		err = convertToStruct(m.Data, d)
		if err == nil {
			c.onNewTask(d)
		}
	case MI_TASK_REMOVED:
		var id uuid.UUID
		err = convertToStruct(m.Data, id)
		if err == nil {
			c.onTaskRemoved(id)
		}
	case MI_INTRODUCTION:
		var d IntroductionData
		err = convertToStruct(m.Data, &d)
		if err == nil {
			c.onIntroduction(d)
		}
	case "bound_to_sequences":
		d := []string{}
		err = convertToStruct(m.Data, d)
		if err == nil {
			c.onBoundToSequences(d)
		}
	case "greetings":
		c.onGreetings()
	default:
		return fmt.Errorf("received unknown message: %s", m.MessageType)
	}
	return
}

func (c *WSClient) onGreetings() {
	if c.opts.OnConnected != nil {
		c.opts.OnConnected()
	}
}

func (c *WSClient) onIntroduction(d IntroductionData) {
	if c.state != StatePreparing {
		wsLogger.Warn("(BUG?) received \"introduction\" message from the server more than twice, this might indicate a bug on the server side or here, on the client")
		return
	}
	wsLogger.Debug("got introduction from the server",
		zap.String("app_id", d.AppId),
		zap.String("server_version", d.ServerVersion),
	)
	c.setState(StateConnected)
	osInfo, err := getOsInfo()
	if err != nil {
		wsLogger.Debug("failed to get os info", zap.Error(err))
	}
	message := &HelloMessage{
		Subscriptions:     []string{},
		SupportedFeatures: []string{"settings:sync", "settings", "purpose:application_host"},
		ClientName:        "Telephonist Agent",
		PID:               os.Getpid(),
		ClientVersion:     "0.1.0dev",
		OS:                osInfo,
		CompatibilityKey:  c.opts.CompatibilityKey,
		InstanceID:        c.opts.InstanceID,
		MachineID:         c.opts.MachineID,
		ConnectionUUID:    c.ConnectionID,
	}
	c.sendMessages <- oRawMessage{
		MessageType: "hello",
		Data:        message,
	}
}

func (c *WSClient) onBoundToSequences(sequences []string) {
	// TODO: implement?
}

func (c *WSClient) onNewEvent(d Event) {
	wsLogger.Debug("got new event",
		zap.String("event_key", d.EventKey),
		zap.String("app_id", d.AppID),
	)
	if channels, exists := c.eventChannels[d.EventKey]; exists {
		for id, ch := range channels {
			select {
			case ch <- d:
			case <-time.After(time.Millisecond * 100):
				wsLogger.Warn(fmt.Sprintf("failed to put event in a queue eventChannels[%s][%d] with 100ms timeout", d.EventKey, id))
			}
		}
	}
}

func (c *WSClient) onNewTasks(tasks *TasksIncomingMessage) {
	if c.opts.OnTasks != nil {
		c.opts.OnTasks(tasks.Tasks)
	}
}

func (c *WSClient) onNewTask(task *DefinedTask) {
	if c.opts.OnTask != nil {
		c.opts.OnTask(task)
	}
}

func (c *WSClient) onTaskRemoved(id uuid.UUID) {
	if c.opts.OnTaskRemoved != nil {
		c.opts.OnTaskRemoved(id)
	}
}

func (c *WSClient) NewQueue() *EventQueue {
	return &EventQueue{
		Channel:    make(chan Event),
		wsc:        c,
		subscribed: make(map[string]uint32),
	}
}

func (c *WSClient) addEventChannel(eventName string, channel eventsChannel) uint32 {
	id := atomic.AddUint32(&c.eventChannelsSeq, 1)
	if channels, exists := c.eventChannels[eventName]; exists {
		channels[id] = channel
	} else {
		c.eventChannels[eventName] = map[uint32]eventsChannel{id: channel}
	}
	return id
}

func (c *WSClient) removeEventChannel(eventName string, id uint32) {
	if channels, exists := c.eventChannels[eventName]; exists {
		delete(channels, id)
	}
}

func (c *WSClient) onServerError(d ErrorData) {
	wsLogger.Warn("received an error from the server",
		zap.String("error_type", d.ErrorType),
		zap.String("exception", d.Exception),
		zap.String("error", d.Error))
}

// #endregion

// #endregion
