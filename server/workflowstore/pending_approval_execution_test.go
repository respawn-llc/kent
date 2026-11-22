package workflowstore

import (
	"errors"
	"reflect"
	"testing"

	"core/server/workflow"
)

func TestApproveTransitionExpandedToFanout(t *testing.T) {
	for _, tc := range []struct {
		name               string
		newBranchNeedsRisk bool
	}{
		{name: "all current branches start and join"},
		{name: "missing branch input leaves approval and source unchanged", newBranchNeedsRisk: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, store, binding := newTestStoreContext(t)
			workflowID := createFanoutJoinWorkflow(t, ctx, store)
			var alternateEdgeID workflow.EdgeID
			saveWorkflowGraphFixture(t, ctx, store, workflowID, func(def workflow.Definition, req *WorkflowGraphSaveRequest) {
				first := workflowGraphSaveEdgeRecord(t, req.Edges, edgeByKey(t, def, "split_a").ID)
				first.RequiresApproval = true
				second := workflowGraphSaveEdgeRecord(t, req.Edges, edgeByKey(t, def, "split_b").ID)
				alternateEdgeID = second.ID
				second.Key = "alternate_b"
				second.TransitionGroupID = testTransitionGroupID("alternate-b")
				req.TransitionGroups = append(req.TransitionGroups, TransitionGroupRecord{
					ID: second.TransitionGroupID, WorkflowID: workflowID,
					SourceNodeID: testNodeID("node-plan-" + workflowID.String()), TransitionID: "alternate_b", DisplayName: "Alternate B",
				})
			})
			linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
			task := createDefaultTask(t, ctx, store, binding.ProjectID)
			source := startTask(t, ctx, store, task.ID).Mutation.Created[0]
			completed, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
				Source: source.Reference, TransitionID: "split", OutputValues: map[string]string{"summary": "submitted"},
			})
			if err != nil || completed.PendingApproval == nil || len(completed.PendingApproval.Branches) != 1 {
				t.Fatalf("single-branch completion did not create its pending approval: %+v, %v", completed, err)
			}
			approvalID := completed.PendingApproval.ID
			wantNodes := make(map[workflow.TransitionBranchKey]workflow.NodeID, 2)
			saveWorkflowGraphFixture(t, ctx, store, workflowID, func(def workflow.Definition, req *WorkflowGraphSaveRequest) {
				first := edgeByKey(t, def, "split_a")
				second := *workflowGraphSaveEdgeRecord(t, req.Edges, alternateEdgeID)
				second.ID, second.Key, second.TransitionGroupID = testEdgeID("new-split-b"), "split_b", first.TransitionGroupID
				if tc.newBranchNeedsRisk {
					second.Parameters = append(second.Parameters, workflow.Parameter{
						Key: "risk", Description: "Risk assessment.", Purpose: workflow.ParameterPurposeOrdinary,
					})
					second.PromptTemplate = "B {{.Params.summary}}. Risk: {{.Params.risk}}."
				}
				req.Edges = append(req.Edges, second)
				wantNodes["split_a"], wantNodes["split_b"] = first.TargetNodeID, second.TargetNodeID
			})
			before, err := store.PendingApproval(ctx, approvalID)
			if err != nil {
				t.Fatal(err)
			}
			applied, err := applyPendingApproval(t, store, ctx, approvalID)
			if tc.newBranchNeedsRisk {
				var move *TaskContextSelectionRequiredError
				var validation CompletionValidationError
				if !errors.As(err, &move) || move.TaskID != task.ID || !errors.As(err, &validation) ||
					!completionHasCode(err, CompletionCodeRequiredOutputMissing) {
					t.Fatalf("approve with missing new branch input: got %v, want missing output and Manual Move", err)
				}
				after, err := store.PendingApproval(ctx, approvalID)
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatalf("failed approval changed the pending choice: before=%+v after=%+v error=%v", before, after, err)
				}
				current, err := store.ListCurrentNodes(ctx, task.ID)
				if err != nil || len(current) != 1 || !current[0].Reference.Equal(source.Reference) {
					t.Fatalf("failed approval changed the current source: %+v, %v", current, err)
				}
				return
			}
			if err != nil || len(applied.Mutation.Removed) != 1 || !applied.Mutation.Removed[0].Equal(source.Reference) ||
				len(applied.AutomaticIntents) != 2 {
				t.Fatalf("approve did not replace the source and start both branches: %+v, %v", applied, err)
			}
			pending, err := store.ListPendingApprovals(ctx, task.ID)
			if err != nil || len(pending) != 0 {
				t.Fatalf("successful approval left a pending choice: %+v, %v", pending, err)
			}
			current, err := store.ListCurrentNodes(ctx, task.ID)
			if err != nil || len(current) != len(wantNodes) {
				t.Fatalf("approve did not persist both branches: %+v, %v", current, err)
			}
			branches := make(map[workflow.TransitionBranchKey]workflow.CurrentNodeReference, len(current))
			for _, node := range current {
				key, scoped := node.Reference.TransitionBranchKey()
				wantNode, expected := wantNodes[key]
				if !scoped || !expected || node.Reference.NodeID != wantNode || node.Reference.TaskID != task.ID ||
					node.CurrentInputValues["summary"] != "submitted" || node.SessionID == nil {
					t.Fatalf("approval did not persist the expected branch, input, and session: %+v", node)
				}
				branches[key] = node.Reference
			}
			if *current[0].SessionID == *current[1].SessionID {
				t.Fatal("approved branches share a session")
			}
			first, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
				Source: branches["split_a"], TransitionID: "join_a", OutputValues: map[string]string{"joined": "first branch"},
			})
			if err != nil || len(first.Mutation.Removed) != 1 || len(first.Mutation.Created) != 0 {
				t.Fatalf("first arrival should wait at the Join: %+v, %v", first, err)
			}
			second, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
				Source: branches["split_b"], TransitionID: "join_b",
			})
			if err != nil || len(second.Mutation.Created) != 1 {
				t.Fatalf("second arrival should release one Join successor: %+v, %v", second, err)
			}
			successor := second.Mutation.Created[0]
			if successor.Reference.IsBranchScoped() || successor.CurrentInputValues["joined"] != "first branch" {
				t.Fatalf("Join did not produce a serial successor carrying the first arrival's output: %+v", successor)
			}
		})
	}
}

