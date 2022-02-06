package taskexecutor

import (
	"fmt"
	"math"
	"path/filepath"
	"syscall"
	"time"

	"github.com/MaratBR/TelephonistAgent/logging"
	"github.com/MaratBR/TelephonistAgent/telephonist"
	"github.com/fsnotify/fsnotify"
	"github.com/go-co-op/gocron"
	"go.uber.org/zap"
)

var (
	logger = logging.ChildLogger("taskexecutor")
)

type TriggeredEvent struct {
	TriggerID uint64
	Trigger   *telephonist.TaskTrigger
	Params    map[string]interface{}
}

type TriggerCallback func(event TriggeredEvent)

type TriggersExecutor interface {
	CanSchedule(triggerType string) bool
	Schedule(trigger *telephonist.TaskTrigger) (uint64, error)
	ScheduleByID(id uint64, trigger *telephonist.TaskTrigger) (uint64, error)
	Unschedule(id uint64) (error, bool)
	Start() error
	Stop()
	IsRunning() bool
	SetCallback(callback TriggerCallback)
}

// #region CRON

type CronTriggersExecutor struct {
	seq           uint64
	cronScheduler *gocron.Scheduler
	callback      TriggerCallback
	jobs          map[uint64]*gocron.Job
	logger        *zap.Logger
}

func NewCronTriggersExecutor() *CronTriggersExecutor {
	return &CronTriggersExecutor{
		cronScheduler: gocron.NewScheduler(time.UTC),
		jobs:          make(map[uint64]*gocron.Job),
		logger:        logger.With(zap.String("struct", "CronTriggersExecutor")),
	}
}

func (e *CronTriggersExecutor) CanSchedule(triggerType string) bool {
	return triggerType == telephonist.TRIGGER_CRON
}

func (e *CronTriggersExecutor) Schedule(trigger *telephonist.TaskTrigger) (uint64, error) {
	return e.ScheduleByID(trigger.GetID(), trigger)
}

func (e *CronTriggersExecutor) ScheduleByID(id uint64, trigger *telephonist.TaskTrigger) (uint64, error) {
	if trigger.Name != telephonist.TRIGGER_CRON {
		panic("invalid trigger type for a CronTriggersExecutor")
	}
	job, err := e.cronScheduler.Cron(trigger.MustString()).Do(e.trigger, id, trigger)
	if err != nil {
		return 0, err
	}
	e.jobs[id] = job
	return id, nil
}

func (e *CronTriggersExecutor) trigger(id uint64, t *telephonist.TaskTrigger) {
	if e.callback != nil {
		e.callback(TriggeredEvent{TriggerID: id, Trigger: t})
	}
}

func (e *CronTriggersExecutor) Unschedule(id uint64) (error, bool) {
	job, ok := e.jobs[id]
	if ok {
		e.cronScheduler.RemoveByReference(job)
	}
	return nil, ok
}

func (e *CronTriggersExecutor) Start() error {
	e.cronScheduler.StartAsync()
	return nil
}

func (e *CronTriggersExecutor) Stop() {
	e.cronScheduler.Stop()
}

func (e *CronTriggersExecutor) IsRunning() bool {
	return e.cronScheduler.IsRunning()
}

func (e *CronTriggersExecutor) SetCallback(callback TriggerCallback) {
	e.callback = callback
}

// #endregion

// #region fnotify

type FSTriggersExecutor struct {
	watcher      *fsnotify.Watcher
	pathTriggers map[string]map[uint64]*telephonist.TaskTrigger
	triggers     map[uint64]*telephonist.TaskTrigger
	logger       *zap.Logger

	callback  TriggerCallback
	isRunning bool
	closeChan chan struct{}
}

func NewFSTriggersExecutor() (*FSTriggersExecutor, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &FSTriggersExecutor{
		watcher:      watcher,
		pathTriggers: make(map[string]map[uint64]*telephonist.TaskTrigger),
		triggers:     make(map[uint64]*telephonist.TaskTrigger),
		logger:       logger.With(zap.String("struct", "FSTriggersExecutor")),
	}, nil
}

func (e *FSTriggersExecutor) CanSchedule(triggerType string) bool {
	return triggerType == telephonist.TRIGGER_FSNOTIFY
}

func (e *FSTriggersExecutor) Schedule(trigger *telephonist.TaskTrigger) (uint64, error) {
	return e.ScheduleByID(trigger.GetID(), trigger)
}

