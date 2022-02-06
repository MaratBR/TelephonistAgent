package taskexecutor

import (
	"fmt"
	"strconv"
	"strings"
)

type AddTaskError struct {
	TriggerErrors []error
	str           string
}

func (e *AddTaskError) Error() string {
	if e.str == "" {
		errorsCount := 0
		sb := strings.Builder{}

		for _, err := range e.TriggerErrors {
			if err != nil {
				errorsCount += 1
			}
		}
		sb.WriteString(fmt.Sprintf("Following triggers' registration errored (%d in total)", errorsCount))
		for index, err := range e.TriggerErrors {
			if err != nil {
				sb.WriteString("\n\t[" + strconv.Itoa(index) + "]: " + err.Error())
			}
		}
		e.str = sb.String()
	}
	return e.str
}
