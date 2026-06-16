// Validation, delete, save, and cached-inspector response fixtures shared across the
// split WorkflowEditorRoute tests.
export const activeLinkResponse = {
  links: [
    {
      id: "link-1",
      project_id: "project-1",
      workflow_id: "workflow-1",
      default: true,
    },
  ],
};

const workflow = {
  workflow_id: "workflow-1",
  display_name: "Delivery",
  description: "",
  version: 1,
  is_project_default: true,
  valid_for_task_creation: true,
  validation_errors: [],
};

export const boardResponse = {
  board: {
    project_id: "project-1",
    project: { project_key: "PROJ", display_name: "Project" },
    selected_workflow: workflow,
    workflows: [workflow],
    groups: [],
    columns: [],
    generated_at_unix_ms: 1,
  },
};

const workflowDeleteImpactResponse = {
  workflow_id: "workflow-1",
  version: 1,
  project_count: 1,
  link_count: 1,
  default_replacement_project_count: 0,
  task_count: 2,
  active_run_count: 0,
  runnable_run_count: 0,
  blocked_task_count: 0,
};

export const workflowDeletePreviewResponse = {
  impact: workflowDeleteImpactResponse,
};

export const workflowDeleteResponse = {
  deleted: true,
  impact: workflowDeleteImpactResponse,
  blockers: [],
};

export const invalidValidationResponse = {
  valid: false,
  errors: [
    {
      code: "workflow.validation.invalid",
      message: "Done transition is invalid.",
      workflow_id: "workflow-1",
      node_id: "node-1",
      transition_group_id: "tg-1",
      edge_id: "edge-1",
      details: {
        input_name: "summary",
        provider_edge_id: "edge-provider",
      },
      related_ids: [],
      blocks_context: true,
    },
  ],
};

export function workflowValidationResponseWithMessages(messages: readonly string[]) {
  return {
    valid: false,
    errors: messages.map((message, index) => {
      const idSuffix = (index + 1).toString();
      return {
        code: "workflow.validation.invalid",
        message,
        workflow_id: "workflow-1",
        node_id: "node-1",
        transition_group_id: `tg-${idSuffix}`,
        edge_id: `edge-${idSuffix}`,
        related_ids: [],
        blocks_context: true,
      };
    }),
  };
}

export const graphValidationResponse = {
  derived_wiring: {
    nodes: [
      {
        node_id: "join",
        join_output_fields: [{ name: "summary", description: "Summary" }],
      },
    ],
    edges: [
      {
        edge_id: "edge-1",
        required_provider_fields: [{ name: "summary", description: "Summary" }],
      },
    ],
  },
  results: {
    draft: invalidValidationResponse,
    execution: invalidValidationResponse,
  },
};

export const validGraphValidationResponse = {
  derived_wiring: {
    edges: [],
    nodes: [],
    transition_groups: [],
  },
  results: {
    draft: { errors: [], valid: true },
    execution: { errors: [], valid: true },
  },
};

const emptyAgentPromptExecutionValidationResponse = {
  valid: false,
  errors: [
    {
      code: "workflow.validation.transition_prompt_required",
      message: "transition into an agent node requires a prompt",
      workflow_id: "workflow-1",
      node_id: "review",
      transition_group_id: "tg-review",
      edge_id: "edge-review",
      related_ids: [],
      blocks_context: true,
    },
  ],
};

export const blockedDraftValidationMessage = "transition into an agent node requires a prompt";

export const blockedDraftGraphValidationResponse = {
  derived_wiring: graphValidationResponse.derived_wiring,
  results: {
    draft: {
      valid: false,
      errors: [
        {
          code: "workflow.validation.transition_prompt_required",
          message: blockedDraftValidationMessage,
          workflow_id: "workflow-1",
          node_id: "review",
          transition_group_id: "tg-review",
          edge_id: "edge-review",
          related_ids: [],
          blocks_context: true,
        },
      ],
    },
    execution: { errors: [], valid: true },
  },
};

export const emptyAgentPromptExecutionGraphValidationResponse = {
  derived_wiring: graphValidationResponse.derived_wiring,
  results: {
    draft: { errors: [], valid: true },
    execution: emptyAgentPromptExecutionValidationResponse,
  },
};

