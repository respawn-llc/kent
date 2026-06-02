package workflow_test

import (
	"slices"
	"testing"

	"builder/server/workflow"
)

func TestValidateDefaultWorkflowPasses(t *testing.T) {
	def := validWorkflow()

	result := workflow.ValidateDefinition(def, workflow.ValidationOptions{
		Context:      workflow.ValidationContextTaskCreation,
		RoleResolver: workflow.StaticRoleResolver{"coder": true},
	})

	if result.HasErrors() {
		t.Fatalf("expected valid workflow, got errors: %+v", result.Errors)
	}
	if !result.Valid() {
		t.Fatalf("expected result.Valid()")
	}
}

func TestCanonicalContextSourceNormalizesBoundaryValues(t *testing.T) {
	selected := workflow.CanonicalContextSource(workflow.ContextSource{Kind: " selected_node ", NodeKey: " implementation "})
	if selected.Kind != workflow.ContextSourceSelectedNode || selected.NodeKey != "implementation" {
		t.Fatalf("selected context source = %+v, want trimmed selected node", selected)
	}

	immediate := workflow.CanonicalContextSource(workflow.ContextSource{Kind: " immediate_source ", NodeKey: " implementation "})
	if immediate.Kind != workflow.ContextSourceImmediateSource || immediate.NodeKey != "" {
		t.Fatalf("immediate context source = %+v, want normalized immediate source", immediate)
	}
}

func TestValidateWorkflowAllowsDefaultAgentRole(t *testing.T) {
	def := validWorkflow()
	def.Nodes[1].SubagentRole = workflow.DefaultAgentRole

	result := workflow.ValidateDefinition(def, workflow.ValidationOptions{
		Context:      workflow.ValidationContextTaskCreation,
		RoleResolver: workflow.StaticRoleResolver{"coder": true},
	})

	if result.HasErrors() {
		t.Fatalf("expected default role to be valid, got errors: %+v", result.Errors)
	}
}

func TestDraftValidationAllowsSemanticErrorsButBlocksHardStorageErrors(t *testing.T) {
	def := validWorkflow()
	def.Nodes = append(def.Nodes, workflow.Node{
		WorkflowID:     def.ID,
		ID:             "node_detached",
		Key:            "detached",
		DisplayName:    "Detached",
		Kind:           workflow.NodeKindAgent,
		SubagentRole:   "missing",
		PromptTemplate: "Do detached work.",
	})

	draft := workflow.ValidateDefinition(def, workflow.ValidationOptions{Context: workflow.ValidationContextDraft})

	assertHasCodes(t, draft, workflow.CodeNodeUnreachableFromStart, workflow.CodeNonTerminalCannotReachTerminal, workflow.CodeAgentRoleMissing)
	if draft.HasBlockingErrors() {
		t.Fatalf("draft semantic errors should not block saving: %+v", draft.BlockingErrors())
	}

	def.Nodes[0].Kind = workflow.NodeKindAgent
	def.Nodes[1].Kind = workflow.NodeKindStart
	def.Nodes[2].Kind = workflow.NodeKindStart

	hard := workflow.ValidateDefinition(def, workflow.ValidationOptions{Context: workflow.ValidationContextDraft})

	assertHasCodes(t, hard, workflow.CodeMultipleStartNodes)
	if !hard.HasBlockingErrors() {
		t.Fatalf("draft hard storage error should block saving")
	}
}

func TestTaskCreationValidationRejectsInvalidGraphWithAccumulatedErrors(t *testing.T) {
	def := validWorkflow()
	def.Nodes = append(def.Nodes, workflow.Node{
		WorkflowID:     def.ID,
		ID:             "node_detached",
		Key:            "detached",
		DisplayName:    "Detached",
		Kind:           workflow.NodeKindAgent,
		SubagentRole:   "missing",
		PromptTemplate: "Do detached work.",
	})
	def.TransitionGroups = nil
	def.Edges = nil

	result := workflow.ValidateDefinition(def, workflow.ValidationOptions{Context: workflow.ValidationContextTaskCreation})

	assertHasCodes(t, result,
		workflow.CodeInvalidStartOutgoingShape,
		workflow.CodeNodeUnreachableFromStart,
		workflow.CodeNonTerminalCannotReachTerminal,
		workflow.CodeAgentRoleMissing,
	)
	if !result.HasBlockingErrors() {
		t.Fatalf("task creation errors should block")
	}
}

func TestStartNodeRules(t *testing.T) {
	tests := []struct {
		name string
		edit func(*workflow.Definition)
		code workflow.ValidationErrorCode
	}{
		{
			name: "missing start",
			edit: func(def *workflow.Definition) {
				def.Nodes[0].Kind = workflow.NodeKindAgent
				def.Nodes[0].SubagentRole = "coder"
				def.Nodes[0].PromptTemplate = "Work."
			},
			code: workflow.CodeMissingStartNode,
		},
		{
			name: "multiple starts",
			edit: func(def *workflow.Definition) {
				def.Nodes[1].Kind = workflow.NodeKindStart
			},
			code: workflow.CodeMultipleStartNodes,
		},
		{
			name: "start execution config",
			edit: func(def *workflow.Definition) {
				def.Nodes[0].SubagentRole = "coder"
				def.Nodes[0].PromptTemplate = "Do work."
				def.Nodes[0].OutputFields = []workflow.OutputField{{Name: "summary", Description: "Summary."}}
			},
			code: workflow.CodeInvalidStartNode,
		},
		{
			name: "start incoming edge",
			edit: func(def *workflow.Definition) {
				addTransitionForValidationTest(def, "group_restart", "node_agent", "restart", "Restart", "edge_restart", "restart", "node_start")
			},
			code: workflow.CodeInvalidStartNode,
		},
		{
			name: "start has two groups",
			edit: func(def *workflow.Definition) {
				addTransitionForValidationTest(def, "group_alt", "node_start", "alt", "Alternative", "edge_alt", "alt", "node_agent")
			},
			code: workflow.CodeInvalidStartOutgoingShape,
		},
		{
			name: "start group has two edges",
			edit: func(def *workflow.Definition) {
				def.Edges = append(def.Edges, workflow.Edge{
					WorkflowID:        def.ID,
					ID:                "edge_start_second",
					Key:               "second",
					TransitionGroupID: "group_start",
					TargetNodeID:      "node_agent",
					ContextMode:       workflow.ContextModeNewSession,
				})
			},
			code: workflow.CodeInvalidStartOutgoingShape,
		},
		{
			name: "start targets terminal",
			edit: func(def *workflow.Definition) {
				def.Edges[0].TargetNodeID = "node_done"
			},
			code: workflow.CodeInvalidStartOutgoingShape,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := validWorkflow()
			tt.edit(&def)

			result := workflow.ValidateDefinition(def, workflow.ValidationOptions{
				Context:      workflow.ValidationContextTaskCreation,
				RoleResolver: workflow.StaticRoleResolver{"coder": true},
			})

			assertHasCodes(t, result, tt.code)
		})
	}
}

func TestNodeKindRules(t *testing.T) {
	tests := []struct {
		name string
		edit func(*workflow.Definition)
		code workflow.ValidationErrorCode
	}{
		{
			name: "terminal outgoing edge",
			edit: func(def *workflow.Definition) {
				addTransitionForValidationTest(def, "group_done_again", "node_done", "again", "Again", "edge_done_again", "again", "node_agent")
			},
			code: workflow.CodeTerminalHasOutgoingEdge,
		},
		{
			name: "terminal executable",
			edit: func(def *workflow.Definition) {
				def.Nodes[2].SubagentRole = "coder"
				def.Nodes[2].PromptTemplate = "No."
			},
			code: workflow.CodeTerminalIsExecutable,
		},
		{
			name: "join executable",
			edit: func(def *workflow.Definition) {
				def.Nodes[1].Kind = workflow.NodeKindJoin
				def.Nodes[1].SubagentRole = "coder"
				def.Nodes[1].PromptTemplate = "No."
			},
			code: workflow.CodeJoinIsExecutable,
		},
		{
			name: "join also terminal",
			edit: func(def *workflow.Definition) {
				def.Nodes[2].Kind = workflow.NodeKindJoin
			},
			code: workflow.CodeInvalidJoinNode,
		},
		{
			name: "join invalid outgoing shape",
			edit: func(def *workflow.Definition) {
				def.Nodes[1].Kind = workflow.NodeKindJoin
				def.Nodes[1].SubagentRole = ""
				def.Nodes[1].PromptTemplate = ""
				addTransitionForValidationTest(def, "group_join_alt", "node_agent", "alt", "Alternative", "edge_join_alt", "alt", "node_done")
			},
			code: workflow.CodeInvalidJoinOutgoingShape,
		},
		{
			name: "unknown kind",
			edit: func(def *workflow.Definition) {
				def.Nodes[1].Kind = workflow.NodeKind("robot")
			},
			code: workflow.CodeInvalidNodeKind,
		},
		{
			name: "start input fields",
			edit: func(def *workflow.Definition) {
				def.Nodes[0].InputFields = []workflow.InputField{{Name: "summary", Description: "Summary."}}
			},
			code: workflow.CodeInvalidInputField,
		},
		{
			name: "join input fields",
			edit: func(def *workflow.Definition) {
				def.Nodes[1].Kind = workflow.NodeKindJoin
				def.Nodes[1].SubagentRole = ""
				def.Nodes[1].PromptTemplate = ""
				def.Nodes[1].InputFields = []workflow.InputField{{Name: "summary", Description: "Summary."}}
			},
			code: workflow.CodeInvalidInputField,
		},
		{
			name: "terminal input fields",
			edit: func(def *workflow.Definition) {
				def.Nodes[2].InputFields = []workflow.InputField{{Name: "summary", Description: "Summary."}}
			},
			code: workflow.CodeInvalidInputField,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := validWorkflow()
			tt.edit(&def)

			result := workflow.ValidateDefinition(def, workflow.ValidationOptions{
				Context:      workflow.ValidationContextTaskCreation,
				RoleResolver: workflow.StaticRoleResolver{"coder": true},
			})

			assertHasCodes(t, result, tt.code)
		})
	}
}

