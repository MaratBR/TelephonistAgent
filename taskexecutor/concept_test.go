package taskexecutor

import (
	"bytes"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func sleep(seconds int) {
	cmd := exec.Command("/bin/sh")
	cmd.Stdin = strings.NewReader("sleep " + strconv.Itoa(seconds) + "\necho 1\nexit")

	stderr, err := cmd.StderrPipe()
	if err != nil {
		panic(err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		panic(err)
	}

	stdout2 := bytes.NewBuffer([]byte{})
	stderr2 := bytes.NewBuffer([]byte{})

	go func() {
		io.Copy(stdout2, stdout)
	}()

	go func() {
		io.Copy(stderr2, stderr)
	}()

	err = cmd.Run()

	if err != nil {
		panic(err)
	}
	v, err := io.ReadAll(stdout2)
	if err != nil {
		panic(err)
	}
	if len(v) != 2 {
		panic(len(v))
	}
	v, err = io.ReadAll(stderr2)
	if err != nil {
		panic(err)
	}
	if len(v) != 0 {
		panic(len(v))
	}
}

func TestConcept(t *testing.T) {
	sleep(10)
}

func TestConceptManyTimes(t *testing.T) {
	var count int64 = 1
	seconds := 60
	for i := int64(0); i < count; i++ {
		go func() {
			sleep(seconds)
			atomic.AddInt64(&count, -1)
		}()
	}
	time.Sleep(time.Second * time.Duration(seconds+1))
	if count > 0 {
		panic(count)
	}
}
