import {
  AssigneeSelection,
  CompletionMode,
  ContextMode,
  ContextSourceKind,
  ExecutionTargetMode,
  GraphEntityType,
  NodeKind,
  ParameterPurpose,
  SelectorApplicabilityReason,
  ThinkingSelection,
  ValidationMode,
  ValidationErrorCode,
} from "@app/server-api-contract/gen/kent/api/workflow_definition/workflow_definition_pb";
import { ContractError } from "./errors";

function workflowEnum<Code extends number, Name extends string>(entries: readonly (readonly [Code, Name])[]) {
  const names = new Map<number, Name>(entries);
  const codes = new Map<string, Code>(entries.map(([code, name]) => [name, code]));
  return {
    encode(name: string): Code {
      const code = codes.get(name);
      if (code === undefined) throw new ContractError(`Unsupported Workflow value ${name}.`);
      return code;
    },
    decode(code: number): Name {
      const name = names.get(code);
      if (name === undefined) throw new ContractError(`Unsupported Workflow value ${code.toString()}.`);
      return name;
    },
  };
}

export const workflowNodeKind = workflowEnum([
  [NodeKind.WORKFLOW_NODE_KIND_START, "start"],
  [NodeKind.WORKFLOW_NODE_KIND_AGENT, "agent"],
  [NodeKind.WORKFLOW_NODE_KIND_SCRIPT, "script"],
  [NodeKind.WORKFLOW_NODE_KIND_JOIN, "join"],
  [NodeKind.WORKFLOW_NODE_KIND_TERMINAL, "terminal"],
]);
export const workflowValidationMode = workflowEnum([
  [ValidationMode.WORKFLOW_VALIDATION_MODE_DRAFT, "draft"],
  [ValidationMode.WORKFLOW_VALIDATION_MODE_TASK_CREATION, "task_creation"],
  [ValidationMode.WORKFLOW_VALIDATION_MODE_EXECUTION, "execution"],
]);
export const workflowExecutionTargetMode = workflowEnum([
  [ExecutionTargetMode.WORKFLOW_EXECUTION_TARGET_MODE_NONE, "none"],
  [ExecutionTargetMode.WORKFLOW_EXECUTION_TARGET_MODE_HEAD, "head"],
  [ExecutionTargetMode.WORKFLOW_EXECUTION_TARGET_MODE_DEFAULT_BRANCH, "default_branch"],
  [ExecutionTargetMode.WORKFLOW_EXECUTION_TARGET_MODE_CUSTOM_REF, "custom_ref"],
  [ExecutionTargetMode.WORKFLOW_EXECUTION_TARGET_MODE_ASK_ON_FIRST_EXECUTION, "ask_on_first_execution"],
]);
export const workflowCompletionMode = workflowEnum([
  [CompletionMode.WORKFLOW_COMPLETION_MODE_AUTO, "auto"],
  [CompletionMode.WORKFLOW_COMPLETION_MODE_STRUCTURED_OUTPUT, "structured_output"],
  [CompletionMode.WORKFLOW_COMPLETION_MODE_TOOL, "tool"],
  [CompletionMode.WORKFLOW_COMPLETION_MODE_SHELL_COMMAND, "shell_command"],
  [CompletionMode.WORKFLOW_COMPLETION_MODE_UNSTRUCTURED_OUTPUT, "unstructured_output"],
]);
export const workflowAssigneeSelection = workflowEnum([
  [AssigneeSelection.WORKFLOW_ASSIGNEE_SELECTION_CONFIGURED, "configured"],
  [AssigneeSelection.WORKFLOW_ASSIGNEE_SELECTION_PREVIOUS_NODE, "previous_node"],
]);
export const workflowThinkingSelection = workflowEnum([
  [ThinkingSelection.WORKFLOW_THINKING_SELECTION_CONFIGURED, "configured"],
  [ThinkingSelection.WORKFLOW_THINKING_SELECTION_PREVIOUS_NODE, "previous_node"],
]);
export const workflowContextMode = workflowEnum([
  [ContextMode.WORKFLOW_CONTEXT_MODE_NEW_SESSION, "new_session"],
  [ContextMode.WORKFLOW_CONTEXT_MODE_CONTINUE_SESSION, "continue_session"],
  [ContextMode.WORKFLOW_CONTEXT_MODE_COMPACT_AND_CONTINUE_SESSION, "compact_and_continue_session"],
]);
export const workflowContextSourceKind = workflowEnum([
  [ContextSourceKind.WORKFLOW_CONTEXT_SOURCE_KIND_IMMEDIATE_SOURCE, "immediate_source"],
  [ContextSourceKind.WORKFLOW_CONTEXT_SOURCE_KIND_SELECTED_NODE, "selected_node"],
  [ContextSourceKind.WORKFLOW_CONTEXT_SOURCE_KIND_PREVIOUS_TARGET, "previous_target"],
  [ContextSourceKind.WORKFLOW_CONTEXT_SOURCE_KIND_PREVIOUS_TARGET_OR_NEW, "previous_target_or_new"],
]);
export const workflowParameterPurpose = workflowEnum([
  [ParameterPurpose.WORKFLOW_PARAMETER_PURPOSE_ORDINARY, "ordinary"],
  [ParameterPurpose.WORKFLOW_PARAMETER_PURPOSE_TARGET_ASSIGNEE, "target_assignee"],
  [ParameterPurpose.WORKFLOW_PARAMETER_PURPOSE_TARGET_THINKING, "target_thinking"],
]);
export const workflowSelectorReason = workflowEnum([
  [SelectorApplicabilityReason.WORKFLOW_SELECTOR_APPLICABILITY_REASON_ELIGIBLE, "eligible"],
  [SelectorApplicabilityReason.WORKFLOW_SELECTOR_APPLICABILITY_REASON_TOPOLOGY, "topology"],
  [SelectorApplicabilityReason.WORKFLOW_SELECTOR_APPLICABILITY_REASON_CONTEXT_SOURCE, "context_source"],
  [SelectorApplicabilityReason.WORKFLOW_SELECTOR_APPLICABILITY_REASON_NO_CALLABLE_ROLES, "no_callable_roles"],
  [
    SelectorApplicabilityReason.WORKFLOW_SELECTOR_APPLICABILITY_REASON_NO_THINKING_SUPPORT,
    "no_thinking_support",
  ],
  [
    SelectorApplicabilityReason.WORKFLOW_SELECTOR_APPLICABILITY_REASON_UNAVAILABLE_CONFIGURATION,
    "unavailable_configuration",
  ],
  [
    SelectorApplicabilityReason.WORKFLOW_SELECTOR_APPLICABILITY_REASON_SOLE_CALLABLE_ROLE,
    "sole_callable_role",
  ],
  [
    SelectorApplicabilityReason.WORKFLOW_SELECTOR_APPLICABILITY_REASON_NO_THINKING_LEVELS,
    "no_thinking_levels",
  ],
  [
    SelectorApplicabilityReason.WORKFLOW_SELECTOR_APPLICABILITY_REASON_SOLE_THINKING_LEVEL,
    "sole_thinking_level",
  ],
]);
export const workflowGraphEntityType = workflowEnum([
  [GraphEntityType.WORKFLOW_GRAPH_ENTITY_TYPE_EDGE, "edge"],
  [GraphEntityType.WORKFLOW_GRAPH_ENTITY_TYPE_NODE, "node"],
  [GraphEntityType.WORKFLOW_GRAPH_ENTITY_TYPE_NODE_GROUP, "node_group"],
  [GraphEntityType.WORKFLOW_GRAPH_ENTITY_TYPE_TRANSITION_GROUP, "transition_group"],
]);

