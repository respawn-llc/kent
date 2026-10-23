package runtimeinput

type Kind string

const (
	KindText          Kind = "text"
	KindPromptCommand Kind = "prompt_command"
)

type PromptCommand struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Input struct {
	Kind          Kind           `json:"kind"`
	Text          *string        `json:"text,omitempty"`
	PromptCommand *PromptCommand `json:"prompt_command,omitempty"`
}

func Text(text string) Input {
	return Input{Kind: KindText, Text: &text}
}

func Command(name, arguments string) Input {
	return Input{
		Kind:          KindPromptCommand,
		PromptCommand: &PromptCommand{Name: name, Arguments: arguments},
	}
}

func NewBuiltinPromptCommand(command BuiltinPromptCommand, arguments string) PromptCommand {
	return PromptCommand{Name: command.Name(), Arguments: arguments}
}

func BuiltinCommand(command BuiltinPromptCommand, arguments string) Input {
	prompt := NewBuiltinPromptCommand(command, arguments)
	return Input{
		Kind:          KindPromptCommand,
		PromptCommand: &prompt,
	}
}
