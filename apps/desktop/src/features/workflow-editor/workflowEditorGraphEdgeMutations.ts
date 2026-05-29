import type { WorkflowEdge } from "../../api";
import type { DraftWorkflowDefinition } from "./workflowEditorDraft";
import { uniqueWorkflowModelKey } from "./workflowEditorGraphKeys";
import {
  edgesForTransitionGroup,
  removeEdges,
  transitionIDsForSource,
} from "./workflowEditorGraphMutationHelpers";
import {
  emptySummary,
  workflowEditorGraphMutationWarnings,
  type ConnectWorkflowNodesInput,
  type EditWorkflowEdgeRouteInput,
  type WorkflowEditorGraphMutationResult,
  workflowSelection,
  unchanged,
} from "./workflowEditorGraphMutationTypes";

export function connectWorkflowNodes(
  draft: DraftWorkflowDefinition,
  input: ConnectWorkflowNodesInput,
): WorkflowEditorGraphMutationResult {
  const source = draft.nodes.find((node) => node.id === input.sourceNodeID);
  const target = draft.nodes.find((node) => node.id === input.targetNodeID);
  if (source === undefined || target === undefined) {
    return unchanged(draft, workflowEditorGraphMutationWarnings.missingConnectNodes);
  }
  if (source.kind === "terminal") {
    return unchanged(draft, workflowEditorGraphMutationWarnings.terminalOutgoingEdge);
  }
  if (target.kind === "start") {
    return unchanged(draft, workflowEditorGraphMutationWarnings.startIncomingEdge);
  }
  const transitionID =
    input.transitionID ??
    uniqueWorkflowModelKey(target.key, transitionIDsForSource(draft, input.sourceNodeID));
  const transitionName = input.transitionName ?? target.name;
  const edgeKey =
    input.edgeKey ?? uniqueWorkflowModelKey(
      target.key,
      edgesForTransitionGroup(draft, input.transitionGroupID).map((edge) => edge.key),
    );
  const transitionGroup = {
    id: input.transitionGroupID,
    name: transitionName,
    sourceNodeID: input.sourceNodeID,
    transitionID,
    workflowID: draft.workflow.id,
  };
  const edge: WorkflowEdge = {
    contextMode: "new_session",
    contextSource: { kind: "immediate_source", nodeKey: "" },
    id: input.edgeID,
    inputBindings: [],
    key: edgeKey,
    outputRequirements: [],
    requiresApproval: false,
    targetNodeID: input.targetNodeID,
    transitionGroupID: input.transitionGroupID,
    workflowID: draft.workflow.id,
  };
  return {
    draft: {
      ...draft,
      edges: [...draft.edges, edge],
      transitionGroups: [...draft.transitionGroups, transitionGroup],
    },
    nextSelection: { edgeID: edge.id, kind: "edge" },
    summary: emptySummary,
    warnings: [],
  };
}

export function deleteWorkflowEdge(
  draft: DraftWorkflowDefinition,
  edgeID: string,
): WorkflowEditorGraphMutationResult {
  const edge = draft.edges.find((item) => item.id === edgeID);
  if (edge === undefined) {
    return unchanged(draft, workflowEditorGraphMutationWarnings.edgeNotFound);
  }
  return removeEdges(draft, [edgeID], workflowSelection);
}

export function editWorkflowEdgeRoute(
  draft: DraftWorkflowDefinition,
  input: EditWorkflowEdgeRouteInput,
): WorkflowEditorGraphMutationResult {
  const edge = draft.edges.find((item) => item.id === input.edgeID);
  if (edge === undefined) {
    return unchanged(draft, workflowEditorGraphMutationWarnings.edgeNotFound);
  }
  return {
    draft: {
      ...draft,
      edges: draft.edges.map((item) =>
        item.id === input.edgeID
          ? {
              ...item,
              contextMode: input.contextMode ?? item.contextMode,
              contextSource: input.contextSource ?? item.contextSource,
              key: input.edgeKey ?? item.key,
              requiresApproval: input.requiresApproval ?? item.requiresApproval,
            }
          : item,
      ),
      transitionGroups: draft.transitionGroups.map((group) =>
        group.id === edge.transitionGroupID
          ? {
              ...group,
              name: input.transitionName ?? group.name,
              transitionID: input.transitionID ?? group.transitionID,
            }
          : group,
      ),
    },
    nextSelection: { edgeID: input.edgeID, kind: "edge" },
    summary: emptySummary,
    warnings: [],
  };
}
