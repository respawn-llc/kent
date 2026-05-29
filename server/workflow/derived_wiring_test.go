package workflow_test

import (
	"reflect"
	"testing"

	"builder/server/workflow"
)

func TestDeriveWiringPropagatesAgentInputsAcrossNormalEdge(t *testing.T) {
	def := inputWorkflow()

	derived := workflow.DeriveWiring(def)

	if len(derived.Diagnostics) > 0 {
		t.Fatalf("expected no diagnostics, got %+v", derived.Diagnostics)
	}
	assertInputBindings(t, derived.InputBindingsForEdge("edge_plan_implement"), []workflow.InputBinding{
		{Name: "plan", Source: workflow.BindingSourceTransitionOutput, Field: "plan"},
		{Name: "risk", Source: workflow.BindingSourceTransitionOutput, Field: "risk"},
	})
	assertOutputFields(t, derived.RequiredProvisionFieldsForTransitionGroup("group_plan_implement"), []workflow.OutputField{
		{Name: "plan", Description: "Implementation plan."},
		{Name: "risk", Description: "Known implementation risk."},
	})
	assertOutputFields(t, derived.PossibleProvisionFieldsForNode("node_plan"), []workflow.OutputField{
		{Name: "plan", Description: "Implementation plan."},
		{Name: "risk", Description: "Known implementation risk."},
	})
}

func TestDeriveWiringUnionsFanoutTargetInputsPerTransitionGroup(t *testing.T) {
	def := fanoutInputWorkflow()

	derived := workflow.DeriveWiring(def)

	if len(derived.Diagnostics) > 0 {
		t.Fatalf("expected no diagnostics, got %+v", derived.Diagnostics)
	}
	assertOutputFields(t, derived.RequiredProvisionFieldsForTransitionGroup("group_plan_split"), []workflow.OutputField{
		{Name: "plan", Description: "Implementation plan."},
		{Name: "risk", Description: "Known implementation risk."},
	})
	assertOutputFields(t, derived.PossibleProvisionFieldsForNode("node_plan"), []workflow.OutputField{
		{Name: "plan", Description: "Implementation plan."},
		{Name: "risk", Description: "Known implementation risk."},
	})
	assertInputBindings(t, derived.InputBindingsForEdge("edge_split_a"), []workflow.InputBinding{
		{Name: "plan", Source: workflow.BindingSourceTransitionOutput, Field: "plan"},
	})
	assertInputBindings(t, derived.InputBindingsForEdge("edge_split_b"), []workflow.InputBinding{
		{Name: "risk", Source: workflow.BindingSourceTransitionOutput, Field: "risk"},
	})
}

func TestDeriveWiringReportsProvisionFieldOverlapForIncompatibleInputs(t *testing.T) {
	def := fanoutInputWorkflow()
	for index := range def.Nodes {
		if def.Nodes[index].ID == "node_review" {
			def.Nodes[index].InputFields = []workflow.InputField{
				{Name: "plan", Description: "Plan review criteria."},
			}
		}
	}

	derived := workflow.DeriveWiring(def)

	assertDerivedDiagnosticCodes(t, derived, workflow.CodeProvisionFieldOverlap)
}

func TestDeriveWiringDistributesJoinInputsToSelectedProviderEdges(t *testing.T) {
	def := joinProviderWorkflow()

	derived := workflow.DeriveWiring(def)

	if len(derived.Diagnostics) > 0 {
		t.Fatalf("expected no diagnostics, got %+v", derived.Diagnostics)
	}
	assertOutputFields(t, derived.JoinOutputFieldsForNode("node_join"), []workflow.OutputField{
		{Name: "plan", Description: "Implementation plan."},
		{Name: "risk", Description: "Known implementation risk."},
	})
	assertOutputFields(t, derived.RequiredProviderFieldsForJoinEdge("edge_branch_a_join"), []workflow.OutputField{
		{Name: "plan", Description: "Implementation plan."},
	})
	assertOutputFields(t, derived.RequiredProviderFieldsForJoinEdge("edge_branch_b_join"), []workflow.OutputField{
		{Name: "risk", Description: "Known implementation risk."},
	})
	assertOutputFields(t, derived.RequiredProvisionFieldsForTransitionGroup("group_branch_a_join"), []workflow.OutputField{
		{Name: "plan", Description: "Implementation plan."},
	})
	assertOutputFields(t, derived.RequiredProvisionFieldsForTransitionGroup("group_branch_b_join"), []workflow.OutputField{
		{Name: "risk", Description: "Known implementation risk."},
	})
}

