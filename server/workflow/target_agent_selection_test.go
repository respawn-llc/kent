package workflow_test

import (
	"errors"
	"testing"

	"core/server/workflow"
)

type targetAgentCatalogStub struct {
	fallback   map[string]workflow.TargetAgentRole
	selectable []workflow.TargetAgentRole
}

func (c targetAgentCatalogStub) ResolveConfiguredRole(role string) (workflow.TargetAgentRole, bool) {
	value, ok := c.fallback[role]
	return value, ok
}

func (c targetAgentCatalogStub) ExplicitCallableRoles() []workflow.TargetAgentRole {
	return append([]workflow.TargetAgentRole(nil), c.selectable...)
}

func TestTransitionSelectionMaterializesConfiguredRole(t *testing.T) {
	for _, test := range []struct {
		name      string
		role      *workflow.TargetAgentRole
		wantError workflow.TargetAgentSelectionErrorCode
	}{
		{name: "available", role: &workflow.TargetAgentRole{Identity: "coder", QuestionsEnabled: true}},
		{name: "Questions disabled", role: &workflow.TargetAgentRole{Identity: "coder"}, wantError: workflow.TargetAgentSelectionErrorQuestionsDisabled},
		{name: "unavailable", wantError: workflow.TargetAgentSelectionErrorUnavailableRole},
	} {
		t.Run(test.name, func(t *testing.T) {
			catalog := targetAgentCatalogStub{fallback: map[string]workflow.TargetAgentRole{}}
			if test.role != nil {
				catalog.fallback["coder"] = *test.role
			}
			plan, err := workflow.PlanTransitionSelection(workflow.TransitionParameterContractRequest{
				SourceKind: workflow.NodeKindAgent, TargetKind: workflow.NodeKindAgent,
				TargetRole: "coder", Catalog: catalog,
				Materialization: &workflow.TransitionSelectionMaterializationRequest{},
			})
			if test.wantError != "" {
				var selectionErr workflow.TargetAgentSelectionError
				if !errors.As(err, &selectionErr) || selectionErr.Code != test.wantError {
					t.Fatalf("error = %v, want %s", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if plan.ExecutionSelection == nil || plan.ExecutionSelection.Assignee != "coder" ||
				plan.ExecutionSelection.Origin != workflow.AssigneeOriginConfiguredFallback {
				t.Fatalf("configured selection = %+v", plan.ExecutionSelection)
			}
		})
	}
}

func TestTransitionSelectionMaterializesZeroOneManyRoles(t *testing.T) {
	for _, test := range []struct {
		name      string
		roles     []workflow.TargetAgentRole
		submitted string
		wantRole  string
		wantError workflow.TargetAgentSelectionErrorCode
	}{
		{name: "zero roles", wantError: workflow.TargetAgentSelectionErrorNoSelectableRoles},
		{
			name:      "sole role ignores supplied value",
			roles:     []workflow.TargetAgentRole{{Identity: "only", ExplicitAgentCallable: true}},
			submitted: "ignored", wantRole: "only",
		},
		{
			name:     "sole role without supplied value",
			roles:    []workflow.TargetAgentRole{{Identity: "only", ExplicitAgentCallable: true}},
			wantRole: "only",
		},
		{
			name:      "many roles canonicalize supplied value",
			roles:     []workflow.TargetAgentRole{{Identity: "zeta", ExplicitAgentCallable: true}, {Identity: "alpha", ExplicitAgentCallable: true}},
			submitted: " ALPHA ", wantRole: "alpha",
		},
		{
			name:      "many roles reject unavailable value",
			roles:     []workflow.TargetAgentRole{{Identity: "high", ExplicitAgentCallable: true}, {Identity: "low", ExplicitAgentCallable: true}},
			submitted: "missing", wantError: workflow.TargetAgentSelectionErrorUnavailableRole,
		},
		{
			name:      "many roles require value",
			roles:     []workflow.TargetAgentRole{{Identity: "high", ExplicitAgentCallable: true}, {Identity: "low", ExplicitAgentCallable: true}},
			wantError: workflow.TargetAgentSelectionErrorUnavailableRole,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan, err := workflow.PlanTransitionSelection(workflow.TransitionParameterContractRequest{
				Edge:       workflow.Edge{AssigneeSelection: workflow.AssigneeSelectionPreviousNode},
				SourceKind: workflow.NodeKindAgent, TargetKind: workflow.NodeKindAgent,
				Catalog:         targetAgentCatalogStub{selectable: test.roles},
				Materialization: &workflow.TransitionSelectionMaterializationRequest{SubmittedRole: test.submitted},
			})
			if test.wantError != "" {
				var selectionErr workflow.TargetAgentSelectionError
				if !errors.As(err, &selectionErr) || selectionErr.Code != test.wantError {
					t.Fatalf("error = %v, want %s", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if plan.ExecutionSelection == nil || plan.ExecutionSelection.Assignee != test.wantRole ||
				plan.ExecutionSelection.Origin != workflow.AssigneeOriginTransitionSelected {
				t.Fatalf("selection = %+v, want selected %s even with configured Questions disabled", plan.ExecutionSelection, test.wantRole)
			}
		})
	}
}
