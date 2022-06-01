package db

import (
	"github.com/MaratBR/TelephonistAgent/telephonist"
	"github.com/google/uuid"
)

type SequenceDBRecord struct {
	TaskID    uuid.UUID
	TaskName  string
	State     telephonist.SequenceState
	OnlyLocal bool
	BackendID string
}
