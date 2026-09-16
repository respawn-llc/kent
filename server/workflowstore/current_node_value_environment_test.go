package workflowstore

import (
	"context"
	"testing"
	"time"

	"core/server/workflow"
	"core/shared/runtimeids"
)

func TestCompleteCurrentNodeMaterializesChainedInputsAndPriorTransitionParameters(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createMaterializedCurrentNodeWorkflow(t, ctx, store)
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)

	started := startTask(t, ctx, store, task.ID)
	if len(started.Mutation.Created) != 1 {
		t.Fatalf("StartTask mutation = %+v, want plan current node", started.Mutation)
	}
	reviewResult, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       started.Mutation.Created[0].Reference,
		TransitionID: "review",
		OutputValues: map[string]string{"summary": "approved plan"},
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode plan: %v", err)
	}
	if len(reviewResult.Mutation.Created) != 1 {
		t.Fatalf("plan completion mutation = %+v, want review current node", reviewResult.Mutation)
	}
	review := reviewResult.Mutation.Created[0]
	if review.CurrentInputValues["summary"] != "approved plan" {
		t.Fatalf("review current inputs = %+v, want materialized summary", review.CurrentInputValues)
	}
	if review.PriorValues.TransitionParameters["review"]["summary"] != "approved plan" {
		t.Fatalf("review prior Transition parameters = %+v, want review transition summary retained for downstream audit", review.PriorValues)
	}

	auditResult, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       review.Reference,
		TransitionID: "audit",
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode review: %v", err)
	}
	if len(auditResult.Mutation.Created) != 1 {
		t.Fatalf("review completion mutation = %+v, want audit current node", auditResult.Mutation)
	}
	audit := auditResult.Mutation.Created[0]
	if audit.PriorValues.TransitionParameters["review"]["summary"] != "approved plan" {
		t.Fatalf("audit prior Transition parameters = %+v, want review transition summary carried from review current node", audit.PriorValues)
	}
	startContext, err := store.ResolveCurrentNodeStartContext(ctx, audit.Reference)
	if err != nil {
		t.Fatalf("ResolveCurrentNodeStartContext audit: %v", err)
	}
	if startContext.CurrentNode.PriorValues.TransitionParameters["review"]["summary"] != "approved plan" {
		t.Fatalf("audit start prior Transition parameters = %+v, want review transition namespace", startContext.CurrentNode.PriorValues)
	}
	currentNodes, err := store.ListCurrentNodes(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListCurrentNodes: %v", err)
	}
	if len(currentNodes) != 1 ||
		!currentNodes[0].Reference.Equal(audit.Reference) ||
		currentNodes[0].PriorValues.TransitionParameters["review"]["summary"] != "approved plan" {
		t.Fatalf("current nodes = %+v, want audit-owned materialized values", currentNodes)
	}
	doneResult, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       audit.Reference,
		TransitionID: "done",
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode audit: %v", err)
	}
	if !doneResult.CommitReceipt.Committed ||
		len(doneResult.AutomaticIntents) != 0 ||
		doneResult.SourceSessionID != nil ||
		doneResult.SessionReuseClassification != workflow.SessionReuseNone {
		t.Fatalf("terminal completion outcome = %+v, want complete no-successor facts", doneResult)
	}
	currentNodes, err = store.ListCurrentNodes(ctx, task.ID)
	if err != nil || len(currentNodes) != 1 {
		t.Fatalf("completed current Nodes = %+v: %v", currentNodes, err)
	}
	if currentNodes[0].EnteredByEdgeID != nil ||
		currentNodes[0].PriorValues.TransitionParameters["review"]["summary"] != "approved plan" {
		t.Fatalf("terminal reference cleanup lost materialized result: %+v", currentNodes[0])
	}
}

func TestCompleteCurrentNodeMaterializesCurrentAndPriorTransitionCommentary(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createMaterializedCurrentNodeWorkflow(t, ctx, store)
	saveWorkflowGraphFixture(t, ctx, store, workflowID, func(_ workflow.Definition, req *WorkflowGraphSaveRequest) {
		workflowGraphSaveEdgeRecord(
			t,
			req.Edges,
			testEdgeID("edge-review-"+workflowID.String()),
		).PromptTemplate = "Review {{.Params.commentary}}."
		workflowGraphSaveEdgeRecord(
			t,
			req.Edges,
			testEdgeID("edge-audit-"+workflowID.String()),
		).PromptTemplate = "Audit {{.Params.review.commentary}}."
	})
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)

	plan := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	reviewResult, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       plan.Reference,
		TransitionID: "review",
		OutputValues: map[string]string{"summary": "approved plan"},
		Commentary:   "plan handoff",
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode plan: %v", err)
	}
	review := reviewResult.Mutation.Created[0]
	if review.CurrentInputValues[workflow.RuntimePromptParameterCommentary] != "plan handoff" {
		t.Fatalf("review current inputs = %+v, want direct commentary", review.CurrentInputValues)
	}
	if review.PriorValues.TransitionParameters["review"][workflow.RuntimePromptParameterCommentary] != "plan handoff" {
		t.Fatalf("review prior Transition values = %+v, want review commentary", review.PriorValues)
	}
	reviewStart, err := store.ResolveCurrentNodeStartContext(ctx, review.Reference)
	if err != nil {
		t.Fatalf("ResolveCurrentNodeStartContext review: %v", err)
	}
	if reviewStart.ParameterValues[workflow.RuntimePromptParameterCommentary] != "plan handoff" {
		t.Fatalf("review start parameters = %+v, want direct commentary", reviewStart.ParameterValues)
	}

	auditResult, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       review.Reference,
		TransitionID: "audit",
		Commentary:   "review handoff",
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode review: %v", err)
	}
	audit := auditResult.Mutation.Created[0]
	if audit.CurrentInputValues[workflow.RuntimePromptParameterCommentary] != "review handoff" {
		t.Fatalf("audit current inputs = %+v, want direct commentary", audit.CurrentInputValues)
	}
	if audit.PriorValues.TransitionParameters["review"][workflow.RuntimePromptParameterCommentary] != "plan handoff" {
		t.Fatalf("audit prior Transition values = %+v, want review commentary", audit.PriorValues)
	}
}