func TestGraphReachabilityAndCycles(t *testing.T) {
	t.Run("detached island rejected", func(t *testing.T) {
		def := validWorkflow()
		def.Nodes = append(def.Nodes, workflow.Node{
			WorkflowID:     def.ID,
			ID:             "node_island",
			Key:            "island",
			DisplayName:    "Island",
			Kind:           workflow.NodeKindAgent,
			SubagentRole:   "coder",
			PromptTemplate: "Island.",
		})

		result := validateForTask(def)

		assertHasCodes(t, result, workflow.CodeNodeUnreachableFromStart, workflow.CodeNonTerminalCannotReachTerminal)
	})

	t.Run("cycle allowed when terminal reachable", func(t *testing.T) {
		def := validWorkflow()
		addAgentLoop(&def, "node_agent", "loop", "edge_loop", "loop")

		result := validateForTask(def)

		assertNoCode(t, result, workflow.CodeNonTerminalCannotReachTerminal)
		assertNoCode(t, result, workflow.CodeInvalidFanoutJoinTopology)
	})

	t.Run("self loop allowed when terminal reachable", func(t *testing.T) {
		def := validWorkflow()
		addAgentLoop(&def, "node_agent", "self", "edge_self", "self")

		result := validateForTask(def)

		assertNoCode(t, result, workflow.CodeNonTerminalCannotReachTerminal)
		assertNoCode(t, result, workflow.CodeInvalidFanoutJoinTopology)
	})
}

func TestValidationMessagesIncludeNodeDisplayName(t *testing.T) {
	t.Run("input fields identify the node and field ordinal", func(t *testing.T) {
		def := validWorkflow()
		node := nodeByKeyForValidationTest(t, &def, "implement")
		node.DisplayName = "Planning Recon"
		node.InputFields = []workflow.InputField{
			{Name: "Bad Field", Description: "Field with invalid identifier."},
			{Name: "missing_description", Description: " "},
			{Name: "long_description", Description: stringOf("a", workflow.MaxInputFieldDescriptionChars+1)},
		}

		result := validateForTask(def)

		assertValidationMessage(t, result, workflow.CodeInvalidInputField, "node_agent", "Node Planning Recon: input field #1 field name is invalid")
		assertValidationMessage(t, result, workflow.CodeInputFieldDescriptionRequired, "node_agent", "Node Planning Recon: input field #2 description is required")
		assertValidationMessage(t, result, workflow.CodeInputSchemaTooLarge, "node_agent", "Node Planning Recon: input field #3 description is too large")
	})

	t.Run("output fields identify the node and field ordinal", func(t *testing.T) {
		def := validWorkflow()
		node := nodeByKeyForValidationTest(t, &def, "implement")
		node.DisplayName = "Planning Recon"
		node.OutputFields = []workflow.OutputField{
			{Name: "Bad Field", Description: "Field with invalid identifier."},
			{Name: "summary", Description: " "},
		}

		result := validateForTask(def)

		assertValidationMessage(t, result, workflow.CodeInvalidOutputField, "node_agent", "Node Planning Recon: output field #1 field name is invalid")
		assertValidationMessage(t, result, workflow.CodeOutputFieldDescriptionRequired, "node_agent", "Node Planning Recon: output field #2 description is required")
	})

	t.Run("reachability identifies the node", func(t *testing.T) {
		def := validWorkflow()
		def.Nodes = append(def.Nodes, workflow.Node{
			WorkflowID:     def.ID,
			ID:             "node_planning_recon",
			Key:            "planning_recon",
			DisplayName:    "Planning Recon",
			Kind:           workflow.NodeKindAgent,
			SubagentRole:   "coder",
			PromptTemplate: "Plan the work.",
		})

		result := validateForTask(def)

		assertValidationMessage(t, result, workflow.CodeNodeUnreachableFromStart, "node_planning_recon", "Node Planning Recon not reachable")
		assertValidationMessage(t, result, workflow.CodeNonTerminalCannotReachTerminal, "node_planning_recon", "Node Planning Recon cannot reach a terminal")
	})
}

func TestIdentifierAndReferenceRules(t *testing.T) {
	tests := []struct {
		name string
		edit func(*workflow.Definition)
		code workflow.ValidationErrorCode
	}{
		{name: "missing workflow id", edit: func(def *workflow.Definition) { def.ID = "" }, code: workflow.CodeMissingWorkflowID},
		{name: "invalid workflow display name", edit: func(def *workflow.Definition) { def.DisplayName = " " }, code: workflow.CodeInvalidDisplayName},
		{name: "missing node id", edit: func(def *workflow.Definition) { def.Nodes[1].ID = "" }, code: workflow.CodeMissingNodeID},
		{name: "duplicate node id", edit: func(def *workflow.Definition) { def.Nodes[1].ID = def.Nodes[0].ID }, code: workflow.CodeDuplicateNodeID},
		{name: "missing node key", edit: func(def *workflow.Definition) { def.Nodes[1].Key = "" }, code: workflow.CodeMissingNodeKey},
		{name: "invalid node key", edit: func(def *workflow.Definition) { def.Nodes[1].Key = "Bad-Key" }, code: workflow.CodeInvalidNodeKey},
		{name: "duplicate node key", edit: func(def *workflow.Definition) { def.Nodes[1].Key = def.Nodes[0].Key }, code: workflow.CodeDuplicateNodeKey},
		{name: "invalid node display name", edit: func(def *workflow.Definition) {
			def.Nodes[1].DisplayName = stringOf("a", workflow.MaxDisplayNameChars+1)
		}, code: workflow.CodeInvalidDisplayName},
		{name: "missing transition group id", edit: func(def *workflow.Definition) { def.TransitionGroups[0].ID = "" }, code: workflow.CodeMissingTransitionGroupID},
		{name: "duplicate transition group id", edit: func(def *workflow.Definition) { def.TransitionGroups[1].ID = def.TransitionGroups[0].ID }, code: workflow.CodeDuplicateTransitionGroupID},
		{name: "empty transition group", edit: func(def *workflow.Definition) { def.Edges = def.Edges[1:] }, code: workflow.CodeEmptyTransitionGroup},
		{name: "missing transition id", edit: func(def *workflow.Definition) { def.TransitionGroups[1].TransitionID = "" }, code: workflow.CodeMissingTransitionID},
		{name: "invalid transition id", edit: func(def *workflow.Definition) { def.TransitionGroups[1].TransitionID = "Done!" }, code: workflow.CodeInvalidTransitionID},
		{name: "invalid transition group display name", edit: func(def *workflow.Definition) { def.TransitionGroups[1].DisplayName = "" }, code: workflow.CodeInvalidDisplayName},
		{name: "duplicate transition id per source", edit: func(def *workflow.Definition) {
			addTransitionForValidationTest(def, "group_second_done", "node_agent", "done", "Done Again", "edge_second_done", "second_done", "node_done")
		}, code: workflow.CodeDuplicateTransitionID},
		{name: "edge transition group missing", edit: func(def *workflow.Definition) { def.Edges[1].TransitionGroupID = "missing" }, code: workflow.CodeEdgeTransitionGroupMissing},
		{name: "missing edge id", edit: func(def *workflow.Definition) { def.Edges[1].ID = "" }, code: workflow.CodeMissingEdgeID},
		{name: "duplicate edge id", edit: func(def *workflow.Definition) { def.Edges[1].ID = def.Edges[0].ID }, code: workflow.CodeDuplicateEdgeID},
		{name: "missing edge key", edit: func(def *workflow.Definition) { def.Edges[1].Key = "" }, code: workflow.CodeMissingEdgeKey},
		{name: "invalid edge key", edit: func(def *workflow.Definition) { def.Edges[1].Key = "Done!" }, code: workflow.CodeInvalidEdgeKey},
		{name: "duplicate edge key per group", edit: func(def *workflow.Definition) {
			def.Edges = append(def.Edges, workflow.Edge{WorkflowID: def.ID, ID: "edge_start_dup", Key: "start", TransitionGroupID: "group_start", TargetNodeID: "node_agent", ContextMode: workflow.ContextModeNewSession})
		}, code: workflow.CodeDuplicateEdgeKey},
		{name: "edge target missing", edit: func(def *workflow.Definition) { def.Edges[1].TargetNodeID = "missing" }, code: workflow.CodeEdgeTargetMissing},
		{name: "cross workflow node reference", edit: func(def *workflow.Definition) { def.Nodes[1].WorkflowID = "other" }, code: workflow.CodeCrossWorkflowReference},
		{name: "cross workflow group reference", edit: func(def *workflow.Definition) { def.TransitionGroups[1].WorkflowID = "other" }, code: workflow.CodeCrossWorkflowReference},
		{name: "cross workflow edge reference", edit: func(def *workflow.Definition) { def.Edges[1].WorkflowID = "other" }, code: workflow.CodeCrossWorkflowReference},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := validWorkflow()
			tt.edit(&def)

			result := validateForTask(def)

			assertHasCodes(t, result, tt.code)
		})
	}
}

