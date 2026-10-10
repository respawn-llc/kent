package runtimeids

import (
	"fmt"
	"strconv"
)

const MaxSessionShortID int64 = 9_007_199_254_740_991

type SessionShortID struct{ value int64 }

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

func ParseSessionSelector(raw string) (SessionSelector, error) {
	if value, err := strconv.ParseInt(raw, 10, 64); err == nil {
		for _, character := range raw {
			if character < '0' || character > '9' {
				return nil, fmt.Errorf("session short ID must contain only decimal digits")
			}
		}
		return NewSessionShortID(value)
	}
	id, err := ParseSessionID(raw)
	if err != nil {
		return nil, err
	}
	if !id.IsCanonicalUUIDv4() {
		return nil, fmt.Errorf("session selector must be a positive number or canonical UUIDv4")
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
