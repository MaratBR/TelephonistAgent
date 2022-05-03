package logging

import (
	"github.com/rs/zerolog"
)

var (
	baseLogger zerolog.Logger
	rootLogger zerolog.Logger
)

func ChildLogger(name string) zerolog.Logger {
	return rootLogger.With().Str("logger", name).Logger()
}

func commonInit() {
	zerolog.TimestampFieldName = "t"
	zerolog.LevelFieldName = "l"
}
