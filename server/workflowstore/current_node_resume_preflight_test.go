package workflowstore

import (
	"errors"
	"reflect"
	"testing"

	"core/server/workflow"
)

func TestTaskResumeMissingOrdinaryInputDoesNotBlockParallelSibling(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createFanoutJoinWorkflow(t, ctx, store)
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	source := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	completed, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source: source.Reference, TransitionID: "split",
		OutputValues: map[string]string{"summary": "saved summary"},
	})
	if err != nil {
		t.Fatal(err)
	}
	branches := completed.Mutation.Created
	if len(branches) != 2 {
		t.Fatalf("expected two branches: %+v", branches)
	}
	for _, branch := range branches {
		if err := store.InterruptCurrentNode(ctx, branch.Reference,
			workflow.CurrentNodeInterruptionReasonUserInterrupt,
			workflow.CurrentNodeInterruptionDetail{Code: string(workflow.CurrentNodeInterruptionReasonUserInterrupt)}); err != nil {
			t.Fatal(err)
		}
	}
	definition, _, err := store.GetDefinition(ctx, workflowID)
	if err != nil {
		t.Fatal(err)
	}
	edge, err := currentNodeDefinitionEnteringEdge(definition, branches[0])
	if err != nil {
		t.Fatal(err)
	}
	parameters := append(edge.Parameters, workflow.Parameter{Key: "risk", Description: "Risk.", Purpose: workflow.ParameterPurposeOrdinary})
	raw, err := marshalJSONArray(parameters)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE workflow_edges SET parameters_json = ? WHERE id = ?`, raw, string(edge.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PlanTaskResume(ctx, task.ID, []workflow.CurrentNodeReference{branches[0].Reference}, nil); err == nil {
		t.Fatal("Resume accepted a missing ordinary Parameter")
	} else {
		var validation *CurrentNodeResumeValidationError
		if !errors.As(err, &validation) || len(validation.Diagnostics) != 1 || validation.Diagnostics[0].ParameterKey != "risk" {
			t.Fatalf("expected typed missing-risk error: %v", err)
		}
	}
	plan, err := store.PlanTaskResume(ctx, task.ID, []workflow.CurrentNodeReference{branches[1].Reference}, nil)
	if err != nil {
		t.Fatalf("Resume blocked independent sibling: %v", err)
	}
	if _, err := store.CommitTaskResume(ctx, plan, plannedSessionsForStoreTest(t, ctx, store, plan.StartContexts())); err != nil {
		t.Fatal(err)
	}
}

func TestTaskResumePreservesSavedSelectionAfterIncomingSelectorEdits(t *testing.T) {
	for _, edit := range []string{"assignee", "thinking", "deleted fallback", "deleted saved role"} {
		t.Run(edit, func(t *testing.T) {
			ctx, store, binding := newTestStoreContext(t)
			workflowID := createMaterializedCurrentNodeWorkflow(t, ctx, store)
			linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
			task := createDefaultTask(t, ctx, store, binding.ProjectID)
			source := startTask(t, ctx, store, task.ID).Mutation.Created[0]
			completed, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
				Source: source.Reference, TransitionID: "review",
				OutputValues: map[string]string{"summary": "saved summary"},
			})
			if err != nil {
				t.Fatal(err)
			}
			current := completed.Mutation.Created[0]
			if err := store.InterruptCurrentNode(ctx, current.Reference,
				workflow.CurrentNodeInterruptionReasonUserInterrupt,
				workflow.CurrentNodeInterruptionDetail{Code: string(workflow.CurrentNodeInterruptionReasonUserInterrupt)}); err != nil {
				t.Fatal(err)
			}
			switch edit {
			case "assignee", "thinking":
				parameters := []workflow.Parameter{{Key: "summary", Description: "Summary.", Purpose: workflow.ParameterPurposeOrdinary}}
				assignee, thinking := workflow.AssigneeSelectionConfigured, workflow.ThinkingSelectionConfigured
				if edit == "assignee" {
					assignee = workflow.AssigneeSelectionPreviousNode
					parameters = append(parameters, workflow.Parameter{Key: "role", Description: "Assignee.", Purpose: workflow.ParameterPurposeTargetAssignee})
				} else {
					thinking = workflow.ThinkingSelectionPreviousNode
					parameters = append(parameters, workflow.Parameter{Key: "effort", Description: "Thinking.", Purpose: workflow.ParameterPurposeTargetThinking})
				}
				raw, err := marshalJSONArray(parameters)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := store.db.ExecContext(ctx, `
