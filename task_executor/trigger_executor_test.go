package taskexecutor_test

import (
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	taskexecutor "github.com/MaratBR/TelephonistAgent/task_executor"
	"github.com/MaratBR/TelephonistAgent/telephonist"
)

func touchFile(fileName string) {
	_, err := os.Stat(fileName)
	if os.IsNotExist(err) {
		file, err := os.Create(fileName)
		if err != nil {
			log.Fatal(err)
		}
		defer file.Close()
	} else {
		currentTime := time.Now().Local()
		err = os.Chtimes(fileName, currentTime, currentTime)
		if err != nil {
			fmt.Println(err)
		}
	}
}

func TestFSTrigger(t *testing.T) {
	e, err := taskexecutor.NewFSTriggersExecutor()
	var lastEvent *taskexecutor.TriggeredEvent
	e.SetCallback(func(event taskexecutor.TriggeredEvent) {
		lastEvent = new(taskexecutor.TriggeredEvent)
		*lastEvent = event
	})
	if err != nil {
		t.Error(err)
	}
	if e.CanSchedule(telephonist.TRIGGER_CRON) {
		t.Fatal("invalid trigger can be scheduled")
	}

	if !e.CanSchedule(telephonist.TRIGGER_FSNOTIFY) {
		t.Fatal("valid trigger can not be scheduled")
	}

	fileName := "/tmp/testfile_telephonist"
	err = e.Start()
	if err != nil {
		t.Error(err)
	}
	touchFile(fileName)
	_, err = e.Schedule(&telephonist.TaskTrigger{
		Name: telephonist.TRIGGER_FSNOTIFY,
		Body: []byte("\"" + fileName + "\""), // JSON string
	})
	if err != nil {
		t.Error(err)
	}
	touchFile(fileName)
	time.Sleep(time.Millisecond * 100)
	if lastEvent == nil {
		t.Fatal("trigger did not work")
	}

	e.Stop()
}
