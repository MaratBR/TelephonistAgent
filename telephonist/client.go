package telephonist

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	log "github.com/inconshreveable/log15"
	"github.com/parnurzeal/gorequest"
)

var (
	ErrInvalidWebsocketMessage error = errors.New("invalid websocket message received")
	ErrClientIsAlreadyRunning  error = errors.New("client is already running")
)

const (
	maxMessageSize = 4096
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	VERSION        = "0.1.0"
)

type ClientState uint8

func (s ClientState) String() string {
	switch s {
	case StateConnecting:
		return "Connecting"
	case StateDisconnected:
		return "Disconnected"
	case StateReady:
		return "Ready"
	case StatePreparing:
		return "Preparing"
	default:
		return "Unknown"
	}
}

const (
	StateDisconnected ClientState = 0
	StateConnecting   ClientState = 1
	StatePreparing    ClientState = 2
	StateReady        ClientState = 3
)

type ClientOptions struct {
	APIKey                 string
	URL                    *url.URL
	Timeout                time.Duration
	Logger                 log.Logger
	ConnectionRetryTimeout time.Duration
	CompatibilityKey       string
	InstanceID             string
	MachineID              string
	OnNewEvent             OnEventCallback
	OnPersistState         OnPersistStateCallback
}

type ClientPersistentState struct {
	BoundSequences []string
}

func newPersistentState() *ClientPersistentState {
	return &ClientPersistentState{
		BoundSequences: []string{},
	}
}

type OnPersistStateCallback func(state *ClientPersistentState) error
type OnEventCallback func(event Event) error

type Client struct {
	log log.Logger

	// rest api options
	opts         ClientOptions
	runLoopMutex *sync.Mutex
	conn         *websocket.Conn

	// ws
	isRunning        bool
	closeChan        chan struct{}
	sendMessages     chan rawMessage
	state            ClientState
	persistentState  *ClientPersistentState
	ready            bool
	userAgent        string
	boundSequenceIDs []string
}

func NewClient(options ClientOptions) (*Client, error) {
	if options.URL.Scheme != "http" && options.URL.Scheme != "https" {
		return nil, errors.New("schema must be http or https")
	}
	if options.ConnectionRetryTimeout == 0 {
		options.ConnectionRetryTimeout = time.Second * 30
	}
	if options.Timeout == 0 {
		options.Timeout = time.Second * 20
	}
	if options.CompatibilityKey == "" {
		options.CompatibilityKey = "golang-supervisor-compatibility-v1"
	}
	logger := log.New(log.Ctx{"module": "telephonist/client"})
	if options.Logger != nil {
		logger = options.Logger
	}
	return &Client{
		opts:         options,
		runLoopMutex: &sync.Mutex{},
		log:          logger,
		state:        StateDisconnected,
		userAgent: fmt.Sprintf(
			"Telephonist Supervisor (Golang) " + VERSION),
		persistentState: newPersistentState(),
	}, nil
}

//#region REST API

func (c Client) postJSON(relativeURL string, data interface{}, response interface{}) (gorequest.Response, CombinedError) {
	req := gorequest.New().Post(c.getUrl(relativeURL)).Set("Authorization", "Bearer "+c.opts.APIKey)
	if data != nil {
		req = req.SendStruct(data)
	}
	var (
		err  CombinedError
		resp gorequest.Response
	)
	if response == nil {
		resp, _, err = req.End()
	} else {
		resp, _, err = req.EndStruct(response)
	}
	if err == nil && resp.StatusCode > 299 {
		return resp, CombinedError{HTTPError{Status: resp.StatusCode}}
	}
	return resp, err
}

func (c Client) getUrl(url string) string {
	u := *c.opts.URL
	u.Path = path.Join(u.Path, url)
	return u.String()
}

func (c Client) Publish(event EventData) CombinedError {
	resp, err := c.postJSON("events/publish", event, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return []error{fmt.Errorf("unexpected status code: {0}", resp.Status)}
	}
	log.Debug("published event", log.Ctx{"name": event.Name})
	return nil
}

func (c Client) UpdateSequencMeta(sequenceID string, meta map[string]interface{}) CombinedError {
	resp, err := c.postJSON("events/sequence/"+sequenceID+"/meta", meta, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return []error{fmt.Errorf("unexpected status code: {0}", resp.Status)}
	}
	log.Debug("updated sequence meta", log.Ctx{"sequence_id": sequenceID, "meta": meta})
	return nil
}