func TestDeriveWiringReportsMissingJoinInputProvider(t *testing.T) {
	def := joinProviderWorkflow()
	for index := range def.Nodes {
		if def.Nodes[index].ID == "node_join" {
			def.Nodes[index].JoinInputProviders = []workflow.JoinInputProvider{
				{InputName: "plan", ProviderEdgeID: "edge_branch_a_join"},
			}
		}
	}

	derived := workflow.DeriveWiring(def)

	assertDerivedDiagnosticCodes(t, derived, workflow.CodeMissingJoinInputProvider)
	assertDerivedDiagnosticMessage(t, derived, workflow.CodeMissingJoinInputProvider, "node_join", "Node Join: join input risk requires a provider edge")
	assertDerivedDiagnosticsBlock(t, derived)
}

func TestDeriveWiringReportsDuplicateJoinInputProvider(t *testing.T) {
	def := joinProviderWorkflow()
	for index := range def.Nodes {
		if def.Nodes[index].ID == "node_join" {
			def.Nodes[index].JoinInputProviders = append(def.Nodes[index].JoinInputProviders, workflow.JoinInputProvider{
				InputName:      "plan",
				ProviderEdgeID: "edge_branch_b_join",
			})
		}
	}

	derived := workflow.DeriveWiring(def)

	assertDerivedDiagnosticCodes(t, derived, workflow.CodeDuplicateJoinInputProvider)
	assertDerivedDiagnosticsBlock(t, derived)
}

func TestDeriveWiringReportsUnavailableJoinInputProvider(t *testing.T) {
	def := joinProviderWorkflow()
	for index := range def.Nodes {
		if def.Nodes[index].ID == "node_join" {
			def.Nodes[index].JoinInputProviders = []workflow.JoinInputProvider{
				{InputName: "plan", ProviderEdgeID: "edge_split_a"},
				{InputName: "risk", ProviderEdgeID: "edge_branch_b_join"},
			}
		}
	}

	derived := workflow.DeriveWiring(def)

	assertDerivedDiagnosticCodes(t, derived, workflow.CodeInvalidJoinInputProvider)
	assertDerivedDiagnosticsBlock(t, derived)
	assertOutputFields(t, derived.RequiredProviderFieldsForJoinEdge("edge_split_a"), nil)
}

func TestDeriveWiringReportsJoinOutgoingGroupWithMultipleEdges(t *testing.T) {
	def := joinProviderWorkflow()
	def.Edges = append(def.Edges, workflow.Edge{
		WorkflowID:        def.ID,
		ID:                "edge_join_done_extra",
		Key:               "extra",
		TransitionGroupID: "group_join_consume",
		TargetNodeID:      "node_done",
		ContextMode:       workflow.ContextModeNewSession,
	})

	derived := workflow.DeriveWiring(def)

	assertDerivedDiagnosticCodes(t, derived, workflow.CodeInvalidJoinOutgoingShape)
	assertDerivedDiagnosticMessage(t, derived, workflow.CodeInvalidJoinOutgoingShape, "node_join", "Node Join must have exactly one edge in its outgoing transition group")
	assertDerivedDiagnosticsBlock(t, derived)
}

