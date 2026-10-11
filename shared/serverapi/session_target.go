package serverapi

import "core/shared/runtimeids"

type SessionTargetError struct {
	Identity runtimeids.SessionIdentity
	Cause    error
}

func (e *SessionTargetError) Error() string { return e.Cause.Error() }
func (e *SessionTargetError) Unwrap() error { return e.Cause }

type SessionSelfTargetError struct {
	SessionID runtimeids.SessionID
}

func (*SessionSelfTargetError) Error() string { return "invoking Session cannot target itself" }

type RuntimeUnavailableError struct {
	SessionID *runtimeids.SessionID
	ShortID   *runtimeids.SessionShortID
}

func (*RuntimeUnavailableError) Error() string { return ErrRuntimeUnavailable.Error() }
func (*RuntimeUnavailableError) Unwrap() error { return ErrRuntimeUnavailable }
