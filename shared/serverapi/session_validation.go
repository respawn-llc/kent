package serverapi

import (
	"errors"
	"path/filepath"
	"strings"
)

// ErrSessionIDRequired is returned when a session id is empty or whitespace-only.
var ErrSessionIDRequired = errors.New("session_id is required")

// ErrSessionIDNotSingle is returned when a session id is not a single,
// container-relative session id (absolute, traversal, or contains separators).
var ErrSessionIDNotSingle = errors.New("session_id must be a single session id")

func validateRequiredSessionID(sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return ErrSessionIDRequired
	}
	return nil
}

func validateScopedSessionID(sessionID string) error {
	trimmed := strings.TrimSpace(sessionID)
	if err := validateRequiredSessionID(trimmed); err != nil {
		return err
	}
	if filepath.IsAbs(trimmed) || trimmed == "." || trimmed == ".." {
		return ErrSessionIDNotSingle
	}
	if strings.Contains(trimmed, "/") || strings.Contains(trimmed, "\\") {
		return ErrSessionIDNotSingle
	}
	if filepath.Clean(trimmed) != trimmed {
		return ErrSessionIDNotSingle
	}
	return nil
}