func TestDeriveWiringReportsInputsOnFirstExecutableNode(t *testing.T) {
	def := inputWorkflow()
	for index := range def.Nodes {
		if def.Nodes[index].ID == "node_plan" {
			def.Nodes[index].PromptTemplate = "Plan from {{.Inputs.task_context}}."
			def.Nodes[index].InputFields = []workflow.InputField{
				{Name: "task_context", Description: "Task context."},
			}
		}
	}

	derived := workflow.DeriveWiring(def)

	assertDerivedDiagnosticCodes(t, derived, workflow.CodeInvalidFirstNodeInput)
	assertDerivedDiagnosticMessage(t, derived, workflow.CodeInvalidFirstNodeInput, "node_plan", "Node Plan cannot declare upstream inputs as the first executable node")
	assertDerivedDiagnosticsBlock(t, derived)
	assertOutputFields(t, derived.RequiredProvisionFieldsForTransitionGroup("group_start"), nil)
}

func TestValidateDefinitionAcceptsPromptInputsDeclaredOnNode(t *testing.T) {
	def := inputWorkflow()

	result := workflow.ValidateDefinition(def, workflow.ValidationOptions{
		Context:      workflow.ValidationContextTaskCreation,
		RoleResolver: workflow.StaticRoleResolver{"coder": true},
	})

	assertNoCode(t, result, workflow.CodeInvalidTemplatePlaceholder)
}

func TestValidateDefinitionReportsUnknownPromptInputAgainstNodeInputs(t *testing.T) {
	def := inputWorkflow()
	for index := range def.Nodes {
		if def.Nodes[index].ID == "node_implement" {
			def.Nodes[index].PromptTemplate = "Use {{.Inputs.missing}}."
		}
	}

	result := workflow.ValidateDefinition(def, workflow.ValidationOptions{
		Context:      workflow.ValidationContextTaskCreation,
		RoleResolver: workflow.StaticRoleResolver{"coder": true},
	})

	assertHasCodes(t, result, workflow.CodeInvalidTemplatePlaceholder)
}

func TestValidateDefinitionInputFieldRules(t *testing.T) {
	tests := []struct {
		name string
		edit func(*workflow.InputField)
		code workflow.ValidationErrorCode
	}{
		{
			name: "invalid name",
			edit: func(field *workflow.InputField) { field.Name = "Plan" },
			code: workflow.CodeInvalidInputField,
		},
		{
			name: "reserved commentary name",
			edit: func(field *workflow.InputField) { field.Name = "commentary" },
			code: workflow.CodeInvalidInputField,
		},
		{
			name: "duplicate name",
			edit: func(field *workflow.InputField) { field.Name = "plan" },
			code: workflow.CodeDuplicateInputField,
		},
		{
			name: "description required",
			edit: func(field *workflow.InputField) { field.Description = " " },
			code: workflow.CodeInputFieldDescriptionRequired,
		},
		{
			name: "description too large",
			edit: func(field *workflow.InputField) {
				field.Description = stringOf("a", workflow.MaxInputFieldDescriptionChars+1)
			},
			code: workflow.CodeInputSchemaTooLarge,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := inputWorkflow()
			for index := range def.Nodes {
				if def.Nodes[index].ID == "node_implement" {
					def.Nodes[index].InputFields = append(def.Nodes[index].InputFields, workflow.InputField{Name: "extra", Description: "Extra input."})
					tt.edit(&def.Nodes[index].InputFields[len(def.Nodes[index].InputFields)-1])
				}
			}

			result := workflow.ValidateDefinition(def, workflow.ValidationOptions{
				Context:      workflow.ValidationContextTaskCreation,
				RoleResolver: workflow.StaticRoleResolver{"coder": true},
			})

			assertHasCodes(t, result, tt.code)
		})
	}
}

