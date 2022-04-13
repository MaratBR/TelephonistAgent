package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/MaratBR/TelephonistAgent/taskexecutor"
	"github.com/MaratBR/TelephonistAgent/taskscheduler"
	"github.com/MaratBR/TelephonistAgent/telephonist"
	"github.com/MaratBR/TelephonistAgent/utils"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type ApplicationScheduler struct {
	taskScheduler taskscheduler.TaskScheduler
	client        *telephonist.Client
	executor      taskexecutor.Executor
	wsc           *telephonist.WSClient
	config        *ExecutorConfigFile
	configPath    string
	triggerEvents chan taskscheduler.TaskTriggeredEvent
	InstanceID    uuid.UUID
}

type ApplicationSchedulerOptions struct {
	ConfigPath string
	Client     *telephonist.Client
}

func NewApplicationScheduler(options ApplicationSchedulerOptions) (*ApplicationScheduler, error) {
	e := &ApplicationScheduler{
		config:        NewSchedulerConfig(options.ConfigPath),
		configPath:    options.ConfigPath,
		client:        options.Client,
		executor:      taskexecutor.NewShellExecutor(),
		triggerEvents: make(chan taskscheduler.TaskTriggeredEvent),
	}
	e.InstanceID = e.getOrSetInstanceID()
	return e, nil
}

// Start loads executor config, starts underlying task exectur and also starts telephonist client
func (e *ApplicationScheduler) Start() error {
	if e.taskScheduler != nil {
		return nil
	}
	err := e.config.LoadOrWrite()
	if err != nil {
		return utils.ChainError("failed to load executor config", err)
	}
	e.wsc = e.client.WS(telephonist.WSClientOptions{
		OnTask:        e.onTask,
		OnTaskRemoved: e.onTaskRemoved,
		OnTasks:       e.onTasks,
		InstanceID:    e.InstanceID,
		ConnectionID:  uuid.NewSHA1(e.InstanceID, []byte("connection_id")),
		OnConnected:   e.onConnected,
	})
	e.wsc.StartAsync()
	err = e.createExecutor()
	if err != nil {
		return utils.ChainError("failed to create an executor", err)
	}

	err = e.taskScheduler.Start()
	if err != nil {
		return utils.ChainError("failed to start executor", err)
	}
	return nil
}

func (e *ApplicationScheduler) onConnected() {
	//
}

func (e *ApplicationScheduler) getOrSetInstanceID() uuid.UUID {
	var u uuid.UUID

	path := filepath.Join(filepath.Dir(e.configPath), ".telephonist-instance-id")
	if data, err := os.ReadFile(path); err == nil {
		u, err = uuid.Parse(string(data))
		if err != nil {
			u = uuid.New()
			os.WriteFile(path, []byte(u.String()), os.ModePerm)
		}
	} else {
		u = uuid.New()
		if os.IsNotExist(err) {
			os.WriteFile(path, []byte(u.String()), os.ModePerm)
		}
	}

	return u
}

func (e *ApplicationScheduler) createExecutor() error {
	if e.client == nil {
		panic("Telephonist client is not set")
	}
	fsExecutor, err := taskscheduler.NewFSTriggersScheduler()
	if err != nil {
		return err
	}
	triggerExecutor := taskscheduler.NewCompositeTriggersScheduler(
		taskscheduler.NewCronTriggersScheduler(),
		taskscheduler.NewTelephonistScheduler(e.wsc),
		fsExecutor,
	)
	e.taskScheduler = taskscheduler.NewTaskScheduler(taskscheduler.TaskSchedulerOptions{
		TriggersScheduler: triggerExecutor,
	})
	e.taskScheduler.SetCallback(e.onTrigger)
	return nil
}

func (e *ApplicationScheduler) Stop() {
	if e.taskScheduler == nil {
		return
	}
	e.taskScheduler.Stop()
	e.wsc.Stop()
}

// SyncConfig sends a sychronization message to the server and synchronizes tasks list
// with the server, this only needs to be done once on each connection
func (e *ApplicationScheduler) SyncConfig() error {
	return e.wsc.SendTasksSync()
}

func (e *ApplicationScheduler) onTrigger(event taskscheduler.TaskTriggeredEvent) {
	if !e.executor.CanExecute(event.Task) {
		// probably will never happen
		logger.Error("encountered a task, that cannot be executed")
		return
	}
	logger.Info(
		fmt.Sprintf("running task %s", event.Task.Name),
	)
	go e.executeTask(event)
}