func TestOutputBindingsTemplatesContextAndRoles(t *testing.T) {
	tests := []struct {
		name string
		edit func(*workflow.Definition)
		code workflow.ValidationErrorCode
	}{
		{name: "invalid output field name", edit: func(def *workflow.Definition) { def.Nodes[1].OutputFields[0].Name = "Bad" }, code: workflow.CodeInvalidOutputField},
		{name: "too long output field name", edit: func(def *workflow.Definition) {
			def.Nodes[1].OutputFields[0].Name = "a" + stringOf("b", workflow.MaxOutputFieldNameChars)
		}, code: workflow.CodeInvalidOutputField},
		{name: "reserved output field name transition_id", edit: func(def *workflow.Definition) {
			def.Nodes[1].OutputFields[0].Name = "transition_id"
		}, code: workflow.CodeInvalidOutputField},
		{name: "reserved output field name commentary", edit: func(def *workflow.Definition) {
			def.Nodes[1].OutputFields[0].Name = "commentary"
		}, code: workflow.CodeInvalidOutputField},
		{name: "duplicate output field", edit: func(def *workflow.Definition) {
			def.Nodes[1].OutputFields = append(def.Nodes[1].OutputFields, workflow.OutputField{Name: "summary", Description: "Another summary."})
		}, code: workflow.CodeDuplicateOutputField},
		{name: "output description required", edit: func(def *workflow.Definition) { def.Nodes[1].OutputFields[0].Description = " " }, code: workflow.CodeOutputFieldDescriptionRequired},
		{name: "output description too large", edit: func(def *workflow.Definition) {
			def.Nodes[1].OutputFields[0].Description = stringOf("a", workflow.MaxOutputFieldDescriptionChars+1)
		}, code: workflow.CodeOutputSchemaTooLarge},
		{name: "invalid template placeholder", edit: func(def *workflow.Definition) {
			def.Nodes[1].PromptTemplate = "Use {{.Inputs.missing}}."
			def.Edges[0].InputBindings = []workflow.InputBinding{{Name: "task_title", Source: workflow.BindingSourceTask, Field: "title"}}
		}, code: workflow.CodeInvalidTemplatePlaceholder},
		{name: "invalid template syntax", edit: func(def *workflow.Definition) {
			def.Nodes[1].PromptTemplate = "Use {{.Inputs.task_title"
			def.Edges[0].InputBindings = []workflow.InputBinding{{Name: "task_title", Source: workflow.BindingSourceTask, Field: "title"}}
		}, code: workflow.CodeInvalidTemplatePlaceholder},
		{name: "invalid context mode", edit: func(def *workflow.Definition) { def.Edges[1].ContextMode = workflow.ContextMode("reuse") }, code: workflow.CodeInvalidContextMode},
		{name: "agent role required", edit: func(def *workflow.Definition) { def.Nodes[1].SubagentRole = "" }, code: workflow.CodeAgentRoleRequired},
		{name: "agent role missing", edit: func(def *workflow.Definition) { def.Nodes[1].SubagentRole = "reviewer" }, code: workflow.CodeAgentRoleMissing},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := validWorkflow()
			tt.edit(&def)

			result := validateForTask(def)

			assertHasCodes(t, result, tt.code)
		})
	}

	t.Run("valid input bindings and template placeholders pass", func(t *testing.T) {
		def := validWorkflow()
		def.Nodes[1].PromptTemplate = "Implement {{.Inputs.task_title}} with {{.Inputs.prior_summary}}."
		def.Nodes[1].InputFields = []workflow.InputField{
			{Name: "task_title", Description: "Task title."},
			{Name: "prior_summary", Description: "Prior summary."},
		}
		def.Edges[0].InputBindings = []workflow.InputBinding{
			{Name: "task_title", Source: workflow.BindingSourceTask, Field: "title"},
			{Name: "prior_summary", Source: workflow.BindingSourceTransitionOutput, Field: "commentary"},
		}

		result := validateForTask(def)

		assertNoCode(t, result, workflow.CodeInvalidInputBinding)
		assertNoCode(t, result, workflow.CodeInvalidTemplatePlaceholder)
	})

	t.Run("template functions do not count as input placeholders", func(t *testing.T) {
		def := validWorkflow()
		def.Nodes[1].PromptTemplate = `{{if eq .Inputs.task_title "Task"}}{{printf "%s" .Inputs.prior_summary}}{{end}}`
		def.Nodes[1].InputFields = []workflow.InputField{
			{Name: "task_title", Description: "Task title."},
			{Name: "prior_summary", Description: "Prior summary."},
		}
		def.Edges[0].InputBindings = []workflow.InputBinding{
			{Name: "task_title", Source: workflow.BindingSourceTask, Field: "title"},
			{Name: "prior_summary", Source: workflow.BindingSourceTransitionOutput, Field: "commentary"},
		}

		result := validateForTask(def)

		assertNoCode(t, result, workflow.CodeInvalidTemplatePlaceholder)
	})

	t.Run("unknown input placeholder exposes structured details", func(t *testing.T) {
		def := validWorkflow()
		def.Nodes[1].PromptTemplate = "Use {{.Inputs.missing}}."

		result := validateForTask(def)

		for _, err := range result.Errors {
			if err.Code == workflow.CodeInvalidTemplatePlaceholder {
				if err.InputName != "missing" || err.Placeholder != ".Inputs.missing" {
					t.Fatalf("placeholder details = %+v", err)
				}
				return
			}
		}
		t.Fatalf("missing %s in %+v", workflow.CodeInvalidTemplatePlaceholder, result.Errors)
	})

	t.Run("valid prior node placeholder passes", func(t *testing.T) {
		def := inputWorkflow()
		for index := range def.Nodes {
			if def.Nodes[index].ID == "node_plan" {
				def.Nodes[index].OutputFields = []workflow.OutputField{{Name: "summary", Description: "Plan summary."}}
			}
			if def.Nodes[index].ID == "node_implement" {
				def.Nodes[index].PromptTemplate = "Use {{.Nodes.plan.summary}}."
				def.Nodes[index].InputFields = nil
			}
		}

		result := validateForTask(def)

		assertNoCode(t, result, workflow.CodeInvalidTemplatePlaceholder)
	})

	t.Run("missing prior node placeholder exposes structured details", func(t *testing.T) {
		def := inputWorkflow()
		for index := range def.Nodes {
			if def.Nodes[index].ID == "node_plan" {
				def.Nodes[index].OutputFields = []workflow.OutputField{{Name: "summary", Description: "Plan summary."}}
			}
			if def.Nodes[index].ID == "node_implement" {
				def.Nodes[index].PromptTemplate = "Use {{.Nodes.deleted.summary}}."
				def.Nodes[index].InputFields = nil
			}
		}

		result := validateForTask(def)

		for _, err := range result.Errors {
			if err.Code == workflow.CodeInvalidTemplatePlaceholder {
				if err.FieldName != "summary" || err.Placeholder != ".Nodes.deleted.summary" {
					t.Fatalf("placeholder details = %+v", err)
				}
				return
			}
		}
		t.Fatalf("missing %s in %+v", workflow.CodeInvalidTemplatePlaceholder, result.Errors)
	})

	t.Run("future node placeholder blocks task validation", func(t *testing.T) {
		def := inputWorkflow()
		for index := range def.Nodes {
			if def.Nodes[index].ID == "node_implement" {
				def.Nodes[index].OutputFields = []workflow.OutputField{{Name: "summary", Description: "Implementation summary."}}
			}
			if def.Nodes[index].ID == "node_plan" {
				def.Nodes[index].PromptTemplate = "Plan {{.Nodes.implement.summary}}."
			}
		}

		result := validateForTask(def)

		assertHasCodes(t, result, workflow.CodeInvalidTemplatePlaceholder)
	})

	t.Run("malformed node placeholder blocks task validation", func(t *testing.T) {
		def := inputWorkflow()
		for index := range def.Nodes {
			if def.Nodes[index].ID == "node_plan" {
				def.Nodes[index].OutputFields = []workflow.OutputField{{Name: "summary", Description: "Plan summary."}}
			}
			if def.Nodes[index].ID == "node_implement" {
				def.Nodes[index].PromptTemplate = "Use {{.Nodes.plan}} and {{.Nodes.plan.summary.extra}}."
				def.Nodes[index].InputFields = nil
			}
		}

		result := validateForTask(def)

		assertHasCodes(t, result, workflow.CodeInvalidTemplatePlaceholder)
	})

	t.Run("self node placeholder blocks task validation", func(t *testing.T) {
		def := inputWorkflow()
		for index := range def.Nodes {
			if def.Nodes[index].ID == "node_implement" {
				def.Nodes[index].OutputFields = []workflow.OutputField{{Name: "summary", Description: "Implementation summary."}}
				def.Nodes[index].PromptTemplate = "Use {{.Nodes.implement.summary}}."
				def.Nodes[index].InputFields = nil
			}
		}

		result := validateForTask(def)

		assertHasCodes(t, result, workflow.CodeInvalidTemplatePlaceholder)
	})

	t.Run("unknown node output placeholder blocks task validation", func(t *testing.T) {
		def := inputWorkflow()
		for index := range def.Nodes {
			if def.Nodes[index].ID == "node_plan" {
				def.Nodes[index].OutputFields = []workflow.OutputField{{Name: "summary", Description: "Plan summary."}}
			}
			if def.Nodes[index].ID == "node_implement" {
				def.Nodes[index].PromptTemplate = "Use {{.Nodes.plan.deleted}}."
				def.Nodes[index].InputFields = nil
			}
		}

		result := validateForTask(def)

		assertHasCodes(t, result, workflow.CodeInvalidTemplatePlaceholder)
	})

	t.Run("malformed input placeholder blocks task validation", func(t *testing.T) {
		def := inputWorkflow()
		for index := range def.Nodes {
			if def.Nodes[index].ID == "node_implement" {
				def.Nodes[index].PromptTemplate = "Use {{.Inputs}} and {{.Inputs.plan.extra}}."
			}
		}

		result := validateForTask(def)

		assertHasCodes(t, result, workflow.CodeInvalidTemplatePlaceholder)
	})

	t.Run("dynamic node placeholder lookup blocks task validation", func(t *testing.T) {
		def := inputWorkflow()
		for index := range def.Nodes {
			if def.Nodes[index].ID == "node_implement" {
				def.Nodes[index].PromptTemplate = `Use {{index .Nodes "plan" "summary"}}.`
				def.Nodes[index].InputFields = nil
			}
		}

		result := validateForTask(def)

		assertHasCodes(t, result, workflow.CodeInvalidTemplatePlaceholder)
	})

	t.Run("named template node placeholder blocks task validation", func(t *testing.T) {
		def := inputWorkflow()
		for index := range def.Nodes {
			if def.Nodes[index].ID == "node_implement" {
				def.Nodes[index].PromptTemplate = `{{define "frag"}}{{.Nodes.deleted.summary}}{{end}}{{template "frag" .}}`
				def.Nodes[index].InputFields = nil
			}
		}

		result := validateForTask(def)

		assertHasCodes(t, result, workflow.CodeInvalidTemplatePlaceholder)
	})

	t.Run("variable chained node placeholder blocks task validation", func(t *testing.T) {
		def := inputWorkflow()
		for index := range def.Nodes {
			if def.Nodes[index].ID == "node_implement" {
				def.Nodes[index].PromptTemplate = `{{$root := .}}{{$root.Nodes.deleted.summary}}`
				def.Nodes[index].InputFields = nil
			}
		}

		result := validateForTask(def)

		assertHasCodes(t, result, workflow.CodeInvalidTemplatePlaceholder)
	})

	t.Run("template invocation node placeholder blocks task validation", func(t *testing.T) {
		def := inputWorkflow()
		for index := range def.Nodes {
			if def.Nodes[index].ID == "node_implement" {
				def.Nodes[index].PromptTemplate = `{{define "frag"}}{{.}}{{end}}{{template "frag" .Nodes.deleted.summary}}`
				def.Nodes[index].InputFields = nil
			}
		}

		result := validateForTask(def)

		assertHasCodes(t, result, workflow.CodeInvalidTemplatePlaceholder)
	})

	t.Run("index variable node placeholder blocks task validation", func(t *testing.T) {
		def := inputWorkflow()
		for index := range def.Nodes {
			if def.Nodes[index].ID == "node_implement" {
				def.Nodes[index].PromptTemplate = `{{$root := .}}{{index $root.Nodes "deleted" "summary"}}`
				def.Nodes[index].InputFields = nil
			}
		}

		result := validateForTask(def)

		assertHasCodes(t, result, workflow.CodeInvalidTemplatePlaceholder)
	})

	t.Run("index root variable node placeholder blocks task validation", func(t *testing.T) {
		def := inputWorkflow()
		for index := range def.Nodes {
			if def.Nodes[index].ID == "node_implement" {
				def.Nodes[index].PromptTemplate = `{{$root := .}}{{index $root "Nodes" "deleted" "summary"}}`
				def.Nodes[index].InputFields = nil
			}
		}

		result := validateForTask(def)

		assertHasCodes(t, result, workflow.CodeInvalidTemplatePlaceholder)
	})

	t.Run("piped index node placeholder blocks task validation", func(t *testing.T) {
		def := inputWorkflow()
		for index := range def.Nodes {
			if def.Nodes[index].ID == "node_implement" {
				def.Nodes[index].PromptTemplate = `{{$root := .}}{{$root | index "Nodes" "deleted" "summary"}}`
				def.Nodes[index].InputFields = nil
			}
		}

		result := validateForTask(def)

		assertHasCodes(t, result, workflow.CodeInvalidTemplatePlaceholder)
	})

	t.Run("named template index node placeholder blocks task validation", func(t *testing.T) {
		def := inputWorkflow()
		for index := range def.Nodes {
			if def.Nodes[index].ID == "node_implement" {
				def.Nodes[index].PromptTemplate = `{{define "frag"}}{{$root := .}}{{index $root "Nodes" "deleted" "summary"}}{{end}}{{template "frag" .}}`
				def.Nodes[index].InputFields = nil
			}
		}

		result := validateForTask(def)

		assertHasCodes(t, result, workflow.CodeInvalidTemplatePlaceholder)
	})
}