func TestValidateDefinitionIncludesDerivedWiringDiagnostics(t *testing.T) {
	t.Run("missing join provider blocks draft save", func(t *testing.T) {
		def := joinProviderWorkflow()
		for index := range def.Nodes {
			if def.Nodes[index].ID == "node_join" {
				def.Nodes[index].JoinInputProviders = []workflow.JoinInputProvider{
					{InputName: "plan", ProviderEdgeID: "edge_branch_a_join"},
				}
			}
		}

		result := workflow.ValidateDefinition(def, workflow.ValidationOptions{
			Context:      workflow.ValidationContextDraft,
			RoleResolver: workflow.StaticRoleResolver{"coder": true},
		})

		assertHasCodes(t, result, workflow.CodeMissingJoinInputProvider)
		if !result.HasBlockingErrors() {
			t.Fatalf("expected missing join provider to block draft save")
		}
	})

	t.Run("first executable node input blocks draft save", func(t *testing.T) {
		def := inputWorkflow()
		for index := range def.Nodes {
			if def.Nodes[index].ID == "node_plan" {
				def.Nodes[index].InputFields = []workflow.InputField{{Name: "task_context", Description: "Task context."}}
			}
		}

		result := workflow.ValidateDefinition(def, workflow.ValidationOptions{
			Context:      workflow.ValidationContextDraft,
			RoleResolver: workflow.StaticRoleResolver{"coder": true},
		})

		assertHasCodes(t, result, workflow.CodeInvalidFirstNodeInput)
		if !result.HasBlockingErrors() {
			t.Fatalf("expected first-node input to block draft save")
		}
	})
}

func inputWorkflow() workflow.Definition {
	return workflow.Definition{
		ID:          "workflow_inputs",
		DisplayName: "Input Workflow",
		Nodes: []workflow.Node{
			{WorkflowID: "workflow_inputs", ID: "node_start", Key: "backlog", DisplayName: "Backlog", Kind: workflow.NodeKindStart},
			{
				WorkflowID:     "workflow_inputs",
				ID:             "node_plan",
				Key:            "plan",
				DisplayName:    "Plan",
				Kind:           workflow.NodeKindAgent,
				SubagentRole:   "coder",
				PromptTemplate: "Plan.",
			},
			{
				WorkflowID:     "workflow_inputs",
				ID:             "node_implement",
				Key:            "implement",
				DisplayName:    "Implement",
				Kind:           workflow.NodeKindAgent,
				SubagentRole:   "coder",
				PromptTemplate: "Use {{.Inputs.plan}} and {{.Inputs.risk}}.",
				InputFields: []workflow.InputField{
					{Name: "plan", Description: "Implementation plan."},
					{Name: "risk", Description: "Known implementation risk."},
				},
			},
			{WorkflowID: "workflow_inputs", ID: "node_done", Key: "done", DisplayName: "Done", Kind: workflow.NodeKindTerminal},
		},
		TransitionGroups: []workflow.TransitionGroup{
			{WorkflowID: "workflow_inputs", ID: "group_start", SourceNodeID: "node_start", TransitionID: "start", DisplayName: "Start"},
			{WorkflowID: "workflow_inputs", ID: "group_plan_implement", SourceNodeID: "node_plan", TransitionID: "implement", DisplayName: "Implement"},
			{WorkflowID: "workflow_inputs", ID: "group_implement_done", SourceNodeID: "node_implement", TransitionID: "done", DisplayName: "Done"},
		},
		Edges: []workflow.Edge{
			{WorkflowID: "workflow_inputs", ID: "edge_start_plan", Key: "start", TransitionGroupID: "group_start", TargetNodeID: "node_plan", ContextMode: workflow.ContextModeNewSession},
			{WorkflowID: "workflow_inputs", ID: "edge_plan_implement", Key: "implement", TransitionGroupID: "group_plan_implement", TargetNodeID: "node_implement", ContextMode: workflow.ContextModeNewSession},
			{WorkflowID: "workflow_inputs", ID: "edge_implement_done", Key: "done", TransitionGroupID: "group_implement_done", TargetNodeID: "node_done", ContextMode: workflow.ContextModeNewSession},
		},
	}
}

