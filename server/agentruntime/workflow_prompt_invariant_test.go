package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"core/server/llm"
	"core/server/sessionstore"
	"core/shared/config"
	"core/shared/invariant"
	"core/shared/sessioncontract"
	"core/shared/textutil"
	"core/shared/toolspec"
)

func TestWorkflowAgentMissingInstructionsFailsAccordingToDebugMode(t *testing.T) {
	for _, debug := range []bool{false, true} {
		name := "production"
		if debug {
			name = "debug"
		}
		t.Run(name, func(t *testing.T) {
			testWorkflowAgentMissingInstructions(t, debug)
		})
	}
}

func testWorkflowAgentMissingInstructions(t *testing.T, debug bool) {
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
	engine := mustNewWorkflowTestEngine(
		t,
		mustCreateTestSession(t),
		client,
		testWorkflowConfig(nil, config.WorkflowCompletionModeShellCommand),
		Config{
			Model: "gpt-6-sol",
			Debug: debug,
			EnabledTools: []toolspec.ID{
				toolspec.ToolExecCommand,
			},
		},
	)

	var recovered any
	var runErr error
	func() {
		defer func() { recovered = recover() }()
		runErr = engine.stepLifecycle.Run(
			context.Background(),
			exclusiveStepOptions{ActiveKind: ActiveKindWorkflowTurn},
			func(stepCtx context.Context, stepID string) error {
				_, err := engine.stepFlow.RunStepLoopWithOptions(stepCtx, stepID, stepLoopOptions{})
				return err
			},
		)
	}()
	if len(client.calls) != 0 {
		t.Fatal("workflow without instructions dispatched a model request")
	}
	if debug {
		diagnostic, ok := recovered.(invariant.Diagnostic)
		if !ok || diagnostic.Scope != invariant.ScopeWorkflowExecution {
			t.Fatalf("debug invariant panic = %#v, want Workflow diagnostic", recovered)
		}
		return
	}
	if recovered != nil {
		t.Fatalf("production workflow invariant panicked: %v", recovered)
	}
	if !errors.Is(runErr, errWorkflowInstructionsMissing) {
		t.Fatalf("production workflow error = %v, want missing instructions", runErr)
	}
}

func TestCompletedWorkflowContractDoesNotClassifyOrdinaryTurnAsWorkflow(t *testing.T) {
	store := mustCreateTestSession(t)
	if err := store.SetWorkflowSnapshot(session.WorkflowSnapshot{CompletionMode: sessioncontract.WorkflowCompletionModeTool}); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkModelDispatchLocked(session.LockedContract{
		Model: "gpt-6-sol",
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
