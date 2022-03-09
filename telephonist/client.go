package telephonist

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"reflect"
	"time"

	"github.com/MaratBR/TelephonistAgent/logging"
	"github.com/google/uuid"
	"github.com/parnurzeal/gorequest"
	"go.uber.org/zap"
)

var (
	ErrInvalidWebsocketMessage   = errors.New("invalid websocket message received")
	ErrClientIsAlreadyRunning    = errors.New("client is already running")
	ErrLogWorkerIsAlreadyRunning = errors.New("log worker is already running")
	ErrClientIsNotConnected      = errors.New("client is not connected")
	ErrCloseMessage              = errors.New("CloseMessage received")

	apiLogger = logging.ChildLogger("telephonist-api")
)

const (
	maxMessageSize = 4096
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	VERSION        = "0.1.1"

	MI_TASKS             = "tasks"
	MI_TASK_UPDATED      = "task_updated"
	MI_TASK_REMOVED      = "task_removed"
	MI_INTRODUCTION      = "introduction"
	MI_NEW_EVENT         = "new_event"
	MI_ERROR             = "error"
	MI_LOGS_SENT         = "logs_sent"
	MO_TASK_SYNC         = "synchronize"
	MO_SUBSCRIBE         = "subscribe"
	MO_UNSUBSCRIBE       = "unsubscribe"
	MO_SET_SUBSCRIPTIONS = "set_subscriptions"
	MO_SEND_LOG          = "send_log"
)

const (
	_AUTHORIZATION = "Authorization" // yes i'm one of those people, don't judge
)

type TasksUpdatedCallback func([]*DefinedTask)
type TaskRemovedCallback func(uuid.UUID)
type TaskAddedCallback func(*DefinedTask)