func (c Client) CreateSequence(data CreateSequenceRequest) (*IDResponse, CombinedError) {
	resp := new(IDResponse)
	_, err := c.postJSON("events/sequence", data, resp)
	if err != nil {
		return nil, err
	}
	log.Debug("create new sequence", log.Ctx{"sequence_id": resp.ID})
	return resp, nil
}

func (c Client) FinishSequence(sequenceID string, body FinishSequenceRequest) (*DetailResponse, CombinedError) {
	resp := new(DetailResponse)
	_, _, errs := gorequest.New().
		Post(c.getUrl("events/sequence/"+sequenceID+"/finish")).
		SendStruct(body).
		Set("Authorization", "Bearer "+c.opts.APIKey).
		EndStruct(resp)
	if errs != nil {
		return nil, CombinedError(errs)
	}
	return resp, nil
}

func (c Client) issueWSTicket() (string, []error) {
	req := gorequest.New()
	resp, _, errs := req.
		Post(c.getUrl("applications/issue-ws-ticket")).
		Set("Authorization", "Bearer "+c.opts.APIKey).
		End()
	if errs != nil {
		return "", errs
	}
	if resp.StatusCode != 200 {
		return "", []error{fmt.Errorf("unexpected status code: {0}", resp.Status)}
	}
	decoder := json.NewDecoder(resp.Body)
	var ticketBody TicketResponse
	err := decoder.Decode(&ticketBody)
	if err != nil {
		return "", []error{fmt.Errorf("failed to decode backend response (WS ticket): %s", err.Error())}
	}
	return ticketBody.Ticket, nil
}

//#endregion

//#region Websocket connection

func (c Client) IsRunning() bool {
	return c.isRunning
}

func (c *Client) Run() error {
	c.runLoopMutex.Lock()
	if c.isRunning {
		c.runLoopMutex.Unlock()
		return ErrClientIsAlreadyRunning
	}
	c.isRunning = true
	c.runLoopMutex.Unlock()
	ticket, errs := c.issueWSTicket()
	if errs != nil {
		if len(errs) == 1 {
			return fmt.Errorf("failed to issue WS ticket: %s", errs[0].Error())
		} else {
			sb := strings.Builder{}
			for index, err := range errs {
				sb.WriteString("\n\t" + strconv.Itoa(index+1) + ") " + err.Error())
			}
			return fmt.Errorf("failed to issue WS ticket: %s", sb.String())
		}
	}
	return c.runWithTicket(ticket)
}

func (c *Client) Stop() {
	if !c.isRunning {
		c.closeChan <- struct{}{}
	}
}

func (c *Client) requireState(expectedState ClientState) {
	if c.state != expectedState {
		c.log.Crit(fmt.Sprintf("invalid client state encountered expected: %s, got: %s", c.state, expectedState))
	}
}

func (c *Client) setState(state ClientState) {
	c.state = state
}

func (c *Client) transitionState(expectedState, newState ClientState) {
	c.requireState(expectedState)
	c.setState(newState)
}

func (c *Client) runWithTicket(ticket string) error {
	c.transitionState(StateDisconnected, StateConnecting)
	defer c.setState(StateDisconnected)

	c.closeChan = make(chan struct{})
	c.sendMessages = make(chan rawMessage)

	var schema string
	if c.opts.URL.Scheme == "https" {
		schema = "wss"
	} else {
		schema = "ws"
	}
	var err error
	u := url.URL{
		Scheme:   schema,
		Host:     c.opts.URL.Host,
		Path:     path.Join(c.opts.URL.Path, "applications/ws"),
		RawQuery: "ticket=" + ticket,
	}
	log.Debug(fmt.Sprintf("connecting to %s", u.String()))

	c.conn, _, err = websocket.DefaultDialer.Dial(u.String(), http.Header{
		"User-Agent": []string{c.userAgent},
	})
	if err != nil {
		return err
	}
	c.transitionState(StateConnecting, StatePreparing)
	go c.readPump()
	go c.writePump()

	<-c.closeChan

	if c.opts.OnPersistState != nil {
		c.opts.OnPersistState(c.persistentState)
	}

	return nil
}

