// Workflow definition response fixtures shared across the split WorkflowEditorRoute tests.
export const workflowDefinitionResponse = {
  definition: {
    workflow: {
      id: "workflow-1",
      name: "Delivery",
      description: "",
      version: 1,
    },
    node_groups: [
      {
        group_id: "group-1",
        workflow_id: "workflow-1",
        group_key: "core",
        display_name: "Core",
        sort_order: 1,
        node_ids: ["node-1"],
      },
    ],
    nodes: [
      {
        id: "node-1",
        workflow_id: "workflow-1",
        key: "implement",
        kind: "agent",
        display_name: "Implement",
        group_id: "group-1",
        group_key: "core",
        subagent_role: "coder",
        prompt_template: "Implement the task.",
        input_fields: [{ name: "summary", description: "Summary" }],
        output_fields: [{ name: "summary", description: "Summary" }],
      },
      {
        id: "join",
        workflow_id: "workflow-1",
        key: "join",
        kind: "join",
        display_name: "Join",
      },
      {
        id: "done",
        workflow_id: "workflow-1",
        key: "done",
        kind: "terminal",
        display_name: "Done",
      },
    ],
    transition_groups: [
      {
        id: "tg-1",
        workflow_id: "workflow-1",
        source_node_id: "node-1",
        transition_id: "join",
        display_name: "Join",
      },
      {
        id: "tg-2",
        workflow_id: "workflow-1",
        source_node_id: "join",
        transition_id: "done",
        display_name: "Done",
      },
    ],
    edges: [
      {
        id: "edge-1",
        workflow_id: "workflow-1",
        transition_group_id: "tg-1",
        key: "done",
        target_node_id: "join",
        requires_approval: false,
        context_mode: "new_session",
        context_source: { kind: "immediate_source" },
        parameters: [{ key: "summary", description: "Summary" }],
      },
      {
        id: "edge-2",
        workflow_id: "workflow-1",
        transition_group_id: "tg-2",
        key: "done",
        target_node_id: "done",
        requires_approval: true,
        context_mode: "compact_and_continue_session",
        context_source: { kind: "selected_node", node_key: "implement" },
      },
    ],
  },
};

export const workflowDefinitionResponseWithReviewBranch = {
  definition: {
    ...workflowDefinitionResponse.definition,
    nodes: [
      ...workflowDefinitionResponse.definition.nodes,
      {
        id: "review",
        workflow_id: "workflow-1",
        key: "review",
        kind: "agent",
        display_name: "Review",
        subagent_role: "reviewer",
        prompt_template: "Legacy review prompt.",
      },
    ],
    transition_groups: [
      ...workflowDefinitionResponse.definition.transition_groups,
      {
        id: "tg-review",
        workflow_id: "workflow-1",
        source_node_id: "node-1",
        transition_id: "review",
        display_name: "Review",
      },
    ],
    edges: [
      ...workflowDefinitionResponse.definition.edges,
      {
        id: "edge-review",
        workflow_id: "workflow-1",
        transition_group_id: "tg-review",
        key: "review",
        target_node_id: "review",
        requires_approval: false,
        context_mode: "new_session",
        context_source: { kind: "immediate_source" },
        prompt_template: "Review the task.",
        parameters: [{ key: "summary", description: "Summary" }],
      },
    ],
  },
};

export const workflowDefinitionResponseWithStartAndReviewBranch = {
  definition: {
    ...workflowDefinitionResponseWithReviewBranch.definition,
    nodes: [
      {
        id: "start",
        workflow_id: "workflow-1",
        key: "start",
        kind: "start",
        display_name: "Start",
      },
      ...workflowDefinitionResponseWithReviewBranch.definition.nodes,
    ],
    transition_groups: [
      {
        id: "tg-start",
        workflow_id: "workflow-1",
        source_node_id: "start",
        transition_id: "implement",
        display_name: "Implement",
      },
      ...workflowDefinitionResponseWithReviewBranch.definition.transition_groups,
    ],
    edges: [
      {
        id: "edge-start",
        workflow_id: "workflow-1",
        transition_group_id: "tg-start",
        key: "implement",
        target_node_id: "node-1",
        requires_approval: false,
        context_mode: "new_session",
        context_source: { kind: "immediate_source" },
        prompt_template: "Implement the task.",
      },
      ...workflowDefinitionResponseWithReviewBranch.definition.edges,
    ],
  },
};