func TestRuntimeValidationBlocksUnimplementedExecutionFeatures(t *testing.T) {
	t.Run("approval-gated edges are valid runtime features", func(t *testing.T) {
		def := validWorkflow()
		def.Edges[1].RequiresApproval = true

		draft := workflow.ValidateDefinition(def, workflow.ValidationOptions{Context: workflow.ValidationContextDraft})
		assertNoCode(t, draft, workflow.CodeUnsupportedApprovalExecution)
		if draft.HasBlockingErrors() {
			t.Fatalf("draft approval should not block saving: %+v", draft.BlockingErrors())
		}

		task := validateForTask(def)
		assertNoCode(t, task, workflow.CodeUnsupportedApprovalExecution)
		if task.HasBlockingErrors() {
			t.Fatalf("task approval should not block execution: %+v", task.BlockingErrors())
		}
	})

	t.Run("context modes are valid runtime features", func(t *testing.T) {
		for _, mode := range []workflow.ContextMode{workflow.ContextModeContinueSession, workflow.ContextModeCompactAndContinueSession} {
			t.Run(string(mode), func(t *testing.T) {
				def := validWorkflow()
				def.Edges[1].ContextMode = mode

				draft := workflow.ValidateDefinition(def, workflow.ValidationOptions{Context: workflow.ValidationContextDraft})
				assertNoCode(t, draft, workflow.CodeUnsupportedContextMode)
				if draft.HasBlockingErrors() {
					t.Fatalf("draft context mode should not block saving: %+v", draft.BlockingErrors())
				}

				task := validateForTask(def)
				assertNoCode(t, task, workflow.CodeUnsupportedContextMode)
				if task.HasBlockingErrors() {
					t.Fatalf("task context mode should not block execution: %+v", task.BlockingErrors())
				}
			})
		}
	})

	t.Run("join targets are valid runtime features", func(t *testing.T) {
		def := fanoutWorkflow()

		draft := workflow.ValidateDefinition(def, workflow.ValidationOptions{Context: workflow.ValidationContextDraft})
		assertNoCode(t, draft, workflow.CodeUnsupportedJoinExecution)
		if draft.HasBlockingErrors() {
			t.Fatalf("draft join should not block saving: %+v", draft.BlockingErrors())
		}

		task := validateForTask(def)
		assertNoCode(t, task, workflow.CodeUnsupportedJoinExecution)
		if task.HasBlockingErrors() {
			t.Fatalf("task join should not block execution: %+v", task.BlockingErrors())
		}
	})

	t.Run("join bindings are valid runtime features", func(t *testing.T) {
		def := validWorkflow()
		def.Edges[0].InputBindings = []workflow.InputBinding{{Name: "joined", Source: workflow.BindingSourceJoin, Field: "aggregate"}}

		draft := workflow.ValidateDefinition(def, workflow.ValidationOptions{Context: workflow.ValidationContextDraft})
		assertNoCode(t, draft, workflow.CodeUnsupportedJoinBinding)
		if draft.HasBlockingErrors() {
			t.Fatalf("draft join binding should not block saving: %+v", draft.BlockingErrors())
		}

		task := validateForTask(def)
		assertNoCode(t, task, workflow.CodeUnsupportedJoinBinding)
		if task.HasBlockingErrors() {
			t.Fatalf("task join binding should not block execution: %+v", task.BlockingErrors())
		}
	})
}