func fanoutInputWorkflow() workflow.Definition {
	def := inputWorkflow()
	for index := range def.Nodes {
		if def.Nodes[index].ID == "node_implement" {
			def.Nodes[index].InputFields = []workflow.InputField{
				{Name: "plan", Description: "Implementation plan."},
			}
		}
	}
	def.Nodes = append(def.Nodes, workflow.Node{
		WorkflowID:     def.ID,
		ID:             "node_review",
		Key:            "review",
		DisplayName:    "Review",
		Kind:           workflow.NodeKindAgent,
		SubagentRole:   "coder",
		PromptTemplate: "Review {{.Inputs.risk}}.",
		InputFields: []workflow.InputField{
			{Name: "risk", Description: "Known implementation risk."},
		},
	})
	def.TransitionGroups[1] = workflow.TransitionGroup{WorkflowID: def.ID, ID: "group_plan_split", SourceNodeID: "node_plan", TransitionID: "split", DisplayName: "Split"}
	def.Edges[1] = workflow.Edge{WorkflowID: def.ID, ID: "edge_split_a", Key: "implement", TransitionGroupID: "group_plan_split", TargetNodeID: "node_implement", ContextMode: workflow.ContextModeNewSession}
	def.Edges = append(def.Edges, workflow.Edge{WorkflowID: def.ID, ID: "edge_split_b", Key: "review", TransitionGroupID: "group_plan_split", TargetNodeID: "node_review", ContextMode: workflow.ContextModeNewSession})
	return def
}

func joinProviderWorkflow() workflow.Definition {
	return workflow.Definition{
		ID:          "workflow_join_inputs",
		DisplayName: "Join Input Workflow",
		Nodes: []workflow.Node{
			{WorkflowID: "workflow_join_inputs", ID: "node_start", Key: "backlog", DisplayName: "Backlog", Kind: workflow.NodeKindStart},
			{WorkflowID: "workflow_join_inputs", ID: "node_split", Key: "split", DisplayName: "Split", Kind: workflow.NodeKindAgent, SubagentRole: "coder", PromptTemplate: "Split."},
			{WorkflowID: "workflow_join_inputs", ID: "node_branch_a", Key: "branch_a", DisplayName: "Branch A", Kind: workflow.NodeKindAgent, SubagentRole: "coder", PromptTemplate: "A."},
			{WorkflowID: "workflow_join_inputs", ID: "node_branch_b", Key: "branch_b", DisplayName: "Branch B", Kind: workflow.NodeKindAgent, SubagentRole: "coder", PromptTemplate: "B."},
			{
				WorkflowID:  "workflow_join_inputs",
				ID:          "node_join",
				Key:         "join",
				DisplayName: "Join",
				Kind:        workflow.NodeKindJoin,
				JoinInputProviders: []workflow.JoinInputProvider{
					{InputName: "plan", ProviderEdgeID: "edge_branch_a_join"},
					{InputName: "risk", ProviderEdgeID: "edge_branch_b_join"},
				},
			},
			{
				WorkflowID:     "workflow_join_inputs",
				ID:             "node_consume",
				Key:            "consume",
				DisplayName:    "Consume",
				Kind:           workflow.NodeKindAgent,
				SubagentRole:   "coder",
				PromptTemplate: "Use {{.Inputs.plan}} and {{.Inputs.risk}}.",
				InputFields: []workflow.InputField{
					{Name: "plan", Description: "Implementation plan."},
					{Name: "risk", Description: "Known implementation risk."},
				},
			},
			{WorkflowID: "workflow_join_inputs", ID: "node_done", Key: "done", DisplayName: "Done", Kind: workflow.NodeKindTerminal},
		},
		TransitionGroups: []workflow.TransitionGroup{
			{WorkflowID: "workflow_join_inputs", ID: "group_start", SourceNodeID: "node_start", TransitionID: "start", DisplayName: "Start"},
			{WorkflowID: "workflow_join_inputs", ID: "group_split", SourceNodeID: "node_split", TransitionID: "split", DisplayName: "Split"},
			{WorkflowID: "workflow_join_inputs", ID: "group_branch_a_join", SourceNodeID: "node_branch_a", TransitionID: "join", DisplayName: "Join"},
			{WorkflowID: "workflow_join_inputs", ID: "group_branch_b_join", SourceNodeID: "node_branch_b", TransitionID: "join", DisplayName: "Join"},
			{WorkflowID: "workflow_join_inputs", ID: "group_join_consume", SourceNodeID: "node_join", TransitionID: "consume", DisplayName: "Consume"},
			{WorkflowID: "workflow_join_inputs", ID: "group_consume_done", SourceNodeID: "node_consume", TransitionID: "done", DisplayName: "Done"},
		},
		Edges: []workflow.Edge{
			{WorkflowID: "workflow_join_inputs", ID: "edge_start_split", Key: "start", TransitionGroupID: "group_start", TargetNodeID: "node_split", ContextMode: workflow.ContextModeNewSession},
			{WorkflowID: "workflow_join_inputs", ID: "edge_split_a", Key: "branch_a", TransitionGroupID: "group_split", TargetNodeID: "node_branch_a", ContextMode: workflow.ContextModeNewSession},
			{WorkflowID: "workflow_join_inputs", ID: "edge_split_b", Key: "branch_b", TransitionGroupID: "group_split", TargetNodeID: "node_branch_b", ContextMode: workflow.ContextModeNewSession},
			{WorkflowID: "workflow_join_inputs", ID: "edge_branch_a_join", Key: "join_a", TransitionGroupID: "group_branch_a_join", TargetNodeID: "node_join", ContextMode: workflow.ContextModeNewSession},
			{WorkflowID: "workflow_join_inputs", ID: "edge_branch_b_join", Key: "join_b", TransitionGroupID: "group_branch_b_join", TargetNodeID: "node_join", ContextMode: workflow.ContextModeNewSession},
			{WorkflowID: "workflow_join_inputs", ID: "edge_join_consume", Key: "consume", TransitionGroupID: "group_join_consume", TargetNodeID: "node_consume", ContextMode: workflow.ContextModeNewSession},
			{WorkflowID: "workflow_join_inputs", ID: "edge_consume_done", Key: "done", TransitionGroupID: "group_consume_done", TargetNodeID: "node_done", ContextMode: workflow.ContextModeNewSession},
		},
	}
}