export const workflowDefinitionResponseWithSeparateGroupedBranchTransitions = {
  definition: {
    workflow: {
      id: "workflow-1",
      name: "Delivery",
      description: "",
      version: 1,
    },
    node_groups: [
      {
        group_id: "group-parallel",
        workflow_id: "workflow-1",
        group_key: "parallel",
        display_name: "Implement parallel",
        sort_order: 1,
        node_ids: ["impl-a", "impl-b", "join"],
      },
    ],
    nodes: [
      {
        id: "start",
        workflow_id: "workflow-1",
        key: "backlog",
        kind: "start",
        display_name: "Backlog",
      },
      {
        id: "plan",
        workflow_id: "workflow-1",
        key: "plan",
        kind: "agent",
        display_name: "Plan",
        subagent_role: "fast",
        prompt_template: "Plan.",
      },
      {
        id: "impl-a",
        workflow_id: "workflow-1",
        key: "new_agent",
        kind: "agent",
        display_name: "New agent",
        group_id: "group-parallel",
        group_key: "parallel",
        subagent_role: "default",
        prompt_template: "A.",
      },
      {
        id: "impl-b",
        workflow_id: "workflow-1",
        key: "implement",
        kind: "agent",
        display_name: "Implement",
        group_id: "group-parallel",
        group_key: "parallel",
        subagent_role: "fast",
        prompt_template: "B.",
      },
      {
        id: "join",
        workflow_id: "workflow-1",
        key: "implement_join",
        kind: "join",
        display_name: "Implement join",
        group_id: "group-parallel",
        group_key: "parallel",
      },
      {
        id: "done",
        workflow_id: "workflow-1",
        key: "done",
        kind: "terminal",
        display_name: "Done",
      },
    ],
    transition_groups: [
      {
        id: "tg-start-plan",
        workflow_id: "workflow-1",
        source_node_id: "start",
        transition_id: "start",
        display_name: "Start",
      },
      {
        id: "tg-plan-new-agent",
        workflow_id: "workflow-1",
        source_node_id: "plan",
        transition_id: "new_agent",
        display_name: "Implement",
      },
      {
        id: "tg-plan-implement",
        workflow_id: "workflow-1",
        source_node_id: "plan",
        transition_id: "implement",
        display_name: "Implement",
      },
      {
        id: "tg-impl-a-join",
        workflow_id: "workflow-1",
        source_node_id: "impl-a",
        transition_id: "join",
        display_name: "Implement join",
      },
      {
        id: "tg-impl-b-join",
        workflow_id: "workflow-1",
        source_node_id: "impl-b",
        transition_id: "join",
        display_name: "Implement join",
      },
      {
        id: "tg-join-done",
        workflow_id: "workflow-1",
        source_node_id: "join",
        transition_id: "done",
        display_name: "Done",
      },
    ],
    edges: [
      {
        id: "edge-start-plan",
        workflow_id: "workflow-1",
        transition_group_id: "tg-start-plan",
        key: "start",
        target_node_id: "plan",
        requires_approval: false,
        context_mode: "new_session",
        context_source: { kind: "immediate_source" },
        prompt_template: "Plan.",
      },
      {
        id: "edge-plan-new-agent",
        workflow_id: "workflow-1",
        transition_group_id: "tg-plan-new-agent",
        key: "new_agent",
        target_node_id: "impl-a",
        requires_approval: false,
        context_mode: "new_session",
        context_source: { kind: "immediate_source" },
        prompt_template: "A.",
      },
      {
        id: "edge-plan-implement",
        workflow_id: "workflow-1",
        transition_group_id: "tg-plan-implement",
        key: "implement",
        target_node_id: "impl-b",
        requires_approval: false,
        context_mode: "new_session",
        context_source: { kind: "immediate_source" },
        prompt_template: "B.",
      },
      {
        id: "edge-impl-a-join",
        workflow_id: "workflow-1",
        transition_group_id: "tg-impl-a-join",
        key: "join_a",
        target_node_id: "join",
        requires_approval: false,
        context_mode: "new_session",
        context_source: { kind: "immediate_source" },
      },
      {
        id: "edge-impl-b-join",
        workflow_id: "workflow-1",
        transition_group_id: "tg-impl-b-join",
        key: "join_b",
        target_node_id: "join",
        requires_approval: false,
        context_mode: "new_session",
        context_source: { kind: "immediate_source" },
      },
      {
        id: "edge-join-done",
        workflow_id: "workflow-1",
        transition_group_id: "tg-join-done",
        key: "done",
        target_node_id: "done",
        requires_approval: false,
        context_mode: "new_session",
        context_source: { kind: "immediate_source" },
      },
    ],
  },
};

export const workflowDefinitionResponseWithValidGroupedBranches = {
  definition: {
    ...workflowDefinitionResponseWithSeparateGroupedBranchTransitions.definition,
    transition_groups: workflowDefinitionResponseWithSeparateGroupedBranchTransitions.definition.transition_groups.filter(
      (group) => group.id !== "tg-plan-implement",
    ),
    edges: workflowDefinitionResponseWithSeparateGroupedBranchTransitions.definition.edges.map((edge) =>
      edge.id === "edge-plan-implement" ? { ...edge, transition_group_id: "tg-plan-new-agent" } : edge,
    ),
  },
};

