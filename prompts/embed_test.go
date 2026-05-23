package prompts

import (
	"strings"
	"testing"

	"builder/cli/selfcmd"
)

func TestRenderSystemPromptTemplateUsesTypedFields(t *testing.T) {
	rendered := renderSystemPromptTemplate("calls={{.EstimatedToolCallsForContext}} cmd={{.BuilderRunCommand}} edit={{.EditingToolName}}", SystemPromptTemplateArgs{
		EstimatedToolCallsForContext: 123,
		EditingToolName:              "edit",
	}, "")
	if !strings.Contains(rendered, "calls=123") {
		t.Fatalf("expected estimated tool calls rendered, got %q", rendered)
	}
	expectedCmd := "cmd=" + selfcmd.RunCommandPrefix()
	if !strings.Contains(rendered, expectedCmd) || strings.Contains(rendered, "{{") {
		t.Fatalf("expected %q in rendered output, got %q", expectedCmd, rendered)
	}
	if !strings.Contains(rendered, "edit=edit") {
		t.Fatalf("expected editing tool name rendered, got %q", rendered)
	}
}

func TestCustomSystemPromptResolvesDefaultSystemPromptPlaceholder(t *testing.T) {
	defaultPrompt := BaseSystemPrompt(SystemPromptTemplateArgs{
		EstimatedToolCallsForContext: 123,
	})
	rendered, err := RenderCustomSystemPrompt("custom\n{{.DefaultSystemPrompt}}", false, SystemPromptTemplateArgs{
		EstimatedToolCallsForContext: 123,
	})
	if err != nil {
		t.Fatalf("RenderCustomSystemPrompt: %v", err)
	}
	if !strings.Contains(rendered, "custom\n") {
		t.Fatalf("expected custom prefix, got %q", rendered)
	}
	if !strings.Contains(rendered, defaultPrompt) || strings.Contains(rendered, "{{") {
		t.Fatalf("expected default prompt placeholder rendered, got %q", rendered)
	}
}

func TestCustomSystemPromptResolvesDefaultSystemPromptSectionPlaceholders(t *testing.T) {
	rendered, err := RenderCustomSystemPrompt(strings.Join([]string{
		"{{.DefaultSystemPromptPersonality}}",
		"{{.DefaultSystemPromptHarnessWorkflowAutonomy}}",
		"{{.DefaultSystemPromptAmbiguityAndOutputQuality}}",
		"{{.DefaultSystemPromptFinalAnswerAndFormatting}}",
		"{{.DefaultSystemPromptDelegation}}",
	}, "\n---\n"), false, SystemPromptTemplateArgs{
		EstimatedToolCallsForContext: 123,
		EditingToolName:              "patch",
	})
	if err != nil {
		t.Fatalf("RenderCustomSystemPrompt: %v", err)
	}
	for _, want := range []string{
		"autonomous coding agent named Builder",
		"Your agentic environment",
		"Product ambiguity and planning",
		"Final answer instructions",
		selfcmd.RunCommandPrefix(),
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected section prompt to contain %q, got %q", want, rendered)
		}
	}
	if strings.Contains(rendered, "{{") {
		t.Fatalf("expected section placeholders rendered, got %q", rendered)
	}
}

func TestBaseSystemPromptAssemblesDefaultSections(t *testing.T) {
	rendered := BaseSystemPrompt(SystemPromptTemplateArgs{
		EstimatedToolCallsForContext: 123,
		EditingToolName:              "patch",
	})
	for _, want := range []string{
		"autonomous coding agent named Builder",
		"Your agentic environment",
		"Product ambiguity and planning",
		"Final answer instructions",
		"Delegating work",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected base prompt to contain %q, got %q", want, rendered)
		}
	}
	if strings.Contains(rendered, "{{") {
		t.Fatalf("expected base prompt placeholders rendered, got %q", rendered)
	}
}

func TestWorkflowModePromptsOverrideNoopFinal(t *testing.T) {
	for name, prompt := range map[string]string{
		"tool":              WorkflowToolModePrompt,
		"structured_output": WorkflowStructuredOutputModePrompt,
	} {
		if !strings.Contains(prompt, "Do not use `NO_OP` in workflow mode") {
			t.Fatalf("%s workflow prompt missing NO_OP override: %q", name, prompt)
		}
		if !strings.Contains(prompt, "keep polling it with `write_stdin`") {
			t.Fatalf("%s workflow prompt missing polling recovery path: %q", name, prompt)
		}
	}
}

func TestDefaultSystemPromptAssemblyCannotReferenceFullDefaultPrompt(t *testing.T) {
	_, err := renderSystemPromptTemplateErr("{{.DefaultSystemPrompt}}", SystemPromptTemplateArgs{
		EstimatedToolCallsForContext: 123,
		EditingToolName:              "patch",
	}, "")
	if err == nil {
		t.Fatal("expected default system prompt assembly to reject DefaultSystemPrompt recursion")
	}
	if !strings.Contains(err.Error(), "DefaultSystemPrompt") {
		t.Fatalf("expected error to mention DefaultSystemPrompt, got %v", err)
	}
}

func TestCustomSystemPromptRejectsRemovedManualEditInstructionPlaceholder(t *testing.T) {
	_, err := RenderCustomSystemPrompt("{{.ManualEditInstruction}}", false, SystemPromptTemplateArgs{
		EstimatedToolCallsForContext: 123,
		EditingToolName:              "patch",
	})
	if err == nil {
		t.Fatal("expected removed ManualEditInstruction placeholder to fail")
	}
	if !strings.Contains(err.Error(), "ManualEditInstruction") {
		t.Fatalf("expected error to mention ManualEditInstruction, got %v", err)
	}
}

func TestRenderGoalNudgePrompt(t *testing.T) {
	rendered := RenderGoalNudgePrompt("ship /goal mode", "active")
	for _, want := range []string{
		"ship /goal mode",
		"Current goal status: active",
		"builder goal complete",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected goal nudge to contain %q, got %q", want, rendered)
		}
	}
	if strings.Contains(rendered, "{{") {
		t.Fatalf("expected goal nudge placeholders rendered, got %q", rendered)
	}
}

func TestRenderGoalSetPrompt(t *testing.T) {
	rendered := RenderGoalSetPrompt("ship /goal mode")
	if !strings.Contains(rendered, "ship /goal mode") {
		t.Fatalf("expected goal set prompt to contain objective, got %q", rendered)
	}
	if strings.Contains(rendered, "{{") {
		t.Fatalf("expected goal set placeholders rendered, got %q", rendered)
	}
}

func TestRenderGoalResumePrompt(t *testing.T) {
	rendered := RenderGoalResumePrompt("ship /goal mode")
	if !strings.Contains(rendered, "ship /goal mode") {
		t.Fatalf("expected goal resume prompt to contain objective, got %q", rendered)
	}
	if strings.Contains(rendered, "{{") {
		t.Fatalf("expected goal resume placeholders rendered, got %q", rendered)
	}
}

func TestRenderGoalAlreadyCompletePrompt(t *testing.T) {
	rendered := RenderGoalAlreadyCompletePrompt("ship /goal mode")
	want := "No active goal present. Last goal was already completed:\nship /goal mode"
	if rendered != want {
		t.Fatalf("already-complete prompt = %q, want %q", rendered, want)
	}
}
