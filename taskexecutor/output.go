package taskexecutor

import (
	"bytes"
	"io"
	"sync"
	"time"

	"github.com/MaratBR/TelephonistAgent/telephonist"
)

type outputBufferProxy struct {
	buf      *OutputBuffer
	isStderr bool
}

func (p *outputBufferProxy) Write(data []byte) (n int, err error) {
	return p.buf.Write(data, p.isStderr)
}

type OutputBuffer struct {
	Stderr io.Writer
	Stdout io.Writer

	opts       OutputBufferOptions
	lastFlush  time.Time
	stdout     bytes.Buffer
	stderr     bytes.Buffer
	records    []telephonist.LogRecord
	flushClose chan struct{}
	mut        sync.Mutex
}

type LogFlushCallback func([]telephonist.LogRecord)

type OutputBufferOptions struct {
	UseTmpFile    bool
	FlushEvery    time.Duration
	FlushCallback LogFlushCallback
}

func NewOutputBuffer(opts *OutputBufferOptions) *OutputBuffer {
	buffer := &OutputBuffer{
		records: []telephonist.LogRecord{},
		opts:    *opts,
	}
	buffer.Stdout = &outputBufferProxy{isStderr: false, buf: buffer}
	buffer.Stderr = &outputBufferProxy{isStderr: true, buf: buffer}

	return buffer
}

func (buf *OutputBuffer) Flush() {
	if len(buf.records) == 0 {
		return
	}
	buf.mut.Lock()
	defer buf.mut.Unlock()
	buf.lastFlush = time.Now()
	if buf.opts.FlushCallback != nil {
		buf.opts.FlushCallback(buf.records)
	}
	buf.records = []telephonist.LogRecord{}
}

func (buf *OutputBuffer) Start() {
	buf.lastFlush = time.Now()
	buf.flushClose = make(chan struct{})
	go buf.flushLoop()
}

func (buf *OutputBuffer) Stop() {
	buf.flushClose <- struct{}{}
}

func (buf *OutputBuffer) flushLoop() {
outer:
	for {
		select {
		case <-buf.flushClose:
			buf.Flush()
			break outer
		case <-time.After(buf.opts.FlushEvery):
			buf.Flush()
		}
	}
}

func (buf *OutputBuffer) Write(data []byte, isStderr bool) (int, error) {
	length := len(data)
	var w bytes.Buffer
	if isStderr {
		w = buf.stderr
	} else {
		w = buf.stdout
	}
	{
		leftover := w.Len()
		if leftover != 0 {
			newData := make([]byte, w.Len()+len(data))
			copy(newData, w.Bytes())
			copy(newData[leftover:], data)
			w.Reset()
			data = newData
		}
	}

	lines, remaining := getLines(data)
	records := make([]telephonist.LogRecord, len(lines))

	for i := 0; i < len(lines); i++ {
		records[i].Severity = telephonist.SEVERITY_ERROR
		records[i].Body = lines[i]
	}

	buf.mut.Lock()
	defer buf.mut.Unlock()

	buf.records = append(buf.records, records...)

	if len(remaining) > 0 {
		w.Write(remaining)
	}

	if buf.lastFlush.Add(buf.opts.FlushEvery).Before(time.Now()) {
		buf.Flush()
	}

	return length, nil
}
