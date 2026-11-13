package workflowstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"core/server/workflow"
)

func TestCompletionContractsApplyProtectedParameterConsumptionPolicies(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createMaterializedCurrentNodeWorkflow(t, ctx, store)
	saveWorkflowGraphFixture(t, ctx, store, workflowID, func(_ workflow.Definition, req *WorkflowGraphSaveRequest) {
		edge := workflowGraphSaveEdgeRecord(t, req.Edges, testEdgeID("edge-audit-"+workflowID.String()))
		edge.AssigneeSelection = workflow.AssigneeSelectionPreviousNode
		edge.PromptTemplate = "Audit {{.Params.role}}."
		edge.Parameters = []workflow.Parameter{{
			Key:     "role",
			Purpose: workflow.ParameterPurposeTargetAssignee,
		}}
	})
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	plan := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	review, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source:       plan.Reference,
		TransitionID: "review",
		OutputValues: map[string]string{"summary": "plan complete"},
	})
	if err != nil {
		t.Fatalf("complete plan: %v", err)
	}
	_, err = completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source:       review.Mutation.Created[0].Reference,
		TransitionID: "audit",
	})
	var validationErr CompletionValidationError
	if !errors.As(err, &validationErr) || !completionHasCode(err, CompletionCodeUnavailableTargetAgentRole) {
		t.Fatalf("missing protected role error = %v, want unavailable-role validation", err)
	}
	completed, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source:       review.Mutation.Created[0].Reference,
		TransitionID: "audit",
		OutputValues: map[string]string{"role": "reviewer"},
	})
	if err != nil {
		t.Fatalf("complete audit with protected role: %v", err)
	}
	if len(completed.Mutation.Created) != 1 ||
		completed.Mutation.Created[0].AgentExecutionSelection == nil ||
		completed.Mutation.Created[0].AgentExecutionSelection.Assignee != "reviewer" ||
		completed.Mutation.Created[0].AgentExecutionSelection.Origin != workflow.AssigneeOriginTransitionSelected {
		t.Fatalf("materialized target = %+v, want selected reviewer", completed.Mutation.Created)
	}
	if completed.Mutation.Created[0].CurrentInputValues["role"] != "reviewer" {
		t.Fatalf("materialized target current inputs = %+v, want exposed selector role", completed.Mutation.Created[0].CurrentInputValues)
	}
}

func TestAutomaticCompletionMaterializesSoleRoleWithoutLeakingIgnoredValues(t *testing.T) {
	for _, entry := range []struct {
		source workflow.NodeKind
		mode   workflow.ContextMode
	}{
		{workflow.NodeKindAgent, workflow.ContextModeNewSession},
		{workflow.NodeKindScript, workflow.ContextModeNewSession},
		{workflow.NodeKindAgent, workflow.ContextModeCompactAndContinueSession},
	} {
		for _, levels := range [][]string{nil, {"high"}} {
			for _, flow := range []string{"completion", "approval", "manual move"} {
				t.Run(fmt.Sprintf("%s/%s/%d levels/%s", entry.source, entry.mode, len(levels), flow), func(t *testing.T) {
					requireIgnoredSelectorMaterialization(t, entry.source, entry.mode, levels, flow)
				})
			}
		}
	}
}