export const workflowDefinitionResponseWithStartNode = {
  definition: {
    ...workflowDefinitionResponse.definition,
    nodes: [
      {
        id: "start",
        workflow_id: "workflow-1",
        key: "start",
        kind: "start",
        display_name: "Start",
      },
      ...workflowDefinitionResponse.definition.nodes,
    ],
    transition_groups: [
      {
        id: "tg-start",
        workflow_id: "workflow-1",
        source_node_id: "start",
        transition_id: "implement",
        display_name: "Implement",
      },
      ...workflowDefinitionResponse.definition.transition_groups,
    ],
    edges: [
      {
        id: "edge-start",
        workflow_id: "workflow-1",
        transition_group_id: "tg-start",
        key: "implement",
        target_node_id: "node-1",
        requires_approval: false,
        context_mode: "new_session",
        context_source: { kind: "immediate_source" },
        prompt_template: "Implement the task.",
      },
      ...workflowDefinitionResponse.definition.edges,
    ],
  },
};

export const workflowDefinitionResponseWithUnrelatedAgent = {
  definition: {
    ...workflowDefinitionResponseWithStartNode.definition,
    nodes: [
      ...workflowDefinitionResponseWithStartNode.definition.nodes,
      {
        id: "review",
        workflow_id: "workflow-1",
        key: "review",
        kind: "agent",
        display_name: "Review",
        subagent_role: "coder",
        prompt_template: "Review the task.",
      },
    ],
  },
};

export const workflowDefinitionResponseWithLoopbackTarget = {
  definition: {
    ...workflowDefinitionResponseWithStartNode.definition,
    transition_groups: workflowDefinitionResponseWithStartNode.definition.transition_groups.map((group) =>
      group.id === "tg-2" ? { ...group, transition_id: "rework", display_name: "Rework" } : group,
    ),
    edges: workflowDefinitionResponseWithStartNode.definition.edges.map((edge) =>
      edge.id === "edge-2"
        ? {
            ...edge,
            id: "edge-rework",
            key: "rework",
            target_node_id: "node-1",
            requires_approval: false,
            context_mode: "new_session",
            context_source: { kind: "immediate_source" },
            prompt_template: "Rework the task.",
          }
        : edge,
    ),
  },
};

export const workflowDefinitionResponseWithStartGroupAndUnrelatedAgent = {
  definition: {
    ...workflowDefinitionResponseWithUnrelatedAgent.definition,
    node_groups: [
      {
        ...workflowDefinitionResponseWithUnrelatedAgent.definition.node_groups[0],
        node_ids: ["node-1", "join"],
      },
    ],
    nodes: workflowDefinitionResponseWithUnrelatedAgent.definition.nodes.map((node) =>
      node.id === "join" ? { ...node, group_id: "group-1", group_key: "core" } : node,
    ),
  },
};

export function workflowDefinitionResponseWithAgentRole(subagentRole: string) {
  return {
    definition: {
      ...workflowDefinitionResponse.definition,
      nodes: workflowDefinitionResponse.definition.nodes.map((node) =>
        node.id === "node-1" ? { ...node, subagent_role: subagentRole } : node,
      ),
    },
  };
}

export function workflowDefinitionResponseWithRevision(version: number) {
  return {
    definition: {
      ...workflowDefinitionResponse.definition,
      workflow: {
        ...workflowDefinitionResponse.definition.workflow,
        version: version,
      },
    },
  };
}

export function workflowDefinitionResponseWithEdgeApproval(
  edgeID: string,
  requiresApproval: boolean,
  version: number,
) {
  return {
    definition: {
      ...workflowDefinitionResponse.definition,
      workflow: {
        ...workflowDefinitionResponse.definition.workflow,
        version: version,
      },
      edges: workflowDefinitionResponse.definition.edges.map((edge) =>
        edge.id === edgeID ? { ...edge, requires_approval: requiresApproval } : edge,
      ),
    },
  };
}

export function workflowDefinitionResponseWithEdgePrompt(edgeID: string, promptTemplate: string, version: number) {
  return {
    definition: {
      ...workflowDefinitionResponseWithReviewBranch.definition,
      workflow: {
        ...workflowDefinitionResponseWithReviewBranch.definition.workflow,
        version: version,
      },
      edges: workflowDefinitionResponseWithReviewBranch.definition.edges.map((edge) =>
        edge.id === edgeID ? { ...edge, prompt_template: promptTemplate } : edge,
      ),
    },
  };
}
