package workflowstore

import (
	"testing"

	"core/server/workflow"
)

func TestJoinCompletionRejectsStaleArrivalPlanAndFreshPlanReleasesJoin(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createFanoutJoinWorkflow(t, ctx, store)
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	source := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	split, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source: source.Reference, OutputValues: map[string]string{"summary": "fanout"},
	})
	if err != nil {
		t.Fatal(err)
	}
	branches := make(map[workflow.TransitionBranchKey]workflow.CurrentNode)
	for _, node := range split.Mutation.Created {
		key, ok := node.Reference.TransitionBranchKey()
		if !ok {
			t.Fatal("fanout created an unscoped Current Node")
		}
		branches[key] = node
	}
	first, err := store.PlanCurrentNodeCompletion(ctx, CurrentNodeCompletionRequest{
		Source: branches["split_a"].Reference, TransitionID: "join_a",
		OutputValues: map[string]string{"joined": "first branch"},
	})
	if err != nil {
		t.Fatal(err)
	}
	secondRequest := CurrentNodeCompletionRequest{Source: branches["split_b"].Reference, TransitionID: "join_b"}
	stale, err := store.PlanCurrentNodeCompletion(ctx, secondRequest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CommitCurrentNodeCompletion(ctx, first, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CommitCurrentNodeCompletion(ctx, stale, nil); err == nil {
		t.Fatal("stale Join plan discarded the already committed branch arrival")
	}
	remaining, err := store.ListCurrentNodes(ctx, task.ID)
	if err != nil || len(remaining) != 1 || !remaining[0].Reference.Equal(secondRequest.Source) {
		t.Fatalf("stale Join commit changed pending branch: %+v, %v", remaining, err)
	}
	fresh, err := store.PlanCurrentNodeCompletion(ctx, secondRequest)
	if err != nil {
		t.Fatal(err)
	}
	released, err := store.CommitCurrentNodeCompletion(ctx, fresh, plannedSessionsForStoreTest(t, ctx, store, fresh.StartContexts()))
	if err != nil || len(released.Mutation.Created) != 1 || released.Mutation.Created[0].Reference.IsBranchScoped() {
		t.Fatalf("fresh Join plan did not release one serial successor: %+v, %v", released, err)
	}
	if released.Mutation.Created[0].CurrentInputValues["joined"] != "first branch" {
		t.Fatalf("Join lost the earlier committed branch output: %+v", released.Mutation.Created[0].CurrentInputValues)
	}
}
