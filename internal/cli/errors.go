package cli

import "errors"

var errInputTooLarge = errors.New("input exceeds size limit")

type ExitCode int

const (
	ExitOK ExitCode = iota
	ExitInput
	ExitExecution
	ExitOutput
	ExitAssertion
)

type failure struct {
	Code, Message string
	Exit          ExitCode
}

func inputFailure(code, message string) *failure {
	return &failure{Code: code, Message: message, Exit: ExitInput}
}
