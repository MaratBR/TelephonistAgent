package main

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/urfave/cli/v2"
	"go.uber.org/zap"
)

var (
	ErrNoTasksToWaitFor = errors.New("no tasks to wait for")
	ErrSomeTasksFailed  = errors.New("some tasks have failed")
)

type BackgroundFunction func(c *BackgroundFunctionContext) error

type BackgroundFunctionContext struct {
	Cli          *cli.Context
	CloseChannel chan struct{}
}

type RestartInfo struct {
	AttemptInARow int
	LastError     error
	TaskName      string
}

type TimeoutPolicy interface {
	Timeout(RestartInfo) time.Duration
}
type TimeoutPolicyFunction func(RestartInfo) time.Duration

func (fn TimeoutPolicyFunction) Timeout(i RestartInfo) time.Duration {
	return fn(i)
}

type BackgroundFunctionDescriptor struct {
	Function             BackgroundFunction
	Restart              bool
	RestartTimeout       time.Duration
	RestartIfNoError     bool
	Name                 string
	RestartTimeoutPolicy TimeoutPolicy
}

type backgroundFunctionInternal struct {
	descriptor   BackgroundFunctionDescriptor
	closeChannel chan struct{}
	lastError    error
	isRunning    bool
}

type ShutdownMessage struct{}

type BackgroundTasks struct {
	wg  *sync.WaitGroup
	fns map[string]*backgroundFunctionInternal
}

func NewBackgroundTasks() *BackgroundTasks {
	return &BackgroundTasks{
		wg:  &sync.WaitGroup{},
		fns: make(map[string]*backgroundFunctionInternal),
	}
}

func (h BackgroundTasks) AddFunction(descriptor BackgroundFunctionDescriptor) error {
	_, exists := h.fns[descriptor.Name]
	if exists {
		return fmt.Errorf("background task \"%s\" already registered", descriptor.Name)
	}
	h.fns[descriptor.Name] = &backgroundFunctionInternal{
		descriptor:   descriptor,
		closeChannel: make(chan struct{}),
	}
	return nil
}

func (h BackgroundTasks) Run(c *cli.Context) error {
	if h.countRunning() != 0 {
		return errors.New("not all tasks are done running")
	}
	for _, fn := range h.fns {
		h.runInBackground(c, fn)
	}
	return nil
}

func (h BackgroundTasks) runInBackground(c *cli.Context, fn *backgroundFunctionInternal) {
	h.wg.Add(1)
	fn.isRunning = true
	go h.wrappedFunction(c, fn)
}

func (h BackgroundTasks) wrappedFunction(c *cli.Context, fn *backgroundFunctionInternal) {
	ctx := BackgroundFunctionContext{
		Cli:          c,
		CloseChannel: fn.closeChannel,
	}
	defer func() {
		logger.Debug("exiting...", zap.String("taskName", fn.descriptor.Name))
		fn.isRunning = false
		h.wg.Done()
	}()

	logger.Info("starting the task "+fn.descriptor.Name,
		zap.String("taskName", fn.descriptor.Name),
		zap.Duration("restartTimeout", fn.descriptor.RestartTimeout),
	)

	for {
		fn.lastError = fn.descriptor.Function(&ctx)

		if fn.lastError != nil {
			logger.Error(fn.lastError.Error(),
				zap.String("taskName", fn.descriptor.Name),
			)
			if fn.descriptor.Restart {
				logger.Info(fmt.Sprintf("restarting due to an error in %s...", fn.descriptor.RestartTimeout.String()),
					zap.String("taskName", fn.descriptor.Name),
					zap.Duration("restartTimeout", fn.descriptor.RestartTimeout),
				)
				time.Sleep(fn.descriptor.RestartTimeout)
				continue
			}
		} else {
			if fn.descriptor.Restart && fn.descriptor.RestartIfNoError {
				logger.Info(fmt.Sprintf("RestartIfNoError is set to true, no error occured, restarting in %s...", fn.descriptor.RestartTimeout.String()),
					zap.String("taskName", fn.descriptor.Name),
					zap.Duration("restartTimeout", fn.descriptor.RestartTimeout),
				)
				time.Sleep(fn.descriptor.RestartTimeout)
				continue
			}
		}
		break
	}
}

func (h BackgroundTasks) WaitForAll() error {
	if h.countRunning() == 0 {
		return ErrNoTasksToWaitFor
	}
	h.wg.Wait()
	errorsCount := 0
	for _, fn := range h.fns {
		if fn.lastError != nil {
			logger.Error("BackgroundTasks: lastError",
				zap.String("error", fn.lastError.Error()),
			)
			errorsCount += 1
		}
	}
	return ErrSomeTasksFailed
}

func (h BackgroundTasks) countRunning() int {
	count := 0
	for _, fn := range h.fns {
		if fn.isRunning {
			count += 1
		}
	}
	return count
}
