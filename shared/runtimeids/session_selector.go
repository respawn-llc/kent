package runtimeids

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const MaxSessionShortID int64 = 9_007_199_254_740_991

type SessionShortID struct{ value int64 }

// SessionShortIDFromProto converts a number already accepted by the wire contract.
func SessionShortIDFromProto(value int64) SessionShortID {
	return SessionShortID{value: value}
}

func NewSessionShortID(value int64) (SessionShortID, error) {
	if value <= 0 || value > MaxSessionShortID {
		return SessionShortID{}, fmt.Errorf("session short ID must be between 1 and %d", MaxSessionShortID)
	}
	return SessionShortID{value: value}, nil
}

func (id SessionShortID) Int64() int64 { return id.value }

func (id SessionShortID) String() string { return strconv.FormatInt(id.value, 10) }

// SessionSelector is an input address, never an operation's canonical identity.
type SessionSelector interface {
	fmt.Stringer
	sessionSelector()
}

func (SessionID) sessionSelector()      {}
func (SessionShortID) sessionSelector() {}

// InvalidSessionShortIDError prevents malformed numeric targets from being
// reinterpreted as ordinary prompt text by command dispatch.
type InvalidSessionShortIDError struct{ Cause error }

func (e *InvalidSessionShortIDError) Error() string { return e.Cause.Error() }
func (e *InvalidSessionShortIDError) Unwrap() error { return e.Cause }

func ParseSessionSelector(raw string) (SessionSelector, error) {
	value, parseErr := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if parseErr == nil || errors.Is(parseErr, strconv.ErrRange) {
		if parseErr != nil {
			return nil, &InvalidSessionShortIDError{Cause: parseErr}
		}
		for _, character := range raw {
			if character < '0' || character > '9' {
				return nil, &InvalidSessionShortIDError{Cause: fmt.Errorf("session short ID must contain only decimal digits")}
			}
		}
		id, err := NewSessionShortID(value)
		if err != nil {
			return nil, &InvalidSessionShortIDError{Cause: err}
		}
		return id, nil
	}
	id, err := ParseSessionID(raw)
	if err != nil {
		return nil, err
	}
	return id, nil
}

// SessionIdentity is a read projection; the optional number is owned by metadata.
type SessionIdentity struct {
	ID      SessionID
	ShortID *SessionShortID
}

func (identity SessionIdentity) String() string {
	if identity.ShortID != nil {
		return identity.ShortID.String()
	}
	return identity.ID.String()
}
