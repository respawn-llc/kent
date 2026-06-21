package runtime

import (
	"context"
	"testing"

	"core/server/llm"
	"core/server/session"
	"core/server/tools"
	"core/shared/config"
)

// The goal and workflow drivers reach completion through one seam (CompletionAdapter). The goal
// adapter decides completion from out-of-band state and ignores the model's final answer; the
// workflow adapter decides it from a decoded final answer. Both report through objectiveOutcome.

func TestGoalContinuationAdapterDecidesFromState(t *testing.T) {
	store := mustCreateTestSession(t)
	eng := mustNewTestEngine(t, store, &fakeClient{}, tools.NewRegistry(), Config{})
	adapter := eng.goalContinuation()
	ctx := context.Background()

	outcome, err := adapter.Evaluate(ctx, llm.Message{})
	if err != nil || !outcome.Applicable || !outcome.Done {
		t.Fatalf("no goal: outcome=%+v err=%v, want Applicable+Done", outcome, err)
	}

	if _, err := eng.SetGoal("keep going", session.GoalActorUser); err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	// The final answer is ignored: an active goal keeps driving regardless of model output.
	outcome, err = adapter.Evaluate(ctx, llm.Message{Phase: llm.MessagePhaseFinal, Content: "anything"})
	if err != nil || !outcome.Applicable || outcome.Done || outcome.Complete != nil {
		t.Fatalf("active goal: outcome=%+v err=%v, want Applicable, not Done, no side effect", outcome, err)
	}

	if _, err := eng.SetGoalStatus(session.GoalStatusComplete, session.GoalActorUser); err != nil {
		t.Fatalf("SetGoalStatus: %v", err)
	}
	outcome, err = adapter.Evaluate(ctx, llm.Message{})
	if err != nil || !outcome.Done {
		t.Fatalf("completed goal: outcome=%+v err=%v, want Done", outcome, err)
	}
}

func TestWorkflowCompletionAdapterDecidesFromOutput(t *testing.T) {
	ctx := context.Background()
	newAdapter := func(mode config.WorkflowCompletionMode) workflowCompletionAdapter {
		store := mustCreateTestSession(t)
		eng := mustNewWorkflowTestEngine(t, store, &fakeClient{}, testWorkflowConfig(&fakeWorkflowController{}, mode), Config{})
		return (&defaultStepExecutor{engine: eng}).workflowCompletionAdapter()
	}

	valid, err := newAdapter(config.WorkflowCompletionModeUnstructured).Evaluate(ctx, llm.Message{Phase: llm.MessagePhaseFinal, Content: `{"commentary":"complete","summary":"done"}`})
	if err != nil || !valid.Applicable || !valid.Done || valid.Complete == nil {
		t.Fatalf("valid final: outcome=%+v err=%v, want Applicable+Done with a completion side effect", valid, err)
	}

	invalid, err := newAdapter(config.WorkflowCompletionModeUnstructured).Evaluate(ctx, llm.Message{Phase: llm.MessagePhaseFinal, Content: `{"summary":""}`})
	if err != nil || !invalid.Applicable || invalid.Done || invalid.Continue == nil {
		t.Fatalf("invalid final: outcome=%+v err=%v, want Applicable, not Done, Continue carries the decode error", invalid, err)
	}

	nonFinal, err := newAdapter(config.WorkflowCompletionModeUnstructured).Evaluate(ctx, llm.Message{Content: "thinking"})
	if err != nil || nonFinal.Applicable {
		t.Fatalf("non-final: outcome=%+v err=%v, want not Applicable", nonFinal, err)
	}

	// Tool/shell completion modes never complete from a normal final answer; the adapter declines so
	// the driver applies its mode-specific protocol nudge.
	toolMode, err := newAdapter(config.WorkflowCompletionModeTool).Evaluate(ctx, llm.Message{Phase: llm.MessagePhaseFinal, Content: `{"commentary":"complete","summary":"done"}`})
	if err != nil || toolMode.Applicable {
		t.Fatalf("tool mode final: outcome=%+v err=%v, want not Applicable", toolMode, err)
	}
}
