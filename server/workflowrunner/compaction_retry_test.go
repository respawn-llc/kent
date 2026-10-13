package workflowrunner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"

	"core/server/llm"
	"core/server/workflow"
	"core/server/workflowruntime"
	"core/server/workflowstore"
	"core/shared/config"
	"core/shared/runtimeids"
)

func TestResumeCreatesUnstartedSessionWithItsAssignedRole(t *testing.T) {
	for _, mode := range []workflow.ContextMode{workflow.ContextModeNewSession, workflow.ContextModeContinueSession} {
		t.Run(string(mode), func(t *testing.T) { testResumeCreatesUnstartedSessionWithItsAssignedRole(t, mode) })
	}
}

func testResumeCreatesUnstartedSessionWithItsAssignedRole(t *testing.T, mode workflow.ContextMode) {
	f := newCurrentNodeRunnerFixture(t, ScriptedCancellation(), ScriptedFinalAnswer(`{"commentary":"done"}`))
	sourceSelection := workflow.ContextSource{Kind: workflow.ContextSourceImmediateSource}
	if mode == workflow.ContextModeContinueSession {
		sourceSelection.Kind = workflow.ContextSourcePreviousTargetOrNew
	}
	workflowID := createCurrentNodeTwoStepWorkflowWithTransition(t, f.store, "Unstarted reviewer",
		currentNodeWorkflowStep{kind: workflow.NodeKindAgent, role: "coder", prompt: "Implement."},
		currentNodeWorkflowStep{kind: workflow.NodeKindAgent, role: "reviewer", prompt: "Review."},
		currentNodeLinearTransition{id: "next", mode: mode, contextSource: sourceSelection},
	)
	task := f.createTask(t, workflowID)
	ctx := t.Context()
	source := f.startTask(t, task)
	f.waitForModelRequests(t, 1)
	f.waitForTaskQuiescence(t, task.ID)
	plan, err := f.store.PlanCurrentNodeCompletion(ctx, workflowstore.CurrentNodeCompletionRequest{
		Source: source, TransitionID: "next",
	})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := f.starter.PrepareCurrentNode(ctx, plan.StartContexts()[0], workflowruntime.TaskPromptDeliveryAssignment)
	if err != nil {
		t.Fatal(err)
	}
	started, err := f.store.CommitCurrentNodeCompletion(ctx, plan, []workflowstore.PlannedCurrentNodeSession{*prepared.Session})
	if err != nil {
		t.Fatal(err)
	}
	node := started.Mutation.Created[0]
	if err := f.store.InterruptCurrentNode(ctx, node.Reference, "startup_failed", workflow.NewCurrentNodeInterruptionDetail("startup_failed", errors.New("startup stopped before file creation"))); err != nil {
		t.Fatal(err)
	}
	f.restartRuntime(t)
	if _, err := f.controller.ResumeTask(ctx, task.ID, nil); err != nil {
		t.Fatal(err)
	}
	requests := f.waitForModelRequests(t, 2)
	f.waitForTaskQuiescence(t, task.ID)
	if requests[1].SessionID == nil || *requests[1].SessionID != prepared.Session.SessionID.String() {
		t.Fatal("Resume replaced the committed Session")
	}
	if requests[1].Model != f.cfg.Settings.Subagents["reviewer"].Settings.Model {
		t.Fatal("Resume lost the assigned role for an unstarted Session")
	}
}

func TestResumeDoesNotRecreateEstablishedSession(t *testing.T) {
	f := newCurrentNodeRunnerFixture(t, ScriptedCancellation())
	task := f.createTask(t, createCurrentNodeAgentWorkflow(t, f.store))
	reference := f.startTask(t, task)
	f.waitForModelRequests(t, 1)
	f.waitForTaskQuiescence(t, task.ID)
	association, err := f.store.LatestTaskSessionForNode(t.Context(), reference)
	if err != nil {
		t.Fatal(err)
	}
	record, err := f.metadata.ResolvePersistedSession(t.Context(), association.SessionID.String())
	if err != nil {
		t.Fatal(err)
	}
	if record.Meta.LastSequence == 0 {
		t.Fatal("fixture never established its conversation")
	}
	if err := os.Rename(record.SessionDir, record.SessionDir+".retained"); err != nil {
		t.Fatal(err)
	}
	f.restartRuntime(t)
	if _, err := f.controller.ResumeTask(t.Context(), task.ID, nil); err == nil {
		f.waitForCurrentNode(t, task.ID, func(nodes []workflow.CurrentNode) bool {
			return len(nodes) == 1 && nodes[0].Scheduling != nil && nodes[0].Scheduling.Interruption != nil &&
				nodes[0].Scheduling.Interruption.Detail.Code == "workflow_runtime_start_failed"
		})
	}
	f.waitForTaskQuiescence(t, task.ID)
	if _, err := os.Stat(record.SessionDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Resume recreated an established Session: %v", err)
	}
	if len(f.client.Requests()) != 1 {
		t.Fatal("Resume dispatched a model request without established history")
	}
}

