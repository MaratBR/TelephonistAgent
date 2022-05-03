package taskexecutor

import (
	"encoding/json"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/MaratBR/TelephonistAgent/telephonist"
)

type TaskExecutionDescriptor struct {
	Task          *telephonist.DefinedTask
	Params        map[string]interface{}
	FlushCallback LogFlushCallback
}

type ExecutorInfo struct {
	Name             string
	AllowedTaskTypes []string
	Details          interface{}
}

type Executor interface {
	Execute(descriptor *TaskExecutionDescriptor) error
	CanExecute(task *telephonist.DefinedTask) bool
	Explain() ExecutorInfo
}

type ShellExecutor struct {
}

func NewShellExecutor() *ShellExecutor {
	return &ShellExecutor{}
}

func (e *ShellExecutor) Explain() ExecutorInfo {
	return ExecutorInfo{
		Name:             "ShellExecutor",
		AllowedTaskTypes: []string{telephonist.TASK_TYPE_EXEC, telephonist.TASK_TYPE_SCRIPT},
	}
}

func (e *ShellExecutor) CanExecute(task *telephonist.DefinedTask) bool {
	return task.Body.Type == telephonist.TASK_TYPE_EXEC || task.Body.Type == telephonist.TASK_TYPE_SCRIPT
}

func (e *ShellExecutor) Execute(descriptor *TaskExecutionDescriptor) error {
	if descriptor.Task.Body.Type != telephonist.TASK_TYPE_EXEC && descriptor.Task.Body.Type != telephonist.TASK_TYPE_SCRIPT {
		panic("invali task type")
	}

	body := descriptor.Task.MustString() + "\n"
	var shell string

	if descriptor.Task.Body.Type == telephonist.TASK_TYPE_SCRIPT {
		shell = findShebang(body)
		if shell == "" {
			shell = "/bin/bash"
		}
	} else {
		shell = "/bin/bash"
	}
	cmd := exec.Command(shell)

	// fill environment variables
	cmd.Env = make([]string, len(descriptor.Params)+1)
	index := 0
	for param, value := range descriptor.Params {
		if value == nil {
			continue
		}
		var stringValue string
		switch value.(type) {
		case string:
			stringValue = value.(string)
		case int:
			stringValue = strconv.Itoa(value.(int))
		default:
			str, err := json.Marshal(value)
			if err != nil {
				stringValue = string(str)
			} else {
				continue
			}
		}
		cmd.Env[index] = param + "=" + stringValue
		index += 1
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	io.WriteString(stdin, body)
	stdin.Close()

	// output (i.e. logs)
	output := NewOutputBuffer(&OutputBufferOptions{
		UseTmpFile:    true,
		FlushEvery:    time.Second,
		FlushCallback: descriptor.FlushCallback,
	})

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		io.Copy(output.Stdout, stdout)
		wg.Done()
	}()

	go func() {
		io.Copy(output.Stderr, stderr)
		wg.Done()
	}()

	output.Start()
	err = cmd.Run()
	wg.Wait()
	output.Stop()
	return err
}
