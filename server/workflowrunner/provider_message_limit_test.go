package workflowrunner

import (
	"context"
	"testing"

	"core/server/llm"
	"core/server/workflow"
	"core/server/workflowruntime"
	"core/shared/config"
	"core/shared/textutil"
)

func TestCurrentNodeContinuesAfterProviderMessageLimitCompaction(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	finish := ScriptedFinalAnswer(`{"commentary":"done"}`)
	finish.BeforeResponse = func(ctx context.Context) error {
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	client := NewCompactingScriptedClient(
		llm.ProviderCapabilities{ProviderID: "test", SupportsResponsesAPI: true, SupportsResponsesCompact: true},
		[]llm.CompactionResponse{workflowPostCompletionCompactionResponse("recovery")},
		ScriptedRuntimeError(&llm.ProviderAPIError{ProviderID: "test", StatusCode: textutil.Value(200),
			ProviderType: "response.incomplete", ProviderCode: "max_messages"}),
		finish,
	)
	f := newCurrentNodeRunnerFixtureWithClient(t, client)
	f.starter.cfg.Settings.CompactionMode = config.CompactionModeNative
	writeCurrentNodeConfig(t, f.starter.cfg)
	task := f.createTask(t, createCurrentNodeAgentWorkflow(t, f.store))
	f.startTask(t, task)
	requests := f.waitForModelRequests(t, 2)
	nodes := f.waitForCurrentNode(t, task.ID, func(nodes []workflow.CurrentNode) bool {
		return len(nodes) == 1 && nodes[0].SessionID != nil
	})
	node := nodes[0]
	if node.Scheduling == nil || node.Scheduling.Interruption != nil {
		t.Fatalf("recovery interrupted the Current Node: %+v", node)
	}
	if requests[0].SessionID == nil || requests[1].SessionID == nil ||
		*requests[0].SessionID != *requests[1].SessionID || *requests[1].SessionID != node.SessionID.String() {
		t.Fatal("recovery changed the Session")
	}
	if len(client.CompactionCalls()) != 1 {
		t.Fatalf("recovery compactions=%d", len(client.CompactionCalls()))
	}
	for _, request := range requests {
		assignments := workflowAssignments(request)
		if len(assignments) != 1 || assignments[0].sourcePath != workflowruntime.CurrentNodePromptIdentity(node.Reference) {
			t.Fatal("recovery changed the Task or Current Node assignment")
		}
	}
	release <- struct{}{}
	f.waitForCurrentNode(t, task.ID, func(nodes []workflow.CurrentNode) bool {
		return len(nodes) == 1 && nodes[0].Scheduling == nil
	})
	f.waitForTaskQuiescence(t, task.ID)
	if count, err := f.store.CountTaskSessions(t.Context(), task.ID); err != nil || count != 1 {
		t.Fatalf("Session count=%d, error=%v", count, err)
	}
}