func TestFanoutJoinTopology(t *testing.T) {
	t.Run("valid fanout has one nearest common join", func(t *testing.T) {
		def := fanoutWorkflow()

		result := validateForTask(def)

		assertNoCode(t, result, workflow.CodeInvalidFanoutJoinTopology)
	})

	t.Run("valid fanout allows farther common join after unique nearest join", func(t *testing.T) {
		def := fanoutWorkflow()
		def.Nodes = append(def.Nodes,
			workflow.Node{WorkflowID: def.ID, ID: "node_impl_a_late", Key: "impl_a_late", DisplayName: "Implement A Late", Kind: workflow.NodeKindAgent, SubagentRole: "coder", PromptTemplate: "A late."},
			workflow.Node{WorkflowID: def.ID, ID: "node_impl_b_late", Key: "impl_b_late", DisplayName: "Implement B Late", Kind: workflow.NodeKindAgent, SubagentRole: "coder", PromptTemplate: "B late."},
			workflow.Node{WorkflowID: def.ID, ID: "node_join_late", Key: "join_late", DisplayName: "Join Late", Kind: workflow.NodeKindJoin},
		)
		def.TransitionGroups = append(def.TransitionGroups,
			workflow.TransitionGroup{WorkflowID: def.ID, ID: "group_impl_a_late", SourceNodeID: "node_impl_a", TransitionID: "join_late", DisplayName: "Join Late"},
			workflow.TransitionGroup{WorkflowID: def.ID, ID: "group_impl_a_late_join", SourceNodeID: "node_impl_a_late", TransitionID: "join_late", DisplayName: "Join Late"},
			workflow.TransitionGroup{WorkflowID: def.ID, ID: "group_impl_b_late", SourceNodeID: "node_impl_b", TransitionID: "join_late", DisplayName: "Join Late"},
			workflow.TransitionGroup{WorkflowID: def.ID, ID: "group_impl_b_late_join", SourceNodeID: "node_impl_b_late", TransitionID: "join_late", DisplayName: "Join Late"},
			workflow.TransitionGroup{WorkflowID: def.ID, ID: "group_join_late_done", SourceNodeID: "node_join_late", TransitionID: "done", DisplayName: "Done"},
		)
		def.Edges = append(def.Edges,
			workflow.Edge{WorkflowID: def.ID, ID: "edge_impl_a_late", Key: "late_a", TransitionGroupID: "group_impl_a_late", TargetNodeID: "node_impl_a_late", ContextMode: workflow.ContextModeNewSession},
			workflow.Edge{WorkflowID: def.ID, ID: "edge_impl_a_late_join", Key: "late_join_a", TransitionGroupID: "group_impl_a_late_join", TargetNodeID: "node_join_late", ContextMode: workflow.ContextModeNewSession},
			workflow.Edge{WorkflowID: def.ID, ID: "edge_impl_b_late", Key: "late_b", TransitionGroupID: "group_impl_b_late", TargetNodeID: "node_impl_b_late", ContextMode: workflow.ContextModeNewSession},
			workflow.Edge{WorkflowID: def.ID, ID: "edge_impl_b_late_join", Key: "late_join_b", TransitionGroupID: "group_impl_b_late_join", TargetNodeID: "node_join_late", ContextMode: workflow.ContextModeNewSession},
			workflow.Edge{WorkflowID: def.ID, ID: "edge_join_late_done", Key: "done", TransitionGroupID: "group_join_late_done", TargetNodeID: "node_done", ContextMode: workflow.ContextModeNewSession},
		)

		result := validateForTask(def)

		assertNoCode(t, result, workflow.CodeInvalidFanoutJoinTopology)
	})

	tests := []struct {
		name string
		edit func(*workflow.Definition)
	}{
		{
			name: "terminal before join",
			edit: func(def *workflow.Definition) {
				def.Edges[2].TargetNodeID = "node_done"
			},
		},
		{
			name: "nested fanout before join",
			edit: func(def *workflow.Definition) {
				def.Nodes = append(def.Nodes, workflow.Node{WorkflowID: def.ID, ID: "node_extra", Key: "extra", DisplayName: "Extra", Kind: workflow.NodeKindAgent, SubagentRole: "coder", PromptTemplate: "Extra."})
				def.Edges[2].TargetNodeID = "node_extra"
				def.TransitionGroups = append(def.TransitionGroups, workflow.TransitionGroup{WorkflowID: def.ID, ID: "group_extra_fanout", SourceNodeID: "node_extra", TransitionID: "split", DisplayName: "Split"})
				def.Edges = append(def.Edges,
					workflow.Edge{WorkflowID: def.ID, ID: "edge_extra_a", Key: "extra_a", TransitionGroupID: "group_extra_fanout", TargetNodeID: "node_impl_a", ContextMode: workflow.ContextModeNewSession},
					workflow.Edge{WorkflowID: def.ID, ID: "edge_extra_b", Key: "extra_b", TransitionGroupID: "group_extra_fanout", TargetNodeID: "node_impl_b", ContextMode: workflow.ContextModeNewSession},
				)
			},
		},
		{
			name: "cycle before join",
			edit: func(def *workflow.Definition) {
				addAgentLoop(def, "node_impl_a", "cycle", "edge_cycle", "cycle")
			},
		},
		{
			name: "ambiguous nearest join",
			edit: func(def *workflow.Definition) {
				def.Nodes = append(def.Nodes, workflow.Node{WorkflowID: def.ID, ID: "node_join2", Key: "join2", DisplayName: "Join 2", Kind: workflow.NodeKindJoin})
				def.TransitionGroups = append(def.TransitionGroups,
					workflow.TransitionGroup{WorkflowID: def.ID, ID: "group_impl_a_join2", SourceNodeID: "node_impl_a", TransitionID: "join2", DisplayName: "Join 2"},
					workflow.TransitionGroup{WorkflowID: def.ID, ID: "group_impl_b_join2", SourceNodeID: "node_impl_b", TransitionID: "join2", DisplayName: "Join 2"},
					workflow.TransitionGroup{WorkflowID: def.ID, ID: "group_join2_done", SourceNodeID: "node_join2", TransitionID: "done", DisplayName: "Done"},
				)
				def.Edges = append(def.Edges,
					workflow.Edge{WorkflowID: def.ID, ID: "edge_impl_a_join2", Key: "join2", TransitionGroupID: "group_impl_a_join2", TargetNodeID: "node_join2", ContextMode: workflow.ContextModeNewSession},
					workflow.Edge{WorkflowID: def.ID, ID: "edge_impl_b_join2", Key: "join2", TransitionGroupID: "group_impl_b_join2", TargetNodeID: "node_join2", ContextMode: workflow.ContextModeNewSession},
					workflow.Edge{WorkflowID: def.ID, ID: "edge_join2_done", Key: "done", TransitionGroupID: "group_join2_done", TargetNodeID: "node_done", ContextMode: workflow.ContextModeNewSession},
				)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := fanoutWorkflow()
			tt.edit(&def)

			result := validateForTask(def)

			assertHasCodes(t, result, workflow.CodeInvalidFanoutJoinTopology)
		})
	}
}

func TestNodeGroupV1ParallelGroupValidation(t *testing.T) {
	t.Run("valid group has branches join fanout and branch join edges", func(t *testing.T) {
		def := fanoutWorkflow()
		addV1NodeGroup(&def)

		result := validateForTask(def)

		assertNoCode(t, result, workflow.CodeInvalidNodeGroup)
	})

	t.Run("one branch draft group is invalid but non-blocking", func(t *testing.T) {
		def := fanoutWorkflow()
		addV1NodeGroup(&def)
		def.Nodes = setNodeGroup(def.Nodes, "node_impl_b", "")

		result := workflow.ValidateDefinition(def, workflow.ValidationOptions{Context: workflow.ValidationContextDraft, RoleResolver: workflow.StaticRoleResolver{"coder": true}})

		assertHasCodes(t, result, workflow.CodeInvalidNodeGroup)
		if !result.HasBlockingErrors() {
			t.Fatalf("invalid draft node group shape should block graph save")
		}
	})

	t.Run("missing join is invalid", func(t *testing.T) {
		def := fanoutWorkflow()
		addV1NodeGroup(&def)
		def.Nodes = setNodeGroup(def.Nodes, "node_join", "")

		result := validateForTask(def)

		assertHasCodes(t, result, workflow.CodeInvalidNodeGroup)
	})

	t.Run("missing fanout is invalid", func(t *testing.T) {
		def := fanoutWorkflow()
		addV1NodeGroup(&def)
		def.Edges = def.Edges[:1]

		result := validateForTask(def)

		assertHasCodes(t, result, workflow.CodeInvalidNodeGroup)
	})

	t.Run("start backed fanout explains split agent workaround", func(t *testing.T) {
		def := fanoutWorkflow()
		addV1NodeGroup(&def)
		transitionGroupByIDForValidationTest(t, &def, "group_split").SourceNodeID = "node_start"

		result := workflow.ValidateDefinition(def, workflow.ValidationOptions{Context: workflow.ValidationContextDraft, RoleResolver: workflow.StaticRoleResolver{"coder": true}})

		assertValidationMessage(
			t,
			result,
			workflow.CodeInvalidNodeGroup,
			"",
			"Node Backlog cannot directly fan out into a node group yet; insert one split agent after it, fan out from that agent into the group, then join the branches",
		)
	})

	t.Run("start backed fanout message uses renamed start node", func(t *testing.T) {
		def := fanoutWorkflow()
		addV1NodeGroup(&def)
		nodeByKeyForValidationTest(t, &def, "backlog").DisplayName = "Inbox"
		transitionGroupByIDForValidationTest(t, &def, "group_split").SourceNodeID = "node_start"

		result := workflow.ValidateDefinition(def, workflow.ValidationOptions{Context: workflow.ValidationContextDraft, RoleResolver: workflow.StaticRoleResolver{"coder": true}})

		assertValidationMessage(
			t,
			result,
			workflow.CodeInvalidNodeGroup,
			"",
			"Node Inbox cannot directly fan out into a node group yet; insert one split agent after it, fan out from that agent into the group, then join the branches",
		)
	})

	t.Run("separate start backed branch transitions explain split agent workaround", func(t *testing.T) {
		def := fanoutWorkflow()
		addV1NodeGroup(&def)
		transitionGroupByIDForValidationTest(t, &def, "group_split").SourceNodeID = "node_start"
		def.TransitionGroups = append(def.TransitionGroups, workflow.TransitionGroup{
			WorkflowID:   def.ID,
			ID:           "group_start_impl_b",
			SourceNodeID: "node_start",
			TransitionID: "split_b",
			DisplayName:  "Split B",
		})
		edgeByIDForValidationTest(t, &def, "edge_split_b").TransitionGroupID = "group_start_impl_b"

		result := workflow.ValidateDefinition(def, workflow.ValidationOptions{Context: workflow.ValidationContextDraft, RoleResolver: workflow.StaticRoleResolver{"coder": true}})

		assertValidationMessage(
			t,
			result,
			workflow.CodeInvalidNodeGroup,
			"",
			"Node Backlog cannot directly fan out into a node group yet; insert one split agent after it, fan out from that agent into the group, then join the branches",
		)
	})

	t.Run("separate source branch transitions explain single fanout repair", func(t *testing.T) {
		def := fanoutWorkflow()
		addV1NodeGroup(&def)
		def.TransitionGroups = append(def.TransitionGroups, workflow.TransitionGroup{
			WorkflowID:   def.ID,
			ID:           "group_plan_impl_b",
			SourceNodeID: "node_plan",
			TransitionID: "implement_b",
			DisplayName:  "Implement B",
		})
		edgeByIDForValidationTest(t, &def, "edge_split_b").TransitionGroupID = "group_plan_impl_b"

		result := workflow.ValidateDefinition(def, workflow.ValidationOptions{Context: workflow.ValidationContextDraft, RoleResolver: workflow.StaticRoleResolver{"coder": true}})

		assertValidationMessage(
			t,
			result,
			workflow.CodeInvalidNodeGroup,
			"",
			"Node Plan uses separate transitions into the node group branches; use one transition from Plan with one edge to each branch, then connect every branch to the join",
		)
	})

	t.Run("stale same source branch transition with valid fanout explains single fanout repair", func(t *testing.T) {
		def := fanoutWorkflow()
		addV1NodeGroup(&def)
		def.TransitionGroups = append(def.TransitionGroups, workflow.TransitionGroup{
			WorkflowID:   def.ID,
			ID:           "group_plan_impl_b_stale",
			SourceNodeID: "node_plan",
			TransitionID: "implement_b_stale",
			DisplayName:  "Implement B",
		})
		def.Edges = append(def.Edges, workflow.Edge{
			WorkflowID:        def.ID,
			ID:                "edge_plan_impl_b_stale",
			Key:               "implement_b_stale",
			TransitionGroupID: "group_plan_impl_b_stale",
			TargetNodeID:      "node_impl_b",
			ContextMode:       workflow.ContextModeNewSession,
		})

		result := workflow.ValidateDefinition(def, workflow.ValidationOptions{Context: workflow.ValidationContextDraft, RoleResolver: workflow.StaticRoleResolver{"coder": true}})

		assertValidationMessage(
			t,
			result,
			workflow.CodeInvalidNodeGroup,
			"",
			"Node Plan uses separate transitions into the node group branches; use one transition from Plan with one edge to each branch, then connect every branch to the join",
		)
	})

	t.Run("duplicate fanout branch edge is invalid", func(t *testing.T) {
		def := fanoutWorkflow()
		addV1NodeGroup(&def)
		def.Edges = append(def.Edges, workflow.Edge{
			WorkflowID:        def.ID,
			ID:                "edge_split_b_duplicate",
			Key:               "split_b_duplicate",
			TransitionGroupID: "group_split",
			TargetNodeID:      "node_impl_b",
			ContextMode:       workflow.ContextModeNewSession,
		})

		result := workflow.ValidateDefinition(def, workflow.ValidationOptions{Context: workflow.ValidationContextDraft, RoleResolver: workflow.StaticRoleResolver{"coder": true}})

		assertValidationMessage(
			t,
			result,
			workflow.CodeInvalidNodeGroup,
			"",
			"node group must be represented by one fan-out transition group and branch edges into its join",
		)
	})
}