export const workflowValidationCode = workflowEnum([
  [ValidationErrorCode.MISSING_WORKFLOW_ID, "workflow.validation.missing_workflow_id"],
  [ValidationErrorCode.MISSING_NODE_ID, "workflow.validation.missing_node_id"],
  [ValidationErrorCode.DUPLICATE_NODE_ID, "workflow.validation.duplicate_node_id"],
  [ValidationErrorCode.MISSING_NODE_KEY, "workflow.validation.missing_node_key"],
  [ValidationErrorCode.INVALID_NODE_KEY, "workflow.validation.invalid_node_key"],
  [ValidationErrorCode.DUPLICATE_NODE_KEY, "workflow.validation.duplicate_node_key"],
  [ValidationErrorCode.MISSING_START_NODE, "workflow.validation.missing_start_node"],
  [ValidationErrorCode.MULTIPLE_START_NODES, "workflow.validation.multiple_start_nodes"],
  [ValidationErrorCode.INVALID_START_NODE, "workflow.validation.invalid_start_node"],
  [ValidationErrorCode.INVALID_START_OUTGOING_SHAPE, "workflow.validation.invalid_start_outgoing_shape"],
  [ValidationErrorCode.TERMINAL_HAS_OUTGOING_EDGE, "workflow.validation.terminal_has_outgoing_edge"],
  [ValidationErrorCode.TERMINAL_IS_EXECUTABLE, "workflow.validation.terminal_is_executable"],
  [ValidationErrorCode.JOIN_IS_EXECUTABLE, "workflow.validation.join_is_executable"],
  [ValidationErrorCode.INVALID_JOIN_NODE, "workflow.validation.invalid_join_node"],
  [ValidationErrorCode.INVALID_JOIN_OUTGOING_SHAPE, "workflow.validation.invalid_join_outgoing_shape"],
  [ValidationErrorCode.NODE_UNREACHABLE_FROM_START, "workflow.validation.node_unreachable_from_start"],
  [
    ValidationErrorCode.NON_TERMINAL_CANNOT_REACH_TERMINAL,
    "workflow.validation.non_terminal_cannot_reach_terminal",
  ],
  [ValidationErrorCode.MISSING_TRANSITION_GROUP_ID, "workflow.validation.missing_transition_group_id"],
  [ValidationErrorCode.DUPLICATE_TRANSITION_GROUP_ID, "workflow.validation.duplicate_transition_group_id"],
  [ValidationErrorCode.EMPTY_TRANSITION_GROUP, "workflow.validation.empty_transition_group"],
  [ValidationErrorCode.MISSING_TRANSITION_ID, "workflow.validation.missing_transition_id"],
  [ValidationErrorCode.INVALID_TRANSITION_ID, "workflow.validation.invalid_transition_id"],
  [ValidationErrorCode.DUPLICATE_TRANSITION_ID, "workflow.validation.duplicate_transition_id"],
  [ValidationErrorCode.EDGE_TRANSITION_GROUP_MISSING, "workflow.validation.edge_transition_group_missing"],
  [ValidationErrorCode.MISSING_EDGE_ID, "workflow.validation.missing_edge_id"],
  [ValidationErrorCode.DUPLICATE_EDGE_ID, "workflow.validation.duplicate_edge_id"],
  [ValidationErrorCode.MISSING_EDGE_KEY, "workflow.validation.missing_edge_key"],
  [ValidationErrorCode.INVALID_EDGE_KEY, "workflow.validation.invalid_edge_key"],
  [ValidationErrorCode.DUPLICATE_EDGE_KEY, "workflow.validation.duplicate_edge_key"],
  [ValidationErrorCode.EDGE_TARGET_MISSING, "workflow.validation.edge_target_missing"],
  [ValidationErrorCode.CROSS_WORKFLOW_REFERENCE, "workflow.validation.cross_workflow_reference"],
  [ValidationErrorCode.INVALID_OUTPUT_FIELD, "workflow.validation.invalid_output_field"],
  [ValidationErrorCode.DUPLICATE_OUTPUT_FIELD, "workflow.validation.duplicate_output_field"],
  [
    ValidationErrorCode.OUTPUT_FIELD_DESCRIPTION_REQUIRED,
    "workflow.validation.output_field_description_required",
  ],
  [ValidationErrorCode.OUTPUT_SCHEMA_TOO_LARGE, "workflow.validation.output_schema_too_large"],
  [ValidationErrorCode.INVALID_INPUT_FIELD, "workflow.validation.invalid_input_field"],
  [ValidationErrorCode.DUPLICATE_INPUT_FIELD, "workflow.validation.duplicate_input_field"],
  [
    ValidationErrorCode.INPUT_FIELD_DESCRIPTION_REQUIRED,
    "workflow.validation.input_field_description_required",
  ],
  [ValidationErrorCode.INPUT_SCHEMA_TOO_LARGE, "workflow.validation.input_schema_too_large"],
  [ValidationErrorCode.INVALID_PARAMETER, "workflow.validation.invalid_parameter"],
  [ValidationErrorCode.DUPLICATE_PARAMETER, "workflow.validation.duplicate_parameter"],
  [ValidationErrorCode.PARAMETER_DESCRIPTION_REQUIRED, "workflow.validation.parameter_description_required"],
  [ValidationErrorCode.PARAMETER_SCHEMA_TOO_LARGE, "workflow.validation.parameter_schema_too_large"],
  [ValidationErrorCode.TRANSITION_PROMPT_REQUIRED, "workflow.validation.transition_prompt_required"],
  [ValidationErrorCode.TRANSITION_PROMPT_FORBIDDEN, "workflow.validation.transition_prompt_forbidden"],
  [ValidationErrorCode.UNKNOWN_OUTPUT_REQUIREMENT, "workflow.validation.unknown_output_requirement"],
  [ValidationErrorCode.INVALID_INPUT_BINDING, "workflow.validation.invalid_input_binding"],
  [ValidationErrorCode.INVALID_TEMPLATE_PLACEHOLDER, "workflow.validation.invalid_template_placeholder"],
  [ValidationErrorCode.PROVISION_FIELD_OVERLAP, "workflow.validation.provision_field_overlap"],
  [ValidationErrorCode.MISSING_JOIN_INPUT_PROVIDER, "workflow.validation.missing_join_input_provider"],
  [ValidationErrorCode.DUPLICATE_JOIN_INPUT_PROVIDER, "workflow.validation.duplicate_join_input_provider"],
  [ValidationErrorCode.INVALID_JOIN_INPUT_PROVIDER, "workflow.validation.invalid_join_input_provider"],
  [ValidationErrorCode.INVALID_FIRST_NODE_INPUT, "workflow.validation.invalid_first_node_input"],
  [ValidationErrorCode.INVALID_CONTEXT_MODE, "workflow.validation.invalid_context_mode"],
  [ValidationErrorCode.INVALID_ASSIGNEE_SELECTION, "workflow.validation.invalid_assignee_selection"],
  [ValidationErrorCode.INVALID_THINKING_SELECTION, "workflow.validation.invalid_thinking_selection"],
  [ValidationErrorCode.INVALID_PARAMETER_PURPOSE, "workflow.validation.invalid_parameter_purpose"],
  [ValidationErrorCode.MISSING_PROTECTED_PARAMETER, "workflow.validation.missing_protected_parameter"],
  [ValidationErrorCode.DUPLICATE_PROTECTED_PARAMETER, "workflow.validation.duplicate_protected_parameter"],
  [ValidationErrorCode.INVALID_CONTEXT_SOURCE, "workflow.validation.invalid_context_source"],
  [ValidationErrorCode.INVALID_CONTINUE_SESSION_ROLE, "workflow.validation.invalid_continue_session_role"],
  [
    ValidationErrorCode.ASSIGNEE_SELECTION_INAPPLICABLE,
    "workflow.validation.assignee_selection_inapplicable",
  ],
  [ValidationErrorCode.ASSIGNEE_SELECTION_UNAVAILABLE, "workflow.validation.assignee_selection_unavailable"],
  [
    ValidationErrorCode.THINKING_SELECTION_INAPPLICABLE,
    "workflow.validation.thinking_selection_inapplicable",
  ],
  [ValidationErrorCode.THINKING_SELECTION_UNAVAILABLE, "workflow.validation.thinking_selection_unavailable"],
  [ValidationErrorCode.INVALID_FANOUT_JOIN_TOPOLOGY, "workflow.validation.invalid_fanout_join_topology"],
  [ValidationErrorCode.INVALID_NODE_GROUP, "workflow.validation.invalid_node_group"],
  [ValidationErrorCode.UNSUPPORTED_CONTEXT_MODE, "workflow.validation.unsupported_context_mode"],
  [ValidationErrorCode.UNSUPPORTED_APPROVAL_EXECUTION, "workflow.validation.unsupported_approval_execution"],
  [ValidationErrorCode.UNSUPPORTED_JOIN_EXECUTION, "workflow.validation.unsupported_join_execution"],
  [ValidationErrorCode.UNSUPPORTED_JOIN_BINDING, "workflow.validation.unsupported_join_binding"],
  [ValidationErrorCode.AGENT_ROLE_REQUIRED, "workflow.validation.agent_role_required"],
  [ValidationErrorCode.AGENT_ROLE_MISSING, "workflow.validation.agent_role_missing"],
  [
    ValidationErrorCode.AGENT_ROLE_REQUIRED_TOOL_DISABLED,
    "workflow.validation.agent_role_required_tool_disabled",
  ],
  [ValidationErrorCode.INVALID_NODE_KIND, "workflow.validation.invalid_node_kind"],
  [ValidationErrorCode.INVALID_DISPLAY_NAME, "workflow.validation.invalid_display_name"],
  [
    ValidationErrorCode.INVALID_EXECUTION_TARGET_POLICY,
    "workflow.validation.invalid_execution_target_policy",
  ],
  [
    ValidationErrorCode.EXECUTION_TARGET_CUSTOM_REF_REQUIRED,
    "workflow.validation.execution_target_custom_ref_required",
  ],
  [ValidationErrorCode.SCRIPT_PATH_MISSING, "workflow.validation.script_path_missing"],
  [
    ValidationErrorCode.SCRIPT_PATH_RELATIVE_CHECK_SKIPPED,
    "workflow.validation.script_path_relative_check_skipped",
  ],
  [ValidationErrorCode.SCRIPT_WORKTREE_ROOT_MISSING, "workflow.validation.script_worktree_root_missing"],
  [ValidationErrorCode.SCRIPT_PATH_NOT_FOUND, "workflow.validation.script_path_not_found"],
  [ValidationErrorCode.SCRIPT_PATH_INACCESSIBLE, "workflow.validation.script_path_inaccessible"],
  [ValidationErrorCode.SCRIPT_PATH_IS_DIRECTORY, "workflow.validation.script_path_is_directory"],
  [ValidationErrorCode.SCRIPT_PATH_NOT_EXECUTABLE, "workflow.validation.script_path_not_executable"],
  [
    ValidationErrorCode.SESSION_SOURCE_CANNOT_OWN_SESSION,
    "workflow.validation.session_source_cannot_own_session",
  ],
  [ValidationErrorCode.SESSION_TRANSITION_MISSING, "workflow.validation.session_transition_missing"],
  [
    ValidationErrorCode.SESSION_TRANSITION_NOT_GUARANTEED,
    "workflow.validation.session_transition_not_guaranteed",
  ],
  [ValidationErrorCode.SESSION_TRANSITION_AMBIGUOUS, "workflow.validation.session_transition_ambiguous"],
]);
