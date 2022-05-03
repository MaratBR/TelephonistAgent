package telephonist

import (
	"encoding/json"
	"errors"
	"hash/fnv"
	"time"

	"github.com/google/uuid"
)

type TicketResponse struct {
	// Expiration time.Time `json:"exp"`  // ignore this, we don't use it anyway
	Ticket string `json:"ticket"`
}

type DetailResponse struct {
	Detail string `json:"detail"`
}

type IDResponse struct {
	ID string `json:"_id"`
}

type ApplicationView struct {
	ID              string   `json:"_id"`
	Tags            []string `json:"tags"`
	Disabled        bool     `json:"disabled"`
	Name            bool     `json:"name"`
	Description     bool     `json:"description"`
	AccessKey       string   `json:"access_key"`
	ApplicationType string   `json:"application_type"`
	// AreSettingsAllowed bool `json:"are_settings_allowed"`
}

type CreateSequenceRequest struct {
	CustomName   *string                `json:"custom_name"`
	Description  *string                `json:"description"`
	Meta         map[string]interface{} `json:"meta"`
	TaskID       uuid.UUID              `json:"task_id"`
	ConnectionID uuid.UUID              `json:"connection_id"`
}

const (
	TRIGGER_CRON     = "cron"
	TRIGGER_EVENT    = "event"
	TRIGGER_FSNOTIFY = "fsnotify"
)

type TaskTrigger struct {
	Name     string          `json:"name" validate:"oneof=cron event fsnotify"`
	Body     json.RawMessage `json:"body"`
	Disabled bool            `json:"disabled,omitempty"`
}

// #region TriggerID

var _fnvHash = fnv.New64a()

// GetID returns hash-based id of given trigger
// NOT thread-safe
func (trigger *TaskTrigger) GetID() uint64 {
	_fnvHash.Write([]byte(trigger.Name))
	_fnvHash.Write([]byte{0})
	_fnvHash.Write(trigger.Body)
	id := _fnvHash.Sum64()
	_fnvHash.Reset()
	return id
}

// #endregion

func (t *TaskTrigger) Unmarshal(ptr interface{}) error {
	return json.Unmarshal(t.Body, ptr)
}

func (t *TaskTrigger) MushUnmarshal(ptr interface{}) {
	err := t.Unmarshal(ptr)
	if err != nil {
		panic(err)
	}
}

func (t *TaskTrigger) MustString() string {
	var s string
	t.MushUnmarshal(&s)
	return s
}

func (t *TaskTrigger) Validate() error {
	var ptr interface{}

	switch t.Name {
	case TRIGGER_CRON:
	case TRIGGER_EVENT:
	case TRIGGER_FSNOTIFY:
		ptr = new(string)
		break
	default:
		return errors.New("invalid trigger type")
	}

	if err := t.Unmarshal(ptr); err != nil {
		return errors.New("invalid trigger body")
	}

	return nil
}

const (
	TASK_TYPE_EXEC      = "exec"
	TASK_TYPE_SCRIPT    = "script"
	TASK_TYPE_ARBITRARY = "arbitrary"
)

type TaskBody struct {
	Value json.RawMessage `json:"value"`
	Type  string          `json:"type" validate:"oneof=exec script arbitrary"`
}

type DefineTaskRequest struct {
	ID          uuid.UUID         `json:"_id"`
	Name        string            `json:"name"`
	Description *string           `json:"description,omitempty"`
	Body        TaskBody          `json:"body"`
	Env         map[string]string `json:"env,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Triggers    []TaskTrigger     `json:"triggers,omitempty" validate:"dive,validate_self"`
}

type DefinedTask struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Body        TaskBody          `json:"body"`
	Env         map[string]string `json:"env"`
	Tags        []string          `json:"tags"`
	Triggers    []*TaskTrigger    `json:"triggers" validate:"dive,required"`
	ID          uuid.UUID         `json:"_id"`
	LastUpdated time.Time         `json:"last_updated"`
	Disabled    bool              `json:"disabled,omitempty"`
}

func (t *DefinedTask) UnmarshalBody(v interface{}) error {
	return json.Unmarshal(t.Body.Value, v)
}

func (t *DefinedTask) MustString() string {
	var s string
	err := t.UnmarshalBody(&s)
	if err != nil {
		panic(t)
	}
	return s
}

type TaskSyncResponse struct {
	Tasks        []*DefinedTask `json:"tasks"`
	RemovedTasks []*DefinedTask `json:"removed_tasks"`
}

type TakenTasks struct {
	Taken        []string `json:"taken"`
	Free         []string `json:"free"`
	BelongToSelf []string `json:"belong_to_self"`
}

type TaskView struct {
	// TODO: just put the same fields here
	DefinedTask
	QualifiedName string `json:"qualified_name"`
}

type FinishSequenceRequest struct {
	Error     *string `json:"error_message"`
	IsSkipped bool    `json:"is_skipped"`
}

type LogSeverity int

const (
	SEVERITY_UNKNOWN LogSeverity = 0
	SEVERITY_DEBUG   LogSeverity = 10
	SEVERITY_INFO    LogSeverity = 20
	SEVERITY_WARNING LogSeverity = 30
	SEVERITY_ERROR   LogSeverity = 40
	SEVERITY_FATAL   LogSeverity = 50
)

type LogEntry struct {
	Body       interface{} `json:"body"`
	SequenceID *string     `json:"sequence_id"`
	Severity   LogSeverity `json:"severity"`
	CreatedAt  time.Time
}

type ErrorData struct {
	ErrorType string `json:"error_type"`
	Error     string `json:"error"`
	Exception string `json:"exception"`
}

type Event struct {
	ID          string      `json:"_id"`
	AppID       string      `json:"app_id"`
	CreatedAt   time.Time   `json:"created_at"`
	EventKey    string      `json:"event_key"`
	EventType   string      `json:"event_type"`
	RelatedTask string      `json:"related_task"`
	Data        interface{} `json:"data"`
	PublisherIP string      `json:"publisher_id"`
	SequenceID  string      `json:"sequence_id"`
}

type EventData struct {
	Name       string      `json:"name"`
	Data       interface{} `json:"data"`
	SequenceID string      `json:"sequence_id,omitempty"`
}

type oRawMessage struct {
	MessageType string      `json:"t"`
	Data        interface{} `json:"d"`
}

type iRawMesage struct {
	MessageType string          `json:"t"`
	Data        json.RawMessage `json:"d"`
}

type HelloMessage struct {
	Subscriptions     []string  `json:"subscriptions"`
	SupportedFeatures []string  `json:"supported_features"`
	ClientName        string    `json:"name"`
	ClientVersion     string    `json:"version"`
	CompatibilityKey  string    `json:"compatibility_key"`
	OS                string    `json:"os_info"`
	ConnectionUUID    uuid.UUID `json:"connection_uuid"`
	PID               int       `json:"pid"`
	InstanceID        uuid.UUID `json:"instance_id"`
	MachineID         string    `json:"machine_id"`
}

type IntroductionData struct {
	ServerVersion  string `json:"server_version"`
	Authentication string `json:"authentication"`
	AppId          string `json:"app_id"`
}

type LogRecord struct {
	Time     int64       `json:"t"`
	Severity LogSeverity `json:"severity"`
	Body     interface{} `json:"body"`
}

type LogMessage struct {
	SequenceID string      `json:"sequence_id"`
	Logs       []LogRecord `json:"logs"`
}

type CreateApplicationRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	DisplayName string   `json:"display_name"`
	Tags        []string `json:"tags"`
}

type CodeRegistrationCompleted struct {
	Key string `json:"key"`
	ID  string `json:"_id"`
}
