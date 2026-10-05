package serverapi

import (
	"errors"
	"fmt"
	"strings"

	"core/shared/runtimeinput"
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

func (e *PromptCommandError) Validate() error {
	if e == nil {
		return errors.New("prompt command error is required")
	}
	switch e.Kind {
	case PromptCommandErrorKindCatalogRead, PromptCommandErrorKindCommandNotFound, PromptCommandErrorKindCommandRead:
	default:
		return fmt.Errorf("unknown prompt command error kind %q", e.Kind)
	}
	if e.Command != nil {
		if strings.TrimSpace(*e.Command) == "" {
			return errors.New("prompt command error command cannot be blank")
		}
		if parsed, err := runtimeinput.ParsePromptCommandName(*e.Command); err != nil || parsed.String() != *e.Command {
			return errors.New("prompt command error command must be canonical")
		}
	}
	if (e.Kind == PromptCommandErrorKindCommandNotFound || e.Kind == PromptCommandErrorKindCommandRead) && e.Command == nil {
		return errors.New("command-specific prompt command error requires command")
	}
	return nil
}