func TestContextSourceValidation(t *testing.T) {
	t.Run("default immediate source preserves existing workflows", func(t *testing.T) {
		def := validWorkflow()

		result := validateForTask(def)

		assertNoCode(t, result, workflow.CodeInvalidContextSource)
	})

	t.Run("direct selected source node validates", func(t *testing.T) {
		def := validWorkflow()
		def.Nodes = append(def.Nodes, workflow.Node{WorkflowID: def.ID, ID: "node_review", Key: "review", DisplayName: "Review", Kind: workflow.NodeKindAgent, SubagentRole: "coder", PromptTemplate: "Review."})
		def.TransitionGroups[1] = workflow.TransitionGroup{WorkflowID: def.ID, ID: "group_review", SourceNodeID: "node_agent", TransitionID: "review", DisplayName: "Review"}
		def.Edges[1] = workflow.Edge{WorkflowID: def.ID, ID: "edge_review", Key: "review", TransitionGroupID: "group_review", TargetNodeID: "node_review", ContextMode: workflow.ContextModeContinueSession, ContextSource: workflow.ContextSource{Kind: workflow.ContextSourceSelectedNode, NodeKey: "implement"}, OutputRequirements: []workflow.OutputRequirement{{FieldName: "summary"}}}
		def.TransitionGroups = append(def.TransitionGroups, workflow.TransitionGroup{WorkflowID: def.ID, ID: "group_done", SourceNodeID: "node_review", TransitionID: "done", DisplayName: "Done"})
		def.Edges = append(def.Edges, workflow.Edge{WorkflowID: def.ID, ID: "edge_done", Key: "done", TransitionGroupID: "group_done", TargetNodeID: "node_done", ContextMode: workflow.ContextModeNewSession})

		result := validateForTask(def)

		assertNoCode(t, result, workflow.CodeInvalidContextSource)
	})

	t.Run("post join selected dominator source validates", func(t *testing.T) {
		def := reviewAcceptanceWorkflow()
		edge := edgeByIDForValidationTest(t, &def, "edge_accept_open_pr")
		edge.ContextSource = workflow.ContextSource{Kind: workflow.ContextSourceSelectedNode, NodeKey: "implementation"}
		edge.ContextMode = workflow.ContextModeContinueSession

		result := validateForTask(def)

		assertNoCode(t, result, workflow.CodeInvalidContextSource)
	})

	t.Run("future node is invalid", func(t *testing.T) {
		def := reviewAcceptanceWorkflow()
		edge := edgeByIDForValidationTest(t, &def, "edge_implementation_review")
		edge.ContextMode = workflow.ContextModeContinueSession
		edge.ContextSource = workflow.ContextSource{Kind: workflow.ContextSourceSelectedNode, NodeKey: "open_pr"}

		result := validateForTask(def)

		assertHasCodes(t, result, workflow.CodeInvalidContextSource)
	})

	t.Run("optional branch is invalid", func(t *testing.T) {
		def := reviewAcceptanceWorkflow()
		def.Nodes = append(def.Nodes, workflow.Node{WorkflowID: def.ID, ID: "node_optional", Key: "optional", DisplayName: "Optional", Kind: workflow.NodeKindAgent, SubagentRole: "coder", PromptTemplate: "Optional."})
		def.TransitionGroups = append(def.TransitionGroups,
			workflow.TransitionGroup{WorkflowID: def.ID, ID: "group_implementation_optional", SourceNodeID: "node_implementation", TransitionID: "optional", DisplayName: "Optional"},
			workflow.TransitionGroup{WorkflowID: def.ID, ID: "group_optional_review", SourceNodeID: "node_optional", TransitionID: "review", DisplayName: "Review"},
		)
		def.Edges = append(def.Edges,
			workflow.Edge{WorkflowID: def.ID, ID: "edge_implementation_optional", Key: "optional", TransitionGroupID: "group_implementation_optional", TargetNodeID: "node_optional", ContextMode: workflow.ContextModeNewSession, OutputRequirements: []workflow.OutputRequirement{{FieldName: "summary"}}},
			workflow.Edge{WorkflowID: def.ID, ID: "edge_optional_review", Key: "review", TransitionGroupID: "group_optional_review", TargetNodeID: "node_code_review", ContextMode: workflow.ContextModeNewSession},
		)
		edge := edgeByIDForValidationTest(t, &def, "edge_accept_open_pr")
		edge.ContextMode = workflow.ContextModeContinueSession
		edge.ContextSource = workflow.ContextSource{Kind: workflow.ContextSourceSelectedNode, NodeKey: "optional"}

		result := validateForTask(def)

		assertHasCodes(t, result, workflow.CodeInvalidContextSource)
	})

	t.Run("sibling fanout branch after join is invalid in v1", func(t *testing.T) {
		def := reviewAcceptanceWorkflow()
		edge := edgeByIDForValidationTest(t, &def, "edge_accept_open_pr")
		edge.ContextMode = workflow.ContextModeContinueSession
		edge.ContextSource = workflow.ContextSource{Kind: workflow.ContextSourceSelectedNode, NodeKey: "code_review"}

		result := validateForTask(def)

		assertHasCodes(t, result, workflow.CodeInvalidContextSource)
	})

	t.Run("selected source must be agent", func(t *testing.T) {
		for _, key := range []workflow.ModelKey{"backlog", "review_join", "done"} {
			t.Run(string(key), func(t *testing.T) {
				def := reviewAcceptanceWorkflow()
				edge := edgeByIDForValidationTest(t, &def, "edge_accept_open_pr")
				edge.ContextMode = workflow.ContextModeContinueSession
				edge.ContextSource = workflow.ContextSource{Kind: workflow.ContextSourceSelectedNode, NodeKey: key}

				result := validateForTask(def)

				assertHasCodes(t, result, workflow.CodeInvalidContextSource)
			})
		}
	})

	t.Run("missing node key is invalid", func(t *testing.T) {
		def := reviewAcceptanceWorkflow()
		edge := edgeByIDForValidationTest(t, &def, "edge_accept_open_pr")
		edge.ContextMode = workflow.ContextModeContinueSession
		edge.ContextSource = workflow.ContextSource{Kind: workflow.ContextSourceSelectedNode, NodeKey: "missing"}

		result := validateForTask(def)

		assertHasCodes(t, result, workflow.CodeInvalidContextSource)
	})

	t.Run("selected target node is invalid", func(t *testing.T) {
		def := reviewAcceptanceWorkflow()
		edge := edgeByIDForValidationTest(t, &def, "edge_accept_open_pr")
		edge.ContextMode = workflow.ContextModeContinueSession
		edge.ContextSource = workflow.ContextSource{Kind: workflow.ContextSourceSelectedNode, NodeKey: "open_pr"}

		result := validateForTask(def)

		assertHasCodes(t, result, workflow.CodeInvalidContextSource)
	})

	t.Run("start edge explicit context source is invalid", func(t *testing.T) {
		def := reviewAcceptanceWorkflow()
		edge := edgeByIDForValidationTest(t, &def, "edge_start")
		edge.ContextMode = workflow.ContextModeContinueSession
		edge.ContextSource = workflow.ContextSource{Kind: workflow.ContextSourceSelectedNode, NodeKey: "implementation"}

		result := validateForTask(def)

		assertHasCodes(t, result, workflow.CodeInvalidContextSource)
	})

	t.Run("new session cannot select context source", func(t *testing.T) {
		def := reviewAcceptanceWorkflow()
		edge := edgeByIDForValidationTest(t, &def, "edge_accept_open_pr")
		edge.ContextMode = workflow.ContextModeNewSession
		edge.ContextSource = workflow.ContextSource{Kind: workflow.ContextSourceSelectedNode, NodeKey: "implementation"}

		result := validateForTask(def)

		assertHasCodes(t, result, workflow.CodeInvalidContextSource)
	})

	t.Run("continue session role mismatch is invalid but compact continue allows it", func(t *testing.T) {
		for _, tc := range []struct {
			name        string
			contextMode workflow.ContextMode
			wantCode    bool
		}{
			{name: "continue", contextMode: workflow.ContextModeContinueSession, wantCode: true},
			{name: "compact", contextMode: workflow.ContextModeCompactAndContinueSession, wantCode: false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				def := reviewAcceptanceWorkflow()
				edge := edgeByIDForValidationTest(t, &def, "edge_accept_open_pr")
				edge.ContextMode = tc.contextMode
				edge.ContextSource = workflow.ContextSource{Kind: workflow.ContextSourceSelectedNode, NodeKey: "implementation"}
				nodeByKeyForValidationTest(t, &def, "open_pr").SubagentRole = workflow.DefaultAgentRole

				result := validateForTask(def)

				if tc.wantCode {
					assertHasCodes(t, result, workflow.CodeInvalidContinueSessionRole)
				} else {
					assertNoCode(t, result, workflow.CodeInvalidContinueSessionRole)
					assertNoCode(t, result, workflow.CodeInvalidContextSource)
				}
			})
		}
	})

	t.Run("immediate source continuation after join is invalid", func(t *testing.T) {
		def := reviewAcceptanceWorkflow()
		edge := edgeByIDForValidationTest(t, &def, "edge_join_accept")
		edge.ContextMode = workflow.ContextModeContinueSession

		result := validateForTask(def)

		assertHasCodes(t, result, workflow.CodeInvalidContextSource)
	})

	t.Run("rework loop remains statically valid", func(t *testing.T) {
		def := reviewAcceptanceWorkflow()
		def.TransitionGroups = append(def.TransitionGroups, workflow.TransitionGroup{WorkflowID: def.ID, ID: "group_accept_rework", SourceNodeID: "node_final_acceptance", TransitionID: "needs_changes", DisplayName: "Needs Changes"})
		def.Edges = append(def.Edges, workflow.Edge{WorkflowID: def.ID, ID: "edge_accept_rework", Key: "rework", TransitionGroupID: "group_accept_rework", TargetNodeID: "node_implementation", ContextMode: workflow.ContextModeCompactAndContinueSession})
		edge := edgeByIDForValidationTest(t, &def, "edge_accept_open_pr")
		edge.ContextMode = workflow.ContextModeContinueSession
		edge.ContextSource = workflow.ContextSource{Kind: workflow.ContextSourceSelectedNode, NodeKey: "implementation"}

		result := validateForTask(def)

		assertNoCode(t, result, workflow.CodeInvalidContextSource)
	})

	t.Run("draft reports nonblocking context source semantics", func(t *testing.T) {
		def := reviewAcceptanceWorkflow()
		edge := edgeByIDForValidationTest(t, &def, "edge_accept_open_pr")
		edge.ContextMode = workflow.ContextModeContinueSession
		edge.ContextSource = workflow.ContextSource{Kind: workflow.ContextSourceSelectedNode, NodeKey: "code_review"}

		result := workflow.ValidateDefinition(def, workflow.ValidationOptions{Context: workflow.ValidationContextDraft, RoleResolver: workflow.StaticRoleResolver{"coder": true}})

		assertHasCodes(t, result, workflow.CodeInvalidContextSource)
		if result.HasBlockingErrors() {
			t.Fatalf("draft context source semantics should not block saving: %+v", result.BlockingErrors())
		}
	})
}