type ClientOptions struct {
	APIKey string   `validate:"required"`
	URL    *url.URL `validate:"required"`
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
type eventsChannel chan Event

type Client struct {
	// rest api options
	opts      ClientOptions
	userAgent string
}

func NewClient(options ClientOptions) (*Client, error) {
	if options.URL == nil {
		return nil, errors.New("URL is required")
	}
	if options.APIKey == "" {
		return nil, errors.New("APIKey is required")
	}
	if options.URL.Scheme != "http" && options.URL.Scheme != "https" {
		return nil, errors.New("schema must be http or https")
	}
	return &Client{
		opts: options,
		userAgent: fmt.Sprintf(
			"Telephonist Agent (Golang) " + VERSION),
	}, nil
}

// #region Initialization and utils

func (c *Client) httpPost(relativeURL string, data interface{}, response interface{}) (gorequest.Response, *CombinedError) {
	if data != nil {
		typ := reflect.TypeOf(data)

		if typ.Kind() == reflect.Struct || (typ.Kind() == reflect.Ptr && typ.Elem().Kind() == reflect.Struct) {
			err := Validator.Struct(data)
			if err != nil {
				return nil, &CombinedError{Errors: []error{err}}
			}
		}
	}
	req := gorequest.New().
		Post(c.getUrl(relativeURL).String()).
		Set(_AUTHORIZATION, "Bearer "+c.opts.APIKey)
	if data != nil {
		req = req.SendStruct(data)
	}
	var (
		errs []error
		resp gorequest.Response
	)
	if response == nil {
		resp, _, errs = req.End()
	} else {
		resp, _, errs = req.EndStruct(response)
	}
	if len(errs) != 0 {
		if resp != nil && resp.StatusCode >= 300 {
			return resp, &CombinedError{Errors: []error{&UnexpectedStatusCode{Status: resp.StatusCode, StatusText: resp.Status}}}
		}
		return resp, &CombinedError{Errors: errs}
	}
	return resp, nil
}

func (c *Client) delete(path string) *CombinedError {
	resp, _, err := gorequest.New().
		Delete(c.getUrl(path).String()).
		Set(_AUTHORIZATION, "Bearer "+c.opts.APIKey).
		End()
	if err != nil {
		return &CombinedError{Errors: err}
	}
	if resp.StatusCode >= 300 {
		return &CombinedError{Errors: []error{&UnexpectedStatusCode{Status: resp.StatusCode, StatusText: resp.Status}}}
	}
	return nil
}

func (c *Client) get(path string, response interface{}) *CombinedError {
	req := gorequest.New().Get(c.getUrl(path).String()).Set(_AUTHORIZATION, "Bearer "+c.opts.APIKey)

	var resp gorequest.Response
	var err []error
	if response == nil {
		resp, _, err = req.End()
	} else {
		resp, _, err = req.EndStruct(response)
	}
	if err != nil {
		return &CombinedError{Errors: err}
	}
	if resp.StatusCode >= 300 {
		return &CombinedError{Errors: []error{&UnexpectedStatusCode{Status: resp.StatusCode, StatusText: resp.Status}}}
	}
	return nil
}

func (c *Client) getUrl(sUrl string) *url.URL {
	u := new(url.URL)
	*u = *c.opts.URL
	u.Path = path.Join(u.Path, "application-api", sUrl)
	return u
}

// #endregion

// #region REST API

func (c *Client) Probe() *CombinedError {
	return c.get("probe", nil)
}

func (c *Client) Publish(event EventData) *CombinedError {
	_, err := c.httpPost("events/publish", event, nil)
	if err != nil {
		return err
	}
	apiLogger.Debug("published event",
		zap.String("event_name", event.Name),
		zap.String("sequence_id", event.SequenceID),
	)
	return nil
}

func (c Client) UpdateSequencMeta(SequenceID string, meta map[string]interface{}) *CombinedError {
	resp, err := c.httpPost("sequences/"+SequenceID+"/meta", meta, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return &CombinedError{Errors: []error{fmt.Errorf("unexpected status code: %s", resp.Status)}}
	}
	return nil
}

func (c *Client) CreateSequence(data CreateSequenceRequest) (*IDResponse, *CombinedError) {
	resp := new(IDResponse)
	_, err := c.httpPost("sequences", data, resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) FinishSequence(SequenceID string, body FinishSequenceRequest) (*DetailResponse, *CombinedError) {
	resp := new(DetailResponse)
	_, _, errs := gorequest.New().
		Post(c.getUrl("sequences/"+SequenceID+"/finish").String()).
		SendStruct(body).
		Set(_AUTHORIZATION, "Bearer "+c.opts.APIKey).
		EndStruct(resp)
	if errs != nil {
		return nil, &CombinedError{Errors: errs}
	}
	return resp, nil
}

func (c Client) DefineTask(r DefineTaskRequest) *CombinedError {
	_, err := c.httpPost("defined-tasks", r, nil)
	return err
}

func (c Client) DropTask(taskID uuid.UUID) *CombinedError {
	return c.delete("defined-tasks/" + taskID.String())
}

func (c Client) GetTasks() ([]TaskView, *CombinedError) {
	tasks := make([]TaskView, 0)
	err := c.get("defined-tasks", tasks)
	if err != nil {
		return nil, err
	}
	return tasks, nil
}

func (c *Client) SyncTasks(tasks []DefinedTask) (*TaskSyncResponse, *CombinedError) {
	resp := new(TaskSyncResponse)
	_, err := c.httpPost("defined-tasks/synchronize", map[string]interface{}{
		"tasks": tasks,
	}, resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) CheckTaskNamesArray(names []string) (*TakenTasks, *CombinedError) {
	resp := new(TakenTasks)
	_, err := c.httpPost("defined-tasks/check", names, resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c Client) LogThroughAPI(entry LogEntry) *CombinedError {
	_, err := c.httpPost("logs/add", entry, nil)
	return err
}

func (c Client) issueWSTicket() (string, []error) {
	req := gorequest.New()
	resp, _, errs := req.
		Post(c.getUrl("ws/issue-ws-ticket").String()).
		Set(_AUTHORIZATION, "Bearer "+c.opts.APIKey).
		End()
	if errs != nil {
		return "", errs
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", []error{&UnexpectedStatusCode{Status: resp.StatusCode, StatusText: resp.Status}}
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

// #region WS

func (c *Client) WS(options WSClientOptions) *WSClient {
	return NewWSClient(c, options)
}

// #endregion