func TestFanoutResumeAfterSourceCompactionFailure(t *testing.T) {
	for _, restart := range []bool{false, true} {
		t.Run(fmt.Sprintf("restart=%t", restart), func(t *testing.T) {
			testFanoutResumeAfterSourceCompactionFailure(t, restart)
		})
	}
}

func testFanoutResumeAfterSourceCompactionFailure(t *testing.T, restart bool) {
	client := NewCompactingScriptedClient(
		llm.ProviderCapabilities{ProviderID: "test", SupportsResponsesAPI: true, SupportsResponsesCompact: true},
		nil,
		ScriptedFinalAnswer(`{"transition":"split","commentary":"source"}`),
		ScriptedFinalAnswer(`{"commentary":"branch a"}`),
		ScriptedFinalAnswer(`{"commentary":"branch b"}`),
	)
	f := newCurrentNodeRunnerFixtureWithClient(t, client)
	f.starter.cfg.Settings.CompactionMode = config.CompactionModeNative
	writeCurrentNodeConfig(t, f.starter.cfg)
	workflowID, branchNodeIDs := createCurrentNodeFanoutWorkflow(t, f.store, false, workflow.ContextModeCompactAndContinueSession)
	task := f.createTask(t, workflowID)
	f.startTask(t, task)
	nodes := f.waitForCurrentNode(t, task.ID, func(nodes []workflow.CurrentNode) bool {
		if len(nodes) != len(branchNodeIDs) {
			return false
		}
		for _, node := range nodes {
			if !node.Reference.IsBranchScoped() || node.Scheduling == nil || node.Scheduling.Interruption == nil {
				return false
			}
		}
		return true
	})
	f.waitForTaskQuiescence(t, task.ID)
	if len(client.CompactionCalls()) != 1 || len(client.Requests()) != 1 {
		t.Fatal("expected source compaction failure before any branch execution")
	}
	bindings := make(map[runtimeids.SessionID]workflow.CurrentNodeReference, len(nodes))
	for _, node := range nodes {
		if node.SessionID == nil {
			t.Fatal("interrupted branch has no Session")
		}
		bindings[*node.SessionID] = node.Reference
	}
	client.mu.Lock()
	client.responses = []llm.CompactionResponse{
		workflowPostCompletionCompactionResponse("branch a"),
		workflowPostCompletionCompactionResponse("branch b"),
	}
	client.mu.Unlock()

	if restart {
		f.restartRuntime(t)
	}
	if _, err := f.controller.ResumeTask(context.Background(), task.ID, nil); err != nil {
		t.Fatalf("resume after failed source compaction: %v", err)
	}
	requests := f.waitForModelRequests(t, len(nodes)+1)
	f.waitForTaskQuiescence(t, task.ID)
	seen := make(map[runtimeids.SessionID]bool, len(nodes))
	for _, request := range requests[1:] {
		if request.SessionID == nil {
			t.Fatal("branch request has no Session")
		}
		id, err := runtimeids.ParseSessionID(*request.SessionID)
		if err != nil {
			t.Fatal(err)
		}
		reference, ok := bindings[id]
		if !ok || seen[id] {
			t.Fatalf("branch replaced or duplicated Session %s", id)
		}
		seen[id] = true
		assignments := workflowAssignments(request)
		if len(assignments) != 1 || assignments[0].sourcePath != workflowruntime.CurrentNodePromptIdentity(reference) {
			t.Fatalf("branch assignment does not match its retained identity: %+v", assignments)
		}
	}
	if len(seen) != len(bindings) {
		t.Fatal("Resume did not start every interrupted branch")
	}
	if count, err := f.store.CountTaskSessions(t.Context(), task.ID); err != nil || count != int64(len(nodes)+1) {
		t.Fatalf("Resume changed retained Session count: %d, %v", count, err)
	}
	compactions := client.CompactionCalls()
	if len(compactions) != len(nodes)+1 {
		t.Fatalf("compaction attempts = %d; want failed source plus each resumed branch", len(compactions))
	}
	for _, compaction := range compactions[1:] {
		if compaction.Model != requests[0].Model || compaction.ReasoningEffort != requests[0].ReasoningEffort {
			t.Fatal("recovered branch did not compact with outgoing source configuration")
		}
		if len(requests[0].Items) == 0 || len(compaction.Items) < len(requests[0].Items) ||
			!reflect.DeepEqual(compaction.Items[:len(requests[0].Items)], requests[0].Items) {
			t.Fatal("recovered branch lost the source conversation")
		}
	}
}