func (c *Client) writePump() {
	if !c.isRunning {
		log.Crit("cannot start writePump, client isn't running")
	}
	t := time.NewTicker(pingPeriod)
	log.Debug("writePump: starting")
	defer log.Debug("writePump: exiting")
	for {
		select {
		case rm, ok := <-c.sendMessages:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				log.Debug("sendMessages channel is closed, sending close message to the server")
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			var mb []byte
			mb, err = json.Marshal(&rm)
			if err != nil {
				return
			}
			_, err = w.Write(mb)
			if err != nil {
				log.Error("failed to write to socket", log.Ctx{"error": err.Error()})
			}
			err = w.Close()
		case <-t.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) readPump() {
	if c.state != StatePreparing && c.state != StateReady {
		log.Crit("invalid state", StatePreparing, StateReady, c.state)
	}
	if !c.isRunning {
		log.Crit("cannot start readPump, client isn't running")
	}
	var rm rawMessage
	log.Debug("readPump: starting")
	defer log.Debug("readPump: exiting")
	defer close(c.sendMessages)
	for {
		err := c.readMessage(&rm)
		if err != nil {
			if err == ErrInvalidWebsocketMessage {
				continue
			}
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Error("unexpectedly closed the connection", log.Ctx{"error": err.Error()})
			}
			break
		}
		err = c.handleMessage(rm)
		if err != nil {
			log.Error("failed to handle the message", log.Ctx{"msg_type": rm.MessageType, "error": err.Error()})
		}
	}

}

func (c *Client) readMessage(rm *rawMessage) error {
	messageType, message, err := c.conn.ReadMessage()
	if err != nil {
		return err
	}
	if messageType != websocket.TextMessage {
		return ErrInvalidWebsocketMessage
	}
	return json.Unmarshal(message, rm)
}

func (c *Client) handleMessage(m rawMessage) (err error) {
	log.Debug(fmt.Sprintf("handling message \"%s\"", m.MessageType))
	switch m.MessageType {
	case "error":
		var d ErrorData
		err = convertToStruct(m.Data, &d)
		if err == nil {
			c.onServerError(d)
		}
	case "new_event":
		var d Event
		err = convertToStruct(m.Data, &d)
		if err == nil {
			c.onNewEvent(d)
		}
	case "introduction":
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
	default:
		return fmt.Errorf("received unknown message: %s", m.MessageType)
	}
	return
}

func (c *Client) onIntroduction(d IntroductionData) {
	if c.state != StatePreparing {
		log.Warn("received \"introduction\" message from the server more than twice, this might indicate a bug on the server side or here, on the client")
		return
	}
	log.Debug("got inroduction from the server", log.Ctx{"app_id": d.AppId, "connection_id": d.ConnectionId, "server_version": d.ServerVersion})
	c.setState(StateReady)
	osInfo, err := getOsInfo()
	if err != nil {
		log.Debug(fmt.Sprintf("failed to get os info: %s", err.Error()))
	}
	message := &HelloMessage{
		Subscriptions:          []string{},
		SupportedFeatures:      []string{"settings:sync", "settings", "purpose:application_host"},
		ClientName:             "Telephonist Supervisor",
		PID:                    os.Getpid(),
		ClientVersion:          "0.1.0dev",
		OS:                     osInfo,
		CompatibilityKey:       c.opts.CompatibilityKey,
		AssumedApplicationType: "host",
		InstanceID:             c.opts.InstanceID,
		MachineID:              c.opts.MachineID,
	}
	log.Debug(fmt.Sprintf("sending \"hello\" message: %#v", message))
	c.sendMessages <- rawMessage{
		MessageType: "hello",
		Data:        message,
	}
}

func (c Client) onBoundToSequences(sequences []string) {
	c.persistentState.BoundSequences = sequences
	log.Debug("bound_to_sequences", log.Ctx{"sequences": sequences})
	if c.opts.OnPersistState != nil {
		c.opts.OnPersistState(c.persistentState)
	}
}

func (c *Client) onNewEvent(d Event) {
	log.Debug(fmt.Sprintf("got event: %#v", d))
	if c.opts.OnNewEvent != nil {
		c.opts.OnNewEvent(d)
	}
}

func (c *Client) onServerError(d ErrorData) {
	log.Error("received an error from the server", log.Ctx{"error_type": d.ErrorType, "exception": d.Exception, "error": d.Error})
}