func (e *ApplicationScheduler) executeTask(event taskscheduler.TaskTriggeredEvent) error {
	// start new sequence and create params
	var sequenceID string
	time.Sleep(time.Millisecond * 50)

	{
		seq, err := e.client.CreateSequence(telephonist.CreateSequenceRequest{
			TaskID:       event.Task.ID,
			Description:  nil,
			Meta:         map[string]interface{}{},
			ConnectionID: e.wsc.ConnectionID,
		})
		if err != nil {
			return err
		}
		sequenceID = seq.ID
	}

	params := make(map[string]interface{}, len(event.Params)+len(event.TriggerEvent.Params))

	for k, v := range event.TriggerEvent.Params {
		params[k] = v
	}

	for k, v := range event.Params {
		params[k] = v
	}

	params["TELEPHONIST_AGENT"] = "TelephonistAgent " + telephonist.VERSION
	params["SEQUENCE_ID"] = sequenceID

	err := e.executor.Execute(&taskexecutor.TaskExecutionDescriptor{
		Task:   event.Task,
		Params: params,
		FlushCallback: func(lr []telephonist.LogRecord) {
			message := &telephonist.LogMessage{
				SequenceID: sequenceID,
				Logs:       lr,
			}
			e.wsc.SendLogs(message)
		},
	})

	var errString *string
	if err != nil {
		logger.Error("failed to complete task",
			zap.String("taskName", event.Task.Name),
			zap.Stringer("taskID", event.Task.ID),
			zap.Error(err),
		)
		errString = new(string)
		*errString = err.Error()
	} else {
		logger.Debug("task completed",
			zap.String("taskName", event.Task.Name),
			zap.Stringer("taskID", event.Task.ID),
		)
	}
	_, err = e.client.FinishSequence(sequenceID, telephonist.FinishSequenceRequest{
		Error: errString,
	})
	if err != nil {
		return utils.ChainError("failed to finish sequence", err)
	}
	return nil
}

func (e *ApplicationScheduler) onTasks(tasks []*telephonist.DefinedTask) {
	oldTasks := e.config.Value.Tasks
	newMap := getTasksMap(tasks)

	for _, task := range newMap {
		if !e.executor.CanExecute(task) {
			logger.Debug("skipping task that cannot be executed by the executor",
				zap.String("executor", reflect.TypeOf(e.executor).Elem().Name()),
				zap.String("taskType", task.Body.Type),
			)
			continue
		}
		e.onTask(task)
	}

	for id, task := range oldTasks {
		if _, exists := newMap[id]; !exists {
			// this is removed task
			e.onTaskRemoved(task.ID)
		}
	}

	e.config.Value.Tasks = newMap
	e.config.Write()
}

func (e *ApplicationScheduler) onTask(task *telephonist.DefinedTask) {
	e.taskScheduler.Schedule(task)
	e.config.Value.Tasks[task.ID] = task
	e.config.Write()
}

func (e *ApplicationScheduler) onTaskRemoved(id uuid.UUID) {
	delete(e.config.Value.Tasks, id)
	e.config.Write()
	logger.Info(fmt.Sprintf("removed task %s because it was reported as deleted by the backend", id.String()))
}

type ExecutorConfig struct {
	Tasks map[uuid.UUID]*telephonist.DefinedTask
}

func getTasksArray(m map[uuid.UUID]*telephonist.DefinedTask) []*telephonist.DefinedTask {
	tasks := make([]*telephonist.DefinedTask, len(m))
	index := 0
	for _, task := range m {
		tasks[index] = task
		index++
	}
	return tasks
}

func getTasksMap(tasks []*telephonist.DefinedTask) map[uuid.UUID]*telephonist.DefinedTask {
	m := make(map[uuid.UUID]*telephonist.DefinedTask, len(tasks))
	for _, task := range tasks {
		m[task.ID] = task
	}
	return m
}

type ExecutorConfigFile struct {
	file  *ConfigFile
	Value ExecutorConfig
}

func NewSchedulerConfig(path string) *ExecutorConfigFile {
	return &ExecutorConfigFile{
		file: NewConfigFile(path),
		Value: ExecutorConfig{
			Tasks: make(map[uuid.UUID]*telephonist.DefinedTask),
		},
	}
}

func (f *ExecutorConfigFile) Load() error {
	return f.file.Load(&f.Value)
}

func (f *ExecutorConfigFile) LoadOrWrite() error {
	err := f.file.Load(&f.Value)
	if os.IsNotExist(err) {
		err = f.Write()
	}
	return err
}

func (f *ExecutorConfigFile) Write() error {
	return f.file.Write(&f.Value)
}

func (f *ExecutorConfigFile) Validate() error {
	// TODO: complete this method
	return nil
}
