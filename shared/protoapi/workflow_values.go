package protoapi

import (
	"fmt"

	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
)

// Workflow values retain their public CLI/domain spelling independently of the
// generated enum names. Both directions share the same explicit correspondence.
type workflowValues[T ~int32] struct {
	codes map[string]T
	names map[T]string
}

func workflowValueNames[T ~int32](names map[T]string) workflowValues[T] {
	codes := make(map[string]T, len(names))
	for code, name := range names {
		codes[name] = code
	}
	return workflowValues[T]{codes: codes, names: names}
}

func (v workflowValues[T]) Encode(name string) (T, error) {
	code, ok := v.codes[name]
	if !ok {
		return 0, fmt.Errorf("unsupported Workflow value %q", name)
	}
	return code, nil
}

func (v workflowValues[T]) Decode(code T) (string, error) {
	name, ok := v.names[code]
	if !ok {
		return "", fmt.Errorf("unsupported Workflow value %v", code)
	}
	return name, nil
}

var WorkflowNodeKind = workflowValueNames(map[pb.NodeKind]string{
	pb.NodeKind_WORKFLOW_NODE_KIND_START:    "start",
	pb.NodeKind_WORKFLOW_NODE_KIND_AGENT:    "agent",
	pb.NodeKind_WORKFLOW_NODE_KIND_SCRIPT:   "script",
	pb.NodeKind_WORKFLOW_NODE_KIND_JOIN:     "join",
	pb.NodeKind_WORKFLOW_NODE_KIND_TERMINAL: "terminal",
})

var WorkflowValidationMode = workflowValueNames(map[pb.ValidationMode]string{
	pb.ValidationMode_WORKFLOW_VALIDATION_MODE_DRAFT:         "draft",
	pb.ValidationMode_WORKFLOW_VALIDATION_MODE_TASK_CREATION: "task_creation",
	pb.ValidationMode_WORKFLOW_VALIDATION_MODE_EXECUTION:     "execution",
})

var WorkflowProjectLinkDefaultMode = workflowValueNames(map[pb.ProjectLinkDefaultMode]string{
	pb.ProjectLinkDefaultMode_WORKFLOW_PROJECT_LINK_DEFAULT_MODE_NEVER:               "never",
	pb.ProjectLinkDefaultMode_WORKFLOW_PROJECT_LINK_DEFAULT_MODE_ALWAYS:              "always",
	pb.ProjectLinkDefaultMode_WORKFLOW_PROJECT_LINK_DEFAULT_MODE_IF_PROJECT_HAS_NONE: "if_project_has_none",
})

var WorkflowExecutionTargetMode = workflowValueNames(map[pb.ExecutionTargetMode]string{
	pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_NONE:                   "none",
	pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_HEAD:                   "head",
	pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_DEFAULT_BRANCH:         "default_branch",
	pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_CUSTOM_REF:             "custom_ref",
	pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_ASK_ON_FIRST_EXECUTION: "ask_on_first_execution",
})

var WorkflowCompletionMode = workflowValueNames(map[pb.CompletionMode]string{
	pb.CompletionMode_WORKFLOW_COMPLETION_MODE_AUTO:                "auto",
	pb.CompletionMode_WORKFLOW_COMPLETION_MODE_STRUCTURED_OUTPUT:   "structured_output",
	pb.CompletionMode_WORKFLOW_COMPLETION_MODE_TOOL:                "tool",
	pb.CompletionMode_WORKFLOW_COMPLETION_MODE_SHELL_COMMAND:       "shell_command",
	pb.CompletionMode_WORKFLOW_COMPLETION_MODE_UNSTRUCTURED_OUTPUT: "unstructured_output",
})

var WorkflowAssigneeSelection = workflowValueNames(map[pb.AssigneeSelection]string{
	pb.AssigneeSelection_WORKFLOW_ASSIGNEE_SELECTION_CONFIGURED:    "configured",
	pb.AssigneeSelection_WORKFLOW_ASSIGNEE_SELECTION_PREVIOUS_NODE: "previous_node",
})

var WorkflowThinkingSelection = workflowValueNames(map[pb.ThinkingSelection]string{
	pb.ThinkingSelection_WORKFLOW_THINKING_SELECTION_CONFIGURED:    "configured",
	pb.ThinkingSelection_WORKFLOW_THINKING_SELECTION_PREVIOUS_NODE: "previous_node",
})

