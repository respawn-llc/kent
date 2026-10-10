package serverapi

import "core/shared/runtimeids"

type SessionSelfTargetError struct {
	SessionID runtimeids.SessionID
}

func (*SessionSelfTargetError) Error() string { return "invoking Session cannot target itself" }

type RuntimeUnavailableError struct {
	SessionID *runtimeids.SessionID
}

func (*RuntimeUnavailableError) Error() string { return ErrRuntimeUnavailable.Error() }
func (*RuntimeUnavailableError) Unwrap() error { return ErrRuntimeUnavailable }