func TestResolveCurrentNodeStartContextResolvesLatestEnteringSourceSessionID(t *testing.T) {
	ctx, store, binding, cfg := newTestStoreWithConfigContext(t)
	workflowID := createMaterializedCurrentNodeWorkflow(t, ctx, store)
	saveWorkflowGraphFixture(t, ctx, store, workflowID, func(_ workflow.Definition, req *WorkflowGraphSaveRequest) {
		workflowGraphSaveEdgeRecord(
			t,
			req.Edges,
			testEdgeID("edge-review-"+workflowID.String()),
		).PromptTemplate = "Review {{.SessionId}}."
	})
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	plan := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	sourceSessionID := associateAndBindCurrentNodeSessionForTest(t, ctx, store, binding, cfg, plan.Reference)

	reviewResult, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       plan.Reference,
		TransitionID: "review",
		OutputValues: map[string]string{"summary": "approved plan"},
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode plan: %v", err)
	}
	review := reviewResult.Mutation.Created[0]
	startContext, err := store.ResolveCurrentNodeStartContext(ctx, review.Reference)
	if err != nil {
		t.Fatalf("ResolveCurrentNodeStartContext review: %v", err)
	}
	if startContext.PromptSessionID == nil || *startContext.PromptSessionID != sourceSessionID {
		t.Fatalf("prompt source Session ID = %v, want latest source Session %q", startContext.PromptSessionID, sourceSessionID)
	}
}

func TestResolveCurrentNodeStartContextRendersMissingEnteringSourceSessionIDAsEmpty(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createMaterializedCurrentNodeWorkflow(t, ctx, store)
	saveWorkflowGraphFixture(t, ctx, store, workflowID, func(_ workflow.Definition, req *WorkflowGraphSaveRequest) {
		workflowGraphSaveEdgeRecord(
			t,
			req.Edges,
			testEdgeID("edge-review-"+workflowID.String()),
		).PromptTemplate = "Review {{.SessionId}}."
	})
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	plan := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	reviewResult, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       plan.Reference,
		TransitionID: "review",
		OutputValues: map[string]string{"summary": "approved plan"},
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode plan: %v", err)
	}
	startContext, err := store.ResolveCurrentNodeStartContext(ctx, reviewResult.Mutation.Created[0].Reference)
	if err != nil {
		t.Fatalf("ResolveCurrentNodeStartContext review: %v", err)
	}
	if startContext.PromptSessionID != nil {
		t.Fatalf("missing source Session ID = %q, want empty lookup", *startContext.PromptSessionID)
	}
}

func TestResolveCurrentNodeStartContextResolvesLatestPriorTransitionSessionID(t *testing.T) {
	ctx, store, binding, cfg := newTestStoreWithConfigContext(t)
	workflowID := createMaterializedCurrentNodeWorkflow(t, ctx, store)
	saveWorkflowGraphFixture(t, ctx, store, workflowID, func(_ workflow.Definition, req *WorkflowGraphSaveRequest) {
		workflowGraphSaveEdgeRecord(
			t,
			req.Edges,
			testEdgeID("edge-audit-"+workflowID.String()),
		).PromptTemplate = "Audit {{.Params.review.session_id}}."
	})
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	plan := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	reviewResult, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       plan.Reference,
		TransitionID: "review",
		OutputValues: map[string]string{"summary": "approved plan"},
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode plan: %v", err)
	}
	review := reviewResult.Mutation.Created[0]
	auditResult, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       review.Reference,
		TransitionID: "audit",
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode review: %v", err)
	}
	firstSessionID := associateTaskSessionForTest(t, ctx, store, binding, cfg, plan.Reference, time.UnixMilli(1_700_000_000_000).UTC())
	secondSessionID := associateTaskSessionForTest(t, ctx, store, binding, cfg, plan.Reference, time.UnixMilli(1_700_000_001_000).UTC())

	startContext, err := store.ResolveCurrentNodeStartContext(ctx, auditResult.Mutation.Created[0].Reference)
	if err != nil {
		t.Fatalf("ResolveCurrentNodeStartContext audit: %v", err)
	}
	if startContext.PriorSessionIDs["review"] == nil || *startContext.PriorSessionIDs["review"] != secondSessionID {
		t.Fatalf(
			"prior transition Session ID = %v, want latest Session %q after older Session %q",
			startContext.PriorSessionIDs["review"],
			secondSessionID,
			firstSessionID,
		)
	}
}

func TestResolveCurrentNodeStartContextRendersMissingPriorTransitionSessionIDAsEmpty(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createMaterializedCurrentNodeWorkflow(t, ctx, store)
	saveWorkflowGraphFixture(t, ctx, store, workflowID, func(_ workflow.Definition, req *WorkflowGraphSaveRequest) {
		workflowGraphSaveEdgeRecord(
			t,
			req.Edges,
			testEdgeID("edge-audit-"+workflowID.String()),
		).PromptTemplate = "Audit {{.Params.review.session_id}}."
	})
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	plan := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	reviewResult, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       plan.Reference,
		TransitionID: "review",
		OutputValues: map[string]string{"summary": "approved plan"},
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode plan: %v", err)
	}
	auditResult, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       reviewResult.Mutation.Created[0].Reference,
		TransitionID: "audit",
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode review: %v", err)
	}
	startContext, err := store.ResolveCurrentNodeStartContext(ctx, auditResult.Mutation.Created[0].Reference)
	if err != nil {
		t.Fatalf("ResolveCurrentNodeStartContext audit: %v", err)
	}
	if sessionID, present := startContext.PriorSessionIDs["review"]; !present || sessionID != nil {
		t.Fatalf("missing prior Session ID = %v (present=%t), want empty lookup", sessionID, present)
	}
}

