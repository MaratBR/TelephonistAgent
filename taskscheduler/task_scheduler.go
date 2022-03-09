package taskscheduler

import (
	"fmt"
	"time"

	"github.com/MaratBR/TelephonistAgent/telephonist"
	"github.com/google/uuid"
	"go.uber.org/zap"
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
	DefinedTask       telephonist.DefinedTask
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
		// update existing task
		logger.Debug("updating the task", zap.String("taskId", task.ID.String()))
		newTriggers := make(map[uint64]*telephonist.TaskTrigger, len(task.Triggers))

		// add or update new triggers
		for _, newTrigger := range task.Triggers {
			id, _ := e.updateTrigger(task.ID, newTrigger)
			newTriggers[id] = newTrigger
		}

		// find old trigger and remove them
		for id, oldTrigger := range t.ScheduledTriggers {
			if _, exists := newTriggers[id]; !exists {
				// remove old trigger
				// TODO: do not ignore the error here
				logger.Debug("removed the trigger",
					zap.Uint64("triggerID", id),
					zap.String("name", oldTrigger.Trigger.Name),
					zap.String("body", string(oldTrigger.Trigger.Body)),
				)
				e.removeTrigger(t.DefinedTask.ID, id)
			}
		}
	} else {
		logger.Debug("scheduling new task",
			zap.String("taskId", task.ID.String()),
			zap.Int("triggersTotal", len(task.Triggers)),
		)
		e.tasks[task.ID] = &ScheduledTask{
			ScheduledTriggers: make(map[uint64]*ScheduledTrigger, len(task.Triggers)),
			DefinedTask:       *task,
		}
		if len(task.Triggers) == 0 {
			logger.Warn(fmt.Sprintf("task %s has no triggers", task.Name))
		}
		for _, trigger := range task.Triggers {
			e.updateTrigger(task.ID, trigger)
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

	logger.Debug("updateTrigger", zap.Uint64("triggerID", id), zap.String("name", trigger.Name), zap.String("body", string(trigger.Body)))

	// check if trigger is already scheduled, if it isn't - schedule it
	if _, exists := st.ScheduledTriggers[id]; !exists {
		_, err := e.triggersScheduler.ScheduleByID(id, trigger)
		if err != nil {
			return 0, err
		}
		scheduledTrigger := &ScheduledTrigger{
			Trigger:         *trigger,
			SchedulingError: err,
		}
		st.ScheduledTriggers[id] = scheduledTrigger
		e.triggerTasks[id] = map[uuid.UUID]*telephonist.DefinedTask{taskID: &st.DefinedTask}
	} else {
		e.triggerTasks[id][taskID] = &st.DefinedTask
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
