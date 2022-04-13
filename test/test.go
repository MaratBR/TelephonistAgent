package main

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type test struct {
	running    int64
	total      int64
	completed  int64
	successful int64
	failed     int64
	seconds    int
	period     int
}

func (t *test) spawn() {
	go func() {
		atomic.AddInt64(&t.running, 1)
		atomic.AddInt64(&t.total, 1)
		err := sleep(t.seconds)
		atomic.AddInt64(&t.running, -1)
		atomic.AddInt64(&t.completed, 1)
		if err == nil {
			atomic.AddInt64(&t.successful, 1)
		} else {
			atomic.AddInt64(&t.failed, 1)
		}
	}()
}

func (t *test) run() {
	var max big.Int
	max.SetInt64(50)
	for {
		if t.running < 200 {
			count, _ := rand.Int(rand.Reader, &max)
			for i := 0; i < int(count.Int64()); i++ {
				t.spawn()
			}
		}
		time.Sleep(time.Second * time.Duration(t.period))
	}
}

func sleep(seconds int) error {
	cmd := exec.Command("/bin/sh")

	cmd.Stdin = strings.NewReader("sleep " + strconv.Itoa(seconds) + "\necho 1\nexit")

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
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
		return err
	}
	v, err := io.ReadAll(stdout2)
	if err != nil {
		return err
	}
	if len(v) != 2 {
		return err
	}
	v, err = io.ReadAll(stderr2)
	if err != nil {
		return err
	}
	if len(v) != 0 {
		return err
	}
	return nil
}

func main() {
	t := test{seconds: 20, period: 1}
	go t.run()

	for {
		print(fmt.Sprintf("(R/S/F/C(S+F)/T) %d/%d/%d/%d/%d\t\t\r", t.running, t.successful, t.failed, t.completed, t.total))
		time.Sleep(time.Second)
	}
}