func requireIgnoredSelectorMaterialization(t *testing.T, source workflow.NodeKind, mode workflow.ContextMode, levels []string, flow string) {
	t.Helper()
	ctx, store, binding := newTestStoreContext(t)
	store.roleResolver = completionTargetCatalog{
		roles: map[string]workflow.TargetAgentRole{
			"coder": {Identity: "coder", QuestionsEnabled: true},
		},
		selectable: []workflow.TargetAgentRole{
			{
				Identity:              "reviewer",
				ExplicitAgentCallable: true,
				ConfiguredThinking:    "configured",
				Thinking: workflow.ThinkingCapability{
					ReasoningCapable: true,
					Finite:           true,
					Levels:           levels,
				},
			},
		},
	}
	if source == workflow.NodeKindScript {
		path := filepath.Join(binding.CanonicalRoot, "scripts", "review")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	workflowID := createMaterializedCurrentNodeWorkflow(t, ctx, store)
	saveWorkflowGraphFixture(t, ctx, store, workflowID, func(def workflow.Definition, req *WorkflowGraphSaveRequest) {
		if source == workflow.NodeKindScript {
			review := workflowGraphSaveNodeRecord(t, req.Nodes, workflow.NodeIDOf(nodeByKey(t, def, "review")))
			review.Kind = source
			review.SubagentRole = ""
			review.ScriptPath = "scripts/review"
			workflowGraphSaveEdgeRecord(t, req.Edges, testEdgeID("edge-review-"+workflowID.String())).PromptTemplate = ""
		}
		edge := workflowGraphSaveEdgeRecord(t, req.Edges, testEdgeID("edge-audit-"+workflowID.String()))
		edge.ContextMode = mode
		edge.RequiresApproval = flow == "approval"
		edge.AssigneeSelection = workflow.AssigneeSelectionPreviousNode
		edge.ThinkingSelection = workflow.ThinkingSelectionPreviousNode
		edge.Parameters = []workflow.Parameter{
			{Key: "role", Purpose: workflow.ParameterPurposeTargetAssignee},
			{Key: "effort", Purpose: workflow.ParameterPurposeTargetThinking},
		}
	})
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	plan := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	review, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source:       plan.Reference,
		TransitionID: "review",
		OutputValues: map[string]string{"summary": "plan complete"},
	})
	if err != nil {
		t.Fatalf("complete plan: %v", err)
	}
	values := map[string]string{"role": "ignored-role", "effort": "ignored-thinking"}
	var target workflow.CurrentNode
	if flow == "manual move" {
		definition, _, err := store.GetDefinition(ctx, workflowID)
		if err != nil {
			t.Fatal(err)
		}
		transition := workflow.TransitionID("audit")
		request := ManualMoveRequest{
			TaskID:        task.ID,
			TargetNodeID:  workflow.NodeIDOf(nodeByKey(t, definition, "audit")),
			TransitionKey: &transition,
			Values:        map[workflow.ModelKey]map[string]string{"review": values},
		}
		preview, err := store.PreviewManualMove(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		for _, choice := range preview.Choices {
			for _, required := range choice.RequiredValues {
				if required.OutputName == "role" || required.OutputName == "effort" {
					t.Fatalf("ignored selector required by Manual Move: %+v", required)
				}
			}
		}
		prepared, err := store.PrepareManualMove(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		moved, err := applyManualMoveForStoreTest(t, ctx, store, prepared, nil)
		if err != nil {
			t.Fatal(err)
		}
		target = moved.Mutation.Created[0]
	} else {
		completed, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
			Source:       review.Mutation.Created[0].Reference,
			TransitionID: "audit",
			OutputValues: values,
		})
		if err != nil {
			t.Fatalf("complete audit: %v", err)
		}
		if flow == "approval" {
			if completed.PendingApproval == nil {
				t.Fatal("missing pending Approval")
			}
			pending, err := store.PendingApproval(ctx, completed.PendingApproval.ID)
			if err != nil {
				t.Fatal(err)
			}
			requireNoIgnoredSelectorValues(t, pending.Branches[0].Target.CurrentNode)
			catalog := store.roleResolver
			store.roleResolver = emptyTargetAgentCatalog{}
			applied, err := applyPendingApproval(t, store, ctx, pending.ID)
			if err != nil {
				t.Fatal(err)
			}
			store.roleResolver = catalog
			target = applied.Mutation.Created[0]
		} else {
			target = completed.Mutation.Created[0]
		}
	}
	requireNoIgnoredSelectorValues(t, target)
	startContext, err := store.ResolveCurrentNodeStartContext(ctx, target.Reference)
	if err != nil {
		t.Fatal(err)
	}
	requireNoIgnoredSelectorValues(t, startContext.CurrentNode)
	if err := store.InterruptCurrentNode(
		ctx,
		target.Reference,
		workflow.CurrentNodeInterruptionReasonUserInterrupt,
		workflow.CurrentNodeInterruptionDetail{Code: string(workflow.CurrentNodeInterruptionReasonUserInterrupt)},
	); err != nil {
		t.Fatalf("interrupt target with hidden selectors: %v", err)
	}
	classifications, err := store.PreflightTaskResume(ctx, task.ID)
	if err != nil {
		t.Fatalf("preflight resume with hidden selectors: %v", err)
	}
	if len(classifications) != 1 || len(classifications[0].Diagnostics) != 0 {
		t.Fatalf("hidden selector resume classifications = %+v, want one resumable Current Node", classifications)
	}
	selection := target.AgentExecutionSelection
	wantThinking := "configured"
	if len(levels) != 0 {
		wantThinking = levels[0]
	}
	if selection == nil ||
		selection.Assignee != "reviewer" ||
		selection.Origin != workflow.AssigneeOriginTransitionSelected ||
		selection.Thinking == nil ||
		string(*selection.Thinking) != wantThinking {
		t.Fatalf("materialized target selection = %+v, want sole explicit role and finite thinking", selection)
	}
}

