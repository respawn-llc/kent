package workflowstore

import (
	"context"
	"testing"

	"core/server/session"
	"core/server/workflow"
)

func completeCurrentNode(t *testing.T, store *Store, ctx context.Context, request CurrentNodeCompletionRequest) (CurrentNodeCompletionOutcome, error) {
	t.Helper()
	plan, err := store.PlanCurrentNodeCompletion(ctx, request)
	if err != nil {
		return CurrentNodeCompletionOutcome{}, err
	}
	sessions, creations := plannedSessionArtifactsForStoreTest(t, ctx, store, plan.StartContexts())
	result, err := store.CommitCurrentNodeCompletion(ctx, plan, sessions)
	if err == nil {
		materializeSessionArtifactsForStoreTest(t, ctx, store, creations)
	}
	return result, err
}

func applyPendingApproval(t *testing.T, store *Store, ctx context.Context, id workflow.ApprovalID) (PendingApprovalApplyResult, error) {
	t.Helper()
	plan, err := store.PlanPendingApproval(ctx, id)
	if err != nil {
		return PendingApprovalApplyResult{}, err
	}
	sessions, creations := plannedSessionArtifactsForStoreTest(t, ctx, store, plan.StartContexts())
	result, err := store.CommitPendingApproval(ctx, plan, sessions)
	if err == nil {
		materializeSessionArtifactsForStoreTest(t, ctx, store, creations)
	}
	return result, err
}

func moveTask(t *testing.T, store *Store, ctx context.Context, request ManualMoveRequest) (ManualMoveResult, error) {
	t.Helper()
	prepared, err := store.PrepareManualMove(ctx, request)
	if err != nil {
		return ManualMoveResult{}, err
	}
	plan, err := store.PlanManualMove(ctx, prepared, nil)
	if err != nil {
		return ManualMoveResult{}, err
	}
	return store.CommitManualMove(ctx, plan, plannedSessionsForStoreTest(t, ctx, store, plan.StartContexts()))
}

func materializeSessionArtifactsForStoreTest(t *testing.T, ctx context.Context, store *Store, creations []session.CreationPlan) {
	t.Helper()
	for _, creation := range creations {
		_, err := session.MaterializeCommittedCreation(ctx, creation, store.metadata.AuthoritativeSessionStoreOptions()...)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func removeRetainedSessionHistoryForTest(t *testing.T, ctx context.Context, store *Store, reference workflow.CurrentNodeReference) {
	t.Helper()
	if _, err := store.db.ExecContext(ctx, `DELETE FROM session_workflow_node_associations WHERE node_id = ? AND session_id IN (SELECT id FROM sessions WHERE task_id = ?)`, string(reference.NodeID), string(reference.TaskID)); err != nil {
		t.Fatal(err)
	}
}
