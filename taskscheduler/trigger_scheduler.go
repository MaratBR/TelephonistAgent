package taskscheduler

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
	"github.com/rs/zerolog"
)

var (
	logger = logging.ChildLogger("taskexecutor")
)

type TriggeredEvent struct {
	TriggerID uint64
	Trigger   *telephonist.TaskTrigger
	Params    map[string]interface{}
}

type TriggerCallback func(event *TriggeredEvent)

type TriggersSchedulerInfo struct {
	Name                string
	AllowedTriggerTypes []string
	Details             interface{} `json:",omitempty"`
}

type TriggersScheduler interface {
	CanSchedule(triggerType string) bool
	Schedule(trigger *telephonist.TaskTrigger) (uint64, error)
	ScheduleByID(id uint64, trigger *telephonist.TaskTrigger) (uint64, error)
	Unschedule(id uint64) (error, bool)
	Start() error
	Stop()
	IsRunning() bool
	SetCallback(callback TriggerCallback)
	Explain() TriggersSchedulerInfo
}

// #region CRON

type CronTriggersScheduler struct {
	seq           uint64
	cronScheduler *gocron.Scheduler
	callback      TriggerCallback
	jobs          map[uint64]*gocron.Job
	logger        zerolog.Logger
}

func NewCronTriggersScheduler() *CronTriggersScheduler {
	return &CronTriggersScheduler{
		cronScheduler: gocron.NewScheduler(time.UTC),
		jobs:          make(map[uint64]*gocron.Job),
		logger:        logger.With().Str("struct", "CronTriggersScheduler").Logger(),
	}
}

func (e *CronTriggersScheduler) Explain() TriggersSchedulerInfo {
	return TriggersSchedulerInfo{
		Name:                "CronTriggersScheduler",
		AllowedTriggerTypes: []string{telephonist.TRIGGER_CRON},
		Details: map[string]string{
			"Uses": "github.com/go-co-op/gocron",
		},
	}
}

func (e *CronTriggersScheduler) CanSchedule(triggerType string) bool {
	return triggerType == telephonist.TRIGGER_CRON
}

func (e *CronTriggersScheduler) Schedule(trigger *telephonist.TaskTrigger) (uint64, error) {
	return e.ScheduleByID(trigger.GetID(), trigger)
}

func (e *CronTriggersScheduler) ScheduleByID(id uint64, trigger *telephonist.TaskTrigger) (uint64, error) {
	if trigger.Name != telephonist.TRIGGER_CRON {
		panic("invalid trigger type for a CronTriggersScheduler")
	}
	job, err := e.cronScheduler.Cron(trigger.MustString()).Do(e.trigger, id, trigger)
	if err != nil {
		return 0, err
	}
	e.jobs[id] = job
	return id, nil
}

func (e *CronTriggersScheduler) trigger(id uint64, t *telephonist.TaskTrigger) {
	if e.callback != nil {
		e.callback(&TriggeredEvent{TriggerID: id, Trigger: t})
	}
}

func (e *CronTriggersScheduler) Unschedule(id uint64) (error, bool) {
	job, ok := e.jobs[id]
	if ok {
		e.cronScheduler.RemoveByReference(job)
	}
	return nil, ok
}

func (e *CronTriggersScheduler) Start() error {
	e.cronScheduler.StartAsync()
	return nil
}

func (e *CronTriggersScheduler) Stop() {
	e.cronScheduler.Stop()
}

func (e *CronTriggersScheduler) IsRunning() bool {
	return e.cronScheduler.IsRunning()
}

func (e *CronTriggersScheduler) SetCallback(callback TriggerCallback) {
	e.callback = callback
}

// #endregion

// #region fnotify

type FSTriggersScheduler struct {
	watcher      *fsnotify.Watcher
	pathTriggers map[string]map[uint64]*telephonist.TaskTrigger
	triggers     map[uint64]*telephonist.TaskTrigger
	logger       zerolog.Logger

	callback  TriggerCallback
	isRunning bool
	closeChan chan struct{}
}

func NewFSTriggersScheduler() (*FSTriggersScheduler, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &FSTriggersScheduler{
		watcher:      watcher,
		pathTriggers: make(map[string]map[uint64]*telephonist.TaskTrigger),
		triggers:     make(map[uint64]*telephonist.TaskTrigger),
		logger:       logger.With().Str("struct", "FSTriggersScheduler").Logger(),
	}, nil
}

func (e *FSTriggersScheduler) Explain() TriggersSchedulerInfo {
	return TriggersSchedulerInfo{
		Name:                "FSTriggersScheduler",
		AllowedTriggerTypes: []string{telephonist.TRIGGER_FSNOTIFY},
		Details: map[string]string{
			"Uses": "github.com/fsnotify/fsnotify",
		},
	}
}

func (e *FSTriggersScheduler) CanSchedule(triggerType string) bool {
	return triggerType == telephonist.TRIGGER_FSNOTIFY
}

func (e *FSTriggersScheduler) Schedule(trigger *telephonist.TaskTrigger) (uint64, error) {
	return e.ScheduleByID(trigger.GetID(), trigger)
}

