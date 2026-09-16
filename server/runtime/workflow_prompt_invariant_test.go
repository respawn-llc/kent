package runtime

import (
	"context"
	"encoding/json"
	"testing"

	"core/server/llm"
	"core/server/session"
	"core/server/tools"
	"core/server/workflowruntime"
	"core/shared/sessioncontract"
	"core/shared/textutil"
	"core/shared/toolspec"
)

func TestWorkflowAgentPanicsBeforeSecondModelTurnWithoutWorkflowInstructions(t *testing.T) {
	client := &fakeClient{responses: []llm.Response{{
		Assistant: llm.Message{
			Role:    llm.RoleAssistant,
			Phase:   textutil.Value(llm.MessagePhaseCommentary),
			Content: textutil.Value("working"),
		},
		ToolCalls: []llm.ToolCall{{
			ID:    "continue",
			Name:  string(toolspec.ToolExecCommand),
			Input: json.RawMessage(`{"cmd":"true"}`),
		}},
		Usage: llm.Usage{WindowTokens: 200_000},
	}}}
	engine := mustNewTestEngine(
		t,
		mustCreateTestSession(t),
		client,
		newTestToolRegistry(t, tools.HandlerRegistration{
			ID:      toolspec.ToolExecCommand,
			Handler: fakeTool{name: toolspec.ToolExecCommand},
		}),
		Config{
			Model: "gpt-5",
			EnabledTools: []toolspec.ID{
				toolspec.ToolExecCommand,
			},
			WorkflowPrompt: &workflowruntime.PromptContract{
				Identity:       "missing-workflow-instructions",
				CompletionMode: workflowruntime.CompletionModeShellCommand,
			},
		},
	)

	defer func() {
		recovered := recover()
		if recovered != "workflow mode skipped prompt" {
			t.Fatalf("panic = %v, want workflow mode skipped prompt", recovered)
		}
	}()

	_ = engine.stepLifecycle.Run(
		context.Background(),
		exclusiveStepOptions{ActiveKind: ActiveKindWorkflowTurn},
		func(stepCtx context.Context, stepID string) error {
			_, err := engine.stepFlow.RunStepLoopWithOptions(stepCtx, stepID, stepLoopOptions{})
			return err
		},
	)
}

func TestCompletedWorkflowContractDoesNotClassifyOrdinaryTurnAsWorkflow(t *testing.T) {
	store := mustCreateTestSession(t)
	mode := sessioncontract.WorkflowCompletionModeTool
	if err := store.MarkModelDispatchLocked(session.LockedContract{
		Model:                  "gpt-5",
		WorkflowCompletionMode: &mode,
	}); err != nil {
		t.Fatalf("mark completed Workflow contract: %v", err)
	}
	client := &fakeClient{responses: []llm.Response{
		{
			Assistant: llm.Message{
				Role:    llm.RoleAssistant,
				Phase:   textutil.Value(llm.MessagePhaseCommentary),
				Content: textutil.Value("working"),
			},
			ToolCalls: []llm.ToolCall{{
				ID:    "inspect",
				Name:  string(toolspec.ToolExecCommand),
				Input: json.RawMessage(`{"cmd":"true"}`),
			}},
			Usage: llm.Usage{WindowTokens: 200_000},
		},
		{
			Assistant: llm.Message{
				Role:    llm.RoleAssistant,
				Phase:   textutil.Value(llm.MessagePhaseFinal),
				Content: textutil.Value("ordinary answer"),
			},
			Usage: llm.Usage{WindowTokens: 200_000},
		},
	}}
	engine := mustNewExecTestEngine(t, store, client, Config{})
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("ordinary turn panicked with completed Workflow contract: %v", recovered)
		}
	}()

	if _, err := engine.SubmitUserMessage(context.Background(), "continue ordinary conversation"); err != nil {
		t.Fatalf("ordinary continuation: %v", err)
	}
}
