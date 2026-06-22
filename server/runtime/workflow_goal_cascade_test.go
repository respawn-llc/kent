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

func TestWorkflowToolModeCascadeEmitsGoalCompletionAfterToolResult(t *testing.T) {
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

	entries := eng.ChatSnapshot().Entries
	toolResultIdx, goalCompleteIdx := -1, -1
	for i, entry := range entries {
		if entry.ToolCallID == "call_complete" {
			toolResultIdx = i
		}
		if entry.MessageType == llm.MessageTypeGoal {
			goalCompleteIdx = i
		}
	}
	if toolResultIdx < 0 || goalCompleteIdx < 0 {
		t.Fatalf("missing entries: toolResult=%d goalComplete=%d entries=%+v", toolResultIdx, goalCompleteIdx, entries)
	}
	if goalCompleteIdx < toolResultIdx {
		t.Fatalf("goal-completion message (idx %d) precedes complete_node tool result (idx %d); a non-tool item interleaves the tool call/result pair", goalCompleteIdx, toolResultIdx)
	}
}

func TestWorkflowToolModeCascadeSkipsGoalPausedDuringRace(t *testing.T) {
	store := mustCreateTestSession(t)
	active, err := store.SetGoal("stay paused through completion", session.GoalActorUser)
	if err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	if _, err := store.SetGoalStatus(session.GoalStatusPaused, session.GoalActorUser); err != nil {
		t.Fatalf("pause: %v", err)
	}
	controller := &fakeWorkflowController{}
	client := &fakeClient{responses: []llm.Response{
		commentaryResponse("complete", completeNodeCall("call_complete", json.RawMessage(`{"commentary":"complete","summary":"done"}`))),
	}}
	eng := mustNewWorkflowTestEngine(t, store, client, testWorkflowConfig(controller, config.WorkflowCompletionModeTool), Config{
		EnabledTools: []toolspec.ID{toolspec.ToolAskQuestion},
	})
	if _, err := eng.SubmitWorkflowTurn(context.Background()); err != nil {
		t.Fatalf("SubmitWorkflowTurn: %v", err)
	}
	goal := eng.Goal()
	if goal == nil || goal.ID != active.ID || goal.Status != session.GoalStatusPaused {
		t.Fatalf("paused goal after workflow completion = %+v, want left paused", goal)
	}
}
