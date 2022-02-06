//go:build debug

package logging

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func init() {
	var err error
	config := zap.NewDevelopmentConfig()
	config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	Root, err = config.Build()
	if err != nil {
		panic(err)
	}
}
