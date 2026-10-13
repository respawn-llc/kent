package sessioncontract

import (
	"errors"
	"fmt"
)

var ErrSessionNotFound = errors.New("session not found")

type SessionNotFoundError struct {
	SessionID *string
}

func (e *SessionNotFoundError) Error() string {
	if e.SessionID == nil {
		return "Kent session is no longer present. Was it deleted?"
	}
	return fmt.Sprintf("Kent session %s is no longer present. Was it deleted?", *e.SessionID)
}

func (e *SessionNotFoundError) Unwrap() error {
	return ErrSessionNotFound
}
