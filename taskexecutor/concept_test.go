package taskexecutor

import (
	"bytes"
	"io"
	"os/exec"
	"strings"
	"testing"
)

func TestConcept(t *testing.T) {
	cmd := exec.Command("/bin/sh")
	cmd.Stdin = strings.NewReader("sleep 10\necho 1\nexit")

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
