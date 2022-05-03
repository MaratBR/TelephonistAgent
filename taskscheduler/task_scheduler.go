package taskscheduler

import (
	"fmt"
	"time"

	"github.com/MaratBR/TelephonistAgent/telephonist"
	"github.com/MaratBR/TelephonistAgent/utils"
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
	TriggerEvent *TriggeredEvent
	Task         *telephonist.DefinedTask
	Params       map[string]interface{}
}

func (event *TaskTriggeredEvent) CollectMetadata() {

}

type TriggeredTaskCallback func(TaskTriggeredEvent)

type TaskSchedulerInfo struct {
	Name                  string
	TriggersSchedulerInfo TriggersSchedulerInfo
}

type TaskScheduler interface {
	// Schedules (or reschedules, if already scheduled) the task
	Schedule(task *telephonist.DefinedTask) error

	// Removes the task from scheduler
	Unschedule(taskID uuid.UUID) (bool, error)
	Start() error
	Stop()
	SetCallback(TriggeredTaskCallback)
	Explain() TaskSchedulerInfo
}

type TaskSchedulerOptions struct {
	TriggersScheduler TriggersScheduler
}

type ScheduledTrigger struct {
	Trigger         telephonist.TaskTrigger
	SchedulingError error
}

func (st *ScheduledTrigger) IsSuccessful() bool {
	return st.SchedulingError == nil
}

type ScheduledTask struct {
	DefinedTask       *telephonist.DefinedTask
	ScheduledTriggers map[uint64]*ScheduledTrigger
}

type taskScheduler struct {
	triggersScheduler TriggersScheduler
	tasks             map[uuid.UUID]*ScheduledTask
	callback          TriggeredTaskCallback
	triggerTasks      map[uint64]map[uuid.UUID]*telephonist.DefinedTask
}

func NewTaskScheduler(options TaskSchedulerOptions) TaskScheduler {
	if options.TriggersScheduler == nil {
		panic("TriggersScheduler is not set in the options")
	}
	exec := &taskScheduler{
		triggersScheduler: options.TriggersScheduler,
		tasks:             map[uuid.UUID]*ScheduledTask{},
		triggerTasks:      map[uint64]map[uuid.UUID]*telephonist.DefinedTask{},
	}
	exec.triggersScheduler.SetCallback(exec.onTrigger)
	return exec
}

func (e *taskScheduler) Explain() TaskSchedulerInfo {
	return TaskSchedulerInfo{
		Name:                  "taskScheduler",
		TriggersSchedulerInfo: e.triggersScheduler.Explain(),
	}
}

func (e *taskScheduler) UnscheduleTask(taskID uuid.UUID) {
	if t, exists := e.tasks[taskID]; exists {
		for id, _ := range t.ScheduledTriggers {
			e.removeTrigger(taskID, id)
		}
		delete(e.tasks, taskID)
	}
}

func (e *taskScheduler) Schedule(task *telephonist.DefinedTask) error {
	if t, exists := e.tasks[task.ID]; exists {
		t.DefinedTask = task
		// update existing task
		logger.Warn().
			Str("task ID", task.ID.String()).
			Str("task name", task.Name).
			Msg("rescheduling the task")
		newTriggers := make(map[uint64]*telephonist.TaskTrigger, len(task.Triggers))

		// add or update new triggers
		for _, newTrigger := range task.Triggers {
			id, err := e.updateTrigger(task.ID, newTrigger)
			if err != nil {
				logger.Error().
					Err(err).
					Str("trigger", fmt.Sprintf("%#v", newTrigger)).
					Msg("failed to update trigger")
			} else {
				newTriggers[id] = newTrigger
			}
		}

		// find old trigger and remove them
		for id, oldTrigger := range t.ScheduledTriggers {
			if _, exists := newTriggers[id]; !exists {
				logger.Debug().
					Uint64("trigger ID", id).
					Str("name", oldTrigger.Trigger.Name).
					Str("jsonBody", string(oldTrigger.Trigger.Body)).
					Msg("removed the trigger")
				e.removeTrigger(t.DefinedTask.ID, id)
			}
		}
	} else {
		e.tasks[task.ID] = &ScheduledTask{
			ScheduledTriggers: make(map[uint64]*ScheduledTrigger, len(task.Triggers)),
			DefinedTask:       task,
		}
		if len(task.Triggers) == 0 {
			logger.Warn().Str("task name", task.Name).Msg("task has no triggers")
		} else {
			logger.Warn().
				Str("task name", task.Name).
				Str("task ID", task.ID.String()).
				Int("count of triggers", len(task.Triggers)).
				Msg("scheduling new task")
		}
		for _, trigger := range task.Triggers {
			_, err := e.updateTrigger(task.ID, trigger)
			if err != nil {
				logger.Error().
					Err(err).
					Str("task ID", task.ID.String()).
					Str("task name", task.Name).
					Msg("failed to update trigger while scheduling task")
			}
		}
	}
	return nil
}

func (e *taskScheduler) removeTrigger(taskID uuid.UUID, triggerID uint64) {
	if tasks, exists := e.triggerTasks[triggerID]; exists {
		delete(tasks, taskID)
		if len(tasks) == 0 {
			delete(e.triggerTasks, triggerID)
			e.triggersScheduler.Unschedule(triggerID)
		}
	}
}

func (e *taskScheduler) updateTrigger(taskID uuid.UUID, trigger *telephonist.TaskTrigger) (uint64, error) {
	st := e.tasks[taskID]
	id := trigger.GetID()

	// check if trigger is already scheduled, if it isn't - schedule it
	if _, exists := st.ScheduledTriggers[id]; !exists {
		_, err := e.triggersScheduler.ScheduleByID(id, trigger)
		if err != nil {
			return 0, utils.ChainError("failed to schedule trigger", err)
		}
		scheduledTrigger := &ScheduledTrigger{
			Trigger:         *trigger,
			SchedulingError: err,
		}
		st.ScheduledTriggers[id] = scheduledTrigger
		e.triggerTasks[id] = map[uuid.UUID]*telephonist.DefinedTask{taskID: st.DefinedTask}
	} else {
		e.triggerTasks[id][taskID] = st.DefinedTask
	}

	return id, nil
}

func (e *taskScheduler) Unschedule(id uuid.UUID) (bool, error) {
	if st, exists := e.tasks[id]; exists {
		// TODO: do not ignore the error here
		for id, _ := range st.ScheduledTriggers {
			e.triggersScheduler.Unschedule(id)
		}
		delete(e.tasks, id)
		return true, nil
	}
	return false, nil
}

func (e *taskScheduler) Start() error {
	return e.triggersScheduler.Start()
}

func (e *taskScheduler) Stop() {
	e.triggersScheduler.Stop()
}

func (e *taskScheduler) onTrigger(event *TriggeredEvent) {
	if e.callback != nil {
		if tasks, exist := e.triggerTasks[event.TriggerID]; exist {
			for _, task := range tasks {
				e.callback(TaskTriggeredEvent{
					TriggerEvent: event,
					Task:         task,
					Params: map[string]interface{}{
						"TASK_ID":      task.ID,
						"TASK_NAME":    task.Name,
						"TRIGGERED_AT": time.Now().Unix(),
					},
				})
			}
		}
	}
}

func (e *taskScheduler) SetCallback(callback TriggeredTaskCallback) {
	e.callback = callback
}