var WorkflowContextMode = workflowValueNames(map[pb.ContextMode]string{
	pb.ContextMode_WORKFLOW_CONTEXT_MODE_NEW_SESSION:                  "new_session",
	pb.ContextMode_WORKFLOW_CONTEXT_MODE_CONTINUE_SESSION:             "continue_session",
	pb.ContextMode_WORKFLOW_CONTEXT_MODE_COMPACT_AND_CONTINUE_SESSION: "compact_and_continue_session",
})

var WorkflowContextSourceKind = workflowValueNames(map[pb.ContextSourceKind]string{
	pb.ContextSourceKind_WORKFLOW_CONTEXT_SOURCE_KIND_IMMEDIATE_SOURCE:       "immediate_source",
	pb.ContextSourceKind_WORKFLOW_CONTEXT_SOURCE_KIND_SELECTED_NODE:          "selected_node",
	pb.ContextSourceKind_WORKFLOW_CONTEXT_SOURCE_KIND_PREVIOUS_TARGET:        "previous_target",
	pb.ContextSourceKind_WORKFLOW_CONTEXT_SOURCE_KIND_PREVIOUS_TARGET_OR_NEW: "previous_target_or_new",
})

var WorkflowParameterPurpose = workflowValueNames(map[pb.ParameterPurpose]string{
	pb.ParameterPurpose_WORKFLOW_PARAMETER_PURPOSE_ORDINARY:        "ordinary",
	pb.ParameterPurpose_WORKFLOW_PARAMETER_PURPOSE_TARGET_ASSIGNEE: "target_assignee",
	pb.ParameterPurpose_WORKFLOW_PARAMETER_PURPOSE_TARGET_THINKING: "target_thinking",
})

var WorkflowGraphEntityType = workflowValueNames(map[pb.GraphEntityType]string{
	pb.GraphEntityType_WORKFLOW_GRAPH_ENTITY_TYPE_EDGE:             "edge",
	pb.GraphEntityType_WORKFLOW_GRAPH_ENTITY_TYPE_NODE:             "node",
	pb.GraphEntityType_WORKFLOW_GRAPH_ENTITY_TYPE_NODE_GROUP:       "node_group",
	pb.GraphEntityType_WORKFLOW_GRAPH_ENTITY_TYPE_TRANSITION_GROUP: "transition_group",
})

var WorkflowSelectorApplicabilityReason = workflowValueNames(map[pb.SelectorApplicabilityReason]string{
	pb.SelectorApplicabilityReason_WORKFLOW_SELECTOR_APPLICABILITY_REASON_ELIGIBLE:                  "eligible",
	pb.SelectorApplicabilityReason_WORKFLOW_SELECTOR_APPLICABILITY_REASON_TOPOLOGY:                  "topology",
	pb.SelectorApplicabilityReason_WORKFLOW_SELECTOR_APPLICABILITY_REASON_CONTEXT_SOURCE:            "context_source",
	pb.SelectorApplicabilityReason_WORKFLOW_SELECTOR_APPLICABILITY_REASON_NO_CALLABLE_ROLES:         "no_callable_roles",
	pb.SelectorApplicabilityReason_WORKFLOW_SELECTOR_APPLICABILITY_REASON_NO_THINKING_SUPPORT:       "no_thinking_support",
	pb.SelectorApplicabilityReason_WORKFLOW_SELECTOR_APPLICABILITY_REASON_UNAVAILABLE_CONFIGURATION: "unavailable_configuration",
	pb.SelectorApplicabilityReason_WORKFLOW_SELECTOR_APPLICABILITY_REASON_SOLE_CALLABLE_ROLE:        "sole_callable_role",
	pb.SelectorApplicabilityReason_WORKFLOW_SELECTOR_APPLICABILITY_REASON_NO_THINKING_LEVELS:        "no_thinking_levels",
	pb.SelectorApplicabilityReason_WORKFLOW_SELECTOR_APPLICABILITY_REASON_SOLE_THINKING_LEVEL:       "sole_thinking_level",
})