func requireNoIgnoredSelectorValues(t *testing.T, target workflow.CurrentNode) {
	t.Helper()
	for _, key := range []string{"role", "effort"} {
		if value, exists := target.CurrentInputValues[key]; exists {
			t.Errorf("ignored %s leaked into current inputs: %q", key, value)
		}
		if value, exists := target.PriorValues.TransitionParameter("audit", key); exists {
			t.Errorf("ignored %s leaked into prior Parameters: %q", key, value)
		}
	}
}

func TestAutomaticCompletionMaterializesFiniteThinkingSelection(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	store.roleResolver = completionTargetCatalog{
		roles: map[string]workflow.TargetAgentRole{
			"coder": {
				Identity:           "coder",
				QuestionsEnabled:   true,
				ConfiguredThinking: "low",
				Thinking: workflow.ThinkingCapability{
					ReasoningCapable: true,
					Finite:           true,
					Levels:           []string{"low", "high"},
				},
			},
		},
		selectable: []workflow.TargetAgentRole{{Identity: "reviewer", ExplicitAgentCallable: true}},
	}
	workflowID := createMaterializedCurrentNodeWorkflow(t, ctx, store)
	saveWorkflowGraphFixture(t, ctx, store, workflowID, func(_ workflow.Definition, req *WorkflowGraphSaveRequest) {
		edge := workflowGraphSaveEdgeRecord(t, req.Edges, testEdgeID("edge-audit-"+workflowID.String()))
		edge.ThinkingSelection = workflow.ThinkingSelectionPreviousNode
		edge.Parameters = []workflow.Parameter{{
			Key:         "effort",
			Description: "Choose an effort level.",
			Purpose:     workflow.ParameterPurposeTargetThinking,
		}}
	})
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	plan := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	review, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source:       plan.Reference,
		TransitionID: "review",
		OutputValues: map[string]string{"summary": "plan complete"},
	})
	if err != nil {
		t.Fatalf("complete plan: %v", err)
	}
	completed, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source:       review.Mutation.Created[0].Reference,
		TransitionID: "audit",
		OutputValues: map[string]string{"effort": "high"},
	})
	if err != nil {
		t.Fatalf("complete audit: %v", err)
	}
	selection := completed.Mutation.Created[0].AgentExecutionSelection
	if selection == nil || selection.Thinking == nil || string(*selection.Thinking) != "high" {
		t.Fatalf("materialized target selection = %+v, want high thinking", selection)
	}
	if completed.Mutation.Created[0].CurrentInputValues["effort"] != "high" {
		t.Fatalf("materialized target current inputs = %+v, want exposed thinking selector", completed.Mutation.Created[0].CurrentInputValues)
	}
}

func TestAutomaticCompletionMaterializesOpenThinkingSelection(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	store.roleResolver = completionTargetCatalog{
		roles: map[string]workflow.TargetAgentRole{
			"coder": {
				Identity:         "coder",
				QuestionsEnabled: true,
				Thinking: workflow.ThinkingCapability{
					ReasoningCapable: true,
					Finite:           false,
				},
			},
		},
	}
	workflowID := createMaterializedCurrentNodeWorkflow(t, ctx, store)
	saveWorkflowGraphFixture(t, ctx, store, workflowID, func(_ workflow.Definition, req *WorkflowGraphSaveRequest) {
		edge := workflowGraphSaveEdgeRecord(t, req.Edges, testEdgeID("edge-audit-"+workflowID.String()))
		edge.ThinkingSelection = workflow.ThinkingSelectionPreviousNode
		edge.Parameters = []workflow.Parameter{{
			Key:         "effort",
			Description: "Use a provider-specific effort.",
			Purpose:     workflow.ParameterPurposeTargetThinking,
		}}
	})
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	plan := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	review, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source:       plan.Reference,
		TransitionID: "review",
		OutputValues: map[string]string{"summary": "plan complete"},
	})
	if err != nil {
		t.Fatalf("complete plan: %v", err)
	}
	completed, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source:       review.Mutation.Created[0].Reference,
		TransitionID: "audit",
		OutputValues: map[string]string{"effort": "provider-custom"},
	})
	if err != nil {
		t.Fatalf("complete audit: %v", err)
	}
	selection := completed.Mutation.Created[0].AgentExecutionSelection
	if selection == nil || selection.Thinking == nil || string(*selection.Thinking) != "provider-custom" {
		t.Fatalf("materialized target selection = %+v, want provider-custom thinking", selection)
	}
}