export const invalidNodeGroupValidationResponse = {
  derived_wiring: {
    edges: [],
    nodes: [],
    transition_groups: [],
  },
  results: {
    draft: {
      errors: [
        {
          blocks_context: true,
          code: "workflow.validation.invalid_node_group",
          message:
            "Node Backlog cannot directly fan out into a node group yet; insert one split agent after it, fan out from that agent into the group, then join the branches",
          related_ids: ["group-1"],
          workflow_id: "workflow-1",
        },
      ],
      valid: false,
    },
    execution: { errors: [], valid: true },
  },
};

export const graphSaveImpactResponse = {
  removed_node_count: 0,
  removed_transition_group_count: 0,
  removed_edge_count: 0,
  node_task_reference_count: 0,
  edge_task_reference_count: 0,
  active_node_placement_count: 0,
  pending_approval_count: 0,
  active_run_count: 0,
  runnable_run_count: 0,
  start_node_change_count: 0,
  last_terminal_change_count: 0,
  task_referenced_node_kind_change_count: 0,
};

export const cachedWorkflowDefinition = {
  workflow: { id: "workflow-1", name: "Delivery", description: "", version: 1 },
  derivedWiring: { diagnostics: [], edges: [], nodes: [], transitionGroups: [] },
  nodeGroups: [
    { id: "group-1", workflowID: "workflow-1", key: "core", name: "Core", sortOrder: 1, nodeIDs: ["node-1"] },
  ],
  nodes: [
    {
      id: "node-1",
      workflowID: "workflow-1",
      key: "implement",
      kind: "agent",
      name: "Implement",
      groupID: "group-1",
      groupKey: "core",
      subagentRole: "coder",
      promptTemplate: "Implement the task.",
      inputFields: [{ name: "summary", description: "Summary" }],
      joinInputProviders: [],
      outputFields: [{ name: "summary", description: "Summary" }],
    },
    {
      id: "join",
      workflowID: "workflow-1",
      key: "join",
      kind: "join",
      name: "Join",
      groupID: "",
      groupKey: "",
      subagentRole: "",
      promptTemplate: "",
      inputFields: [],
      joinInputProviders: [],
      outputFields: [],
    },
    {
      id: "done",
      workflowID: "workflow-1",
      key: "done",
      kind: "terminal",
      name: "Done",
      groupID: "",
      groupKey: "",
      subagentRole: "",
      promptTemplate: "",
      inputFields: [],
      joinInputProviders: [],
      outputFields: [],
    },
  ],
  transitionGroups: [
    { description: "", id: "tg-1", workflowID: "workflow-1", sourceNodeID: "node-1", transitionID: "join", name: "Join" },
    { description: "", id: "tg-2", workflowID: "workflow-1", sourceNodeID: "join", transitionID: "done", name: "Done" },
  ],
  edges: [
    {
      id: "edge-1",
      workflowID: "workflow-1",
      transitionGroupID: "tg-1",
      key: "done",
      targetNodeID: "join",
      requiresApproval: false,
      contextMode: "new_session",
      contextSource: { kind: "immediate_source", nodeKey: "" },
      inputBindings: [],
      outputRequirements: [],
    },
    {
      id: "edge-2",
      workflowID: "workflow-1",
      transitionGroupID: "tg-2",
      key: "done",
      targetNodeID: "done",
      requiresApproval: true,
      contextMode: "compact_and_continue_session",
      contextSource: { kind: "selected_node", nodeKey: "implement" },
      inputBindings: [],
      outputRequirements: [],
    },
  ],
};

export const cachedValidation = {
  valid: false,
  errors: [
    {
      code: "workflow.validation.invalid",
      message: "Done transition is invalid.",
      workflowID: "workflow-1",
      nodeID: "node-1",
      transitionGroupID: "tg-1",
      edgeID: "edge-1",
      details: { fieldName: "", inputName: "summary", placeholder: "", providerEdgeID: "edge-provider" },
      relatedIDs: [],
      blocksContext: true,
    },
  ],
};
