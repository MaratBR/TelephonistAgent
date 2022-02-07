package main

import (
	"fmt"
	"os"
	"path/filepath"

	taskexecutor "github.com/MaratBR/TelephonistAgent/task_executor"
	"github.com/MaratBR/TelephonistAgent/telephonist"
	"github.com/MaratBR/TelephonistAgent/utils"
	"github.com/google/uuid"
)

type ApplicationExecutor struct {
	executor   taskexecutor.TaskExecutor
	client     *telephonist.Client
	wsc        *telephonist.WSClient
	config     *ExecutorConfigFile
	configPath string
}

type ApplicationExecutorOptions struct {
	ConfigPath string
	Client     *telephonist.Client
}

func NewApplicationExecutor(options ApplicationExecutorOptions) (*ApplicationExecutor, error) {
	e := &ApplicationExecutor{
		config:     NewExecutorConfig(options.ConfigPath),
		configPath: options.ConfigPath,
		client:     options.Client,
	}
	return e, nil
}

// Start loads executor config, starts underlying task exectur and also starts telephonist client
func (e *ApplicationExecutor) Start() error {
	if e.executor != nil {
		return nil
	}
	err := e.config.LoadOrWrite()
	if err != nil {
		return utils.ChainError("failed to load executor config", err)
	}
	e.wsc = e.client.WS(telephonist.WSClientOptions{
		OnTask:        e.onNewTask,
		OnTaskRemoved: e.onTaskRemoved,
		OnTasks:       e.onTasks,
		InstanceID:    e.getOrSetInstanceID(),
		OnConnected:   e.onConnected,
	})
	e.wsc.StartAsync()
	err = e.createExecutor()
	if err != nil {
		return utils.ChainError("failed to create an executor", err)
	}

	err = e.executor.Start()
	if err != nil {
		return utils.ChainError("failed to start executor", err)
	}
	return nil
}

func (e *ApplicationExecutor) onConnected() {
	e.SyncConfig()
}

func (e *ApplicationExecutor) getOrSetInstanceID() uuid.UUID {
	var u uuid.UUID

	path := filepath.Join(filepath.Base(e.configPath), ".telephonist-instance-id")
	if data, err := os.ReadFile(path); err == nil {
		u, err = uuid.Parse(string(data))
		if err != nil {
			u = uuid.New()
		}
	} else {
		u = uuid.New()
		if os.IsNotExist(err) {
			os.WriteFile(path, []byte(path), os.ModePerm)
		}
	}

	return u
}

func (e *ApplicationExecutor) createExecutor() error {
	if e.client == nil {
		panic("Telephonist client is not set")
	}
	fsExecutor, err := taskexecutor.NewFSTriggersExecutor()
	if err != nil {
		return err
	}
	triggerExecutor := taskexecutor.NewCompositeTriggersExecutor(
		taskexecutor.NewCronTriggersExecutor(),
		taskexecutor.NewTelephonistExecutor(e.wsc),
		fsExecutor,
	)
	e.executor = taskexecutor.NewTaskExecutor(taskexecutor.TaskExecutorOptions{
		TriggersExecutor: triggerExecutor,
	})
	e.executor.SetCallback(e.onTrigger)
	return nil
}

func (e *ApplicationExecutor) Stop() {
	if e.executor == nil {
		return
	}
	e.executor.Stop()
	e.wsc.Stop()
}

// SyncConfig sends a sychronization message to the server and synchronizes tasks list
// with the server, this only needs to be done once on each connection
func (e *ApplicationExecutor) SyncConfig() error {
	return e.wsc.SendTasksSync(getTasksArray(e.config.Value.Tasks))
}

func (e *ApplicationExecutor) onTrigger(event taskexecutor.TaskTriggeredEvent) {
	// TODO: implement this
	logger.Info(
		fmt.Sprintf("running task %s", event.Task.Name),
	)
}

func (e *ApplicationExecutor) onTasks(tasks []*telephonist.DefinedTask) {
	oldTasks := e.config.Value.Tasks
	newMap := getTasksMap(tasks)

	for id, task := range newMap {
		if oldTask, exists := oldTasks[id]; exists {
			// this is updated task
			e.onTaskUpdated(oldTask, task)
		} else {
			// completely new task
			e.onNewTask(task)
		}
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

func (e *ApplicationExecutor) onTask(task *telephonist.DefinedTask) {
	old, exists := e.config.Value.Tasks[task.ID]
	if exists {
		e.onTaskUpdated(old, task)
	} else {
		e.onNewTask(task)
	}
}

func (e *ApplicationExecutor) onTaskRemoved(id uuid.UUID) {

}

func (e *ApplicationExecutor) onNewTask(task *telephonist.DefinedTask) {
	e.executor.Schedule(task)
}

func (e *ApplicationExecutor) onTaskUpdated(old, new *telephonist.DefinedTask) {
	e.executor.Schedule(new)
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

func NewExecutorConfig(path string) *ExecutorConfigFile {
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
