package workflowrunner

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"core/internal/testharness/requestfixture"
	"core/internal/testharness/testsetup"
	"core/server/agentruntime"
	"core/server/runtimecontrol"
	"core/server/sessionstore"
	"core/server/workflow"
	"core/server/workflowexecution"
	"core/server/workflowruntime"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeids"

	"google.golang.org/protobuf/proto"
)

func TestRetainedWorkflowGoalResumeRejectsBeforeMutation(t *testing.T) {
	for _, status := range []session.GoalStatus{
		session.GoalStatusPaused,
		session.GoalStatusComplete,
	} {
		t.Run(string(status), func(t *testing.T) {
			testRetainedWorkflowGoalResumeRejectsBeforeMutation(t, status)
		})
	}
}

func testRetainedWorkflowGoalResumeRejectsBeforeMutation(t *testing.T, status session.GoalStatus) {
	t.Helper()
	synctest.Test(t, func(t *testing.T) {
		f, service, sessionID := newRetainedWorkflowGoalFixture(t, status)
		selector := requestfixture.Selector(t, sessionID.String())
		before, err := service.ShowGoal(t.Context(), &runtimepb.GoalShowRequest{SessionId: selector})
		if err != nil {
			t.Fatal(err)
		}
		var beforeRevision int64
		if err := f.authority.WithCurrentRuntime(t.Context(), sessionID, func(_ context.Context, engine *runtime.Engine) error {
			var err error
			beforeRevision, err = engine.TranscriptRevision()
			return err
		}); err != nil {
			t.Fatal(err)
		}
		beforeRequests := len(f.client.Requests())
		result, resumeErr := service.ResumeGoal(t.Context(), &runtimepb.GoalMutationRequest{
			SessionId: selector, Actor: "user",
		})
		synctest.Wait()
		if !errors.Is(resumeErr, runtime.ErrGoalRequiresWorkflowExecution) {
			t.Errorf("ResumeGoal = %+v, %v; want retained Workflow admission error", result, resumeErr)
		}
		after, err := service.ShowGoal(t.Context(), &runtimepb.GoalShowRequest{SessionId: selector})
		if err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(before, after) {
			t.Errorf("rejected ResumeGoal changed the durable Goal: before=%v, after=%v", before, after)
		}
		if err := f.authority.WithCurrentRuntime(t.Context(), sessionID, func(_ context.Context, engine *runtime.Engine) error {
			revision, err := engine.TranscriptRevision()
			if err != nil {
				return err
			}
			if revision != beforeRevision {
				t.Errorf("rejected ResumeGoal persisted transcript changes: revision %d -> %d", beforeRevision, revision)
			}
			if engine.GoalLoopRunning() {
				t.Error("rejected ResumeGoal started an ordinary Goal loop")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if _, active := f.authority.SessionExecution(sessionID); active {
			t.Error("rejected ResumeGoal started a Session execution")
		}
		if requests := len(f.client.Requests()); requests != beforeRequests {
			t.Errorf("rejected ResumeGoal issued model requests: %d -> %d", beforeRequests, requests)
		}
	})
}

func TestRetainedWorkflowGoalControlDoesNotStartExecution(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f, service, sessionID := newRetainedWorkflowGoalFixture(t, session.GoalStatusActive)
		selector := requestfixture.Selector(t, sessionID.String())
		request := &runtimepb.GoalMutationRequest{SessionId: selector, Actor: "user"}
		show := func() *runtimepb.GoalShowSuccess {
			t.Helper()
			result, err := service.ShowGoal(t.Context(), &runtimepb.GoalShowRequest{SessionId: selector})
			if err != nil {
				t.Fatal(err)
			}
			return result
		}
		revision := func() int64 {
			t.Helper()
			var revision int64
			err := f.authority.WithCurrentRuntime(t.Context(), sessionID, func(_ context.Context, engine *runtime.Engine) error {
				if engine.GoalLoopRunning() {
					t.Error("retained Goal control started a Goal loop")
				}
				var err error
				revision, err = engine.TranscriptRevision()
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			return revision
		}
		before, beforeRevision := show(), revision()
		beforeRequests := len(f.client.Requests())
		resumed, err := service.ResumeGoal(t.Context(), request)
		if err != nil || !proto.Equal(resumed.GetGoal(), before.Goal) {
			t.Fatalf("active Resume no-op = %v, %v", resumed, err)
		}
		synctest.Wait()
		if !proto.Equal(before, show()) || revision() != beforeRevision {
			t.Fatal("active Resume no-op changed the Goal or transcript")
		}
		set := &runtimepb.GoalSetRequest{
			Target: &chatpb.ChatTarget{Target: &chatpb.ChatTarget_Session{
				Session: &chatpb.ExistingSessionRequestTarget{SessionId: selector},
			}},
			Objective:       "Replace the retained objective.",
			Actor:           "user",
			ExecutionPolicy: runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_START_OR_CONTINUE,
		}
		if _, err := service.SetResolvedGoal(t.Context(), set); !errors.Is(err, runtime.ErrGoalRequiresWorkflowExecution) {
			t.Fatalf("start-or-continue Set error = %v, want retained Workflow admission error", err)
		}
		synctest.Wait()
		if !proto.Equal(before, show()) || revision() != beforeRevision {
			t.Fatal("rejected Set changed the Goal or transcript")
		}
		set.ExecutionPolicy = runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_PRESERVE_RUNTIME_STATE
		commit, err := service.SetResolvedGoal(t.Context(), set)
		if err != nil || commit.Diagnostic != nil {
			t.Fatalf("passive Set = %v, %v", commit.Diagnostic, err)
		}
		if goal := show().Goal; goal == nil || goal.Objective != set.Objective || goal.Id == before.Goal.Id {
			t.Fatalf("passive replacement Goal = %v", goal)
		}
		paused, err := service.PauseGoal(t.Context(), request)
		if err != nil || paused.GetGoal().GetStatus() != runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_PAUSED {
			t.Fatalf("retained Pause = %v, %v", paused, err)
		}
		completed, err := service.CompleteGoal(t.Context(), request)
		if err != nil || completed.GetGoal().GetStatus() != runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_COMPLETE {
			t.Fatalf("retained Complete = %v, %v", completed, err)
		}
		if _, err := service.ClearGoal(t.Context(), &runtimepb.GoalClearRequest{SessionId: selector, Actor: "user"}); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if show().Goal != nil || revision() != beforeRevision+4 {
			t.Fatal("retained control did not persist Clear and exactly four effective mutation notices")
		}
		if _, active := f.authority.SessionExecution(sessionID); active {
			t.Error("retained Goal control started a Session execution")
		}
		if requests := len(f.client.Requests()); requests != beforeRequests {
			t.Errorf("retained Goal control issued model requests: %d -> %d", beforeRequests, requests)
		}
	})
}

func TestWorkflowGoalResumeAfterTerminalCompletionInOpenStep(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newLiveWorkflowGoalFixture(t, session.GoalStatusPaused)
		execution, ok := f.runner.authority.SessionExecution(f.sessionID)
		if !ok {
			t.Fatal("Workflow execution is missing")
		}
		var stepID runtimeids.StepID
		err := f.runner.authority.WithCurrentRuntime(t.Context(), f.sessionID, func(_ context.Context, engine *runtime.Engine) error {
			active := engine.ActiveRun()
			if active == nil {
				t.Fatal("Workflow Step is missing")
			}
			var err error
			stepID, err = runtimeids.ParseStepID(active.StepID)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		completion, err := f.runner.controller.CompleteAgentCurrentNode(t.Context(), workflowruntime.AgentCompletionRequest{
			Provenance: workflowruntime.AgentCompletionProvenance{ScopeID: execution.Scope().ID(), StepID: stepID},
			SessionID:  f.sessionID, TransitionID: "done", Commentary: "finished",
		})
		if err != nil || !completion.IsApplied() {
			t.Fatalf("terminal completion = %v, %v", completion, err)
		}
		resumed, err := f.service.ResumeGoal(t.Context(), &runtimepb.GoalMutationRequest{
			SessionId: requestfixture.Selector(t, f.sessionID.String()), Actor: "user",
		})
		if err != nil || resumed.GetGoal().GetStatus() != runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE {
			t.Fatalf("Resume in terminal Workflow's still-open Step = %v, %v", resumed, err)
		}
		err = f.runner.authority.WithCurrentRuntime(t.Context(), f.sessionID, func(_ context.Context, engine *runtime.Engine) error {
			active := engine.ActiveRun()
			if active == nil || active.StepID != stepID.String() || !engine.WorkflowTerminalState().Completed {
				t.Error("Goal mutation did not remain in the completed Workflow's open Step")
			}
			if engine.GoalLoopRunning() {
				t.Error("Goal mutation started ordinary continuation after terminal completion")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

type workflowGoalFixture struct {
	runner    *currentNodeRunnerFixture
	service   *runtimecontrol.Service
	sessionID runtimeids.SessionID
	taskID    workflow.TaskID
}

func newRetainedWorkflowGoalFixture(t *testing.T, status session.GoalStatus) (*currentNodeRunnerFixture, *runtimecontrol.Service, runtimeids.SessionID) {
	t.Helper()
	f := newLiveWorkflowGoalFixture(t, status)
	if err := f.runner.controller.Interrupt(t.Context(), workflowexecution.InterruptSelector{TaskID: f.taskID}); err != nil {
		t.Fatal(err)
	}
	f.runner.waitForTaskQuiescence(t, f.taskID)
	f.runner.waitForSessionExecutionQuiescence(t, f.sessionID)
	f.runner.openRetainedRuntime(t, f.sessionID)
	synctest.Wait()
	return f.runner, f.service, f.sessionID
}

func newLiveWorkflowGoalFixture(t *testing.T, status session.GoalStatus) workflowGoalFixture {
	t.Helper()
	response := testsetup.NewStartBarrier()
	step := ScriptedFinalAnswer(`{"transition":"done","commentary":"finished"}`)
	step.BeforeResponse = response.ArriveAndWait
	f := newCurrentNodeRunnerFixture(t, step)
	t.Cleanup(response.Unblock)
	task := f.createTask(t, createCurrentNodeAgentWorkflow(t, f.store))
	currentNode := f.startTask(t, task)
	waitForInteractiveResumeResponse(t, response)
	association, err := f.store.LatestTaskSessionForNode(t.Context(), currentNode)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := association.SessionID
	selector := requestfixture.Selector(t, sessionID.String())
	service := runtimecontrol.NewService(f.authority, f.metadata).
		WithPersistedSessionResolver(f.metadata)
	commit, err := service.SetResolvedGoal(t.Context(), &runtimepb.GoalSetRequest{
		Target: &chatpb.ChatTarget{Target: &chatpb.ChatTarget_Session{
			Session: &chatpb.ExistingSessionRequestTarget{SessionId: selector},
		}},
		Objective:       "Finish the retained Workflow objective.",
		Actor:           "user",
		ExecutionPolicy: runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_START_OR_CONTINUE,
	})
	if err != nil || commit.Diagnostic != nil {
		t.Fatalf("set passive Goal during exact Workflow execution: %v, diagnostic=%v", err, commit.Diagnostic)
	}
	request := &runtimepb.GoalMutationRequest{SessionId: selector, Actor: "user"}
	// A repeated active Resume must not rewrite even the Goal's timestamp.
	time.Sleep(time.Second)
	resumed, err := service.ResumeGoal(t.Context(), request)
	if err != nil || !proto.Equal(resumed.GetGoal(), commit.Mutation.GetGoal()) {
		t.Fatalf("live Workflow active Resume no-op = %v, %v", resumed, err)
	}
	switch status {
	case session.GoalStatusActive:
	case session.GoalStatusPaused:
		_, err = service.PauseGoal(t.Context(), request)
	case session.GoalStatusComplete:
		_, err = service.CompleteGoal(t.Context(), request)
	default:
		t.Fatalf("unsupported initial Goal status %v", status)
	}
	if err != nil {
		t.Fatalf("change passive Goal status: %v", err)
	}
	return workflowGoalFixture{runner: f, service: service, sessionID: sessionID, taskID: task.ID}
}
