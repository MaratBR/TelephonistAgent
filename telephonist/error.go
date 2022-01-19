package telephonist

import (
	"net/http"
	"strconv"
	"strings"
)

type CombinedError []error

func (err CombinedError) Error() string {
	sb := strings.Builder{}
	sb.WriteString("Multiple errors occured:")
	for _, innerError := range err {
		sb.WriteString("\n\t")
		sb.WriteString(innerError.Error())
	}
	return sb.String()
}

func ExtractCombined(err error) ([]error, bool) {
	c, ok := err.(CombinedError)
	return c, ok
}

func IsCombinedError(err error) bool {
	_, ok := ExtractCombined(err)
	return ok
}

func MustExtractCombined(err error) []error {
	c := err.(CombinedError)
	return c
}

type HTTPError struct {
	Status int
}

func (e HTTPError) Error() string {
	s := "Unexpected http status code: " + strconv.Itoa(e.Status) + " - " + http.StatusText(e.Status)
	return s
}
