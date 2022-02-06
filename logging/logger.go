package logging

import "go.uber.org/zap"

var (
	loggers       map[string]*zap.Logger
	Root          *zap.Logger
	DoCompression bool = true
	LogFile            = "/var/log/telephonist-agent/client.log"
)

func Name(name string) zap.Field {
	return zap.String("loggerName", name)
}

func ChildLogger(name string) *zap.Logger {
	return Root.With(Name(name))
}