func validWorkflow() workflow.Definition {
	return workflow.Definition{
		ID:          "workflow_default",
		DisplayName: "Default Workflow",
		Nodes: []workflow.Node{
			{WorkflowID: "workflow_default", ID: "node_start", Key: "backlog", DisplayName: "Backlog", Kind: workflow.NodeKindStart},
			{
				WorkflowID:     "workflow_default",
				ID:             "node_agent",
				Key:            "implement",
				DisplayName:    "Implement",
				Kind:           workflow.NodeKindAgent,
				SubagentRole:   "coder",
				PromptTemplate: "Implement task.",
				OutputFields:   []workflow.OutputField{{Name: "summary", Description: "Summary of completed work."}},
			},
			{WorkflowID: "workflow_default", ID: "node_done", Key: "done", DisplayName: "Done", Kind: workflow.NodeKindTerminal},
		},
		TransitionGroups: []workflow.TransitionGroup{
			{WorkflowID: "workflow_default", ID: "group_start", SourceNodeID: "node_start", TransitionID: "start", DisplayName: "Start"},
			{WorkflowID: "workflow_default", ID: "group_done", SourceNodeID: "node_agent", TransitionID: "done", DisplayName: "Done"},
		},
		Edges: []workflow.Edge{
			{WorkflowID: "workflow_default", ID: "edge_start", Key: "start", TransitionGroupID: "group_start", TargetNodeID: "node_agent", ContextMode: workflow.ContextModeNewSession},
			{
				WorkflowID:         "workflow_default",
				ID:                 "edge_done",
				Key:                "done",
				TransitionGroupID:  "group_done",
				TargetNodeID:       "node_done",
				ContextMode:        workflow.ContextModeNewSession,
				OutputRequirements: []workflow.OutputRequirement{{FieldName: "summary"}},
			},
		},
	}
}

func fanoutWorkflow() workflow.Definition {
	def := workflow.Definition{
		ID:          "workflow_fanout",
		DisplayName: "Fanout Workflow",
		Nodes: []workflow.Node{
			{WorkflowID: "workflow_fanout", ID: "node_start", Key: "backlog", DisplayName: "Backlog", Kind: workflow.NodeKindStart},
			{WorkflowID: "workflow_fanout", ID: "node_plan", Key: "plan", DisplayName: "Plan", Kind: workflow.NodeKindAgent, SubagentRole: "coder", PromptTemplate: "Plan.", OutputFields: []workflow.OutputField{{Name: "summary", Description: "Summary."}}},
			{WorkflowID: "workflow_fanout", ID: "node_impl_a", Key: "impl_a", DisplayName: "Implement A", Kind: workflow.NodeKindAgent, SubagentRole: "coder", PromptTemplate: "A."},
			{WorkflowID: "workflow_fanout", ID: "node_impl_b", Key: "impl_b", DisplayName: "Implement B", Kind: workflow.NodeKindAgent, SubagentRole: "coder", PromptTemplate: "B."},
			{WorkflowID: "workflow_fanout", ID: "node_join", Key: "join", DisplayName: "Join", Kind: workflow.NodeKindJoin},
			{WorkflowID: "workflow_fanout", ID: "node_done", Key: "done", DisplayName: "Done", Kind: workflow.NodeKindTerminal},
		},
		TransitionGroups: []workflow.TransitionGroup{
			{WorkflowID: "workflow_fanout", ID: "group_start", SourceNodeID: "node_start", TransitionID: "start", DisplayName: "Start"},
			{WorkflowID: "workflow_fanout", ID: "group_split", SourceNodeID: "node_plan", TransitionID: "split", DisplayName: "Split"},
			{WorkflowID: "workflow_fanout", ID: "group_impl_a_join", SourceNodeID: "node_impl_a", TransitionID: "join", DisplayName: "Join"},
			{WorkflowID: "workflow_fanout", ID: "group_impl_b_join", SourceNodeID: "node_impl_b", TransitionID: "join", DisplayName: "Join"},
			{WorkflowID: "workflow_fanout", ID: "group_join_done", SourceNodeID: "node_join", TransitionID: "done", DisplayName: "Done"},
		},
		Edges: []workflow.Edge{
			{WorkflowID: "workflow_fanout", ID: "edge_start", Key: "start", TransitionGroupID: "group_start", TargetNodeID: "node_plan", ContextMode: workflow.ContextModeNewSession},
			{WorkflowID: "workflow_fanout", ID: "edge_split_a", Key: "split_a", TransitionGroupID: "group_split", TargetNodeID: "node_impl_a", ContextMode: workflow.ContextModeNewSession},
			{WorkflowID: "workflow_fanout", ID: "edge_split_b", Key: "split_b", TransitionGroupID: "group_split", TargetNodeID: "node_impl_b", ContextMode: workflow.ContextModeNewSession},
			{WorkflowID: "workflow_fanout", ID: "edge_impl_a_join", Key: "join_a", TransitionGroupID: "group_impl_a_join", TargetNodeID: "node_join", ContextMode: workflow.ContextModeNewSession},
			{WorkflowID: "workflow_fanout", ID: "edge_impl_b_join", Key: "join_b", TransitionGroupID: "group_impl_b_join", TargetNodeID: "node_join", ContextMode: workflow.ContextModeNewSession},
			{WorkflowID: "workflow_fanout", ID: "edge_join_done", Key: "done", TransitionGroupID: "group_join_done", TargetNodeID: "node_done", ContextMode: workflow.ContextModeNewSession},
		},
	}
	return def
}