func (e *FSTriggersScheduler) ScheduleByID(id uint64, trigger *telephonist.TaskTrigger) (uint64, error) {
	if trigger.Name != telephonist.TRIGGER_FSNOTIFY {
		panic("invalid trigger type for a FSTriggersScheduler")
	}
	path := normalizePath(trigger.MustString())
	if pt, ok := e.pathTriggers[path]; ok {
		pt[id] = trigger
	} else {
		err := e.watcher.Add(path)
		if err != nil {
			return 0, err
		}
		logger.Debug().Msgf("watching: %s", path)
		e.pathTriggers[path] = map[uint64]*telephonist.TaskTrigger{id: trigger}
	}
	e.triggers[id] = trigger
	return id, nil
}

func (e *FSTriggersScheduler) Unschedule(id uint64) (error, bool) {
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

func (e *FSTriggersScheduler) Start() error {
	if e.isRunning {
		return nil
	}
	e.closeChan = make(chan struct{})
	e.isRunning = true
	go e.start()
	return nil
}

func (e *FSTriggersScheduler) start() {
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

func (e *FSTriggersScheduler) dispatchEvent(event fsnotify.Event) {
	if e.callback == nil {
		return
	}
	path, err := filepath.Abs(event.Name)
	if err != nil {
		return
	}
	logger.Debug().
		Str("path", path).
		Str("op", event.Op.String()).
		Msg("handling fs event")

	metaData := map[string]interface{}{
		"FILEPATH":    path,
		"FSNOTIFY_OP": event.Op.String(),
	}

	for rootPath, triggers := range e.pathTriggers {
		if matchesPath(rootPath, path) {
			for id, trigger := range triggers {
				e.callback(&TriggeredEvent{TriggerID: id, Trigger: trigger, Params: metaData})
			}
		}
	}
}

func (e *FSTriggersScheduler) Stop() {
	e.isRunning = false
	e.watcher.Close()
}

func (e *FSTriggersScheduler) IsRunning() bool {
	return e.isRunning
}

func (e *FSTriggersScheduler) SetCallback(callback TriggerCallback) {
	e.callback = callback
}

// #endregion

// #region Telephonist API

type TelephonistScheduler struct {
	client                *telephonist.WSClient
	seq                   uint64
	queue                 *telephonist.EventQueue
	subscriptionsCounters map[string]int
	triggers              map[uint64]*telephonist.TaskTrigger
	eventTriggers         map[string]map[uint64]*telephonist.TaskTrigger
	callback              TriggerCallback
	logger                zerolog.Logger
}

func NewTelephonistScheduler(client *telephonist.WSClient) *TelephonistScheduler {
	if client == nil {
		panic("client is nil")
	}
	return &TelephonistScheduler{
		client:                client,
		subscriptionsCounters: make(map[string]int),
		eventTriggers:         make(map[string]map[uint64]*telephonist.TaskTrigger),
		triggers:              make(map[uint64]*telephonist.TaskTrigger),
		logger:                logger.With().Str("struct", "TelephonistScheduler").Logger(),
	}
}

func (e *TelephonistScheduler) Explain() TriggersSchedulerInfo {
	return TriggersSchedulerInfo{
		Name:                "TelephonistScheduler",
		AllowedTriggerTypes: []string{telephonist.TRIGGER_EVENT},
	}
}

func (e *TelephonistScheduler) CanSchedule(triggerType string) bool {
	return triggerType == telephonist.TRIGGER_EVENT
}

func (e *TelephonistScheduler) Schedule(trigger *telephonist.TaskTrigger) (uint64, error) {
	return e.ScheduleByID(trigger.GetID(), trigger)
}

func (e *TelephonistScheduler) ScheduleByID(id uint64, trigger *telephonist.TaskTrigger) (uint64, error) {
	if trigger.Name != telephonist.TRIGGER_EVENT {
		panic("invalid trigger type for a TelephonistScheduler")
	}
	event := trigger.MustString()
	e.triggers[id] = trigger
	if triggers, ok := e.eventTriggers[event]; ok {
		triggers[id] = trigger
	} else {
		e.eventTriggers[event] = map[uint64]*telephonist.TaskTrigger{
			id: trigger,
		}
	}
	if count, exists := e.subscriptionsCounters[event]; exists {
		e.subscriptionsCounters[event] = count + 1
	} else {
		e.subscriptionsCounters[event] = 1
		e.queue.Subcribe(event)
	}
	return id, nil
}

func (e *TelephonistScheduler) Unschedule(id uint64) (error, bool) {
	trigger, exists := e.triggers[id]
	if exists {
		delete(e.triggers, id)
		event := trigger.MustString()
		if triggers, ok := e.eventTriggers[event]; ok {
			delete(triggers, id)
			if len(triggers) == 0 {
				delete(e.eventTriggers, event)
			}
		}
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

func (e *TelephonistScheduler) Start() error {
	if e.queue == nil {
		e.queue = e.client.NewQueue()
		go e.drain()
	}
	return nil
}

func (e *TelephonistScheduler) drain() {
	for e.queue != nil {
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
				e.callback(&TriggeredEvent{TriggerID: id, Trigger: trigger})
			}
		}
	}
}

func (e *TelephonistScheduler) Stop() {
	if e.queue != nil {
		e.queue.UnsubscribeAll()
		e.queue = nil
	}
}

func (e *TelephonistScheduler) IsRunning() bool {
	return e.queue != nil
}

func (e *TelephonistScheduler) SetCallback(callback TriggerCallback) {
	e.callback = callback
}

// #endregion

type CompositeSchedulerError struct {
	MainError      error
	RollbackErrors []error
}

func (err *CompositeSchedulerError) Error() string {
	return err.MainError.Error()
}

type CompositeTriggersScheduler struct {
	childSchedulers   []TriggersScheduler
	executorIndexBits uint8
	isRunning         bool
	logger            zerolog.Logger
	ids               map[uint64]int
}

func NewCompositeTriggersScheduler(executors ...TriggersScheduler) *CompositeTriggersScheduler {
	return &CompositeTriggersScheduler{
		childSchedulers:   executors,
		executorIndexBits: uint8(math.Ceil(math.Log2(float64(len(executors))))),
		logger:            logger.With().Str("struct", "CompositeTriggersScheduler").Logger(),
		ids:               make(map[uint64]int),
	}
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

func (e *CompositeTriggersScheduler) Explain() TriggersSchedulerInfo {
	underlyingSchedulers := []TriggersSchedulerInfo{}
	allowedTriggerTypes := []string{}

	for _, scheduler := range e.childSchedulers {
		info := scheduler.Explain()
		underlyingSchedulers = append(underlyingSchedulers, info)

		for _, triggerType := range info.AllowedTriggerTypes {
			if !contains(allowedTriggerTypes, triggerType) {
				allowedTriggerTypes = append(allowedTriggerTypes, triggerType)
			}
		}
	}

	return TriggersSchedulerInfo{
		Name:                "CompositeTriggersScheduler",
		AllowedTriggerTypes: allowedTriggerTypes,
		Details: map[string]interface{}{
			"Children": underlyingSchedulers,
		},
	}
}

func (e *CompositeTriggersScheduler) CanSchedule(triggerType string) bool {
	for _, executor := range e.childSchedulers {
		if executor.CanSchedule(triggerType) {
			return true
		}
	}
	return false
}

func (e *CompositeTriggersScheduler) ScheduleByID(id uint64, trigger *telephonist.TaskTrigger) (uint64, error) {
	for index, executor := range e.childSchedulers {
		if executor.CanSchedule(trigger.Name) {
			id, err := executor.ScheduleByID(id, trigger)
			if err != nil {
				return 0, err
			}
			e.ids[id] = index
			return id, err
		}
	}
	e.logger.Debug().Str("trigger type", trigger.Name).Msg("invalid trigger type")
	return 0, fmt.Errorf("there's no executors that match given trigger type %s", trigger.Name)
}

func (e *CompositeTriggersScheduler) Schedule(trigger *telephonist.TaskTrigger) (uint64, error) {
	for index, executor := range e.childSchedulers {
		if executor.CanSchedule(trigger.Name) {
			id, err := executor.Schedule(trigger)
			if err != nil {
				return 0, err
			}
			id = uint64(index)<<(64-e.executorIndexBits) | id
			return id, err
		}
	}
	e.logger.Debug().Str("trigger type", trigger.Name).Msg("invalid trigger type")
	return 0, fmt.Errorf("there's no executors that match given trigger type %s", trigger.Name)
}

func (e *CompositeTriggersScheduler) Unschedule(id uint64) (error, bool) {
	if execIndex, ok := e.ids[id]; ok {
		return e.childSchedulers[execIndex].Unschedule(id)
	}
	return fmt.Errorf("trigger with id %d is not registered in any of the underlying schedulers", id), false
}

func (e *CompositeTriggersScheduler) Start() error {
	if e.isRunning {
		return nil
	}
	var err error
	var i int
	for i = 0; i < len(e.childSchedulers) && err == nil; i++ {
		err = e.childSchedulers[i].Start()
	}
	if err != nil {
		e.logger.Error().
			Err(err).
			Msg("Failed to start executore because one of the child executors failed to start")
		// stop already running executors
		for j := 0; j <= i; j++ {
			e.childSchedulers[i].Stop()
		}
		return err
	}
	e.isRunning = true
	return nil
}

func (e *CompositeTriggersScheduler) Stop() {
	if !e.isRunning {
		return
	}
	for _, executor := range e.childSchedulers {

		executor.Stop()
	}
	e.isRunning = false
}

func (e *CompositeTriggersScheduler) IsRunning() bool {
	return e.isRunning
}

func (e *CompositeTriggersScheduler) SetCallback(callback TriggerCallback) {
	for _, executor := range e.childSchedulers {
		executor.SetCallback(callback)
	}
}
