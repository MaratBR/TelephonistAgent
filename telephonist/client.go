package telephonist

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"runtime"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/parnurzeal/gorequest"
	log "github.com/sirupsen/logrus"
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
	Logger                 *log.Logger
	ConnectionRetryTimeout time.Duration
	CompatibilityKey       string
}

type Client struct {
	log *log.Logger

	// rest api options
	opts         ClientOptions
	runLoopMutex *sync.Mutex
	conn         *websocket.Conn

	// ws
	isRunning    bool
	closeChan    chan struct{}
	sendMessages chan rawMessage
	state        ClientState
	ready        bool
	userAgent    string
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
	logger := log.StandardLogger()
	if options.Logger != nil {
		logger = options.Logger
	}
	return &Client{
		opts:         options,
		runLoopMutex: &sync.Mutex{},
		log:          logger,
		state:        StateDisconnected,
		userAgent: fmt.Sprintf(
			"application=golang_supervisor, version=%s, golang=%s, arch=%s, goos=%s",
			VERSION, runtime.Version(), runtime.GOARCH, runtime.GOOS),
	}, nil
}

//#region REST API

func (c Client) req() *gorequest.SuperAgent {
	return gorequest.New().
		Timeout(c.opts.Timeout).
		AppendHeader("authorizaton", "Bearer "+c.opts.APIKey)
}

func (c Client) getUrl(url string) string {
	u := c.opts.URL
	u.Path = path.Join(u.Path, url)
	return u.String()
}

func (c Client) Publish(event EventData) []error {
	resp, _, errs := c.req().
		Post(c.getUrl("events/publish")).
		SendStruct(event).
		End()
	if errs != nil {
		return errs
	}
	if resp.StatusCode != 200 {
		return []error{fmt.Errorf("unexpected status code: {0}", resp.Status)}
	}
	log.Debugf("published event %s", event.Name)
	return nil
}

//#endregion

//#region Websocket connection

func (c Client) IsRunning() bool {
	return c.isRunning
}

func (c *Client) Start() error {
	c.runLoopMutex.Lock()
	if c.isRunning {
		c.runLoopMutex.Unlock()
		return ErrClientIsAlreadyRunning
	}
	c.isRunning = true
	c.runLoopMutex.Unlock()
	go c.runWrapped()
	return nil
}

func (c *Client) Stop() {
	if !c.isRunning {
		c.closeChan <- struct{}{}
	}
}

func (c *Client) runWrapped() {
	for c.isRunning {
		err := c.run()
		if err != nil {
			log.Errorln(err)
		}
		log.Debugf("retrying in %s", c.opts.ConnectionRetryTimeout)
		time.Sleep(c.opts.ConnectionRetryTimeout)
	}
}

func (c *Client) requireState(expectedState ClientState) {
	if c.state != expectedState {
		log.Panicf("invalid client state encountered expected: %s, got: %s", c.state, expectedState)
	}
}

func (c *Client) setState(state ClientState) {
	c.state = state
}

func (c *Client) transitionState(expectedState, newState ClientState) {
	c.requireState(expectedState)
	c.setState(newState)
}

func (c *Client) run() error {
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
	u := url.URL{Scheme: schema, Host: c.opts.URL.Host, Path: path.Join(c.opts.URL.Path, "applications/ws")}
	log.Debugf("connecting to %s", u.String())

	c.conn, _, err = websocket.DefaultDialer.Dial(u.String(), http.Header{
		"Authorization": []string{"Bearer " + c.opts.APIKey},
		"User-Agent":    []string{c.userAgent},
	})
	if err != nil {
		return err
	}
	c.transitionState(StateConnecting, StatePreparing)
	go c.readPump()
	c.writePump()
	return nil
}

func (c *Client) writePump() {
	if !c.isRunning {
		log.Panicln("cannot start writePump, client isn't running")
	}
	t := time.NewTicker(pingPeriod)
	log.Debugln("writePump: starting")
	defer log.Debugln("writePump: exiting")
	for {
		select {
		case rm, ok := <-c.sendMessages:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				log.Debugln("sendMessages channel is closed, sending close message to the server")
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
				log.Errorf("failed to write to socket: %s", err.Error())
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
		log.Panicf("invalid state, expected %s or %s, got: %s", StatePreparing, StateReady, c.state)
	}
	if !c.isRunning {
		log.Panicf("cannot start readPump, client isn't running")
	}
	var rm rawMessage
	log.Debugln("readPump: starting")
	defer log.Debugln("readPump: exiting")
	defer close(c.sendMessages)
	for {
		err := c.readMessage(&rm)
		if err != nil {
			if err == ErrInvalidWebsocketMessage {
				continue
			}
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Errorf("unexpectedly closed the connection: %s", err.Error())
			}
			break
		}
		err = c.handleMessage(rm)
		if err != nil {
			log.Errorf("failed to handle the message \"%s\": %s", rm.MessageType, err.Error())
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
	log.Debugf("handling message \"%s\"", m.MessageType)
	switch m.MessageType {
	case "error":
		var d ErrorData
		err = convertToStruct(m.Data, &d)
		if err == nil {
			c.onServerError(d)
		}
	case "new_event":
		var d EventData
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
	log.Debugf("got inroduction from the server server_version=%s connection_id=%s app_id=%s", d.ServerVersion, d.ConnectionId, d.AppId)
	c.setState(StateReady)
	osInfo, err := getOsInfo()
	if err != nil {
		log.Debugf("failed to get os info: %s", err.Error())
	}
	message := &HelloMessage{
		Subscriptions:          []string{},
		SupportedFeatures:      []string{"settings:sync", "settings", "purpose:application_host"},
		Software:               c.userAgent,
		SettingsSchema:         "supervisor-v1",
		PID:                    os.Getpid(),
		SoftwareVersion:        "0.1.0dev",
		OS:                     osInfo,
		CompatibilityKey:       c.opts.CompatibilityKey,
		AssumedApplicationType: "host",
	}
	log.Debugf("sending \"hello\" message: %#v", message)
	c.sendMessages <- rawMessage{
		MessageType: "hello",
		Data:        message,
	}
}

func (c *Client) onNewEvent(d EventData) {
	log.Infof("got event: %d, related_task=%s", d.Name, d.RelatedTask)
}

func (c *Client) onServerError(d ErrorData) {
	log.Errorf("received an error from the server error_type=%s error=%s exception=%s", d.ErrorType, d.Error, d.Exception)
}