UPDATE workflow_edges SET parameters_json = ?, assignee_selection = ?, thinking_selection = ? WHERE id = ?`,
					raw, string(assignee), string(thinking), string(*current.EnteredByEdgeID)); err != nil {
					t.Fatal(err)
				}
				if edit == "thinking" {
					if _, err := store.db.ExecContext(ctx, `UPDATE workflow_edges SET prompt_template = ? WHERE id = ?`,
						"Resume with {{.Params.effort}}.", string(*current.EnteredByEdgeID)); err != nil {
						t.Fatal(err)
					}
				}
			case "deleted fallback":
				if _, err := store.db.ExecContext(ctx, `UPDATE workflow_nodes SET subagent_role = ? WHERE id = ?`,
					"deleted-role", string(current.Reference.NodeID)); err != nil {
					t.Fatal(err)
				}
			case "deleted saved role":
				store.roleResolver = emptyTargetAgentCatalog{}
			}
			plan, err := store.PlanTaskResume(ctx, task.ID, []workflow.CurrentNodeReference{current.Reference}, nil)
			if err != nil {
				t.Fatalf("Resume after %s edit: %v", edit, err)
			}
			contexts := plan.StartContexts()
			if len(contexts) != 1 || !reflect.DeepEqual(contexts[0].CurrentNode.AgentExecutionSelection, current.AgentExecutionSelection) {
				t.Fatalf("Resume changed saved selection: %+v", contexts)
			}
			resumed, err := store.CommitTaskResume(ctx, plan, plannedSessionsForStoreTest(t, ctx, store, contexts))
			if err != nil {
				t.Fatal(err)
			}
			if len(resumed.CurrentNodes) != 1 ||
				!reflect.DeepEqual(resumed.CurrentNodes[0].CurrentInputValues, current.CurrentInputValues) ||
				!reflect.DeepEqual(resumed.CurrentNodes[0].PriorValues, current.PriorValues) ||
				!reflect.DeepEqual(resumed.CurrentNodes[0].AgentExecutionSelection, current.AgentExecutionSelection) {
				t.Fatalf("Resume rewrote materialized values: %+v", resumed.CurrentNodes)
			}
		})
	}
}

func TestPreflightTaskResumeRejectsEditedTransitionParameterWithoutMutatingCurrentNode(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createMaterializedCurrentNodeWorkflow(t, ctx, store)
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)

	plan := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	reviewResult, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source:       plan.Reference,
		TransitionID: "review",
		OutputValues: map[string]string{"summary": "approved plan"},
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode: %v", err)
	}
	review := reviewResult.Mutation.Created[0]
	if review.EnteredByEdgeID == nil {
		t.Fatal("review Current Node has no entering Edge")
	}
	if err := store.InterruptCurrentNode(
		ctx,
		review.Reference,
		workflow.CurrentNodeInterruptionReasonUserInterrupt,
		workflow.CurrentNodeInterruptionDetail{Code: string(workflow.CurrentNodeInterruptionReasonUserInterrupt)},
	); err != nil {
		t.Fatalf("InterruptCurrentNode: %v", err)
	}
	parameters, err := marshalJSONArray([]workflow.Parameter{
		{Key: "summary", Description: "Review summary.", Purpose: workflow.ParameterPurposeOrdinary},
		{Key: "risk", Description: "New risk.", Purpose: workflow.ParameterPurposeOrdinary},
	})
	if err != nil {
		t.Fatalf("marshal edited Transition Parameters: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `
