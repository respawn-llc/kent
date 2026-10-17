package serverapi

import (
	"fmt"
)

type PromptCommandErrorKind string

const (
	PromptCommandErrorKindCatalogRead     PromptCommandErrorKind = "catalog_read"
	PromptCommandErrorKindCommandNotFound PromptCommandErrorKind = "command_not_found"
	PromptCommandErrorKindCommandRead     PromptCommandErrorKind = "command_read"
)

type PromptCommandError struct {
	Kind    PromptCommandErrorKind `json:"kind"`
	Command *string                `json:"command,omitempty"`
}

func (e *PromptCommandError) Error() string {
	if e == nil {
		return "prompt command error"
	}
	if e.Command == nil {
		return "prompt command error: " + string(e.Kind)
	}
	return fmt.Sprintf("prompt command %q error: %s", *e.Command, e.Kind)
}
