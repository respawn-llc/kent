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
				def.TransitionGroups = append(def.TransitionGroups, workflow.TransitionGroup{
					WorkflowID:   def.ID,
					ID:           "group_restart",
					SourceNodeID: "node_agent",
					TransitionID: "restart",
					DisplayName:  "Restart",
				})
				def.Edges = append(def.Edges, workflow.Edge{
					WorkflowID:        def.ID,
					ID:                "edge_restart",
					Key:               "restart",
					TransitionGroupID: "group_restart",
					TargetNodeID:      "node_start",
					ContextMode:       workflow.ContextModeNewSession,
				})
			},
			code: workflow.CodeInvalidStartNode,
		},
		{
			name: "start has two groups",
			edit: func(def *workflow.Definition) {
				def.TransitionGroups = append(def.TransitionGroups, workflow.TransitionGroup{
					WorkflowID:   def.ID,
					ID:           "group_alt",
					SourceNodeID: "node_start",
					TransitionID: "alt",
					DisplayName:  "Alternative",
				})
				def.Edges = append(def.Edges, workflow.Edge{
					WorkflowID:        def.ID,
					ID:                "edge_alt",
					Key:               "alt",
					TransitionGroupID: "group_alt",
					TargetNodeID:      "node_agent",
					ContextMode:       workflow.ContextModeNewSession,
				})
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
				def.TransitionGroups = append(def.TransitionGroups, workflow.TransitionGroup{
					WorkflowID:   def.ID,
					ID:           "group_done_again",
					SourceNodeID: "node_done",
					TransitionID: "again",
					DisplayName:  "Again",
				})
				def.Edges = append(def.Edges, workflow.Edge{
					WorkflowID:        def.ID,
					ID:                "edge_done_again",
					Key:               "again",
					TransitionGroupID: "group_done_again",
					TargetNodeID:      "node_agent",
					ContextMode:       workflow.ContextModeNewSession,
				})
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
				def.TransitionGroups = append(def.TransitionGroups, workflow.TransitionGroup{
					WorkflowID:   def.ID,
					ID:           "group_join_alt",
					SourceNodeID: "node_agent",
					TransitionID: "alt",
					DisplayName:  "Alternative",
				})
				def.Edges = append(def.Edges, workflow.Edge{
					WorkflowID:        def.ID,
					ID:                "edge_join_alt",
					Key:               "alt",
					TransitionGroupID: "group_join_alt",
					TargetNodeID:      "node_done",
					ContextMode:       workflow.ContextModeNewSession,
				})
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
			def.TransitionGroups = append(def.TransitionGroups, workflow.TransitionGroup{
				WorkflowID:   def.ID,
				ID:           "group_second_done",
				SourceNodeID: "node_agent",
				TransitionID: "done",
				DisplayName:  "Done Again",
			})
			def.Edges = append(def.Edges, workflow.Edge{WorkflowID: def.ID, ID: "edge_second_done", Key: "second_done", TransitionGroupID: "group_second_done", TargetNodeID: "node_done", ContextMode: workflow.ContextModeNewSession})
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
		{name: "unknown output requirement", edit: func(def *workflow.Definition) {
			def.Edges[1].OutputRequirements = []workflow.OutputRequirement{{FieldName: "missing"}}
		}, code: workflow.CodeUnknownOutputRequirement},
		{name: "invalid input binding", edit: func(def *workflow.Definition) {
			def.Edges[1].InputBindings = []workflow.InputBinding{{Name: "Review", Source: workflow.BindingSourceTransitionOutput, Field: "summary"}}
		}, code: workflow.CodeInvalidInputBinding},
		{name: "unknown transition output binding", edit: func(def *workflow.Definition) {
			def.Edges[1].InputBindings = []workflow.InputBinding{{Name: "review", Source: workflow.BindingSourceTransitionOutput, Field: "missing"}}
		}, code: workflow.CodeInvalidInputBinding},
		{name: "unknown task binding", edit: func(def *workflow.Definition) {
			def.Edges[1].InputBindings = []workflow.InputBinding{{Name: "task_ref", Source: workflow.BindingSourceTask, Field: "unknown"}}
		}, code: workflow.CodeInvalidInputBinding},
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
		def.Edges[0].InputBindings = []workflow.InputBinding{
			{Name: "task_title", Source: workflow.BindingSourceTask, Field: "title"},
			{Name: "prior_summary", Source: workflow.BindingSourceTransitionOutput, Field: "commentary"},
		}

		result := validateForTask(def)

		assertNoCode(t, result, workflow.CodeInvalidTemplatePlaceholder)
	})
}

func TestRuntimeValidationBlocksUnimplementedExecutionFeatures(t *testing.T) {
	t.Run("approval-gated edges warn in draft and block task creation", func(t *testing.T) {
		def := validWorkflow()
		def.Edges[1].RequiresApproval = true

		draft := workflow.ValidateDefinition(def, workflow.ValidationOptions{Context: workflow.ValidationContextDraft})
		assertHasCodes(t, draft, workflow.CodeUnsupportedApprovalExecution)
		if draft.HasBlockingErrors() {
			t.Fatalf("draft approval warning should not block saving: %+v", draft.BlockingErrors())
		}

		task := validateForTask(def)
		assertHasCodes(t, task, workflow.CodeUnsupportedApprovalExecution)
		if !task.HasBlockingErrors() {
			t.Fatalf("task approval error should block execution")
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

	t.Run("join targets warn in draft and block task creation", func(t *testing.T) {
		def := fanoutWorkflow()

		draft := workflow.ValidateDefinition(def, workflow.ValidationOptions{Context: workflow.ValidationContextDraft})
		assertHasCodes(t, draft, workflow.CodeUnsupportedJoinExecution)
		if draft.HasBlockingErrors() {
			t.Fatalf("draft join warning should not block saving: %+v", draft.BlockingErrors())
		}

		task := validateForTask(def)
		assertHasCodes(t, task, workflow.CodeUnsupportedJoinExecution)
		if !task.HasBlockingErrors() {
			t.Fatalf("task join error should block execution")
		}
	})

	t.Run("join bindings warn in draft and block task creation", func(t *testing.T) {
		def := validWorkflow()
		def.Edges[0].InputBindings = []workflow.InputBinding{{Name: "joined", Source: workflow.BindingSourceJoin, Field: "aggregate"}}

		draft := workflow.ValidateDefinition(def, workflow.ValidationOptions{Context: workflow.ValidationContextDraft})
		assertHasCodes(t, draft, workflow.CodeUnsupportedJoinBinding)
		if draft.HasBlockingErrors() {
			t.Fatalf("draft join binding warning should not block saving: %+v", draft.BlockingErrors())
		}

		task := validateForTask(def)
		assertHasCodes(t, task, workflow.CodeUnsupportedJoinBinding)
		if !task.HasBlockingErrors() {
			t.Fatalf("task join binding error should block execution")
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

func validateForTask(def workflow.Definition) workflow.ValidationResult {
	return workflow.ValidateDefinition(def, workflow.ValidationOptions{
		Context:      workflow.ValidationContextTaskCreation,
		RoleResolver: workflow.StaticRoleResolver{"coder": true},
	})
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

func stringOf(value string, count int) string {
	out := ""
	for range count {
		out += value
	}
	return out
}
