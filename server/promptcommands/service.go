package promptcommands

import (
	"errors"
	"fmt"
	"strings"

	"core/prompts"
	"core/shared/runtimeinput"
	"core/shared/textutil"
)

type ErrorKind string

const (
	ErrorKindCatalogRead     ErrorKind = "catalog_read"
	ErrorKindCommandNotFound ErrorKind = "command_not_found"
	ErrorKindCommandRead     ErrorKind = "command_read"
)

type Error struct {
	Kind    ErrorKind
	Command *string
	cause   error
}

func (e *Error) Error() string {
	if e == nil {
		panic("nil prompt command error")
	}
	switch e.Kind {
	case ErrorKindCatalogRead:
		return "prompt command catalog is unavailable"
	case ErrorKindCommandNotFound:
		if e.Command == nil {
			return "prompt command was not found"
		}
		return fmt.Sprintf("prompt command %q was not found", *e.Command)
	case ErrorKindCommandRead:
		if e.Command == nil {
			return "prompt command could not be read"
		}
		return fmt.Sprintf("prompt command %q could not be read", *e.Command)
	default:
		return "prompt command operation failed"
	}
}

func (e *Error) Unwrap() error {
	if e == nil {
		panic("nil prompt command error")
	}
	return e.cause
}

type Service struct {
	persistenceRoot string
	workspaceRoot   string
}

func New(persistenceRoot, workspaceRoot string) Service {
	return Service{
		persistenceRoot: strings.TrimSpace(persistenceRoot),
		workspaceRoot:   strings.TrimSpace(workspaceRoot),
	}
}

type Placement uint8

const (
	PlacementCurrent Placement = iota
	PlacementFresh
)

type ResolvedCommand struct {
	Text      string
	Placement Placement
}

func (s Service) Resolve(command, arguments string) (ResolvedCommand, error) {
	if err := s.validateRoots(ErrorKindCommandRead); err != nil {
		return ResolvedCommand{}, err
	}
	if name, parseErr := runtimeinput.ParsePromptCommandName(command); parseErr == nil {
		if kind, ok := runtimeinput.BuiltinPromptCommandForName(name); ok {
			return ResolvedCommand{Text: textutil.ExpandPromptTemplate(builtinPromptContent(*kind), arguments), Placement: PlacementFresh}, nil
		}
	}
	candidate, found, err := s.findCandidate(command)
	if err != nil {
		return ResolvedCommand{}, err
	}
	if !found {
		parsed, parseErr := runtimeinput.ParsePromptCommandName(command)
		name := strings.TrimSpace(command)
		if parseErr == nil {
			name = parsed.String()
		}
		return ResolvedCommand{}, &Error{Kind: ErrorKindCommandNotFound, Command: &name}
	}
	return ResolvedCommand{Text: textutil.ExpandPromptTemplate(candidate.content, arguments), Placement: PlacementCurrent}, nil
}

type builtinPromptCommand struct {
	kind    runtimeinput.BuiltinPromptCommand
	content string
}

func builtinPromptCommands() []builtinPromptCommand {
	commands := make([]builtinPromptCommand, 0, len(runtimeinput.BuiltinPromptCommands()))
	for _, kind := range runtimeinput.BuiltinPromptCommands() {
		commands = append(commands, builtinPromptCommand{kind: kind, content: builtinPromptContent(kind)})
	}
	return commands
}

func builtinPromptContent(kind runtimeinput.BuiltinPromptCommand) string {
	switch kind {
	case runtimeinput.BuiltinPromptCommandReview:
		return prompts.ReviewPrompt
	case runtimeinput.BuiltinPromptCommandInit:
		return prompts.InitPrompt
	default:
		panic("invalid built-in prompt command")
	}
}

func (s Service) validateRoots(kind ErrorKind) error {
	if s.persistenceRoot == "" {
		return &Error{Kind: kind, cause: errors.New("persistence root is required")}
	}
	if s.workspaceRoot == "" {
		return &Error{Kind: kind, cause: errors.New("workspace root is required")}
	}
	return nil
}
