package serverapi

import (
	"errors"
)

// ErrSessionIDRequired is returned when a session id is empty or whitespace-only.
var ErrSessionIDRequired = errors.New("session_id is required")
