package taskexecutor

import (
	"github.com/MaratBR/TelephonistAgent/telephonist"
	"github.com/google/uuid"
)

type TaskSchedulingError struct {
	Triggers []error
}

func (ts *TaskSchedulingError) Error() string {
	// TODO: implement this
	return "TODO"
}

type TaskTriggeredEvent struct {
	TriggerEvent TriggeredEvent
	Task         *telephonist.DefinedTask
}

type TriggeredTaskCallback func(TaskTriggeredEvent)

type TaskExecutor interface {
	Schedule(task *telephonist.DefinedTask) error
	Unschedule(taskID uuid.UUID) (bool, error)
	Start() error
	Stop()
	SetCallback(TriggeredTaskCallback)
}

type TaskExecutorOptions struct {
	TriggersExecutor TriggersExecutor
}

func NewTaskExecutor(options TaskExecutorOptions) TaskExecutor {
	if options.TriggersExecutor == nil {
		panic("TriggersExecutor is not set in the options")
	}
	exec := &taskExecutor{
		triggersExecutors: options.TriggersExecutor,
		tasks:             map[uuid.UUID]ScheduledTask{},
	}
	exec.triggersExecutors.SetCallback(exec.onTrigger)
	return exec
}

type ScheduledTrigger struct {
	Trigger         *telephonist.TaskTrigger
	SchedulingError error
}

func (st *ScheduledTrigger) IsSuccessful() bool {
	return st.SchedulingError == nil
}

type ScheduledTask struct {
	DefinedTask       telephonist.DefinedTask
	ScheduledTriggers map[uint64]*ScheduledTrigger
}

type taskExecutor struct {
	triggersExecutors TriggersExecutor
	tasks             map[uuid.UUID]ScheduledTask
	callback          TriggeredTaskCallback
	triggerTasks      map[uint64]map[uuid.UUID]*telephonist.DefinedTask
}

func (e *taskExecutor) UnscheduleTask(taskID uuid.UUID) {
	if t, exists := e.tasks[taskID]; exists {
		for id, _ := range t.ScheduledTriggers {
			e.removeTrigger(id, taskID)
		}
		delete(e.tasks, taskID)
	}
}

func (e *taskExecutor) Schedule(task *telephonist.DefinedTask) error {
	if t, exists := e.tasks[task.ID]; exists {
		// update existing task
		newTriggers := make(map[uint64]*telephonist.TaskTrigger, len(task.Triggers))

		// add or update new triggers
		for _, newTrigger := range task.Triggers {
			id, _ := e.updateTrigger(task.ID, newTrigger)
			newTriggers[id] = newTrigger
		}

		// find old trigger and remove them
		for id, _ := range t.ScheduledTriggers {
			if _, exists := newTriggers[id]; !exists {
				// remove old trigger
				// TODO: do not ignore the error here
				e.removeTrigger(id, t.DefinedTask.ID)
			}
		}
	} else {
		e.tasks[task.ID] = ScheduledTask{
			ScheduledTriggers: make(map[uint64]*ScheduledTrigger, len(task.Triggers)),
			DefinedTask:       *task,
		}
		for _, trigger := range task.Triggers {
			e.updateTrigger(task.ID, trigger)
		}
	}
	return nil
}

func (e *taskExecutor) removeTrigger(triggerID uint64, taskID uuid.UUID) {
	if tasks, exists := e.triggerTasks[triggerID]; exists {
		delete(tasks, taskID)
		if len(tasks) == 0 {
			delete(e.triggerTasks, triggerID)
			// TODO: handle error
			e.triggersExecutors.Unschedule(triggerID)
		}
	}
}

func (e *taskExecutor) updateTrigger(taskID uuid.UUID, trigger *telephonist.TaskTrigger) (uint64, error) {
	st := e.tasks[taskID]
	id := trigger.GetID()

	if strg, exists := st.ScheduledTriggers[id]; exists {
		scheduledID := strg.Trigger.GetID()
		if scheduledID == id {
			// it's the same trigger
			return id, nil
		}
		e.triggersExecutors.Unschedule(scheduledID)
	}
	_, err := e.triggersExecutors.ScheduleByID(id, trigger)
	scheduledTrigger := &ScheduledTrigger{
		Trigger:         trigger,
		SchedulingError: err,
	}
	st.ScheduledTriggers[id] = scheduledTrigger
	return id, err
}

func (e *taskExecutor) Unschedule(id uuid.UUID) (bool, error) {
	if st, exists := e.tasks[id]; exists {
		// TODO: do not ignore the error here
		for id, _ := range st.ScheduledTriggers {
			e.triggersExecutors.Unschedule(id)
		}
		delete(e.tasks, id)
		return true, nil
	}
	return false, nil
}

func (e *taskExecutor) Start() error {
	return e.triggersExecutors.Start()
}

func (e *taskExecutor) Stop() {
	e.triggersExecutors.Stop()
}

func (e *taskExecutor) onTrigger(event TriggeredEvent) {
	if e.callback != nil {
		if tasks, exist := e.triggerTasks[event.TriggerID]; exist {
			for _, task := range tasks {
				e.callback(TaskTriggeredEvent{
					TriggerEvent: event,
					Task:         task,
				})
			}
		}
	}
}

func (e *taskExecutor) SetCallback(callback TriggeredTaskCallback) {
	e.callback = callback
}
