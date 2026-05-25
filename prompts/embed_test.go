package prompts

import (
	"strings"
	"testing"

	"builder/cli/selfcmd"
)

func TestRenderSystemPromptTemplateUsesTypedFields(t *testing.T) {
	rendered := renderSystemPromptTemplate("calls={{.EstimatedToolCallsForContext}} cmd={{.BuilderCommand}} run edit={{.EditingToolName}}", SystemPromptTemplateArgs{
		EstimatedToolCallsForContext: 123,
		EditingToolName:              "edit",
	}, "")
	if !strings.Contains(rendered, "calls=123") {
		t.Fatalf("expected estimated tool calls rendered, got %q", rendered)
	}
	expectedCmd := "cmd=" + selfcmd.BuilderCommand() + " run"
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
		selfcmd.BuilderCommand(),
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

func TestRenderWorkflowTaskInstructionsUsesCompletionModeFragment(t *testing.T) {
	toolInstructions, err := RenderWorkflowToolCompletionInstructions("workflow-1")
	if err != nil {
		t.Fatalf("RenderWorkflowToolCompletionInstructions: %v", err)
	}
	rendered, err := RenderWorkflowTaskInstructions(WorkflowNodeContextArgs{
		TaskId:          "task-1",
		TaskShortId:     "BUI-1",
		TaskTitle:       "Smoke test",
		TaskBody:        "Ask three questions.",
		WorkflowId:      "workflow-1",
		WorkflowShortId: "workflow-1",
		NodeId:          "node-1",
		NodeKey:         "triaging",
		NodeDisplayName: "Triaging",
		ContextMode:     "new_session",
		Transitions: []WorkflowTransition{
			{ID: "actionable", DisplayName: "Actionable"},
			{ID: "not_actionable", DisplayName: "Not Actionable"},
		},
		NodePrompt: "Triage the ticket.",
	}, toolInstructions)
	if err != nil {
		t.Fatalf("RenderWorkflowTaskInstructions: %v", err)
	}
	for _, want := range []string{
		"ticket `BUI-1`",
		"workflow `workflow-1`",
		selfcmd.BuilderCommand() + " task show BUI-1",
		"complete_node",
		"actionable (Actionable)",
		"Triage the ticket.",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected workflow instructions to contain %q, got %q", want, rendered)
		}
	}
	for _, unexpected := range []string{"Required node output fields", "Output fields:"} {
		if strings.Contains(rendered, unexpected) {
			t.Fatalf("workflow instructions should not contain %q, got %q", unexpected, rendered)
		}
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
