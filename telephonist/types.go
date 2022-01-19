package telephonist

import "time"

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

type CreateSequenceRequest struct {
	RelatedTask string                 `json:"related_task"`
	CustomName  *string                `json:"custom_name"`
	Description *string                `json:"description"`
	Meta        map[string]interface{} `json:"meta"`
}

type FinishSequenceRequest struct {
	Error     string `json:"error_message"`
	IsSkipped bool   `json:"is_skipped"`
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
	SequenceId *string     `json:"sequence_id"`
	Data       interface{} `json:"data"`
}

type rawMessage struct {
	MessageType string      `json:"msg_type"`
	Data        interface{} `json:"data"`
}

type SendIf string

const (
	SEND_ALWAYS             SendIf = "always"
	SEND_IF_EXIT_CODE_NOT_0 SendIf = "if_non_0_exit"
	SEND_NEVER              SendIf = "never"
)

type TaskDescriptor struct {
	CronString      string   `json:"cron" yaml:"cron"`
	Command         string   `json:"command" yaml:"command"`
	SendStderr      SendIf   `json:"send_stderr" yaml:"send_stderr"`
	SendStdout      SendIf   `json:"send_stdout" yaml:"send_stdout"`
	OnSuccessEvent  string   `json:"on_success_event" yaml:"on_success_event"`
	OnFailureEvent  string   `json:"on_failure_event" yaml:"on_failure_event"`
	OnCompleteEvent string   `json:"on_complete_event" yaml:"on_complete_event"`
	OnEvents        []string `json:"on_events" yaml:"on_complete_event"`
}

type SupervisorSettingsV1 struct {
	Tasks []TaskDescriptor `json:"tasks" yaml:"tasks"`
}

func DefaultSettings() SupervisorSettingsV1 {
	return SupervisorSettingsV1{Tasks: []TaskDescriptor{}}
}

type HelloMessage struct {
	Subscriptions          []string `json:"subscriptions"`
	SupportedFeatures      []string `json:"supported_features"`
	ClientName             string   `json:"client_name"`
	ClientVersion          string   `json:"client_version"`
	OS                     string   `json:"os"`
	PID                    int      `json:"pid"`
	CompatibilityKey       string   `json:"compatibility_key"`
	AssumedApplicationType string   `json:"assumed_application_type"`
	InstanceID             string   `json:"instance_id"`
	MachineID              string   `json:"machine_id"`
}

type IntroductionData struct {
	ServerVersion  string `json:"server_version"`
	Authentication string `json:"authentication"`
	ConnectionId   string `json:"connection_id"`
	AppId          string `json:"app_id"`
}