func reviewAcceptanceWorkflow() workflow.Definition {
	return workflow.Definition{
		ID:          "workflow_review_acceptance",
		DisplayName: "Review Acceptance Workflow",
		Nodes: []workflow.Node{
			{WorkflowID: "workflow_review_acceptance", ID: "node_start", Key: "backlog", DisplayName: "Backlog", Kind: workflow.NodeKindStart},
			{WorkflowID: "workflow_review_acceptance", ID: "node_implementation", Key: "implementation", DisplayName: "Implementation", Kind: workflow.NodeKindAgent, SubagentRole: "coder", PromptTemplate: "Implement.", OutputFields: []workflow.OutputField{{Name: "summary", Description: "Summary."}}},
			{WorkflowID: "workflow_review_acceptance", ID: "node_code_review", Key: "code_review", DisplayName: "Code Review", Kind: workflow.NodeKindAgent, SubagentRole: "coder", PromptTemplate: "Review."},
			{WorkflowID: "workflow_review_acceptance", ID: "node_qa_test", Key: "qa_test", DisplayName: "QA Test", Kind: workflow.NodeKindAgent, SubagentRole: "coder", PromptTemplate: "QA."},
			{WorkflowID: "workflow_review_acceptance", ID: "node_review_join", Key: "review_join", DisplayName: "Review Join", Kind: workflow.NodeKindJoin},
			{WorkflowID: "workflow_review_acceptance", ID: "node_final_acceptance", Key: "final_acceptance", DisplayName: "Final Acceptance", Kind: workflow.NodeKindAgent, SubagentRole: "coder", PromptTemplate: "Accept."},
			{WorkflowID: "workflow_review_acceptance", ID: "node_open_pr", Key: "open_pr", DisplayName: "Open PR", Kind: workflow.NodeKindAgent, SubagentRole: "coder", PromptTemplate: "Open PR."},
			{WorkflowID: "workflow_review_acceptance", ID: "node_done", Key: "done", DisplayName: "Done", Kind: workflow.NodeKindTerminal},
		},
		TransitionGroups: []workflow.TransitionGroup{
			{WorkflowID: "workflow_review_acceptance", ID: "group_start", SourceNodeID: "node_start", TransitionID: "start", DisplayName: "Start"},
			{WorkflowID: "workflow_review_acceptance", ID: "group_implementation_review", SourceNodeID: "node_implementation", TransitionID: "review", DisplayName: "Review"},
			{WorkflowID: "workflow_review_acceptance", ID: "group_code_review_join", SourceNodeID: "node_code_review", TransitionID: "reviewed", DisplayName: "Reviewed"},
			{WorkflowID: "workflow_review_acceptance", ID: "group_qa_test_join", SourceNodeID: "node_qa_test", TransitionID: "reviewed", DisplayName: "Reviewed"},
			{WorkflowID: "workflow_review_acceptance", ID: "group_join_accept", SourceNodeID: "node_review_join", TransitionID: "accept", DisplayName: "Accept"},
			{WorkflowID: "workflow_review_acceptance", ID: "group_accept_open_pr", SourceNodeID: "node_final_acceptance", TransitionID: "approved", DisplayName: "Approved"},
			{WorkflowID: "workflow_review_acceptance", ID: "group_open_pr_done", SourceNodeID: "node_open_pr", TransitionID: "done", DisplayName: "Done"},
		},
		Edges: []workflow.Edge{
			{WorkflowID: "workflow_review_acceptance", ID: "edge_start", Key: "start", TransitionGroupID: "group_start", TargetNodeID: "node_implementation", ContextMode: workflow.ContextModeNewSession},
			{WorkflowID: "workflow_review_acceptance", ID: "edge_implementation_review", Key: "code_review", TransitionGroupID: "group_implementation_review", TargetNodeID: "node_code_review", ContextMode: workflow.ContextModeNewSession, OutputRequirements: []workflow.OutputRequirement{{FieldName: "summary"}}},
			{WorkflowID: "workflow_review_acceptance", ID: "edge_implementation_qa", Key: "qa_test", TransitionGroupID: "group_implementation_review", TargetNodeID: "node_qa_test", ContextMode: workflow.ContextModeNewSession, OutputRequirements: []workflow.OutputRequirement{{FieldName: "summary"}}},
			{WorkflowID: "workflow_review_acceptance", ID: "edge_code_review_join", Key: "code_review_done", TransitionGroupID: "group_code_review_join", TargetNodeID: "node_review_join", ContextMode: workflow.ContextModeNewSession},
			{WorkflowID: "workflow_review_acceptance", ID: "edge_qa_test_join", Key: "qa_test_done", TransitionGroupID: "group_qa_test_join", TargetNodeID: "node_review_join", ContextMode: workflow.ContextModeNewSession},
			{WorkflowID: "workflow_review_acceptance", ID: "edge_join_accept", Key: "final_acceptance", TransitionGroupID: "group_join_accept", TargetNodeID: "node_final_acceptance", ContextMode: workflow.ContextModeNewSession},
			{WorkflowID: "workflow_review_acceptance", ID: "edge_accept_open_pr", Key: "open_pr", TransitionGroupID: "group_accept_open_pr", TargetNodeID: "node_open_pr", ContextMode: workflow.ContextModeNewSession},
			{WorkflowID: "workflow_review_acceptance", ID: "edge_open_pr_done", Key: "done", TransitionGroupID: "group_open_pr_done", TargetNodeID: "node_done", ContextMode: workflow.ContextModeNewSession},
		},
	}
}

func validateForTask(def workflow.Definition) workflow.ValidationResult {
	return workflow.ValidateDefinition(def, workflow.ValidationOptions{
		Context:      workflow.ValidationContextTaskCreation,
		RoleResolver: workflow.StaticRoleResolver{"coder": true},
	})
}

func edgeByIDForValidationTest(t *testing.T, def *workflow.Definition, id workflow.EdgeID) *workflow.Edge {
	t.Helper()
	for i := range def.Edges {
		if def.Edges[i].ID == id {
			return &def.Edges[i]
		}
	}
	t.Fatalf("edge %q not found", id)
	return nil
}

func transitionGroupByIDForValidationTest(t *testing.T, def *workflow.Definition, id workflow.TransitionGroupID) *workflow.TransitionGroup {
	t.Helper()
	for i := range def.TransitionGroups {
		if def.TransitionGroups[i].ID == id {
			return &def.TransitionGroups[i]
		}
	}
	t.Fatalf("transition group %q not found", id)
	return nil
}

func nodeByKeyForValidationTest(t *testing.T, def *workflow.Definition, key workflow.ModelKey) *workflow.Node {
	t.Helper()
	for i := range def.Nodes {
		if def.Nodes[i].Key == key {
			return &def.Nodes[i]
		}
	}
	t.Fatalf("node %q not found", key)
	return nil
}

func addTransitionForValidationTest(def *workflow.Definition, groupID, sourceNodeID, transitionID, displayName, edgeID, edgeKey, targetNodeID string) {
	def.TransitionGroups = append(def.TransitionGroups, workflow.TransitionGroup{
		WorkflowID:   def.ID,
		ID:           workflow.TransitionGroupID(groupID),
		SourceNodeID: workflow.NodeID(sourceNodeID),
		TransitionID: workflow.TransitionID(transitionID),
		DisplayName:  displayName,
	})
	def.Edges = append(def.Edges, workflow.Edge{
		WorkflowID:        def.ID,
		ID:                workflow.EdgeID(edgeID),
		Key:               workflow.ModelKey(edgeKey),
		TransitionGroupID: workflow.TransitionGroupID(groupID),
		TargetNodeID:      workflow.NodeID(targetNodeID),
		ContextMode:       workflow.ContextModeNewSession,
	})
}

func addV1NodeGroup(def *workflow.Definition) {
	def.NodeGroups = append(def.NodeGroups, workflow.NodeGroup{
		WorkflowID:  def.ID,
		ID:          "group_parallel",
		Key:         "parallel",
		DisplayName: "Parallel",
	})
	def.Nodes = setNodeGroup(def.Nodes, "node_impl_a", "group_parallel")
	def.Nodes = setNodeGroup(def.Nodes, "node_impl_b", "group_parallel")
	def.Nodes = setNodeGroup(def.Nodes, "node_join", "group_parallel")
}

func setNodeGroup(nodes []workflow.Node, nodeID workflow.NodeID, groupID string) []workflow.Node {
	out := append([]workflow.Node(nil), nodes...)
	for index := range out {
		if out[index].ID == nodeID {
			out[index].GroupID = groupID
		}
	}
	return out
}

func addAgentLoop(def *workflow.Definition, source workflow.NodeID, groupSuffix string, edgeID workflow.EdgeID, transitionID string) {
	groupID := workflow.TransitionGroupID("group_" + groupSuffix)
	def.TransitionGroups = append(def.TransitionGroups, workflow.TransitionGroup{
		WorkflowID:   def.ID,
		ID:           groupID,
		SourceNodeID: source,
		TransitionID: workflow.TransitionID(transitionID),
		DisplayName:  "Loop",
	})
	def.Edges = append(def.Edges, workflow.Edge{
		WorkflowID:        def.ID,
		ID:                edgeID,
		Key:               workflow.ModelKey(groupSuffix),
		TransitionGroupID: groupID,
		TargetNodeID:      source,
		ContextMode:       workflow.ContextModeNewSession,
	})
}

func assertHasCodes(t *testing.T, result workflow.ValidationResult, want ...workflow.ValidationErrorCode) {
	t.Helper()
	got := result.Codes()
	for _, code := range want {
		if !slices.Contains(got, code) {
			t.Fatalf("missing validation code %q in %v; errors: %+v", code, got, result.Errors)
		}
	}
}

func assertNoCode(t *testing.T, result workflow.ValidationResult, code workflow.ValidationErrorCode) {
	t.Helper()
	got := result.Codes()
	if slices.Contains(got, code) {
		t.Fatalf("unexpected validation code %q in %v; errors: %+v", code, got, result.Errors)
	}
}

func assertValidationMessage(t *testing.T, result workflow.ValidationResult, code workflow.ValidationErrorCode, nodeID workflow.NodeID, want string) {
	t.Helper()
	for _, err := range result.Errors {
		if err.Code == code && err.NodeID == nodeID {
			if err.Message != want {
				t.Fatalf("message for %s on %s = %q, want %q", code, nodeID, err.Message, want)
			}
			return
		}
	}
	t.Fatalf("missing validation error %s on %s in %+v", code, nodeID, result.Errors)
}

func stringOf(value string, count int) string {
	out := ""
	for range count {
		out += value
	}
	return out
}
