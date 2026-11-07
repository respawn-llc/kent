package workflow_test

import (
	"testing"

	"core/server/workflow"
)

func TestPlanTargetAgentExecutionSelectionClearsRetainedThinkingForFiniteNoThinkingRole(t *testing.T) {
	previousThinking, err := workflow.NewThinkingValue("high")
	if err != nil {
		t.Fatalf("NewThinkingValue: %v", err)
	}
	retained, err := workflow.NewAgentExecutionSelection(
		"reviewer",
		&previousThinking,
		workflow.AssigneeOriginRetainedSession,
	)
	if err != nil {
		t.Fatalf("NewAgentExecutionSelection: %v", err)
	}
	plan, err := workflow.PlanTransitionSelection(workflow.TransitionParameterContractRequest{
		SourceKind: workflow.NodeKindAgent,
		TargetKind: workflow.NodeKindAgent,
		Edge: workflow.Edge{
			ThinkingSelection: workflow.ThinkingSelectionPreviousNode,
		},
		Catalog: targetAgentCatalogStub{
			fallback: map[string]workflow.TargetAgentRole{
				"reviewer": {
					Identity: "reviewer",
					Thinking: workflow.ThinkingCapability{
						ReasoningCapable: true,
						Finite:           true,
					},
				},
			},
		},
		Materialization: &workflow.TransitionSelectionMaterializationRequest{},
		RetainedSession: &retained,
	})
	if err != nil {
		t.Fatalf("PlanTargetAgentExecutionSelection: %v", err)
	}
	if plan.ExecutionSelection == nil || plan.ExecutionSelection.Thinking != nil {
		t.Fatalf("selection = %+v, want explicit no-thinking materialization", plan.ExecutionSelection)
	}
}

func TestPlanTargetAgentExecutionSelectionMaterializesRetainedConfiguredThinking(t *testing.T) {
	retained, err := workflow.NewAgentExecutionSelection("reviewer", nil, workflow.AssigneeOriginRetainedSession)
	if err != nil {
		t.Fatalf("NewAgentExecutionSelection: %v", err)
	}
	plan, err := workflow.PlanTransitionSelection(workflow.TransitionParameterContractRequest{
		SourceKind: workflow.NodeKindAgent,
		TargetKind: workflow.NodeKindAgent,
		Catalog: targetAgentCatalogStub{
			fallback: map[string]workflow.TargetAgentRole{
				"reviewer": {
					Identity:           "reviewer",
					ConfiguredThinking: "high",
				},
			},
		},
		Materialization: &workflow.TransitionSelectionMaterializationRequest{},
		RetainedSession: &retained,
	})
	if err != nil {
		t.Fatalf("PlanTargetAgentExecutionSelection: %v", err)
	}
	selection := plan.ExecutionSelection
	if selection == nil || selection.Thinking == nil || *selection.Thinking != workflow.ThinkingValue("high") {
		t.Fatalf("retained selection = %#v, want high", selection)
	}
}

func TestTransitionSelectionSeparatesEditorAvailabilityFromRetainedThinking(t *testing.T) {
	retained, err := workflow.NewAgentExecutionSelection("saved", nil, workflow.AssigneeOriginConfiguredFallback)
	if err != nil {
		t.Fatal(err)
	}
	request := workflow.TransitionParameterContractRequest{
		Edge: workflow.Edge{
			ContextMode:       workflow.ContextModeContinueSession,
			ThinkingSelection: workflow.ThinkingSelectionPreviousNode,
			Parameters:        []workflow.Parameter{{Key: "effort", Purpose: workflow.ParameterPurposeTargetThinking}},
		},
		SourceKind: workflow.NodeKindAgent,
		TargetKind: workflow.NodeKindAgent,
		TargetRole: "target",
		Catalog: targetAgentCatalogStub{fallback: map[string]workflow.TargetAgentRole{
			"target": {Identity: "target", Thinking: workflow.ThinkingCapability{ReasoningCapable: true, Finite: true, Levels: []string{"low"}}},
			"saved":  {Identity: "saved", Thinking: workflow.ThinkingCapability{ReasoningCapable: true, Finite: true, Levels: []string{"high", "xhigh"}}},
		}},
		RetainedSession: &retained,
		Materialization: &workflow.TransitionSelectionMaterializationRequest{SubmittedThinking: "xhigh"},
	}
	plan, err := workflow.PlanTransitionSelection(request)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Applicability.Thinking.Available || plan.Applicability.Thinking.ParameterVisible {
		t.Fatalf("editor availability = %+v, want automatic target thinking", plan.Applicability.Thinking)
	}
	if len(plan.Contract.Parameters) != 1 || plan.Contract.Parameters[0].Key != "effort" {
		t.Fatalf("retained runtime Parameters = %+v, want required effort", plan.Contract.Parameters)
	}
	if plan.ExecutionSelection == nil || plan.ExecutionSelection.Assignee != "saved" ||
		plan.ExecutionSelection.Thinking == nil || *plan.ExecutionSelection.Thinking != "xhigh" ||
		plan.ExecutionSelection.Origin != workflow.AssigneeOriginRetainedSession {
		t.Fatalf("retained selection = %+v", plan.ExecutionSelection)
	}
}

func TestTransitionSelectionDoesNotReplaceUnknownRetainedRoleWithTargetConfiguration(t *testing.T) {
	retained, err := workflow.NewAgentExecutionSelection("removed", thinkingValuePointer("saved-thinking"), workflow.AssigneeOriginTransitionSelected)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := workflow.PlanTransitionSelection(workflow.TransitionParameterContractRequest{
		SourceKind: workflow.NodeKindAgent,
		TargetKind: workflow.NodeKindAgent,
		TargetRole: "target",
		Catalog: targetAgentCatalogStub{fallback: map[string]workflow.TargetAgentRole{
			"target": {Identity: "target", ConfiguredThinking: "different"},
		}},
		RetainedSession: &retained,
		Materialization: &workflow.TransitionSelectionMaterializationRequest{},
	})
	if err != nil {
		t.Fatal(err)
	}
	selection := plan.ExecutionSelection
	if selection == nil || selection.Assignee != "removed" || selection.Thinking == nil ||
		*selection.Thinking != "saved-thinking" || selection.Origin != workflow.AssigneeOriginRetainedSession {
		t.Fatalf("retained selection = %+v", selection)
	}
}
