package workflowstore

import (
	"testing"
	"time"

	"core/server/workflow"
)

func TestPendingApprovalPresentationDoesNotWaitForTaskRowUpdates(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createMaterializedCurrentNodeWorkflow(t, ctx, store)
	requireApprovalOnWorkflowEdge(t, ctx, store, workflowID, "review")
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	pending := createDefaultTask(t, ctx, store, binding.ProjectID)
	empty := createDefaultTask(t, ctx, store, binding.ProjectID)
	source := startTask(t, ctx, store, pending.ID).Mutation.Created[0]
	completed, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source: source.Reference, TransitionID: "review", OutputValues: map[string]string{"summary": "ready"},
	})
	if err != nil || completed.PendingApproval == nil {
		t.Fatalf("complete source: approval=%+v err=%v", completed.PendingApproval, err)
	}

	writer, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Rollback() }()
	if _, err := store.queries.WithTx(writer).AcquireManualMoveTaskWriteLock(ctx, string(empty.ID)); err != nil {
		t.Fatal(err)
	}

	type readResult struct {
		pending, empty []workflow.PendingApproval
		err            error
	}
	read := make(chan readResult, 1)
	go func() {
		approvals, err := store.ListPendingApprovals(ctx, pending.ID)
		if err != nil {
			read <- readResult{err: err}
			return
		}
		emptyApprovals, err := store.ListPendingApprovals(ctx, empty.ID)
		read <- readResult{pending: approvals, empty: emptyApprovals, err: err}
	}()
	select {
	case result := <-read:
		if result.err != nil || len(result.empty) != 0 || len(result.pending) != 1 ||
			result.pending[0].ID != completed.PendingApproval.ID || !result.pending[0].ChoiceAvailable() {
			t.Fatalf("read pending approvals during task update: %+v", result)
		}
	case <-time.After(time.Second):
		_ = writer.Rollback()
		<-read
		t.Fatal("approval presentation waited on a task row update that does not affect approvals")
	}
}
