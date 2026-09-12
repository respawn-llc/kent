package serverapi

import (
	"errors"
)

var ErrRuntimeCommandNotAccepted = errors.New("runtime command was not accepted")

type RuntimeCommandNotAcceptedError struct {
	Cause error
}

func NewRuntimeCommandNotAcceptedError(cause error) *RuntimeCommandNotAcceptedError {
	return &RuntimeCommandNotAcceptedError{Cause: cause}
}

func (e *RuntimeCommandNotAcceptedError) Error() string {
	if e == nil || e.Cause == nil {
		return ErrRuntimeCommandNotAccepted.Error()
	}
	return ErrRuntimeCommandNotAccepted.Error() + ": " + e.Cause.Error()
}

func (e *RuntimeCommandNotAcceptedError) Unwrap() []error {
	if e == nil || e.Cause == nil {
		return []error{ErrRuntimeCommandNotAccepted}
	}
	return []error{ErrRuntimeCommandNotAccepted, e.Cause}
}
