package logging

import (
	"os"
	"time"

	"github.com/rs/zerolog"
)

func init() {
	commonInit()
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	w := zerolog.ConsoleWriter{Out: os.Stderr}
	w.TimeFormat = time.Stamp
	rootLogger = zerolog.New(w).With().Timestamp().Caller().Logger()
}