func (e *FSTriggersExecutor) ScheduleByID(id uint64, trigger *telephonist.TaskTrigger) (uint64, error) {
	if trigger.Name != telephonist.TRIGGER_FSNOTIFY {
		panic("invalid trigger type for a FSTriggersExecutor")
	}
	path := trigger.MustString()
	if pt, ok := e.pathTriggers[path]; ok {
		pt[id] = trigger
	} else {
		err := e.watcher.Add(path)
		if err != nil {
			return 0, err
		}
		e.pathTriggers[path] = map[uint64]*telephonist.TaskTrigger{id: trigger}
	}
	e.triggers[id] = trigger
	return id, nil
}

func (e *FSTriggersExecutor) trigger(id uint64, t *telephonist.TaskTrigger) {
	if e.callback != nil {
		e.callback(TriggeredEvent{TriggerID: id, Trigger: t})
	}
}

func (e *FSTriggersExecutor) Unschedule(id uint64) (error, bool) {
	trigger, exists := e.triggers[id]
	if exists {
		path := trigger.MustString()
		var pt map[uint64]*telephonist.TaskTrigger
		if pt, exists = e.pathTriggers[path]; exists {
			if len(pt) == 1 {
				err := e.watcher.Remove(path)
				delete(e.pathTriggers, path)
				if err != nil {
					if err, isErrno := err.(syscall.Errno); isErrno && err == syscall.EINVAL {
						// the file descriptor of this path is no longer a valid watch descriptor
						// refer to inotify.go:158 for more info
						// we assume its not longer being watched here
						return nil, true
					}
					return err, true
				}
			} else {
				delete(pt, id)
			}
		}
	}
	return nil, exists
}

func (e *FSTriggersExecutor) Start() error {
	if e.isRunning {
		return nil
	}
	e.closeChan = make(chan struct{})
	e.isRunning = true
	go e.start()
	return nil
}

func (e *FSTriggersExecutor) start() {
	for {
		select {
		case event := <-e.watcher.Events:
			e.dispatchEvent(event)
			break
		case _ = <-e.watcher.Errors:
			// do nothing for now
			// TODO: do something about the error
			break
		case <-e.closeChan:
			return
		}
	}
}

func (e *FSTriggersExecutor) dispatchEvent(event fsnotify.Event) {
	if e.callback == nil {
		return
	}
	path, err := filepath.Abs(event.Name)
	if err != nil {
		return
	}

	if triggers, exists := e.pathTriggers[path]; exists {
		for id, trigger := range triggers {
			e.callback(TriggeredEvent{TriggerID: id, Trigger: trigger})
		}
	}
}

func (e *FSTriggersExecutor) Stop() {
	e.isRunning = false
	e.watcher.Close()
}

func (e *FSTriggersExecutor) IsRunning() bool {
	return e.isRunning
}

func (e *FSTriggersExecutor) SetCallback(callback TriggerCallback) {
	e.callback = callback
}

// #endregion

// #region Telephonist API

type TelephonistExecutor struct {
	client                *telephonist.WSClient
	seq                   uint64
	queue                 *telephonist.EventQueue
	subscriptionsCounters map[string]int
	triggers              map[uint64]*telephonist.TaskTrigger
	eventTriggers         map[string]map[uint64]*telephonist.TaskTrigger
	callback              TriggerCallback
	logger                *zap.Logger
}

func NewTelephonistExecutor(client *telephonist.WSClient) *TelephonistExecutor {
	if client == nil {
		panic("client is nil")
	}
	return &TelephonistExecutor{
		client:                client,
		subscriptionsCounters: make(map[string]int),
		eventTriggers:         make(map[string]map[uint64]*telephonist.TaskTrigger),
		triggers:              make(map[uint64]*telephonist.TaskTrigger),
		logger:                logger.With(zap.String("struct", "TelephonistExecutor")),
	}
}

func (e *TelephonistExecutor) CanSchedule(triggerType string) bool {
	return triggerType == telephonist.TRIGGER_EVENT
}

func (e *TelephonistExecutor) Schedule(trigger *telephonist.TaskTrigger) (uint64, error) {
	return e.ScheduleByID(trigger.GetID(), trigger)
}

func (e *TelephonistExecutor) ScheduleByID(id uint64, trigger *telephonist.TaskTrigger) (uint64, error) {
	if trigger.Name != telephonist.TRIGGER_EVENT {
		panic("invalid trigger type for a TelephonistExecutor")
	}
	event := trigger.MustString()
	e.triggers[id] = trigger
	if count, exists := e.subscriptionsCounters[event]; exists {
		e.subscriptionsCounters[event] = count + 1
	} else {
		e.subscriptionsCounters[event] = 1
		e.queue.Subcribe(event)
	}
	return id, nil
}