func TestAutomaticCompletionRejectsInvalidSelectionBeforeMutation(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	store.roleResolver = completionTargetCatalog{
		roles: map[string]workflow.TargetAgentRole{"coder": {Identity: "coder", QuestionsEnabled: true}},
		selectable: []workflow.TargetAgentRole{
			{Identity: "reviewer", ExplicitAgentCallable: true, Thinking: workflow.ThinkingCapability{ReasoningCapable: true, Finite: true, Levels: []string{"high"}}},
			{Identity: "other", ExplicitAgentCallable: true, Thinking: workflow.ThinkingCapability{ReasoningCapable: true, Finite: true, Levels: []string{"low"}}},
		},
	}
	workflowID := createMaterializedCurrentNodeWorkflow(t, ctx, store)
	saveWorkflowGraphFixture(t, ctx, store, workflowID, func(_ workflow.Definition, req *WorkflowGraphSaveRequest) {
		edge := workflowGraphSaveEdgeRecord(t, req.Edges, testEdgeID("edge-audit-"+workflowID.String()))
		edge.AssigneeSelection = workflow.AssigneeSelectionPreviousNode
		edge.ThinkingSelection = workflow.ThinkingSelectionPreviousNode
		edge.RequiresApproval = true
		edge.Parameters = []workflow.Parameter{
			{Key: "role", Purpose: workflow.ParameterPurposeTargetAssignee},
			{Key: "effort", Purpose: workflow.ParameterPurposeTargetThinking},
		}
	})
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	plan := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	review, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source:       plan.Reference,
		TransitionID: "review",
		OutputValues: map[string]string{"summary": "plan complete"},
	})
	if err != nil {
		t.Fatalf("complete plan: %v", err)
	}
	definition, _, err := store.GetDefinition(ctx, workflowID)
	if err != nil {
		t.Fatal(err)
	}
	transition := workflow.TransitionID("audit")
	for _, test := range []struct {
		name    string
		values  map[string]string
		code    string
		missing bool
	}{
		{name: "missing role", values: map[string]string{"effort": "high"}, code: string(workflow.TargetAgentSelectionErrorUnavailableRole), missing: true},
		{name: "missing thinking", values: map[string]string{"role": "reviewer"}, code: CompletionCodeRequiredOutputMissing, missing: true},
		{name: "unavailable role", values: map[string]string{"role": "not-configured", "effort": "high"}, code: string(workflow.TargetAgentSelectionErrorUnavailableRole)},
		{name: "unsupported role-thinking pair", values: map[string]string{"role": "reviewer", "effort": "low"}, code: string(workflow.TargetAgentSelectionErrorUnsupportedThinking)},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
				Source:       review.Mutation.Created[0].Reference,
				TransitionID: "audit", OutputValues: test.values,
			})
			if !completionHasCode(err, test.code) {
				t.Fatalf("completion error = %v, want %s", err, test.code)
			}
			prepared, err := store.PrepareManualMove(ctx, ManualMoveRequest{
				TaskID: task.ID, TargetNodeID: workflow.NodeIDOf(nodeByKey(t, definition, "audit")),
				TransitionKey: &transition,
				Values:        map[workflow.ModelKey]map[string]string{"review": test.values},
			})
			if test.missing {
				if !errors.Is(err, ErrManualMoveValuesInvalid) {
					t.Fatalf("missing Manual Move value error = %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				_, err = applyManualMoveForStoreTest(t, ctx, store, prepared, nil)
				if !completionHasCode(err, test.code) {
					t.Fatalf("Manual Move selection error = %v, want %s", err, test.code)
				}
			}
		})
	}
	currentNodes, err := store.ListCurrentNodes(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListCurrentNodes after rejected completion: %v", err)
	}
	if len(currentNodes) != 1 || !currentNodes[0].Reference.Equal(review.Mutation.Created[0].Reference) {
		t.Fatalf("current nodes after rejected completion = %+v, want unchanged source", currentNodes)
	}
	approvals, err := store.ListPendingApprovals(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListPendingApprovals after rejected completion: %v", err)
	}
	if len(approvals) != 0 {
		t.Fatalf("pending approvals after rejected completion = %+v, want none", approvals)
	}
}

