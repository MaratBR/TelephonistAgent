package db

import "github.com/google/uuid"

type SequenceState struct {
	TaskID       uuid.UUID
	IsInProgress bool
}
