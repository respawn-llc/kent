package sessioncontract

import (
	"errors"
	"strings"
)

// NormalizeSessionName preserves absence and trims only a present name's edges.
func NormalizeSessionName(name *string) (*string, error) {
	if name == nil {
		return nil, nil
	}
	normalized := strings.TrimSpace(*name)
	if normalized == "" {
		return nil, errors.New("Session name cannot be blank")
	}
	return &normalized, nil
}
