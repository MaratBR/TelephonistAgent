package telephonist

import (
	"strings"
)

type CombinedError struct {
	Errors []error
}

func (err *CombinedError) Error() string {
	if len(err.Errors) == 1 {
		return err.Errors[0].Error()
	}
	sb := strings.Builder{}

	sb.WriteString("Multiple errors occured:")
	for _, innerError := range err.Errors {
		sb.WriteString("\n\t")
		sb.WriteString(innerError.Error())
	}
	return sb.String()
}

func ExtractCombined(err error) ([]error, bool) {
	c, ok := err.(*CombinedError)
	if ok {
		return c.Errors, true
	}
	return nil, false
}

func IsCombinedError(err error) bool {
	_, ok := ExtractCombined(err)
	return ok
}

func MustExtractCombined(err error) []error {
	c := err.(*CombinedError)
	return c.Errors
}

type UnexpectedStatusCode struct {
	Status     int
	StatusText string
}

func (err *UnexpectedStatusCode) Error() string {
	return "Unexpected status code: " + err.StatusText
}
