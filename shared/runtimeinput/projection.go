package runtimeinput

import (
	"errors"
	"strings"
)

func CanonicalCommandText(name string, arguments string) string {
	return formatCommandText(name, arguments)
}

func formatCommandText(name string, arguments string) string {
	text := "/" + name
	if trimmed := strings.TrimSpace(arguments); trimmed != "" {
		text += " " + trimmed
	}
	return text
}

func (c PromptCommand) CanonicalHistoryText() string {
	for _, command := range BuiltinPromptCommands() {
		if c.Name == command.Name() {
			return formatCommandText(command.Alias(), c.Arguments)
		}
	}
	return CanonicalCommandText(c.Name, c.Arguments)
}

func (i Input) CanonicalHistoryText() string {
	if i.Kind == KindShell {
		return i.Shell.CanonicalInput()
	}
	if i.Kind == KindText {
		return *i.Text
	}
	return i.PromptCommand.CanonicalHistoryText()
}

func (i Input) ExecutionText(resolve func(PromptCommand) (string, error)) (string, error) {
	if i.Kind == KindShell {
		return "", errors.New("Shell input requires executable action admission")
	}
	if i.Kind == KindText {
		return *i.Text, nil
	}
	if resolve == nil {
		return "", errors.New("prompt command resolver is required")
	}
	return resolve(*i.PromptCommand)
}