func TestApproveTransitionRewiredToJoin(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createFanoutJoinWorkflow(t, ctx, store)
	midID := testNodeID("approval-before-join")
	approvedEdgeID := testEdgeID("approval-edge-before-join")
	saveWorkflowGraphFixture(t, ctx, store, workflowID, func(def workflow.Definition, req *WorkflowGraphSaveRequest) {
		arrival := workflowGraphSaveEdgeRecord(t, req.Edges, edgeByKey(t, def, "join_a").ID)
		approved := *arrival
		approved.ID, approved.Key, approved.TargetNodeID = approvedEdgeID, "approve_a", midID
		approved.RequiresApproval = true
		approved.PromptTemplate = "Review {{.Params.joined}}."
		midGroup := testTransitionGroupID("approval-node-to-join")
		arrival.TransitionGroupID = midGroup
		req.Nodes = append(req.Nodes, NodeRecord{
			ID: midID, WorkflowID: workflowID, Key: "mid", Kind: workflow.NodeKindAgent, SubagentRole: "coder",
		})
		req.TransitionGroups = append(req.TransitionGroups, TransitionGroupRecord{
			ID: midGroup, WorkflowID: workflowID, SourceNodeID: midID, TransitionID: "arrive", DisplayName: "Arrive",
		})
		req.Edges = append(req.Edges, approved)
	})
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	source := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	split, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source: source.Reference, TransitionID: "split", OutputValues: map[string]string{"summary": "submitted"},
	})
	if err != nil || len(split.Mutation.Created) != 2 {
		t.Fatalf("start fanout: %+v, %v", split, err)
	}
	branches := make(map[workflow.TransitionBranchKey]workflow.CurrentNodeReference, 2)
	for _, target := range split.Mutation.Created {
		key, scoped := target.Reference.TransitionBranchKey()
		if !scoped {
			t.Fatalf("fanout target is unscoped: %+v", target)
		}
		branches[key] = target.Reference
	}
	completed, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source: branches["split_a"], TransitionID: "join_a", OutputValues: map[string]string{"joined": "approved output"},
	})
	if err != nil || completed.PendingApproval == nil {
		t.Fatalf("pause branch before Join: %+v, %v", completed, err)
	}
	saveWorkflowGraphFixture(t, ctx, store, workflowID, func(def workflow.Definition, req *WorkflowGraphSaveRequest) {
		approved := workflowGraphSaveEdgeRecord(t, req.Edges, approvedEdgeID)
		arrival := workflowGraphSaveEdgeRecord(t, req.Edges, edgeByKey(t, def, "join_a").ID)
		done := workflowGraphSaveEdgeRecord(t, req.Edges, edgeByKey(t, def, "done").ID)
		join := workflowGraphSaveNodeRecord(t, req.Nodes, arrival.TargetNodeID)
		join.JoinInputProviders = []workflow.JoinInputProvider{{InputName: "joined", ProviderEdgeID: approvedEdgeID}}
		approved.TargetNodeID, arrival.TargetNodeID, done.TargetNodeID = join.ID, done.TargetNodeID, midID
		approved.PromptTemplate, arrival.Parameters, done.PromptTemplate = "", nil, "Finish."
	})
	applied, err := applyPendingApproval(t, store, ctx, completed.PendingApproval.ID)
	if err != nil || len(applied.Mutation.Created) != 0 || len(applied.Mutation.Removed) != 1 ||
		!applied.Mutation.Removed[0].Equal(branches["split_a"]) {
		t.Fatalf("approval should commit the first Join arrival without starting a node: %+v, %v", applied, err)
	}
	pending, err := store.ListPendingApprovals(ctx, task.ID)
	if err != nil || len(pending) != 0 {
		t.Fatalf("committed Join arrival did not clear its approval: %+v, %v", pending, err)
	}
	joined, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source: branches["split_b"], TransitionID: "join_b",
	})
	if err != nil || len(joined.Mutation.Created) != 1 || joined.Mutation.Created[0].Reference.IsBranchScoped() ||
		joined.Mutation.Created[0].CurrentInputValues["joined"] != "approved output" {
		t.Fatalf("second arrival should start one serial successor using the approved output: %+v, %v", joined, err)
	}
}
