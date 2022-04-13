//go:build !debug

package logging

import (
	"fmt"

	"go.uber.org/zap"
)

func init() {
	var err error
	cfg := zap.NewProductionConfig()
	cfg.Level.SetLevel(zap.DebugLevel)
	Root, err = cfg.Build()
	if err != nil {
		panic(fmt.Sprintf("failed to init logger: %s", err.Error()))
	}
}
