package serverapi

import (
	"encoding/json"
	"testing"

	"core/shared/runtimeinput"
)

func TestRuntimeUserTurnInputIsAValidatedDiscriminatedUnion(t *testing.T) {
	text := runtimeinput.Text("hello")
	if err := text.Validate(); err != nil {
		t.Fatalf("text Validate: %v", err)
	}
	prompt := runtimeinput.Command("prompt:review", "src")
	if err := prompt.Validate(); err != nil {
		t.Fatalf("prompt Validate: %v", err)
	}

	wire, err := json.Marshal(prompt)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded runtimeinput.Input
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if err := decoded.Validate(); err != nil {
		t.Fatalf("decoded Validate: %v", err)
	}
	if decoded.Kind != runtimeinput.KindPromptCommand ||
		decoded.PromptCommand == nil ||
		decoded.PromptCommand.Name != "prompt:review" {
		t.Fatalf("decoded = %+v", decoded)
	}
}

func TestRuntimeUserTurnInputRejectsInvalidCardinality(t *testing.T) {
	tests := []runtimeinput.Input{
		{},
		{Kind: runtimeinput.KindText},
		{Kind: runtimeinput.KindText, Text: runtimeInputStringPtr("text"), PromptCommand: &runtimeinput.PromptCommand{Name: "prompt:x"}},
		{Kind: runtimeinput.KindPromptCommand, PromptCommand: &runtimeinput.PromptCommand{}},
		{Kind: runtimeinput.Kind("other"), Text: runtimeInputStringPtr("text")},
	}
	for _, input := range tests {
		if err := input.Validate(); err == nil {
			t.Fatalf("Validate(%+v) succeeded", input)
		}
	}
}

func runtimeInputStringPtr(value string) *string {
	return &value
}
