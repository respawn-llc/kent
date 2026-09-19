package workflowfixture

import (
	"context"
	"testing"

	"core/server/metadata"
	"core/server/workflowstore"
)

func CompleteCurrentNode(t testing.TB, ctx context.Context, metadata *metadata.Store, store *workflowstore.Store, request workflowstore.CurrentNodeCompletionRequest) (workflowstore.CurrentNodeCompletionOutcome, error) {
	t.Helper()
	plan, err := store.PlanCurrentNodeCompletion(ctx, request)
	if err != nil {
		return workflowstore.CurrentNodeCompletionOutcome{}, err
	}
	return store.CommitCurrentNodeCompletion(ctx, plan, PrepareCurrentNodeSessions(t, ctx, metadata, plan.StartContexts()))
}

func MoveTask(t testing.TB, ctx context.Context, metadata *metadata.Store, store *workflowstore.Store, request workflowstore.ManualMoveRequest) (workflowstore.ManualMoveResult, error) {
	t.Helper()
	prepared, err := store.PrepareManualMove(ctx, request)
	if err != nil {
		return workflowstore.ManualMoveResult{}, err
	}
	plan, err := store.PlanManualMove(ctx, prepared, nil)
	if err != nil {
		return workflowstore.ManualMoveResult{}, err
	}
	return store.CommitManualMove(ctx, plan, PrepareCurrentNodeSessions(t, ctx, metadata, plan.StartContexts()))
}
