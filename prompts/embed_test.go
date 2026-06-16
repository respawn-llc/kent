package prompts

import (
	"errors"
	"strings"
	"testing"

	"core/cli/selfcmd"
)

func TestRenderSystemPromptTemplateUsesTypedFields(t *testing.T) {
	rendered := renderSystemPromptTemplate("calls={{.EstimatedToolCallsForContext}} cmd={{.LaunchCommand}} run edit={{.EditingToolName}}", SystemPromptTemplateArgs{
		EstimatedToolCallsForContext: 123,
		EditingToolName:              "edit",
	}, "")
	if !strings.Contains(rendered, "calls=123") {
		t.Fatalf("expected estimated tool calls rendered, got %q", rendered)
	}
	expectedCmd := "cmd=" + selfcmd.LaunchCommand() + " run"
	if !strings.Contains(rendered, expectedCmd) || strings.Contains(rendered, "{{") {
		t.Fatalf("expected %q in rendered output, got %q", expectedCmd, rendered)
	}
	if !strings.Contains(rendered, "edit=edit") {
		t.Fatalf("expected editing tool name rendered, got %q", rendered)
	}
}

func TestSystemPromptRendersDeprecatedBuilderCommandAlias(t *testing.T) {
	// Custom prompts migrated from Builder may still use the deprecated
	// {{.BuilderCommand}} placeholder; it must render identically to
	// {{.LaunchCommand}} in both the default-prompt and explicit-default paths
	// so those sessions keep starting through the rebrand window.
	for _, defaultPrompt := range []string{"", "base default prompt"} {
		alias := renderSystemPromptTemplate("cmd={{.BuilderCommand}}", SystemPromptTemplateArgs{}, defaultPrompt)
		launch := renderSystemPromptTemplate("cmd={{.LaunchCommand}}", SystemPromptTemplateArgs{}, defaultPrompt)
		if alias != launch {
			t.Fatalf("BuilderCommand alias = %q, want identical to LaunchCommand %q (defaultPrompt=%q)", alias, launch, defaultPrompt)
		}
		if !strings.Contains(alias, selfcmd.LaunchCommand()) {
			t.Fatalf("expected alias render to contain launch command, got %q", alias)
		}
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
	if !strings.Contains(rendered, selfcmd.LaunchCommand()) {
		t.Fatalf("expected section prompts to substitute the launch command, got %q", rendered)
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
	if strings.TrimSpace(rendered) == "" {
		t.Fatal("expected base prompt to assemble a non-empty prompt")
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
	var placeholderErr *UnknownTemplatePlaceholderError
	if !errors.As(err, &placeholderErr) {
		t.Fatalf("expected UnknownTemplatePlaceholderError, got %v", err)
	}
	if placeholderErr.Placeholder != "DefaultSystemPrompt" {
		t.Fatalf("expected placeholder DefaultSystemPrompt, got %q", placeholderErr.Placeholder)
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
	var placeholderErr *UnknownTemplatePlaceholderError
	if !errors.As(err, &placeholderErr) {
		t.Fatalf("expected UnknownTemplatePlaceholderError, got %v", err)
	}
	if placeholderErr.Placeholder != "ManualEditInstruction" {
		t.Fatalf("expected placeholder ManualEditInstruction, got %q", placeholderErr.Placeholder)
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
	// Substituted variables: short id (in the launch command), the transition
	// id/display pair, and the node prompt body must all be injected.
	for _, want := range []string{
		selfcmd.LaunchCommand() + " task show BUI-1",
		"actionable (Actionable)",
		"Triage the ticket.",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected workflow instructions to substitute %q, got %q", want, rendered)
		}
	}
	// The tool-completion fragment passed in must be embedded into the output.
	if !strings.Contains(rendered, toolInstructions) {
		t.Fatalf("expected workflow instructions to embed the completion fragment, got %q", rendered)
	}
	if strings.Contains(rendered, "{{") {
		t.Fatalf("expected workflow instruction placeholders rendered, got %q", rendered)
	}
}

func TestRenderWorkflowTaskInstructionsOmitsCommentReminderWhenNoCommentsExist(t *testing.T) {
	rendered, err := RenderWorkflowTaskInstructions(workflowInstructionsTestArgs(0), "complete the workflow")
	if err != nil {
		t.Fatalf("RenderWorkflowTaskInstructions: %v", err)
	}
	if strings.Contains(rendered, selfcmd.LaunchCommand()+" task comment list BUI-1") {
		t.Fatalf("expected no task comment list reminder for zero comments, got %q", rendered)
	}
}

func TestRenderWorkflowTaskInstructionsIncludesSingularCommentReminder(t *testing.T) {
	rendered, err := RenderWorkflowTaskInstructions(workflowInstructionsTestArgs(1), "complete the workflow")
	if err != nil {
		t.Fatalf("RenderWorkflowTaskInstructions: %v", err)
	}
	for _, want := range []string{
		"1 comment",
		selfcmd.LaunchCommand() + " task comment list BUI-1",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected workflow instructions to include %q, got %q", want, rendered)
		}
	}
	if strings.Contains(rendered, "1 comments") {
		t.Fatalf("expected singular comment grammar, got %q", rendered)
	}
}

func TestRenderWorkflowTaskInstructionsIncludesPluralCommentReminder(t *testing.T) {
	rendered, err := RenderWorkflowTaskInstructions(workflowInstructionsTestArgs(3), "complete the workflow")
	if err != nil {
		t.Fatalf("RenderWorkflowTaskInstructions: %v", err)
	}
	for _, want := range []string{
		"3 comments",
		selfcmd.LaunchCommand() + " task comment list BUI-1",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected workflow instructions to include %q, got %q", want, rendered)
		}
	}
}

func workflowInstructionsTestArgs(taskNumberOfComments int64) WorkflowNodeContextArgs {
	return WorkflowNodeContextArgs{
		TaskId:               "task-1",
		TaskShortId:          "BUI-1",
		TaskTitle:            "Smoke test",
		TaskBody:             "Ask three questions.",
		WorkflowId:           "workflow-1",
		WorkflowShortId:      "workflow-1",
		NodeId:               "node-1",
		NodeKey:              "triaging",
		NodeDisplayName:      "Triaging",
		ContextMode:          "new_session",
		TaskNumberOfComments: taskNumberOfComments,
		Transitions: []WorkflowTransition{
			{ID: "actionable", DisplayName: "Actionable"},
			{ID: "not_actionable", DisplayName: "Not Actionable"},
		},
		NodePrompt: "Triage the ticket.",
	}
}

func TestRenderGoalNudgePrompt(t *testing.T) {
	rendered := RenderGoalNudgePrompt("ship /goal mode", "active")
	// The objective and the launch command must both be substituted in.
	for _, want := range []string{
		"ship /goal mode",
		LaunchCommand(),
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected goal nudge to substitute %q, got %q", want, rendered)
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
	if !strings.Contains(rendered, "ship /goal mode") {
		t.Fatalf("expected already-complete prompt to substitute the objective, got %q", rendered)
	}
	if strings.Contains(rendered, "{{") {
		t.Fatalf("expected already-complete placeholders rendered, got %q", rendered)
	}
}

func TestRenderGoalAgentDuplicateSetDeniedPrompt(t *testing.T) {
	rendered := RenderGoalAgentDuplicateSetDeniedPrompt("ship /goal mode\n\n- preserve markdown", "active")
	// The multi-line objective (markdown preserved) and the status argument must
	// both be substituted into the rendered prompt.
	for _, want := range []string{
		"ship /goal mode\n\n- preserve markdown",
		"active",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("duplicate set prompt missing substituted %q: %q", want, rendered)
		}
	}
	if strings.Contains(rendered, "{{") {
		t.Fatalf("expected duplicate set placeholders rendered, got %q", rendered)
	}
}

func TestRenderGoalCompleteConfirmRequiredPrompt(t *testing.T) {
	rendered := RenderGoalCompleteConfirmRequiredPrompt("ship /goal mode\n\n- preserve markdown")
	if !strings.Contains(rendered, "ship /goal mode\n\n- preserve markdown") {
		t.Fatalf("complete confirm prompt missing substituted objective: %q", rendered)
	}
	if strings.Contains(rendered, "{{") {
		t.Fatalf("expected complete confirm placeholders rendered, got %q", rendered)
	}
}
