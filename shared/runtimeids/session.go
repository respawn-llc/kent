package runtimeids

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

type SessionID struct {
	value string
}

// SessionIDFromProto converts an identity whose canonical UUID-v4 constraints
// have already been checked at the Protobuf boundary.
func SessionIDFromProto(raw string) SessionID {
	return SessionID{value: raw}
}

func ParseSessionID(raw string) (SessionID, error) {
	if _, err := ParseCanonicalUUIDv4(raw, "session_id"); err != nil {
		return SessionID{}, err
	}
	return SessionID{value: raw}, nil
}

func NewSessionID() SessionID {
	return SessionID{value: uuid.NewString()}
}

func (id SessionID) String() string {
	return id.value
}

func (id SessionID) IsZero() bool {
	return id.value == ""
}

func (id SessionID) MarshalJSON() ([]byte, error) {
	if id.IsZero() {
		return nil, fmt.Errorf("session_id is required")
	}
	return json.Marshal(id.String())
}

func (id *SessionID) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	parsed, err := ParseSessionID(raw)
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}