UPDATE workflow_edges
SET parameters_json = ?
WHERE id = ?`, parameters, string(*review.EnteredByEdgeID)); err != nil {
		t.Fatalf("edit entering Transition Branch: %v", err)
	}

	classifications, err := store.PreflightTaskResume(ctx, task.ID)
	if err != nil {
		t.Fatalf("PreflightTaskResume: %v", err)
	}
	if len(classifications) != 1 || !classifications[0].CurrentNode.Reference.Equal(review.Reference) {
		t.Fatalf("classifications = %+v, want one classification for %v", classifications, review.Reference)
	}
	if len(classifications[0].Diagnostics) != 1 {
		t.Fatalf("resume diagnostics = %+v, want one missing-parameter diagnostic", classifications[0].Diagnostics)
	}
	diagnostic := classifications[0].Diagnostics[0]
	if diagnostic.Code != "workflow.resume.parameter_not_materialized" ||
		!diagnostic.CurrentNode.Equal(review.Reference) ||
		diagnostic.EnteringEdgeID != *review.EnteredByEdgeID ||
		diagnostic.ParameterKey != "risk" {
		t.Fatalf("resume diagnostic = %+v, want exact Current Node, entering Edge, and Parameter context", diagnostic)
	}
	currentNodes, err := store.ListCurrentNodes(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListCurrentNodes: %v", err)
	}
	if len(currentNodes) != 1 ||
		currentNodes[0].Scheduling == nil ||
		currentNodes[0].Scheduling.State != workflow.CurrentNodeSchedulingInterrupted {
		t.Fatalf("current nodes = %+v, want interrupted Current Node preserved", currentNodes)
	}
}

func TestWorkflowGraphSaveAllowsParameterEditForInterruptedCurrentNode(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createMaterializedCurrentNodeWorkflow(t, ctx, store)
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)

	plan := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	reviewResult, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source:       plan.Reference,
		TransitionID: "review",
		OutputValues: map[string]string{"summary": "approved plan"},
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode: %v", err)
	}
	review := reviewResult.Mutation.Created[0]
	if err := store.InterruptCurrentNode(
		ctx,
		review.Reference,
		workflow.CurrentNodeInterruptionReasonUserInterrupt,
		workflow.CurrentNodeInterruptionDetail{Code: string(workflow.CurrentNodeInterruptionReasonUserInterrupt)},
	); err != nil {
		t.Fatalf("InterruptCurrentNode: %v", err)
	}

	definition, record, err := store.GetDefinition(ctx, workflowID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	request := NewWorkflowGraphSaveRequest(definition, record.Version)
	edgeID := *review.EnteredByEdgeID
	foundEdge := false
	for index := range request.Edges {
		if request.Edges[index].ID != edgeID {
			continue
		}
		foundEdge = true
		request.Edges[index].Parameters = append(request.Edges[index].Parameters, workflow.Parameter{
			Key:         "risk",
			Description: "New risk.",
			Purpose:     workflow.ParameterPurposeOrdinary,
		})
	}
	if !foundEdge {
		t.Fatalf("workflow edge %q not found in save request", edgeID)
	}
	saved, err := store.SaveWorkflowGraph(ctx, request)
	if err != nil {
		t.Fatalf("SaveWorkflowGraph parameter edit: %v", err)
	}
	if !saved.Saved || workflowGraphSaveBlockerCount(saved.Blockers, "active_transition_contract_changed") != 0 {
		t.Fatalf("SaveWorkflowGraph parameter edit = %+v, want saved without active-transition blocker", saved)
	}
	classifications, err := store.PreflightTaskResume(ctx, task.ID)
	if err != nil {
		t.Fatalf("PreflightTaskResume: %v", err)
	}
	if len(classifications) != 1 || len(classifications[0].Diagnostics) != 1 {
		t.Fatalf("resume classifications = %+v, want one missing-Parameter diagnostic", classifications)
	}
	diagnostic := classifications[0].Diagnostics[0]
	if diagnostic.Code != CurrentNodeResumeParameterNotMaterializedCode ||
		!diagnostic.CurrentNode.Equal(review.Reference) ||
		diagnostic.EnteringEdgeID != edgeID ||
		diagnostic.ParameterKey != "risk" {
		t.Fatalf("resume diagnostic = %+v, want exact Current Node, entering Edge, and Parameter context", diagnostic)
	}
}

func TestWorkflowGraphSaveBlocksRetargetingEnteringEdgeReferencedByCurrentNode(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createMaterializedCurrentNodeWorkflow(t, ctx, store)
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)

	plan := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	reviewResult, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source:       plan.Reference,
		TransitionID: "review",
		OutputValues: map[string]string{"summary": "approved plan"},
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode: %v", err)
	}
	review := reviewResult.Mutation.Created[0]
	if review.EnteredByEdgeID == nil {
		t.Fatal("review Current Node has no entering Edge")
	}

	definition, record, err := store.GetDefinition(ctx, workflowID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	request := NewWorkflowGraphSaveRequest(definition, record.Version)
	gateNodeID := testNodeID("node-retarget-gate-" + workflowID.String())
	gateGroupID := testTransitionGroupID("group-retarget-gate-" + workflowID.String())
	gateEdgeID := testEdgeID("edge-retarget-gate-" + workflowID.String())
	request.Nodes = append(request.Nodes, NodeRecord{
		ID:           gateNodeID,
		WorkflowID:   workflowID,
		Key:          "retarget_gate",
		Kind:         workflow.NodeKindAgent,
		DisplayName:  "Retarget Gate",
		SubagentRole: "coder",
	})
	request.TransitionGroups = append(request.TransitionGroups, TransitionGroupRecord{
		ID:           gateGroupID,
		WorkflowID:   workflowID,
		SourceNodeID: gateNodeID,
		TransitionID: "enter_review",
		DisplayName:  "Enter Review",
	})
	request.Edges = append(request.Edges, EdgeRecord{
		ID:                gateEdgeID,
		WorkflowID:        workflowID,
		TransitionGroupID: gateGroupID,
		Key:               "review",
		TargetNodeID:      review.Reference.NodeID,
		ContextMode:       workflow.ContextModeNewSession,
		PromptTemplate:    "Review the gated plan.",
		AssigneeSelection: workflow.AssigneeSelectionConfigured,
		ThinkingSelection: workflow.ThinkingSelectionConfigured,
	})
	retargeted := false
	for index := range request.Edges {
		if request.Edges[index].ID != *review.EnteredByEdgeID {
			continue
		}
		request.Edges[index].TargetNodeID = gateNodeID
		request.Edges[index].PromptTemplate = "Validate {{.Params.summary}}."
		retargeted = true
	}
	if !retargeted {
		t.Fatalf("workflow edge %q not found in save request", *review.EnteredByEdgeID)
	}

	preview, err := store.PreviewWorkflowGraphSave(ctx, request)
	if err != nil {
		t.Fatalf("PreviewWorkflowGraphSave: %v", err)
	}
	if workflowGraphSaveBlockerCount(preview.Blockers, "task_referenced_edge_group_changed") != 1 {
		t.Fatalf("retarget preview = %+v, want referenced-edge routing blocker", preview)
	}
	saved, err := store.SaveWorkflowGraph(ctx, request)
	if err != nil {
		t.Fatalf("SaveWorkflowGraph: %v", err)
	}
	if saved.Saved {
		t.Fatalf("retarget save = %+v, want blocked graph mutation", saved)
	}
	unchanged, _, err := store.GetDefinition(ctx, workflowID)
	if err != nil {
		t.Fatalf("GetDefinition after blocked save: %v", err)
	}
	persistedEdgeFound := false
	for _, edge := range unchanged.Edges {
		if edge.ID == *review.EnteredByEdgeID {
			persistedEdgeFound = true
			if edge.TargetNodeID != review.Reference.NodeID {
				t.Fatalf(
					"referenced Edge target after blocked save = %q, want %q",
					edge.TargetNodeID,
					review.Reference.NodeID,
				)
			}
			break
		}
	}
	if !persistedEdgeFound {
		t.Fatalf("referenced Edge %q missing after blocked save", *review.EnteredByEdgeID)
	}
}

func TestPreflightTaskResumeRejectsEditedJoinDerivedParameter(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createFanoutJoinWorkflow(t, ctx, store)
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)

	startTask(t, ctx, store, task.ID)
	definition, _, err := store.GetDefinition(ctx, workflowID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	synthID := workflow.NodeIDOf(nodeByKey(t, definition, "synth"))
	synth, err := workflow.NewCurrentNodeReference(task.ID, synthID, nil)
	if err != nil {
		t.Fatalf("NewCurrentNodeReference: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `DELETE FROM task_current_nodes WHERE task_id = ?`, task.ID); err != nil {
		t.Fatalf("delete seeded Current Node: %v", err)
	}
	enteringEdgeID := testEdgeID("edge-join-synth-" + workflowID.String())
	if _, err := store.db.ExecContext(ctx, `
