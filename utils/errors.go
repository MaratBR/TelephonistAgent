package utils

import "strings"

type ChainedError struct {
	InnerError error
	Message    string
	level      int
}

func ChainError(message string, inner error) *ChainedError {
	level := 0
	if chained, ok := inner.(*ChainedError); ok {
		level = chained.level + 1
	}

	return &ChainedError{
		Message:    message,
		InnerError: inner,
		level:      level,
	}
}

func (self *ChainedError) Error() string {
	var sb strings.Builder
	var err error = self
	var chain *ChainedError
	prefix := "\n"
	var ok bool

	for {
		if chain, ok = err.(*ChainedError); !ok {
			sb.WriteString(prefix + strings.ReplaceAll(err.Error(), "\n", "\n"+prefix))
			break
		} else {
			sb.WriteString(prefix + strings.ReplaceAll(chain.Message, "\n", "\n"+prefix))
			prefix += "\t"
			err = chain.InnerError
		}
	}

	return sb.String()
}
