package telephonist

import "time"

type ErrorData struct {
	ErrorType string `json:"error_type"`
	Error     string `json:"error"`
	Exception string `json:"exception"`
}

type EventData struct {
	Name        string      `json:"name"`
	RelatedTask string      `json:"related_task"`
	Data        interface{} `json:"data"`
}

type rawMessage struct {
	MessageType string      `json:"msg_type"`
	Data        interface{} `json:"data"`
}

type NewEventData struct {
	EventType   string      `json:"event_type"`
	SourceID    string      `json:"source_id"`
	SourceIP    string      `json:"source_ip"`
	RelatedTask string      `json:"related_task"`
	SourceType  string      `json:"source_type"`
	Data        interface{} `json:"data"`
	CreatedAt   time.Time   `json:"created_at"`
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
	Subscriptions          []string    `json:"subscriptions"`
	SettingsSchema         interface{} `json:"settings_schema"`
	SupportedFeatures      []string    `json:"supported_features"`
	Software               string      `json:"software"`
	SoftwareVersion        string      `json:"software_version"`
	OS                     string      `json:"os"`
	PID                    int         `json:"pid"`
	CompatibilityKey       string      `json:"compatibility_key"`
	AssumedApplicationType string      `json:"assumed_application_type"`
}

type IntroductionData struct {
	ServerVersion  string `json:"server_version"`
	Authentication string `json:"authentication"`
	ConnectionId   string `json:"connection_id"`
	AppId          string `json:"app_id"`
}