INSERT INTO task_current_nodes (
    task_id, node_id, current_input_values_json, prior_node_values_json,
    scheduling_state, interruption_reason, interruption_detail_json, interrupted_at_unix_ms, entered_by_edge_id,
    effective_assignee, assignee_origin
) VALUES (?, ?, '{"joined":"existing"}', '{"transition_parameters":{}}', 'interrupted', 'user_interrupt', '{}', 1, ?, 'default', 'configured_fallback')`,
		task.ID,
		string(synthID),
		string(enteringEdgeID),
	); err != nil {
		t.Fatalf("seed Join Current Node: %v", err)
	}
	parameters, err := marshalJSONArray([]workflow.Parameter{
		{Key: "joined", Description: "Joined branch summary.", Purpose: workflow.ParameterPurposeOrdinary},
		{Key: "risk", Description: "New derived requirement.", Purpose: workflow.ParameterPurposeOrdinary},
	})
	if err != nil {
		t.Fatalf("marshal edited Join Parameters: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `
UPDATE workflow_edges
SET parameters_json = ?
WHERE id = ?`, parameters, string(testEdgeID("edge-join-a-"+workflowID.String()))); err != nil {
		t.Fatalf("edit Join incoming Transition Branch: %v", err)
	}

	classifications, err := store.PreflightTaskResume(ctx, task.ID)
	if err != nil {
		t.Fatalf("PreflightTaskResume: %v", err)
	}
	if len(classifications) != 1 || len(classifications[0].Diagnostics) != 1 {
		t.Fatalf("Join resume classifications = %+v, want one missing derived binding", classifications)
	}
	diagnostic := classifications[0].Diagnostics[0]
	if diagnostic.Code != CurrentNodeResumeParameterNotMaterializedCode ||
		!diagnostic.CurrentNode.Equal(synth) ||
		diagnostic.EnteringEdgeID != enteringEdgeID ||
		diagnostic.ParameterKey != "risk" {
		t.Fatalf("Join resume diagnostic = %+v, want exact derived binding context", diagnostic)
	}
}

func TestPreflightTaskResumeRejectsEnteringEdgeForDifferentTarget(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createMaterializedCurrentNodeWorkflow(t, ctx, store)
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	startTask(t, ctx, store, task.ID)
	definition, _, err := store.GetDefinition(ctx, workflowID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	agent := nodeByKey(t, definition, "review")
	reference, err := workflow.NewCurrentNodeReference(task.ID, workflow.NodeIDOf(agent), nil)
	if err != nil {
		t.Fatalf("NewCurrentNodeReference: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `DELETE FROM task_current_nodes WHERE task_id = ?`, task.ID); err != nil {
		t.Fatalf("delete Current Node: %v", err)
	}
	enteringEdgeID := testEdgeID("edge-start-" + workflowID.String())
	if _, err := store.db.ExecContext(ctx, `
INSERT INTO task_current_nodes (
    task_id, node_id, current_input_values_json, prior_node_values_json,
    scheduling_state, interruption_reason, interruption_detail_json, interrupted_at_unix_ms, entered_by_edge_id
) VALUES (?, ?, '{}', '{"transition_parameters":{}}', 'interrupted', 'user_interrupt', '{}', 1, ?)`,
		task.ID,
		string(reference.NodeID),
		string(enteringEdgeID),
	); err != nil {
		t.Fatalf("seed mismatched Current Node: %v", err)
	}
	if _, err := store.PreflightTaskResume(ctx, task.ID); err == nil {
		t.Fatal("PreflightTaskResume accepted an entering Edge targeting a different Node")
	}
}
