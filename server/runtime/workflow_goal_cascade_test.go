package runtime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"core/server/llm"
	"core/server/session"
	"core/shared/config"
	"core/shared/toolspec"
)

// Tool-mode (complete_node) is also a valid terminal completion and must cascade-complete an
// active goal — the cascade lives in setWorkflowTerminalState so every completion source is
// covered, not just structured/unstructured output.
func TestWorkflowToolModeTerminalCompletionCascadeCompletesActiveGoal(t *testing.T) {
	store := mustCreateTestSession(t)
	controller := &fakeWorkflowController{}
	client := &fakeClient{responses: []llm.Response{
		commentaryResponse("complete", completeNodeCall("call_complete", json.RawMessage(`{"commentary":"complete","summary":"done"}`))),
	}}
	eng := mustNewWorkflowTestEngine(t, store, client, testWorkflowConfig(controller, config.WorkflowCompletionModeTool), Config{
		EnabledTools: []toolspec.ID{toolspec.ToolAskQuestion},
	})
	if _, err := eng.SetGoal("finish via tool completion", session.GoalActorUser); err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	if _, err := eng.SubmitWorkflowTurn(context.Background()); err != nil {
		t.Fatalf("SubmitWorkflowTurn: %v", err)
	}
	if got := controller.completed.Load(); got != 1 {
		t.Fatalf("completions = %d, want 1", got)
	}
	if terminal := eng.WorkflowTerminalState(); !terminal.Completed || terminal.Source != WorkflowCompletionSourceTool {
		t.Fatalf("terminal state = %+v, want tool completion", terminal)
	}
	goal := eng.Goal()
	if goal == nil || goal.Status != session.GoalStatusComplete {
		t.Fatalf("goal after tool-mode completion = %+v, want auto-completed", goal)
	}
}

// A valid workflow completion submitted while a self-set goal is still active must
// complete the workflow AND auto-complete the active goal in the same step (soft cascade,
// actor=system). The goal is the foreground objective, but a real task completion overrides
// ordering rather than being blocked by the goal.
func TestWorkflowTerminalCompletionCascadeCompletesActiveGoal(t *testing.T) {
	store := mustCreateTestSession(t)
	controller := &fakeWorkflowController{}
	client := &fakeClient{responses: []llm.Response{
		structuredFinalResponse(`{"commentary":"complete","summary":"done"}`),
	}}
	eng := mustNewWorkflowTestEngine(t, store, client, testWorkflowConfig(controller, config.WorkflowCompletionModeUnstructured), Config{
		EnabledTools: []toolspec.ID{toolspec.ToolAskQuestion},
	})
	if _, err := eng.SetGoal("finish the steering rework", session.GoalActorUser); err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	if _, err := eng.SubmitWorkflowTurn(context.Background()); err != nil {
		t.Fatalf("SubmitWorkflowTurn: %v", err)
	}
	if terminal := eng.WorkflowTerminalState(); !terminal.Completed {
		t.Fatalf("terminal state = %+v, want completed", terminal)
	}
	goal := eng.Goal()
	if goal == nil || goal.Status != session.GoalStatusComplete {
		t.Fatalf("goal after workflow completion = %+v, want auto-completed", goal)
	}
}

// Only ACTIVE goals cascade-complete. A paused goal must survive a workflow completion
// untouched (paused goals are out of the foreground continuation).
func TestWorkflowTerminalCompletionLeavesPausedGoalIntact(t *testing.T) {
	store := mustCreateTestSession(t)
	controller := &fakeWorkflowController{}
	client := &fakeClient{responses: []llm.Response{
		structuredFinalResponse(`{"commentary":"complete","summary":"done"}`),
	}}
	eng := mustNewWorkflowTestEngine(t, store, client, testWorkflowConfig(controller, config.WorkflowCompletionModeUnstructured), Config{})
	if _, err := eng.SetGoal("paused objective", session.GoalActorUser); err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	if _, err := eng.SetGoalStatus(session.GoalStatusPaused, session.GoalActorUser); err != nil {
		t.Fatalf("SetGoalStatus paused: %v", err)
	}
	if _, err := eng.SubmitWorkflowTurn(context.Background()); err != nil {
		t.Fatalf("SubmitWorkflowTurn: %v", err)
	}
	goal := eng.Goal()
	if goal == nil || goal.Status != session.GoalStatusPaused {
		t.Fatalf("paused goal after workflow completion = %+v, want still paused", goal)
	}
}

// When a workflow completion is invalid and a goal is active, the continuation nudge must
// carry the goal reminder so the model keeps working toward the objective. We assert that
// the goal's objective text (data the user set) flows into the developer feedback, not the
// reminder's wording.
func TestWorkflowInvalidCompletionNudgeIncludesActiveGoalReminder(t *testing.T) {
	store := mustCreateTestSession(t)
	controller := &fakeWorkflowController{}
	client := &fakeClient{responses: []llm.Response{
		structuredFinalResponse(`{"summary":""}`),
		structuredFinalResponse(`{"commentary":"complete","summary":"done"}`),
	}}
	eng := mustNewWorkflowTestEngine(t, store, client, testWorkflowConfig(controller, config.WorkflowCompletionModeUnstructured), Config{
		EnabledTools: []toolspec.ID{toolspec.ToolAskQuestion},
	})
	if _, err := eng.SetGoal("ship the steering rework end to end", session.GoalActorUser); err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	if _, err := eng.SubmitWorkflowTurn(context.Background()); err != nil {
		t.Fatalf("SubmitWorkflowTurn: %v", err)
	}
	assertDeveloperErrorFeedbackAfterAssistantFinalContains(t, eng, `{"summary":""}`, []string{"ship the steering rework end to end"}, nil)
}

// Regression guard for the R1 deadlock: the cascade-complete must run AFTER
// setWorkflowTerminalState releases e.mu, so a concurrent user-side goal mutation racing the
// terminal completion never deadlocks. The test fails (times out) if the cascade is ever
// moved back under e.mu.
func TestWorkflowTerminalCascadeRacesUserGoalMutationWithoutDeadlock(t *testing.T) {
	store := mustCreateTestSession(t)
	controller := &fakeWorkflowController{}
	started := make(chan struct{})
	release := make(chan struct{})
	client := &hookClient{
		response: structuredFinalResponse(`{"commentary":"complete","summary":"done"}`),
		beforeReturn: func() error {
			close(started)
			<-release
			return nil
		},
	}
	eng := mustNewWorkflowTestEngine(t, store, client, testWorkflowConfig(controller, config.WorkflowCompletionModeUnstructured), Config{
		EnabledTools: []toolspec.ID{toolspec.ToolAskQuestion},
	})
	if _, err := eng.SetGoal("race objective", session.GoalActorUser); err != nil {
		t.Fatalf("SetGoal: %v", err)
	}

	submitDone := make(chan error, 1)
	go func() {
		_, err := eng.SubmitWorkflowTurn(context.Background())
		submitDone <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for workflow turn to start")
	}

	mutateDone := make(chan struct{})
	go func() {
		// Race a user-side goal mutation against the terminal cascade.
		_, _ = eng.SetGoalStatus(session.GoalStatusPaused, session.GoalActorUser)
		close(mutateDone)
	}()
	close(release)

	select {
	case err := <-submitDone:
		if err != nil {
			t.Fatalf("SubmitWorkflowTurn: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock: workflow turn did not finish racing the goal mutation")
	}
	select {
	case <-mutateDone:
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock: user goal mutation did not finish")
	}
}
