package main

import (
	"errors"
	"fmt"
	"sync"
	"time"

	log "github.com/inconshreveable/log15"
	"github.com/urfave/cli/v2"
)

var (
	ErrNoTasksToWaitFor = errors.New("no tasks to wait for")
	ErrSomeTasksFailed  = errors.New("some tasks have failed")
)

type BackgroundFunction func(c *BackgroundFunctionContext) error

type BackgroundFunctionContext struct {
	Cli          *cli.Context
	CloseChannel chan struct{}
	Logger       log.Logger
}

type BackgroundFunctionDescriptor struct {
	Function         BackgroundFunction
	Restart          bool
	RestartTimeout   time.Duration
	RestartIfNoError bool
	Name             string
}

type backgroundFunctionInternal struct {
	descriptor   BackgroundFunctionDescriptor
	closeChannel chan struct{}
	lastError    error
	isRunning    bool
	logger       log.Logger
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
	logger := log.New(log.Ctx{"module": fmt.Sprintf("Background/%s", descriptor.Name)})
	h.fns[descriptor.Name] = &backgroundFunctionInternal{
		descriptor:   descriptor,
		closeChannel: make(chan struct{}),
		logger:       logger,
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
		Logger:       fn.logger,
	}
	defer func() {
		ctx.Logger.Debug("exiting...", fn.descriptor.Name)
		fn.isRunning = false
		h.wg.Done()
	}()

	ctx.Logger.Info("starting the task")

	for {
		fn.lastError = fn.descriptor.Function(&ctx)

		if fn.lastError != nil {
			ctx.Logger.Error(fn.lastError.Error())
			if fn.descriptor.Restart {
				ctx.Logger.Info(fmt.Sprintf("restarting due to an error in %s...", fn.descriptor.RestartTimeout.String()))
				time.Sleep(fn.descriptor.RestartTimeout)
				continue
			}
		} else {
			if fn.descriptor.Restart && fn.descriptor.RestartIfNoError {
				ctx.Logger.Info(fmt.Sprintf("RestartIfNoError is set to true, no error occured, restarting in %s...", fn.descriptor.RestartTimeout.String()))
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
			log.Error(fmt.Sprintf("lastError = %s", fn.lastError.Error()))
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