func (e *TelephonistExecutor) Unschedule(id uint64) (error, bool) {
	trigger, exists := e.triggers[id]
	if exists {
		delete(e.triggers, id)
		event := trigger.MustString()
		count := e.subscriptionsCounters[event]
		if count == 1 {
			delete(e.subscriptionsCounters, event)
			e.queue.Unsubscribe(event)
		} else {
			e.subscriptionsCounters[event] = count - 1
		}
	}
	return nil, exists
}

func (e *TelephonistExecutor) Start() error {
	if e.queue == nil {
		e.queue = e.client.NewQueue()
	}
	return nil
}

func (e *TelephonistExecutor) start() {
	for {
		event, ok := <-e.queue.Channel
		if !ok {
			return
		}
		if e.callback != nil {
			triggers, ok := e.eventTriggers[event.EventKey]
			if !ok {
				continue
			}
			for id, trigger := range triggers {
				e.callback(TriggeredEvent{TriggerID: id, Trigger: trigger})
			}
		}
	}
}

func (e *TelephonistExecutor) Stop() {
	if e.queue != nil {
		e.queue.UnsubscribeAll()
		e.queue = nil
	}
}

func (e *TelephonistExecutor) IsRunning() bool {
	return e.queue != nil
}

func (e *TelephonistExecutor) SetCallback(callback TriggerCallback) {
	e.callback = callback
}

// #endregion

type CompositeExecutorError struct {
	MainError      error
	RollbackErrors []error
}

func (err *CompositeExecutorError) Error() string {
	return err.MainError.Error()
}

type CompositeTriggersExecutor struct {
	executors         []TriggersExecutor
	executorIndexBits uint8
	isRunning         bool
	logger            *zap.Logger
}

func NewCompositeTriggersExecutor(executors ...TriggersExecutor) *CompositeTriggersExecutor {
	return &CompositeTriggersExecutor{
		executors:         executors,
		executorIndexBits: uint8(math.Ceil(math.Log2(float64(len(executors))))),
		logger:            logger.With(zap.String("struct", "CompositeTriggersExecutor")),
	}
}

func (e *CompositeTriggersExecutor) CanSchedule(triggerType string) bool {
	for _, executor := range e.executors {
		if executor.CanSchedule(triggerType) {
			return true
		}
	}
	return false
}

func (e *CompositeTriggersExecutor) ScheduleByID(id uint64, trigger *telephonist.TaskTrigger) (uint64, error) {
	for _, executor := range e.executors {
		if executor.CanSchedule(trigger.Name) {
			id, err := executor.ScheduleByID(id, trigger)
			if err != nil {
				return 0, err
			}
			return id, err
		}
	}
	e.logger.Debug("invalid trigger type", zap.String("triggerType", trigger.Name))
	return 0, fmt.Errorf("there's no executors that match given trigger type %s", trigger.Name)
}

func (e *CompositeTriggersExecutor) Schedule(trigger *telephonist.TaskTrigger) (uint64, error) {
	for index, executor := range e.executors {
		if executor.CanSchedule(trigger.Name) {
			id, err := executor.Schedule(trigger)
			if err != nil {
				return 0, err
			}
			id = uint64(index)<<(64-e.executorIndexBits) | id
			return id, err
		}
	}
	e.logger.Debug("invalid trigger type", zap.String("triggerType", trigger.Name))
	return 0, fmt.Errorf("there's no executors that match given trigger type %s", trigger.Name)
}

func (e *CompositeTriggersExecutor) Unschedule(id uint64) (error, bool) {
	// zeroes first N bits
	originalID := id & ^(((1 << e.executorIndexBits) - 1) << (64 - e.executorIndexBits))
	// gets first N bits as number
	index := id >> (64 - e.executorIndexBits)
	return e.executors[index].Unschedule(originalID)
}

func (e *CompositeTriggersExecutor) Start() error {
	if e.isRunning {
		return nil
	}
	var err error
	var i int
	for i = 0; i < len(e.executors) && err != nil; i++ {
		err = e.executors[i].Start()
	}
	if err != nil {
		e.logger.Error("Failed to start executore because one of the child executors failed to start")
		// stop already running executors
		for j := 0; j <= i; j++ {
			e.executors[i].Stop()
		}
		return err
	}
	e.isRunning = true
	return nil
}

func (e *CompositeTriggersExecutor) Stop() {
	if !e.isRunning {
		return
	}
	for _, executor := range e.executors {

		executor.Stop()
	}
	e.isRunning = false
}

func (e *CompositeTriggersExecutor) IsRunning() bool {
	return e.isRunning
}

func (e *CompositeTriggersExecutor) SetCallback(callback TriggerCallback) {
	for _, executor := range e.executors {
		executor.SetCallback(callback)
	}
}