func TestResolveCurrentNodeStartContextUsesLatestSourceSessionAfterRepeatedVisitWithAnotherTransition(t *testing.T) {
	ctx, store, binding, cfg := newTestStoreWithConfigContext(t)
	workflowID := createPromptNodeReferenceWorkflow(t, ctx, store)
	saveWorkflowGraphFixture(t, ctx, store, workflowID, func(def workflow.Definition, req *WorkflowGraphSaveRequest) {
		plan := nodeByKey(t, def, "plan")
		review := nodeByKey(t, def, "review")
		audit := nodeByKey(t, def, "audit")
		retryGroupID := testTransitionGroupID("group-review-retry-" + workflowID.String())
		alternateGroupID := testTransitionGroupID("group-plan-alternate-" + workflowID.String())
		req.TransitionGroups = append(req.TransitionGroups,
			TransitionGroupRecord{ID: retryGroupID, WorkflowID: workflowID, SourceNodeID: workflow.NodeIDOf(review), TransitionID: "retry", DisplayName: "Retry"},
			TransitionGroupRecord{ID: alternateGroupID, WorkflowID: workflowID, SourceNodeID: workflow.NodeIDOf(plan), TransitionID: "alternate", DisplayName: "Alternate"},
		)
		req.Edges = append(req.Edges,
			EdgeRecord{
				ID:                testEdgeID("edge-review-retry-" + workflowID.String()),
				WorkflowID:        workflowID,
				TransitionGroupID: retryGroupID,
				Key:               "retry",
				TargetNodeID:      workflow.NodeIDOf(plan),
				AssigneeSelection: workflow.AssigneeSelectionConfigured,
				ThinkingSelection: workflow.ThinkingSelectionConfigured,
				ContextMode:       workflow.ContextModeNewSession,
				PromptTemplate:    "Retry.",
			},
			EdgeRecord{
				ID:                testEdgeID("edge-plan-alternate-" + workflowID.String()),
				WorkflowID:        workflowID,
				TransitionGroupID: alternateGroupID,
				Key:               "alternate",
				TargetNodeID:      workflow.NodeIDOf(audit),
				AssigneeSelection: workflow.AssigneeSelectionConfigured,
				ThinkingSelection: workflow.ThinkingSelectionConfigured,
				ContextMode:       workflow.ContextModeNewSession,
				PromptTemplate:    "Alternate {{.SessionId}}.",
			},
		)
	})
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	firstPlan := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	firstSessionID := associateTaskSessionForTest(
		t,
		ctx,
		store,
		binding,
		cfg,
		firstPlan.Reference,
		time.UnixMilli(1_700_000_000_000).UTC(),
	)
	reviewResult, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       firstPlan.Reference,
		TransitionID: "next",
		OutputValues: map[string]string{"summary": "first plan"},
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode first plan: %v", err)
	}
	retryResult, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       reviewResult.Mutation.Created[0].Reference,
		TransitionID: "retry",
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode review retry: %v", err)
	}
	if len(retryResult.Mutation.Created) != 1 {
		t.Fatalf("retry completion = %+v, want repeated plan Current Node", retryResult.Mutation.Created)
	}
	secondPlan := retryResult.Mutation.Created[0]
	secondSessionID := associateTaskSessionForTest(
		t,
		ctx,
		store,
		binding,
		cfg,
		secondPlan.Reference,
		time.UnixMilli(1_700_000_001_000).UTC(),
	)
	if secondSessionID == firstSessionID {
		t.Fatalf("repeated plan Sessions reused %q, want distinct Sessions", secondSessionID)
	}
	auditResult, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       secondPlan.Reference,
		TransitionID: "alternate",
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode second plan alternate: %v", err)
	}
	startContext, err := store.ResolveCurrentNodeStartContext(ctx, auditResult.Mutation.Created[0].Reference)
	if err != nil {
		t.Fatalf("ResolveCurrentNodeStartContext alternate audit: %v", err)
	}
	if startContext.PromptSessionID == nil || *startContext.PromptSessionID != secondSessionID {
		t.Fatalf(
			"repeated source Session ID = %v, want latest second Plan Session %q after alternate transition",
			startContext.PromptSessionID,
			secondSessionID,
		)
	}
}

