import * as wf from "@app/server-api-contract/gen/kent/api/workflow_definition/workflow_definition_pb";
import type { WorkflowGraphDraft } from "./workflowGraphModels";

export const workflowGraphDraftIDs = {
  agentNode: "10000000-0000-4000-8000-000000000002",
  startEdge: "40000000-0000-4000-8000-000000000001",
  startNode: "10000000-0000-4000-8000-000000000001",
  startTransitionGroup: "30000000-0000-4000-8000-000000000001",
} as const;

export const workflowBoundaryGraphIDs = {
  doneNode: "10000000-0000-4000-8000-000000000003",
  edge: "40000000-0000-4000-8000-000000000001",
  joinNode: "10000000-0000-4000-8000-000000000004",
  node: "10000000-0000-4000-8000-000000000002",
  nodeGroup: "20000000-0000-4000-8000-000000000001",
  joinOnlyNodeGroup: "20000000-0000-4000-8000-000000000002",
  relatedEdge: "40000000-0000-4000-8000-000000000002",
  transitionGroup: "30000000-0000-4000-8000-000000000001",
} as const;

export const workflowGraphDraft: WorkflowGraphDraft = {
  nodeGroups: [],
  nodes: [
    {
      id: workflowGraphDraftIDs.startNode,
      key: "backlog",
      kind: "start",
      name: "Backlog",
      groupID: null,
      joinInputProviders: [],
    },
  ],
  transitionGroups: [
    {
      id: workflowGraphDraftIDs.startTransitionGroup,
      sourceNodeID: workflowGraphDraftIDs.startNode,
      transitionID: "start",
      name: "Start",
      description: "Start the workflow.",
    },
  ],
  edges: [
    {
      id: workflowGraphDraftIDs.startEdge,
      transitionGroupID: workflowGraphDraftIDs.startTransitionGroup,
      key: "start",
      targetNodeID: workflowGraphDraftIDs.agentNode,
      assigneeSelection: "configured",
      thinkingSelection: "configured",
      requiresApproval: false,
      contextMode: "new_session",
      contextSource: { kind: "immediate_source", nodeKey: "" },
      promptTemplate: "Start from {{.TaskTitle}}.",
      parameters: [{ key: "brief", description: "Brief", purpose: "ordinary" }],
    },
  ],
};