func TestAutomaticCompletionMaterializesSelectionFromScriptSource(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	path := filepath.Join(binding.CanonicalRoot, "scripts", "review")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	workflowID := createMaterializedCurrentNodeWorkflow(t, ctx, store)
	saveWorkflowGraphFixture(t, ctx, store, workflowID, func(def workflow.Definition, req *WorkflowGraphSaveRequest) {
		review := workflowGraphSaveNodeRecord(t, req.Nodes, workflow.NodeIDOf(nodeByKey(t, def, "review")))
		review.Kind = workflow.NodeKindScript
		review.SubagentRole = ""
		review.ScriptPath = "scripts/review"
		workflowGraphSaveEdgeRecord(t, req.Edges, testEdgeID("edge-review-"+workflowID.String())).PromptTemplate = ""
		edge := workflowGraphSaveEdgeRecord(t, req.Edges, testEdgeID("edge-audit-"+workflowID.String()))
		edge.AssigneeSelection = workflow.AssigneeSelectionPreviousNode
		edge.Parameters = []workflow.Parameter{{
			Key:     "role",
			Purpose: workflow.ParameterPurposeTargetAssignee,
		}}
	})
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	plan := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	script, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source:       plan.Reference,
		TransitionID: "review",
		OutputValues: map[string]string{"summary": "script input"},
	})
	if err != nil {
		t.Fatalf("complete Agent source: %v", err)
	}
	completed, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source:       script.Mutation.Created[0].Reference,
		TransitionID: "audit",
		OutputValues: map[string]string{"role": "reviewer"},
	})
	if err != nil {
		t.Fatalf("complete Script source: %v", err)
	}
	selection := completed.Mutation.Created[0].AgentExecutionSelection
	if selection == nil || selection.Assignee != "reviewer" || selection.Origin != workflow.AssigneeOriginTransitionSelected {
		t.Fatalf("Script completion target selection = %+v, want selected reviewer", selection)
	}
}

type completionTargetCatalog struct {
	roles      map[string]workflow.TargetAgentRole
	selectable []workflow.TargetAgentRole
}

func (c completionTargetCatalog) ResolveConfiguredRole(role string) (workflow.TargetAgentRole, bool) {
	value, ok := c.roles[role]
	return value, ok
}

func (c completionTargetCatalog) ExplicitCallableRoles() []workflow.TargetAgentRole {
	return append([]workflow.TargetAgentRole(nil), c.selectable...)
}

func TestCompletionContractsRejectDormantProtectedValuesAsUnknownOutputs(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createMaterializedCurrentNodeWorkflow(t, ctx, store)
	saveWorkflowGraphFixture(t, ctx, store, workflowID, func(_ workflow.Definition, req *WorkflowGraphSaveRequest) {
		edge := workflowGraphSaveEdgeRecord(t, req.Edges, testEdgeID("edge-audit-"+workflowID.String()))
		edge.Parameters = []workflow.Parameter{
			{Key: "role", Purpose: workflow.ParameterPurposeTargetAssignee},
			{Key: "effort", Purpose: workflow.ParameterPurposeTargetThinking},
		}
	})
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	plan := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	review, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source:       plan.Reference,
		TransitionID: "review",
		OutputValues: map[string]string{"summary": "plan complete"},
	})
	if err != nil {
		t.Fatalf("complete plan: %v", err)
	}
	definition, _, err := store.GetDefinition(ctx, workflowID)
	if err != nil {
		t.Fatal(err)
	}
	transition := workflow.TransitionID("audit")
	for _, key := range []string{"role", "effort"} {
		_, err = completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
			Source:       review.Mutation.Created[0].Reference,
			TransitionID: "audit",
			OutputValues: map[string]string{key: "unknown"},
		})
		if !completionHasCode(err, CompletionCodeUnknownOutputField) {
			t.Fatalf("dormant protected output error = %v, want unknown output", err)
		}
		_, err = store.PrepareManualMove(ctx, ManualMoveRequest{
			TaskID: task.ID, TargetNodeID: workflow.NodeIDOf(nodeByKey(t, definition, "audit")),
			TransitionKey: &transition,
			Values:        map[workflow.ModelKey]map[string]string{"review": {key: "unknown"}},
		})
		if !errors.Is(err, ErrManualMoveValuesInvalid) {
			t.Fatalf("dormant Manual Move value error = %v, want invalid value", err)
		}
	}
}
