package taskscheduler

import "github.com/MaratBR/TelephonistAgent/telephonist"

type registeredTrigger interface {
	IsEnabled() bool
	Disable() error
	Enable() error
}

type eventTrigger struct {
	eventName string
	client    *telephonist.Client
}