func assertInputBindings(t *testing.T, got []workflow.InputBinding, want []workflow.InputBinding) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("input bindings = %+v, want %+v", got, want)
	}
}

func assertOutputFields(t *testing.T, got []workflow.OutputField, want []workflow.OutputField) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("output fields = %+v, want %+v", got, want)
	}
}

func assertDerivedDiagnosticCodes(t *testing.T, derived workflow.DerivedWiring, want ...workflow.ValidationErrorCode) {
	t.Helper()
	codes := make(map[workflow.ValidationErrorCode]bool, len(derived.Diagnostics))
	for _, diagnostic := range derived.Diagnostics {
		codes[diagnostic.Code] = true
	}
	for _, code := range want {
		if !codes[code] {
			t.Fatalf("missing diagnostic code %q in %+v", code, derived.Diagnostics)
		}
	}
}

func assertDerivedDiagnosticMessage(t *testing.T, derived workflow.DerivedWiring, code workflow.ValidationErrorCode, nodeID workflow.NodeID, want string) {
	t.Helper()
	for _, diagnostic := range derived.Diagnostics {
		if diagnostic.Code == code && diagnostic.NodeID == nodeID {
			if diagnostic.Message != want {
				t.Fatalf("diagnostic message for %s on %s = %q, want %q", code, nodeID, diagnostic.Message, want)
			}
			return
		}
	}
	t.Fatalf("missing diagnostic %s on %s in %+v", code, nodeID, derived.Diagnostics)
}

func assertDerivedDiagnosticsBlock(t *testing.T, derived workflow.DerivedWiring) {
	t.Helper()
	for _, diagnostic := range derived.Diagnostics {
		if diagnostic.BlocksContext {
			return
		}
	}
	t.Fatalf("expected at least one blocking diagnostic in %+v", derived.Diagnostics)
}