export const workflowDefinitionResponse = {
  definition: {
    workflow: {
      id: "11111111-1111-4111-8111-111111111111",
      name: "Delivery",
      description: "Delivery workflow",
      version: 9n,
      executionTargetPolicy: {
        mode: wf.ExecutionTargetMode.WORKFLOW_EXECUTION_TARGET_MODE_HEAD,
      },
    },
    nodeGroups: [
      {
        groupId: workflowBoundaryGraphIDs.nodeGroup,
        workflowId: "11111111-1111-4111-8111-111111111111",
        groupKey: "core",
        displayName: "Core",
        sortOrder: 1,
      },
    ],
    nodes: [
      {
        id: workflowBoundaryGraphIDs.node,
        workflowId: "11111111-1111-4111-8111-111111111111",
        key: "implement",
        kind: wf.NodeKind.WORKFLOW_NODE_KIND_AGENT,
        displayName: "Implement",
        groupId: workflowBoundaryGraphIDs.nodeGroup,
        groupKey: "core",
        subagentRole: "coder",
      },
      {
        id: workflowBoundaryGraphIDs.doneNode,
        workflowId: "11111111-1111-4111-8111-111111111111",
        key: "done",
        kind: wf.NodeKind.WORKFLOW_NODE_KIND_TERMINAL,
        displayName: "Done",
        groupId: undefined,
      },
    ],
    transitionGroups: [
      {
        id: workflowBoundaryGraphIDs.transitionGroup,
        workflowId: "11111111-1111-4111-8111-111111111111",
        sourceNodeId: workflowBoundaryGraphIDs.node,
        transitionId: "done",
        displayName: "Done",
        description: "Choose this when implementation is complete.",
      },
    ],
    edges: [
      {
        id: workflowBoundaryGraphIDs.edge,
        workflowId: "11111111-1111-4111-8111-111111111111",
        transitionGroupId: workflowBoundaryGraphIDs.transitionGroup,
        key: "done",
        targetNodeId: workflowBoundaryGraphIDs.doneNode,
        assigneeSelection: wf.AssigneeSelection.WORKFLOW_ASSIGNEE_SELECTION_CONFIGURED,
        thinkingSelection: wf.ThinkingSelection.WORKFLOW_THINKING_SELECTION_CONFIGURED,
        requiresApproval: false,
        contextMode: wf.ContextMode.WORKFLOW_CONTEXT_MODE_NEW_SESSION,
        contextSource: {
          kind: wf.ContextSourceKind.WORKFLOW_CONTEXT_SOURCE_KIND_SELECTED_NODE,
          nodeKey: "implement",
        },
        promptTemplate: "Summarize the implementation.",
        parameters: [
          {
            key: "summary",
            description: "Summary",
            purpose: wf.ParameterPurpose.WORKFLOW_PARAMETER_PURPOSE_ORDINARY,
          },
        ],
        inputBindings: [],
        outputRequirements: [],
      },
    ],
    derivedWiring: {
      nodes: [
        {
          nodeId: workflowBoundaryGraphIDs.node,
          possibleProvisionFields: [
            {
              name: "summary",
              description: "Summary",
            },
          ],
        },
      ],
      transitionGroups: [
        {
          transitionGroupId: workflowBoundaryGraphIDs.transitionGroup,
          requiredProvisionFields: [
            {
              name: "summary",
              description: "Summary",
            },
          ],
        },
      ],
      edges: [
        {
          edgeId: workflowBoundaryGraphIDs.edge,
          inputBindings: [
            {
              name: "summary",
              source: "transition_output",
              field: "summary",
            },
          ],
          requiredProvisionFields: [
            {
              name: "summary",
              description: "Summary",
            },
          ],
          assigneeSelectionApplicability: {
            available: true,
            parameterVisible: true,
            reason: wf.SelectorApplicabilityReason.WORKFLOW_SELECTOR_APPLICABILITY_REASON_ELIGIBLE,
          },
          thinkingSelectionApplicability: {
            available: true,
            parameterVisible: true,
            reason: wf.SelectorApplicabilityReason.WORKFLOW_SELECTOR_APPLICABILITY_REASON_ELIGIBLE,
          },
        },
      ],
    },
  },
};
export const workflowValidationResponse = {
  valid: false,
  errors: [
    {
      code: wf.ValidationErrorCode.INVALID_NODE_KIND,
      message: "Invalid edge",
      workflowId: "11111111-1111-4111-8111-111111111111",
      nodeId: workflowBoundaryGraphIDs.node,
      transitionGroupId: workflowBoundaryGraphIDs.transitionGroup,
      edgeId: workflowBoundaryGraphIDs.edge,
      details: {
        inputName: "summary",
        placeholder: ".Params.summary",
        providerEdgeId: undefined,
      },
      relatedIds: [workflowBoundaryGraphIDs.relatedEdge],
      blocksContext: true,
    },
  ],
};
export const workflowLinksResponse = {
  links: [
    {
      id: "link-1",
      projectId: "project-1",
      workflowId: "11111111-1111-4111-8111-111111111111",
      default: true,
    },
  ],
};
export const workflowDeleteImpactResponse = {
  workflowId: "11111111-1111-4111-8111-111111111111",
  version: 7n,
  projectCount: 1n,
  linkCount: 1n,
  defaultReplacementProjectCount: 0n,
  taskCount: 2n,
  currentNodeCount: 0n,
  pendingApprovalCount: 1n,
  blockedTaskCount: 1n,
};
export const workflowDeletePreviewResponse = {
  impact: workflowDeleteImpactResponse,
};
export const workflowDeleteResponse = {
  deleted: false,
  impact: workflowDeleteImpactResponse,
  blockers: [
    {
      code: "pending_approvals",
      message: "Workflow has pending approvals.",
      count: 1n,
    },
  ],
};
export const workflowGraphSaveImpactResponse = {
  removedNodeGroupCount: 1n,
  removedNodeCount: 0n,
  removedTransitionGroupCount: 0n,
  removedEdgeCount: 1n,
  removedEntities: [
    {
      entityType: wf.GraphEntityType.WORKFLOW_GRAPH_ENTITY_TYPE_EDGE,
      entityId: workflowGraphDraftIDs.startEdge,
    },
    {
      entityType: wf.GraphEntityType.WORKFLOW_GRAPH_ENTITY_TYPE_NODE_GROUP,
      entityId: workflowBoundaryGraphIDs.nodeGroup,
    },
  ],
  nodeTaskReferenceCount: 0n,
  edgeTaskReferenceCount: 0n,
  activeCurrentNodeCount: 0n,
  pendingApprovalCount: 0n,
  startNodeChangeCount: 0n,
  lastTerminalChangeCount: 0n,
  taskReferencedNodeKindChangeCount: 0n,
};
