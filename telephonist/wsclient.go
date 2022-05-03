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
	"github.com/rs/zerolog"
)

var (
	wsLogger = logging.ChildLogger("ws")
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
	wsLogger.Info().Str("url", u.String()).Msg("connecting to the Telephonist server")

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
			wsLogger.Error().Err(c.lastError).Msg("failed to connect to the server")
		} else {
			c.requireState(StatePreparing)
			go c.readPump()
			c.writePump()
		}

		// TODO: make restarting optional
		wsLogger.Warn().Msgf("restarting in %s", restartTimeout.String())
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

func (c *WSClient) SendTasksSync() error {
	return c.SendWebsocketMessage(MO_TASK_SYNC, nil)
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
	expectedArr := zerolog.Arr()
	for _, expected := range expectedState {
		expectedArr.Str(expected.String())
	}

	wsLogger.Fatal().
		Str("got", c.state.String()).
		Array("expected", expectedArr).
		Msg("invalid client state encountered")
}

func (c *WSClient) setState(state ClientState) {
	wsLogger.Debug().Msgf("state: %s -> %s", c.state, state)
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
	u.Path = "/api/application-v1/ws/" + prefix
	u.RawQuery = "ticket=" + ticket
	return u
}

func (c *WSClient) writePump() {
	t := time.NewTicker(pingPeriod)
	wsLogger.Debug().Msg("writePump: starting")

	var msg oRawMessage
	looping := true

	for looping {
		select {
		case msg, looping = <-c.sendMessages:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !looping {
				wsLogger.Debug().Msg("sendMessages channel is closed")
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
				wsLogger.Error().Err(err).Str("string message", string(mb)).Msg("failed to marshal message")
				continue
			}
			_, err = w.Write(mb)
			if err != nil {
				wsLogger.Error().Err(err).Msg("failed to write to socket")
				looping = false
			} else {
				wsLogger.Debug().Str("string message", string(mb)).Msg("WS message")
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
	wsLogger.Debug().Msg("readPump: starting")
	for {
		err := c.readMessage(&rm)
		if err != nil {
			if err == ErrInvalidWebsocketMessage {
				continue
			}
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				wsLogger.Error().Err(err).Msg("unexpectedly closed the connection")
				break
			} else {
				wsLogger.Error().Err(err).Msg("unexpected error encountered, will assume that connection is closed")
			}
		}
		err = c.handleMessage(rm)
		if err != nil {
			wsLogger.Error().
				Str("msg_type", rm.MessageType).
				Err(err).
				Msg("failed to handle the message")
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
		wsLogger.Warn().Msg("connection closed")
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
	err = json.Unmarshal(message, rm)
	if err != nil {
		wsLogger.Error().Msg("received malformed message from the backend")
		return ErrInvalidWebsocketMessage
	}
	return nil
}

// #endregion

// #region Handling messages

func (c *WSClient) handleMessage(m iRawMesage) (err error) {
	wsLogger.Debug().
		Str("msg_type", m.MessageType).
		Msgf("handling message \"%s\"", m.MessageType)

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
		d := []*DefinedTask{}
		err = convertToStruct(m.Data, &d)
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
		err = convertToStruct(m.Data, &id)
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
	case "logs_sent":
		return
	default:
		return fmt.Errorf("received unknown message: %s", m.MessageType)
	}
	return
}

func (c *WSClient) onGreetings() {
	if c.opts.OnConnected != nil {
		c.opts.OnConnected()
	}
	messages := make([]string, len(c.eventChannels))
	i := 0
	for event, _ := range c.eventChannels {
		messages[i] = event
		i += 1
	}
	c.sendMessages <- oRawMessage{
		MessageType: MO_SET_SUBSCRIPTIONS,
		Data:        messages,
	}
}

func (c *WSClient) onIntroduction(d IntroductionData) {
	if c.state != StatePreparing {
		wsLogger.Warn().Msg("(BUG?) received \"introduction\" message from the server more than twice, this might indicate a bug on the server side or here, on the client")
		return
	}
	wsLogger.Debug().
		Str("app_id", d.AppId).
		Str("server_version", d.ServerVersion).
		Msg("got introduction from the server")
	c.setState(StateConnected)
	osInfo, err := getOsInfo()
	if err != nil {
		wsLogger.Warn().Err(err).Msg("failed to get os info")
	}
	message := &HelloMessage{
		Subscriptions:     []string{},
		SupportedFeatures: []string{"settings:sync", "settings", "purpose:application_host"},
		ClientName:        "Telephonist Agent",
		PID:               os.Getpid(),
		ClientVersion:     "0.3.1",
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
	wsLogger.Debug().
		Str("event_key", d.EventKey).
		Str("app_id", d.AppID).
		Msg("got new event")
	if channels, exists := c.eventChannels[d.EventKey]; exists {
		for id, ch := range channels {
			select {
			case ch <- d:
			case <-time.After(time.Millisecond * 1500):
				wsLogger.Warn().Msgf("failed to put event in a queue eventChannels[%s][%d] with 1500ms timeout", d.EventKey, id)
			}
		}
	}
}

func (c *WSClient) onNewTasks(tasks []*DefinedTask) {
	if c.opts.OnTasks != nil {
		c.opts.OnTasks(tasks)
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
		c.sendMessages <- oRawMessage{
			MessageType: MO_SUBSCRIBE,
			Data:        eventName,
		}
	}
	return id
}

func (c *WSClient) removeEventChannel(eventName string, id uint32) {
	if channels, exists := c.eventChannels[eventName]; exists {
		delete(channels, id)
		if len(channels) == 0 {
			delete(c.eventChannels, eventName)
			c.sendMessages <- oRawMessage{
				MessageType: MO_UNSUBSCRIBE,
				Data:        eventName,
			}
		}
	}
}

func (c *WSClient) onServerError(d ErrorData) {
	wsLogger.Warn().
		Str("error_type", d.ErrorType).
		Str("exception", d.Exception).
		Str("error", d.Error).
		Msg("received an error from the server")
}

// #endregion

// #endregion
