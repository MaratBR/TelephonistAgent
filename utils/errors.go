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

func (err *ChainedError) Error() string {
	var sb strings.Builder
	chain := err
	prefix := ""

	for l := err.level; l > 0; l-- {
		sb.WriteString(prefix + strings.ReplaceAll(chain.Message, "\n", "\n"+prefix))
		prefix += "\n"
		chain = chain.InnerError.(*ChainedError)
	}
	sb.WriteString(prefix + strings.ReplaceAll(chain.InnerError.Error(), "\n", "\n"+prefix))

	return sb.String()
}
