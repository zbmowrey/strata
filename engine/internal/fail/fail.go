// Package fail defines the error type that carries a CLI exit code and a
// user-facing hint. Domain packages return *fail.Error for conditions the
// exit-code contract (docs/spec/09-cli.md §2) distinguishes.
package fail

import (
	"errors"
	"fmt"
)

// Exit codes (contract: docs/spec/09-cli.md §2).
const (
	OK          = 0
	Runtime     = 1
	Blocking    = 2
	Partial     = 3
	Usage       = 4
	NotInstance = 5
	Locked      = 6
)

// Error is an error with an exit code and an optional hint.
type Error struct {
	Err  error
	Hint string
	Code int
}

func (e *Error) Error() string {
	if e.Err == nil {
		return "unknown error"
	}
	return e.Err.Error()
}

// Unwrap returns the wrapped error.
func (e *Error) Unwrap() error { return e.Err }

// New builds an *Error with the given code, hint and message.
func New(code int, hint, format string, args ...any) *Error {
	return &Error{Code: code, Hint: hint, Err: fmt.Errorf(format, args...)}
}

// Wrap attaches an exit code and hint to err. A nil err returns nil.
func Wrap(code int, hint string, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Code: code, Hint: hint, Err: err}
}

// CodeOf reports the exit code for err: the code of the outermost *Error in
// its chain, or Runtime for any other non-nil error, or OK for nil.
func CodeOf(err error) int {
	if err == nil {
		return OK
	}
	var fe *Error
	if errors.As(err, &fe) {
		return fe.Code
	}
	return Runtime
}

// HintOf returns the first non-empty hint in err's chain.
func HintOf(err error) string {
	for err != nil {
		var fe *Error
		if !errors.As(err, &fe) {
			return ""
		}
		if fe.Hint != "" {
			return fe.Hint
		}
		err = fe.Err
	}
	return ""
}
