package serverapi

import (
	"errors"

	"core/shared/runtimeids"
)

type SessionTarget interface {
	sessionTarget()
}

type UnresolvedSessionTarget struct{}
type CanonicalSessionTarget struct{ ID runtimeids.SessionID }
type ProjectedSessionTarget struct{ Identity runtimeids.SessionIdentity }

func (UnresolvedSessionTarget) sessionTarget() {}
func (CanonicalSessionTarget) sessionTarget()  {}
func (ProjectedSessionTarget) sessionTarget()  {}

type SessionTargetError struct {
	Target SessionTarget
	Cause  error
}

func (e *SessionTargetError) Error() string { return e.Cause.Error() }
func (e *SessionTargetError) Unwrap() error { return e.Cause }

func (e *SessionTargetError) SessionID() *runtimeids.SessionID {
	switch target := e.Target.(type) {
	case UnresolvedSessionTarget:
		return nil
	case CanonicalSessionTarget:
		return &target.ID
	case ProjectedSessionTarget:
		return &target.Identity.ID
	default:
		panic("Session failure requires target context")
	}
}

func (e *SessionTargetError) Projection() *runtimeids.SessionIdentity {
	switch target := e.Target.(type) {
	case UnresolvedSessionTarget, CanonicalSessionTarget:
		return nil
	case ProjectedSessionTarget:
		return &target.Identity
	default:
		panic("Session failure requires target context")
	}
}

func WithSessionTarget(target SessionTarget, err error) error {
	if err == nil {
		return nil
	}
	var captured *SessionTargetError
	if errors.As(err, &captured) {
		return err
	}
	return &SessionTargetError{Target: target, Cause: err}
}

type SessionSelfTargetError struct {
	SessionID runtimeids.SessionID
}

func (*SessionSelfTargetError) Error() string { return "invoking Session cannot target itself" }
