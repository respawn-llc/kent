package clientui

import (
	"fmt"
	"strings"
)

type ToolCallID string

func (id ToolCallID) Validate() error {
	raw := string(id)
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("tool call id is required")
	}
	if strings.TrimSpace(raw) != raw {
		return fmt.Errorf("tool call id must not have leading or trailing whitespace")
	}
	return nil
}