func TestCompleteCurrentNodeRecoversEnteringTransitionParameterFromCurrentInput(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createMaterializedCurrentNodeWorkflow(t, ctx, store)
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)

	plan := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	reviewResult, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       plan.Reference,
		TransitionID: "review",
		OutputValues: map[string]string{"summary": "approved plan"},
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode plan: %v", err)
	}
	review := reviewResult.Mutation.Created[0]
	if review.CurrentInputValues["summary"] != "approved plan" {
		t.Fatalf("review current inputs = %+v, want entering Transition Parameter", review.CurrentInputValues)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE task_current_nodes
SET prior_node_values_json = '{"transition_parameters":{}}'
WHERE task_id = ?
  AND node_id = ?
  AND transition_branch_key IS NULL`,
		string(review.Reference.TaskID),
		string(review.Reference.NodeID),
	); err != nil {
		t.Fatalf("simulate Current Node missing its entering Transition namespace: %v", err)
	}

	auditResult, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       review.Reference,
		TransitionID: "audit",
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode review with recoverable entering Transition Parameter: %v", err)
	}
	if len(auditResult.Mutation.Created) != 1 {
		t.Fatalf("review completion mutation = %+v, want audit current node", auditResult.Mutation)
	}
	audit := auditResult.Mutation.Created[0]
	if audit.PriorValues.TransitionParameters["review"]["summary"] != "approved plan" {
		t.Fatalf("audit prior Transition values = %+v, want recovered review summary", audit.PriorValues)
	}
}

func TestCompleteCurrentNodeUsesTransitionParametersInsteadOfTargetInputFields(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createMaterializedCurrentNodeWorkflow(t, ctx, store)
	saveWorkflowGraphFixture(t, ctx, store, workflowID, func(def workflow.Definition, req *WorkflowGraphSaveRequest) {
		workflowGraphSaveEdgeRecord(
			t,
			req.Edges,
			testEdgeID("edge-review-"+workflowID.String()),
		).PromptTemplate = "Review {{.Params.summary}}."
	})
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)

	plan := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	completed, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       plan.Reference,
		TransitionID: "review",
		OutputValues: map[string]string{"summary": "approved plan"},
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode with transition parameter contract: %v", err)
	}
	if len(completed.Mutation.Created) != 1 {
		t.Fatalf("completion mutation = %+v, want review current node", completed.Mutation)
	}
	review := completed.Mutation.Created[0]
	if review.CurrentInputValues["summary"] != "approved plan" {
		t.Fatalf("review current inputs = %+v, want transition parameter materialized", review.CurrentInputValues)
	}
	if _, exists := review.CurrentInputValues["changes"]; exists {
		t.Fatalf("review current inputs = %+v, do not want target input field to replace transition contract", review.CurrentInputValues)
	}
}

func TestCompleteCurrentNodePreservesPathSpecificPriorParametersAcrossLoop(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createMaterializedCurrentNodeWorkflow(t, ctx, store)
	saveWorkflowGraphFixture(t, ctx, store, workflowID, func(def workflow.Definition, req *WorkflowGraphSaveRequest) {
		review := nodeByKey(t, def, "review")
		audit := nodeByKey(t, def, "audit")
		auditEdge := workflowGraphSaveEdgeRecord(
			t,
			req.Edges,
			testEdgeID("edge-audit-"+workflowID.String()),
		)
		auditEdge.PromptTemplate = "Audit {{.Params.summary}}."
		auditEdge.Parameters = []workflow.Parameter{{
			Key:         "summary",
			Description: "Audit findings.", Purpose: workflow.ParameterPurposeOrdinary,
		}}
		reworkGroupID := testTransitionGroupID("group-rework-" + workflowID.String())
		req.TransitionGroups = append(req.TransitionGroups, TransitionGroupRecord{
			ID:           reworkGroupID,
			WorkflowID:   workflowID,
			SourceNodeID: workflow.NodeIDOf(audit),
			TransitionID: "rework",
			DisplayName:  "Rework",
		})
		req.Edges = append(req.Edges, EdgeRecord{
			ID:                testEdgeID("edge-rework-" + workflowID.String()),
			WorkflowID:        workflowID,
			TransitionGroupID: reworkGroupID,
			Key:               "rework",
			TargetNodeID:      workflow.NodeIDOf(review),
			ContextMode:       workflow.ContextModeNewSession,
			PromptTemplate:    "Rework {{.Params.audit.summary}}.", AssigneeSelection: workflow.AssigneeSelectionConfigured, ThinkingSelection: workflow.ThinkingSelectionConfigured,
		})
	})
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)

	plan := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	review, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       plan.Reference,
		TransitionID: "review",
		OutputValues: map[string]string{"summary": "approved plan"},
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode plan: %v", err)
	}
	audit, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       review.Mutation.Created[0].Reference,
		TransitionID: "audit",
		OutputValues: map[string]string{"summary": "blocking findings"},
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode review: %v", err)
	}
	if audit.Mutation.Created[0].PriorValues.TransitionParameters["audit"]["summary"] != "blocking findings" {
		t.Fatalf("audit prior values = %+v, want path-specific findings", audit.Mutation.Created[0].PriorValues)
	}
	reworked, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       audit.Mutation.Created[0].Reference,
		TransitionID: "rework",
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode audit: %v", err)
	}
	if reworked.Mutation.Created[0].PriorValues.TransitionParameters["audit"]["summary"] != "blocking findings" {
		t.Fatalf("reworked prior values = %+v, want path-specific findings preserved across loop", reworked.Mutation.Created[0].PriorValues)
	}
}

func TestCompleteCurrentNodeJoinCarriesPriorParametersAndMaterializesJoinOutput(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createFanoutJoinWorkflow(t, ctx, store)
	saveWorkflowGraphFixture(t, ctx, store, workflowID, func(def workflow.Definition, req *WorkflowGraphSaveRequest) {
		synth := nodeByKey(t, def, "synth")
		done := nodeByKind(t, def, workflow.NodeKindTerminal)
		auditID := testNodeID("node-audit-" + workflowID.String())
		auditGroupID := testTransitionGroupID("group-audit-" + workflowID.String())
		req.Nodes = append(req.Nodes, NodeRecord{
			ID:           auditID,
			WorkflowID:   workflowID,
			Key:          "audit",
			Kind:         workflow.NodeKindAgent,
			DisplayName:  "Audit",
			SubagentRole: "coder",
		})
		workflowGraphSaveEdgeRecord(
			t,
			req.Edges,
			testEdgeID("edge-join-synth-"+workflowID.String()),
		).PromptTemplate = "Synthesize {{.Params.joined}} from {{.Params.split.summary}} and {{.Params.split.commentary}}."
		for index := range req.TransitionGroups {
			if req.TransitionGroups[index].SourceNodeID == workflow.NodeIDOf(synth) {
				req.TransitionGroups[index].TransitionID = "audit"
				req.TransitionGroups[index].DisplayName = "Audit"
			}
		}
		for index := range req.Edges {
			if req.Edges[index].TransitionGroupID != testTransitionGroupID("group-synth-done-"+workflowID.String()) {
				continue
			}
			req.Edges[index].Key = "audit"
			req.Edges[index].TargetNodeID = auditID
			req.Edges[index].PromptTemplate = "Audit {{.Params.synthesize.joined}} from {{.Params.split.summary}} and {{.Params.split.commentary}}."
		}
		req.TransitionGroups = append(req.TransitionGroups, TransitionGroupRecord{
			ID:           auditGroupID,
			WorkflowID:   workflowID,
			SourceNodeID: auditID,
			TransitionID: "audit_done",
			DisplayName:  "Done",
		})
		req.Edges = append(req.Edges, EdgeRecord{
			ID:                testEdgeID("edge-audit-done-" + workflowID.String()),
			WorkflowID:        workflowID,
			TransitionGroupID: auditGroupID,
			Key:               "done",
			TargetNodeID:      workflow.NodeIDOf(done),
			ContextMode:       workflow.ContextModeNewSession, AssigneeSelection: workflow.AssigneeSelectionConfigured, ThinkingSelection: workflow.ThinkingSelectionConfigured,
		})
	})
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)

	plan := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	split, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       plan.Reference,
		TransitionID: "split",
		OutputValues: map[string]string{"summary": "approved plan"},
		Commentary:   "implementation handoff",
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode split: %v", err)
	}
	branches := make(map[workflow.TransitionBranchKey]workflow.CurrentNode, len(split.Mutation.Created))
	for _, branch := range split.Mutation.Created {
		branchKey, present := branch.Reference.TransitionBranchKey()
		if !present {
			t.Fatalf("fanout branch = %+v, want branch scope", branch)
		}
		branches[branchKey] = branch
	}
	if _, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       branches["split_a"].Reference,
		TransitionID: "join_a",
		OutputValues: map[string]string{"joined": "joined implementation"},
	}); err != nil {
		t.Fatalf("CompleteCurrentNode join A: %v", err)
	}
	joined, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       branches["split_b"].Reference,
		TransitionID: "join_b",
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode join B: %v", err)
	}
	if len(joined.Mutation.Created) != 1 {
		t.Fatalf("join completion mutation = %+v, want synth current node", joined.Mutation)
	}
	synth := joined.Mutation.Created[0]
	if synth.PriorValues.TransitionParameters["split"]["summary"] != "approved plan" {
		t.Fatalf("synth prior parameter values = %+v, want pre-fanout split output retained across join", synth.PriorValues)
	}
	if synth.PriorValues.TransitionParameters["split"][workflow.RuntimePromptParameterCommentary] != "implementation handoff" {
		t.Fatalf("synth prior parameter values = %+v, want pre-fanout split commentary retained across join", synth.PriorValues)
	}
	if synth.PriorValues.TransitionParameters["synthesize"]["joined"] != "joined implementation" {
		t.Fatalf("synth prior parameter values = %+v, want join transition output under synthesize namespace", synth.PriorValues)
	}

	auditResult, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       synth.Reference,
		TransitionID: "audit",
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode synth: %v", err)
	}
	if len(auditResult.Mutation.Created) != 1 ||
		auditResult.Mutation.Created[0].PriorValues.TransitionParameters["split"]["summary"] != "approved plan" ||
		auditResult.Mutation.Created[0].PriorValues.TransitionParameters["split"][workflow.RuntimePromptParameterCommentary] != "implementation handoff" ||
		auditResult.Mutation.Created[0].PriorValues.TransitionParameters["synthesize"]["joined"] != "joined implementation" {
		t.Fatalf("audit current node = %+v, want propagated pre-fanout and join transition outputs", auditResult.Mutation.Created)
	}
}

func TestResolveCurrentNodeStartContextResolvesRequiredJoinPredecessorSessionIDs(t *testing.T) {
	ctx, store, binding, cfg := newTestStoreWithConfigContext(t)
	workflowID := createFanoutJoinWorkflow(t, ctx, store)
	saveWorkflowGraphFixture(t, ctx, store, workflowID, func(_ workflow.Definition, req *WorkflowGraphSaveRequest) {
		workflowGraphSaveEdgeRecord(
			t,
			req.Edges,
			testEdgeID("edge-join-synth-"+workflowID.String()),
		).PromptTemplate = "Synthesize {{.Params.join_a.session_id}} and {{.Params.join_b.session_id}}."
	})
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	plan := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	splitResult, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       plan.Reference,
		TransitionID: "split",
		OutputValues: map[string]string{"summary": "approved plan"},
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode split: %v", err)
	}
	branches := make(map[workflow.TransitionBranchKey]workflow.CurrentNode, len(splitResult.Mutation.Created))
	for _, branch := range splitResult.Mutation.Created {
		branchKey, present := branch.Reference.TransitionBranchKey()
		if !present {
			t.Fatalf("fanout branch = %+v, want branch scope", branch)
		}
		branches[branchKey] = branch
	}
	associateTaskSessionForTest(
		t,
		ctx,
		store,
		binding,
		cfg,
		branches["split_a"].Reference,
		time.UnixMilli(1_700_000_000_000).UTC(),
	)
	branchASessionID := associateTaskSessionForTest(
		t,
		ctx,
		store,
		binding,
		cfg,
		branches["split_a"].Reference,
		time.UnixMilli(1_700_000_001_000).UTC(),
	)
	branchBSessionID := associateTaskSessionForTest(
		t,
		ctx,
		store,
		binding,
		cfg,
		branches["split_b"].Reference,
		time.UnixMilli(1_700_000_002_000).UTC(),
	)
	if _, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       branches["split_a"].Reference,
		TransitionID: "join_a",
		OutputValues: map[string]string{"joined": "joined A"},
	}); err != nil {
		t.Fatalf("CompleteCurrentNode branch A: %v", err)
	}
	joinResult, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       branches["split_b"].Reference,
		TransitionID: "join_b",
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode branch B: %v", err)
	}
	if len(joinResult.Mutation.Created) != 1 {
		t.Fatalf("join completion = %+v, want synthesize Current Node", joinResult.Mutation.Created)
	}
	startContext, err := store.ResolveCurrentNodeStartContext(ctx, joinResult.Mutation.Created[0].Reference)
	if err != nil {
		t.Fatalf("ResolveCurrentNodeStartContext synthesize: %v", err)
	}
	if startContext.PriorSessionIDs["join_a"] == nil ||
		*startContext.PriorSessionIDs["join_a"] != branchASessionID ||
		startContext.PriorSessionIDs["join_b"] == nil ||
		*startContext.PriorSessionIDs["join_b"] != branchBSessionID {
		t.Fatalf(
			"join predecessor Session IDs = %+v, want join_a=%q and join_b=%q",
			startContext.PriorSessionIDs,
			branchASessionID,
			branchBSessionID,
		)
	}
}

func TestResolveCurrentNodeStartContextResolvesFanoutSourceSessionID(t *testing.T) {
	ctx, store, binding, cfg := newTestStoreWithConfigContext(t)
	workflowID := createFanoutJoinWorkflow(t, ctx, store)
	saveWorkflowGraphFixture(t, ctx, store, workflowID, func(_ workflow.Definition, req *WorkflowGraphSaveRequest) {
		workflowGraphSaveEdgeRecord(
			t,
			req.Edges,
			testEdgeID("edge-split-a-"+workflowID.String()),
		).PromptTemplate = "A {{.SessionId}}."
		workflowGraphSaveEdgeRecord(
			t,
			req.Edges,
			testEdgeID("edge-split-b-"+workflowID.String()),
		).PromptTemplate = "B {{.SessionId}}."
	})
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	plan := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	sourceSessionID := associateTaskSessionForTest(t, ctx, store, binding, cfg, plan.Reference, time.UnixMilli(1_700_000_000_000).UTC())
	splitResult, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       plan.Reference,
		TransitionID: "split",
		OutputValues: map[string]string{"summary": "approved plan"},
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode split: %v", err)
	}
	for _, branch := range splitResult.Mutation.Created {
		startContext, err := store.ResolveCurrentNodeStartContext(ctx, branch.Reference)
		if err != nil {
			t.Fatalf("ResolveCurrentNodeStartContext %v: %v", branch.Reference, err)
		}
		if startContext.PromptSessionID == nil || *startContext.PromptSessionID != sourceSessionID {
			t.Fatalf(
				"fanout branch %v prompt source Session ID = %v, want fanout source Session %q",
				branch.Reference,
				startContext.PromptSessionID,
				sourceSessionID,
			)
		}
	}
}

func TestResolveCurrentNodeStartContextPreservesEarlierFanoutSessionAcrossLaterFanoutBranches(t *testing.T) {
	ctx, store, binding, cfg := newTestStoreWithConfigContext(t)
	workflowID := createSequentialFanoutSessionReferenceWorkflow(t, ctx, store)
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	plan := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	associateTaskSessionForTest(t, ctx, store, binding, cfg, plan.Reference, time.UnixMilli(1_700_000_000_000).UTC())

	firstSplit, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       plan.Reference,
		TransitionID: "split",
		OutputValues: map[string]string{"summary": "approved plan"},
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode first split: %v", err)
	}
	firstBranches := make(map[workflow.TransitionBranchKey]workflow.CurrentNode, len(firstSplit.Mutation.Created))
	for _, branch := range firstSplit.Mutation.Created {
		branchKey, present := branch.Reference.TransitionBranchKey()
		if !present {
			t.Fatalf("first fanout branch = %+v, want branch scope", branch)
		}
		firstBranches[branchKey] = branch
	}
	firstBranchASessionID := associateTaskSessionForTest(
		t,
		ctx,
		store,
		binding,
		cfg,
		firstBranches["split_a"].Reference,
		time.UnixMilli(1_700_000_001_000).UTC(),
	)
	associateTaskSessionForTest(
		t,
		ctx,
		store,
		binding,
		cfg,
		firstBranches["split_b"].Reference,
		time.UnixMilli(1_700_000_002_000).UTC(),
	)
	if _, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       firstBranches["split_a"].Reference,
		TransitionID: "join_a",
		OutputValues: map[string]string{"joined": "joined A"},
	}); err != nil {
		t.Fatalf("CompleteCurrentNode first branch A: %v", err)
	}
	firstJoin, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       firstBranches["split_b"].Reference,
		TransitionID: "join_b",
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode first branch B: %v", err)
	}
	if len(firstJoin.Mutation.Created) != 1 {
		t.Fatalf("first Join completion = %+v, want synthesize Current Node", firstJoin.Mutation.Created)
	}
	secondSplit, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       firstJoin.Mutation.Created[0].Reference,
		TransitionID: "verify",
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode second split: %v", err)
	}
	if len(secondSplit.Mutation.Created) != 2 {
		t.Fatalf("second fanout completion = %+v, want two verification branches", secondSplit.Mutation.Created)
	}
	for _, branch := range secondSplit.Mutation.Created {
		startContext, err := store.ResolveCurrentNodeStartContext(ctx, branch.Reference)
		if err != nil {
			t.Fatalf("ResolveCurrentNodeStartContext %v: %v", branch.Reference, err)
		}
		if startContext.PriorSessionIDs["join_a"] == nil ||
			*startContext.PriorSessionIDs["join_a"] != firstBranchASessionID {
			t.Fatalf(
				"second fanout branch %v prior Session IDs = %+v, want join_a=%q from earlier fanout",
				branch.Reference,
				startContext.PriorSessionIDs,
				firstBranchASessionID,
			)
		}
	}
}

func TestResolveCurrentNodeStartContextRendersEmptyForAmbiguousFanoutSessionScope(t *testing.T) {
	ctx, store, binding, cfg := newTestStoreWithConfigContext(t)
	workflowID := createFanoutJoinWorkflow(t, ctx, store)
	saveWorkflowGraphFixture(t, ctx, store, workflowID, func(def workflow.Definition, req *WorkflowGraphSaveRequest) {
		plan := nodeByKey(t, def, "plan")
		implA := nodeByKey(t, def, "impl_a")
		implB := nodeByKey(t, def, "impl_b")
		splitB := edgeByKey(t, def, "split_b")
		workflowGraphSaveEdgeRecord(t, req.Edges, splitB.ID).TargetNodeID = workflow.NodeIDOf(implA)
		shortcutGroupID := testTransitionGroupID("group-plan-shortcut-" + workflowID.String())
		req.TransitionGroups = append(req.TransitionGroups, TransitionGroupRecord{
			ID:           shortcutGroupID,
			WorkflowID:   workflowID,
			SourceNodeID: workflow.NodeIDOf(plan),
			TransitionID: "shortcut",
			DisplayName:  "Shortcut",
		})
		req.Edges = append(req.Edges, EdgeRecord{
			ID:                testEdgeID("edge-plan-shortcut-" + workflowID.String()),
			WorkflowID:        workflowID,
			TransitionGroupID: shortcutGroupID,
			Key:               "shortcut",
			TargetNodeID:      workflow.NodeIDOf(implB),
			AssigneeSelection: workflow.AssigneeSelectionConfigured,
			ThinkingSelection: workflow.ThinkingSelectionConfigured,
			ContextMode:       workflow.ContextModeNewSession,
			PromptTemplate:    "Shortcut.",
		})
		synthesize := edgeByKey(t, def, "synth")
		workflowGraphSaveEdgeRecord(t, req.Edges, synthesize.ID).PromptTemplate = "Synthesize {{.Params.join_a.session_id}}."
	})
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	plan := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	splitResult, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       plan.Reference,
		TransitionID: "split",
		OutputValues: map[string]string{"summary": "approved plan"},
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode split: %v", err)
	}
	branches := make(map[workflow.TransitionBranchKey]workflow.CurrentNode, len(splitResult.Mutation.Created))
	for _, branch := range splitResult.Mutation.Created {
		branchKey, present := branch.Reference.TransitionBranchKey()
		if !present {
			t.Fatalf("fanout branch = %+v, want branch scope", branch)
		}
		branches[branchKey] = branch
	}
	associateTaskSessionForTest(
		t,
		ctx,
		store,
		binding,
		cfg,
		branches["split_a"].Reference,
		time.UnixMilli(1_700_000_001_000).UTC(),
	)
	associateTaskSessionForTest(
		t,
		ctx,
		store,
		binding,
		cfg,
		branches["split_b"].Reference,
		time.UnixMilli(1_700_000_002_000).UTC(),
	)
	if _, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       branches["split_a"].Reference,
		TransitionID: "join_a",
		OutputValues: map[string]string{"joined": "joined A"},
	}); err != nil {
		t.Fatalf("CompleteCurrentNode branch A: %v", err)
	}
	joinResult, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       branches["split_b"].Reference,
		TransitionID: "join_a",
		OutputValues: map[string]string{"joined": "joined B"},
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode branch B: %v", err)
	}
	if len(joinResult.Mutation.Created) != 1 {
		t.Fatalf("Join completion = %+v, want synthesize Current Node", joinResult.Mutation.Created)
	}
	startContext, err := store.ResolveCurrentNodeStartContext(ctx, joinResult.Mutation.Created[0].Reference)
	if err != nil {
		t.Fatalf("ResolveCurrentNodeStartContext synthesize: %v", err)
	}
	if startContext.PriorSessionIDs["join_a"] != nil {
		t.Fatalf(
			"ambiguous fanout Session ID = %q, want empty lookup",
			*startContext.PriorSessionIDs["join_a"],
		)
	}
}

func TestCompleteCurrentNodeJoinDerivesProvidersFromThreeIncomingBranches(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createFanoutJoinWorkflow(t, ctx, store)
	saveWorkflowGraphFixture(t, ctx, store, workflowID, func(def workflow.Definition, req *WorkflowGraphSaveRequest) {
		branchCID := testNodeID("node-impl-c-" + workflowID.String())
		branchCGroupID := testTransitionGroupID("group-join-c-" + workflowID.String())
		workflowGraphSaveEdgeRecord(
			t,
			req.Edges,
			testEdgeID("edge-join-synth-"+workflowID.String()),
		).PromptTemplate = "Synthesize {{.Params.joined}} {{.Params.compliance_findings}}."
		req.Nodes = append(req.Nodes, NodeRecord{
			ID:           branchCID,
			WorkflowID:   workflowID,
			Key:          "impl_c",
			Kind:         workflow.NodeKindAgent,
			DisplayName:  "Implement C",
			SubagentRole: "coder",
		})
		req.TransitionGroups = append(req.TransitionGroups, TransitionGroupRecord{
			ID:           branchCGroupID,
			WorkflowID:   workflowID,
			SourceNodeID: branchCID,
			TransitionID: "join_c",
			DisplayName:  "Join",
		})
		req.Edges = append(req.Edges,
			EdgeRecord{
				ID:                testEdgeID("edge-split-c-" + workflowID.String()),
				WorkflowID:        workflowID,
				TransitionGroupID: testTransitionGroupID("group-split-" + workflowID.String()),
				Key:               "split_c",
				TargetNodeID:      branchCID,
				ContextMode:       workflow.ContextModeNewSession,
				PromptTemplate:    "C.", AssigneeSelection: workflow.AssigneeSelectionConfigured, ThinkingSelection: workflow.ThinkingSelectionConfigured,
			},
			EdgeRecord{
				ID:                testEdgeID("edge-join-c-" + workflowID.String()),
				WorkflowID:        workflowID,
				TransitionGroupID: branchCGroupID,
				Key:               "join_c",
				TargetNodeID:      workflow.NodeIDOf(nodeByKey(t, def, "join")),
				ContextMode:       workflow.ContextModeNewSession,
				Parameters: []workflow.Parameter{{
					Key:         "compliance_findings",
					Description: "Compliance findings.", Purpose: workflow.ParameterPurposeOrdinary,
				}}, AssigneeSelection: workflow.AssigneeSelectionConfigured, ThinkingSelection: workflow.ThinkingSelectionConfigured,
			},
		)
	})
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)

	plan := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	split, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       plan.Reference,
		TransitionID: "split",
		OutputValues: map[string]string{"summary": "approved plan"},
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode split: %v", err)
	}
	branches := make(map[workflow.TransitionBranchKey]workflow.CurrentNode, len(split.Mutation.Created))
	for _, branch := range split.Mutation.Created {
		branchKey, present := branch.Reference.TransitionBranchKey()
		if !present {
			t.Fatalf("fanout branch = %+v, want branch scope", branch)
		}
		branches[branchKey] = branch
	}
	if _, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       branches["split_a"].Reference,
		TransitionID: "join_a",
		OutputValues: map[string]string{"joined": "joined implementation"},
	}); err != nil {
		t.Fatalf("CompleteCurrentNode join A: %v", err)
	}
	if _, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       branches["split_b"].Reference,
		TransitionID: "join_b",
	}); err != nil {
		t.Fatalf("CompleteCurrentNode join B: %v", err)
	}
	joined, err := store.CompleteCurrentNode(ctx, CurrentNodeCompletionRequest{
		Source:       branches["split_c"].Reference,
		TransitionID: "join_c",
		OutputValues: map[string]string{"compliance_findings": "approved"},
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode join C with incomplete stored provider map: %v", err)
	}
	if len(joined.Mutation.Created) != 1 ||
		joined.Mutation.Created[0].CurrentInputValues["joined"] != "joined implementation" ||
		joined.Mutation.Created[0].CurrentInputValues["compliance_findings"] != "approved" {
		t.Fatalf("joined Current Node = %+v, want Join aggregate materialized without target input fields", joined.Mutation.Created)
	}
}

func createMaterializedCurrentNodeWorkflow(t *testing.T, ctx context.Context, store *Store) runtimeids.WorkflowID {
	t.Helper()
	created, err := store.CreateWorkflow(ctx, CreateWorkflowRequest{Name: "Materialized Current Node Values"})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	planID := testNodeID("node-plan-" + created.ID.String())
	reviewID := testNodeID("node-review-" + created.ID.String())
	auditID := testNodeID("node-audit-" + created.ID.String())
	startGroupID := testTransitionGroupID("group-start-" + created.ID.String())
	reviewGroupID := testTransitionGroupID("group-review-" + created.ID.String())
	auditGroupID := testTransitionGroupID("group-audit-" + created.ID.String())
	doneGroupID := testTransitionGroupID("group-done-" + created.ID.String())
	saveWorkflowGraphFixture(t, ctx, store, created.ID, func(def workflow.Definition, req *WorkflowGraphSaveRequest) {
		start := nodeByKind(t, def, workflow.NodeKindStart)
		done := nodeByKind(t, def, workflow.NodeKindTerminal)
		req.Nodes = append(req.Nodes,
			NodeRecord{
				ID:           planID,
				WorkflowID:   created.ID,
				Key:          "plan",
				Kind:         workflow.NodeKindAgent,
				DisplayName:  "Plan",
				SubagentRole: "coder",
			},
			NodeRecord{
				ID:           reviewID,
				WorkflowID:   created.ID,
				Key:          "review",
				Kind:         workflow.NodeKindAgent,
				DisplayName:  "Review",
				SubagentRole: "coder",
			},
			NodeRecord{
				ID:           auditID,
				WorkflowID:   created.ID,
				Key:          "audit",
				Kind:         workflow.NodeKindAgent,
				DisplayName:  "Audit",
				SubagentRole: "coder",
			},
		)
		req.TransitionGroups = append(req.TransitionGroups,
			TransitionGroupRecord{ID: startGroupID, WorkflowID: created.ID, SourceNodeID: workflow.NodeIDOf(start), TransitionID: "start", DisplayName: "Start"},
			TransitionGroupRecord{ID: reviewGroupID, WorkflowID: created.ID, SourceNodeID: planID, TransitionID: "review", DisplayName: "Review"},
			TransitionGroupRecord{ID: auditGroupID, WorkflowID: created.ID, SourceNodeID: reviewID, TransitionID: "audit", DisplayName: "Audit"},
			TransitionGroupRecord{ID: doneGroupID, WorkflowID: created.ID, SourceNodeID: auditID, TransitionID: "done", DisplayName: "Done"},
		)
		req.Edges = append(req.Edges,
			EdgeRecord{ID: testEdgeID("edge-start-" + created.ID.String()), WorkflowID: created.ID, TransitionGroupID: startGroupID, Key: "start", TargetNodeID: planID, ContextMode: workflow.ContextModeNewSession, PromptTemplate: "Plan the work.", AssigneeSelection: workflow.AssigneeSelectionConfigured, ThinkingSelection: workflow.ThinkingSelectionConfigured},
			EdgeRecord{ID: testEdgeID("edge-review-" + created.ID.String()), WorkflowID: created.ID, TransitionGroupID: reviewGroupID, Key: "review", TargetNodeID: reviewID, ContextMode: workflow.ContextModeNewSession, PromptTemplate: "Review {{.Params.summary}}.", Parameters: []workflow.Parameter{{Key: "summary", Description: "Review summary.", Purpose: workflow.ParameterPurposeOrdinary}}, AssigneeSelection: workflow.AssigneeSelectionConfigured, ThinkingSelection: workflow.ThinkingSelectionConfigured},
			EdgeRecord{ID: testEdgeID("edge-audit-" + created.ID.String()), WorkflowID: created.ID, TransitionGroupID: auditGroupID, Key: "audit", TargetNodeID: auditID, ContextMode: workflow.ContextModeNewSession, PromptTemplate: "Audit {{.Params.review.summary}}.", AssigneeSelection: workflow.AssigneeSelectionConfigured, ThinkingSelection: workflow.ThinkingSelectionConfigured},
			EdgeRecord{ID: testEdgeID("edge-done-" + created.ID.String()), WorkflowID: created.ID, TransitionGroupID: doneGroupID, Key: "done", TargetNodeID: workflow.NodeIDOf(done), ContextMode: workflow.ContextModeNewSession, AssigneeSelection: workflow.AssigneeSelectionConfigured, ThinkingSelection: workflow.ThinkingSelectionConfigured},
		)
	})
	return created.ID
}
