package taskexecutor

import (
	"testing"
)

func TestGetLines(t *testing.T) {
	s := `first line
second line

fourth line, but third with content
   line with indent
        `
	lines, remaining := getLines([]byte(s))
	if len(lines) != 4 {
		t.Fatalf("invalid number of lines: %d", len(lines))
	}
	if lines[0] != "first line" {
		t.Fatalf("invalid line 0: %s", lines[0])
	}
	for i := 0; i < len(lines); i++ {
		t.Log(lines)
	}
	t.Log(len(remaining), remaining)
}
