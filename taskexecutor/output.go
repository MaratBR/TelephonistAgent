package taskexecutor

import (
	"bytes"
	"io"
	"sync"
	"time"

	"github.com/MaratBR/TelephonistAgent/logging"
	"github.com/MaratBR/TelephonistAgent/telephonist"
)

type outputBufferProxy struct {
	buf      *OutputBuffer
	isStderr bool
}

func (p *outputBufferProxy) Write(data []byte) (n int, err error) {
	return p.buf.Write(data, p.isStderr)
}

type recordType byte

const (
	noRecord recordType = iota
	stdoutRecord
	stderrRecord
)

var logger = logging.ChildLogger("OutputBuffer")

type OutputBuffer struct {
	Stderr io.Writer
	Stdout io.Writer

	opts               OutputBufferOptions
	lastFlush          time.Time
	stdout             bytes.Buffer
	stderr             bytes.Buffer
	records            []telephonist.LogRecord
	flushClose         chan struct{}
	closedConfirmation chan struct{}
	mut                sync.Mutex

	currentRecordType recordType
	recordDeadlineUs  int64
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
	logger.Debug().Msg("starting output buffer")
	buf.lastFlush = time.Now()
	buf.flushClose = make(chan struct{})
	buf.closedConfirmation = make(chan struct{})
	go buf.flushLoop()
}

func (buf *OutputBuffer) Stop() {
	logger.Debug().Msg("stopping output buffer...")
	buf.flushClose <- struct{}{}
	<-buf.closedConfirmation
	logger.Debug().Msg("stopped output buffer")
}

func (buf *OutputBuffer) flushLoop() {
outer:
	for {
		select {
		case <-buf.flushClose:
			buf.FlushRecord()
			buf.Flush()
			buf.closedConfirmation <- struct{}{}
			break outer
		case <-time.After(buf.opts.FlushEvery):
			buf.Flush()
		}
	}
}

func (buf *OutputBuffer) FlushRecord() {
	if buf.currentRecordType == noRecord {
		return
	}

	buf.mut.Lock()
	defer buf.mut.Unlock()

	var (
		severity telephonist.LogSeverity
		w        *bytes.Buffer
	)

	if buf.currentRecordType == stderrRecord {
		w = &buf.stderr
		severity = telephonist.SEVERITY_ERROR
	} else if buf.currentRecordType == stdoutRecord {
		w = &buf.stdout
		severity = telephonist.SEVERITY_INFO
	}

	b := make([]byte, w.Len())
	w.Read(b)
	buf.records = append(buf.records, telephonist.LogRecord{
		Severity: severity,
		Time:     time.Now().UnixMicro(),
		Body:     string(b),
	})
	w.Reset()
	buf.currentRecordType = noRecord
}

func (buf *OutputBuffer) Write(data []byte, isStderr bool) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	length := len(data)
	var (
		w       *bytes.Buffer
		recType recordType
	)
	if isStderr {
		recType = stderrRecord
		w = &buf.stderr
	} else {
		recType = stdoutRecord
		w = &buf.stdout
	}

	if buf.currentRecordType == noRecord {
		buf.recordDeadlineUs = time.Now().UnixMicro() + 20_000
	} else if buf.currentRecordType != recType || time.Now().UnixMicro() > buf.recordDeadlineUs {
		buf.FlushRecord()
	}

	buf.currentRecordType = recType
	c, err := w.Write(data)
	if err != nil {
		// TODO panic?
		return c, err
	}

	return length, nil
}