var WorkflowValidationErrorCode = workflowValueNames(map[pb.ValidationErrorCode]string{
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_MISSING_WORKFLOW_ID:                  "workflow.validation.missing_workflow_id",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_MISSING_NODE_ID:                      "workflow.validation.missing_node_id",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_DUPLICATE_NODE_ID:                    "workflow.validation.duplicate_node_id",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_MISSING_NODE_KEY:                     "workflow.validation.missing_node_key",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_INVALID_NODE_KEY:                     "workflow.validation.invalid_node_key",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_DUPLICATE_NODE_KEY:                   "workflow.validation.duplicate_node_key",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_MISSING_START_NODE:                   "workflow.validation.missing_start_node",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_MULTIPLE_START_NODES:                 "workflow.validation.multiple_start_nodes",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_INVALID_START_NODE:                   "workflow.validation.invalid_start_node",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_INVALID_START_OUTGOING_SHAPE:         "workflow.validation.invalid_start_outgoing_shape",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_TERMINAL_HAS_OUTGOING_EDGE:           "workflow.validation.terminal_has_outgoing_edge",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_TERMINAL_IS_EXECUTABLE:               "workflow.validation.terminal_is_executable",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_JOIN_IS_EXECUTABLE:                   "workflow.validation.join_is_executable",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_INVALID_JOIN_NODE:                    "workflow.validation.invalid_join_node",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_INVALID_JOIN_OUTGOING_SHAPE:          "workflow.validation.invalid_join_outgoing_shape",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_NODE_UNREACHABLE_FROM_START:          "workflow.validation.node_unreachable_from_start",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_NON_TERMINAL_CANNOT_REACH_TERMINAL:   "workflow.validation.non_terminal_cannot_reach_terminal",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_MISSING_TRANSITION_GROUP_ID:          "workflow.validation.missing_transition_group_id",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_DUPLICATE_TRANSITION_GROUP_ID:        "workflow.validation.duplicate_transition_group_id",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_EMPTY_TRANSITION_GROUP:               "workflow.validation.empty_transition_group",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_MISSING_TRANSITION_ID:                "workflow.validation.missing_transition_id",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_INVALID_TRANSITION_ID:                "workflow.validation.invalid_transition_id",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_DUPLICATE_TRANSITION_ID:              "workflow.validation.duplicate_transition_id",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_EDGE_TRANSITION_GROUP_MISSING:        "workflow.validation.edge_transition_group_missing",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_MISSING_EDGE_ID:                      "workflow.validation.missing_edge_id",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_DUPLICATE_EDGE_ID:                    "workflow.validation.duplicate_edge_id",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_MISSING_EDGE_KEY:                     "workflow.validation.missing_edge_key",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_INVALID_EDGE_KEY:                     "workflow.validation.invalid_edge_key",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_DUPLICATE_EDGE_KEY:                   "workflow.validation.duplicate_edge_key",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_EDGE_TARGET_MISSING:                  "workflow.validation.edge_target_missing",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_CROSS_WORKFLOW_REFERENCE:             "workflow.validation.cross_workflow_reference",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_INVALID_OUTPUT_FIELD:                 "workflow.validation.invalid_output_field",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_DUPLICATE_OUTPUT_FIELD:               "workflow.validation.duplicate_output_field",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_OUTPUT_FIELD_DESCRIPTION_REQUIRED:    "workflow.validation.output_field_description_required",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_OUTPUT_SCHEMA_TOO_LARGE:              "workflow.validation.output_schema_too_large",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_INVALID_INPUT_FIELD:                  "workflow.validation.invalid_input_field",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_DUPLICATE_INPUT_FIELD:                "workflow.validation.duplicate_input_field",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_INPUT_FIELD_DESCRIPTION_REQUIRED:     "workflow.validation.input_field_description_required",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_INPUT_SCHEMA_TOO_LARGE:               "workflow.validation.input_schema_too_large",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_INVALID_PARAMETER:                    "workflow.validation.invalid_parameter",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_DUPLICATE_PARAMETER:                  "workflow.validation.duplicate_parameter",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_PARAMETER_DESCRIPTION_REQUIRED:       "workflow.validation.parameter_description_required",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_PARAMETER_SCHEMA_TOO_LARGE:           "workflow.validation.parameter_schema_too_large",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_TRANSITION_PROMPT_REQUIRED:           "workflow.validation.transition_prompt_required",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_TRANSITION_PROMPT_FORBIDDEN:          "workflow.validation.transition_prompt_forbidden",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_UNKNOWN_OUTPUT_REQUIREMENT:           "workflow.validation.unknown_output_requirement",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_INVALID_INPUT_BINDING:                "workflow.validation.invalid_input_binding",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_INVALID_TEMPLATE_PLACEHOLDER:         "workflow.validation.invalid_template_placeholder",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_PROVISION_FIELD_OVERLAP:              "workflow.validation.provision_field_overlap",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_MISSING_JOIN_INPUT_PROVIDER:          "workflow.validation.missing_join_input_provider",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_DUPLICATE_JOIN_INPUT_PROVIDER:        "workflow.validation.duplicate_join_input_provider",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_INVALID_JOIN_INPUT_PROVIDER:          "workflow.validation.invalid_join_input_provider",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_INVALID_FIRST_NODE_INPUT:             "workflow.validation.invalid_first_node_input",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_INVALID_CONTEXT_MODE:                 "workflow.validation.invalid_context_mode",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_INVALID_ASSIGNEE_SELECTION:           "workflow.validation.invalid_assignee_selection",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_INVALID_THINKING_SELECTION:           "workflow.validation.invalid_thinking_selection",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_INVALID_PARAMETER_PURPOSE:            "workflow.validation.invalid_parameter_purpose",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_MISSING_PROTECTED_PARAMETER:          "workflow.validation.missing_protected_parameter",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_DUPLICATE_PROTECTED_PARAMETER:        "workflow.validation.duplicate_protected_parameter",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_INVALID_CONTEXT_SOURCE:               "workflow.validation.invalid_context_source",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_INVALID_CONTINUE_SESSION_ROLE:        "workflow.validation.invalid_continue_session_role",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_ASSIGNEE_SELECTION_INAPPLICABLE:      "workflow.validation.assignee_selection_inapplicable",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_ASSIGNEE_SELECTION_UNAVAILABLE:       "workflow.validation.assignee_selection_unavailable",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_THINKING_SELECTION_INAPPLICABLE:      "workflow.validation.thinking_selection_inapplicable",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_THINKING_SELECTION_UNAVAILABLE:       "workflow.validation.thinking_selection_unavailable",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_INVALID_FANOUT_JOIN_TOPOLOGY:         "workflow.validation.invalid_fanout_join_topology",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_INVALID_NODE_GROUP:                   "workflow.validation.invalid_node_group",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_UNSUPPORTED_CONTEXT_MODE:             "workflow.validation.unsupported_context_mode",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_UNSUPPORTED_APPROVAL_EXECUTION:       "workflow.validation.unsupported_approval_execution",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_UNSUPPORTED_JOIN_EXECUTION:           "workflow.validation.unsupported_join_execution",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_UNSUPPORTED_JOIN_BINDING:             "workflow.validation.unsupported_join_binding",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_AGENT_ROLE_REQUIRED:                  "workflow.validation.agent_role_required",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_AGENT_ROLE_MISSING:                   "workflow.validation.agent_role_missing",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_AGENT_ROLE_REQUIRED_TOOL_DISABLED:    "workflow.validation.agent_role_required_tool_disabled",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_INVALID_NODE_KIND:                    "workflow.validation.invalid_node_kind",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_INVALID_DISPLAY_NAME:                 "workflow.validation.invalid_display_name",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_INVALID_EXECUTION_TARGET_POLICY:      "workflow.validation.invalid_execution_target_policy",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_EXECUTION_TARGET_CUSTOM_REF_REQUIRED: "workflow.validation.execution_target_custom_ref_required",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_SCRIPT_PATH_MISSING:                  "workflow.validation.script_path_missing",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_SCRIPT_PATH_RELATIVE_CHECK_SKIPPED:   "workflow.validation.script_path_relative_check_skipped",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_SCRIPT_WORKTREE_ROOT_MISSING:         "workflow.validation.script_worktree_root_missing",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_SCRIPT_PATH_NOT_FOUND:                "workflow.validation.script_path_not_found",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_SCRIPT_PATH_INACCESSIBLE:             "workflow.validation.script_path_inaccessible",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_SCRIPT_PATH_IS_DIRECTORY:             "workflow.validation.script_path_is_directory",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_SCRIPT_PATH_NOT_EXECUTABLE:           "workflow.validation.script_path_not_executable",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_SESSION_SOURCE_CANNOT_OWN_SESSION:    "workflow.validation.session_source_cannot_own_session",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_SESSION_TRANSITION_MISSING:           "workflow.validation.session_transition_missing",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_SESSION_TRANSITION_NOT_GUARANTEED:    "workflow.validation.session_transition_not_guaranteed",
	pb.ValidationErrorCode_VALIDATION_ERROR_CODE_SESSION_TRANSITION_AMBIGUOUS:         "workflow.validation.session_transition_ambiguous",
})
