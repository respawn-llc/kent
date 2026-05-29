import type { WorkflowNode } from "../../api";
import type { DraftWorkflowDefinition, DraftWorkflowNode } from "./workflowEditorDraft";
import { uniqueWorkflowModelKey } from "./workflowEditorGraphKeys";
import {
  deleteNodeIDsInternal,
  refreshNodeGroupMembership,
  transitionGroupDifference,
} from "./workflowEditorGraphMutationHelpers";
import {
  emptySummary,
  workflowEditorGraphMutationWarnings,
  workflowSelection,
  unchanged,
  type AddWorkflowNodeInput,
  type AddWorkflowNodeToGroupInput,
  type CreateWorkflowNodeGroupInput,
  type WorkflowEditorGraphMutationResult,
} from "./workflowEditorGraphMutationTypes";
import { inferNodeGroupV1Topology } from "./workflowEditorGraphNodeGroupTopology";

export function addWorkflowNode(
  draft: DraftWorkflowDefinition,
  input: AddWorkflowNodeInput,
): WorkflowEditorGraphMutationResult {
  const name = input.name ?? defaultNodeName(input.kind);
  const key = input.key ?? uniqueWorkflowModelKey(name, draft.nodes.map((node) => node.key));
  const node: DraftWorkflowNode = {
    groupID: "",
    groupKey: "",
    id: input.id,
    inputFields: [],
    joinInputProviders: [],
    key,
    kind: input.kind,
    name,
    promptTemplate: input.kind === "agent" ? input.promptTemplate ?? "" : "",
    subagentRole: input.kind === "agent" ? input.subagentRole ?? "default" : "",
    workflowID: draft.workflow.id,
  };
  return {
    draft: { ...draft, nodes: [...draft.nodes, node] },
    nextSelection: { kind: "node", nodeID: node.id },
    summary: emptySummary,
    warnings: [],
  };
}

export function deleteWorkflowNode(
  draft: DraftWorkflowDefinition,
  nodeID: string,
): WorkflowEditorGraphMutationResult {
  const node = draft.nodes.find((item) => item.id === nodeID);
  if (node === undefined) {
    return unchanged(draft, workflowEditorGraphMutationWarnings.nodeNotFound);
  }
  if (node.kind === "start") {
    return unchanged(draft, workflowEditorGraphMutationWarnings.startNodeDelete);
  }
  if (node.kind === "terminal" && draft.nodes.filter((item) => item.kind === "terminal").length <= 1) {
    return unchanged(draft, workflowEditorGraphMutationWarnings.lastTerminalDelete);
  }
  const removedTransitionGroupIDs = new Set(
    draft.transitionGroups.filter((group) => group.sourceNodeID === nodeID).map((group) => group.id),
  );
  const removedEdgeIDs = new Set(
    draft.edges
      .filter((edge) => edge.targetNodeID === nodeID || removedTransitionGroupIDs.has(edge.transitionGroupID))
      .map((edge) => edge.id),
  );
  const nextDraft = deleteNodeIDsInternal(draft, new Set([nodeID]));
  return {
    draft: nextDraft,
    nextSelection: workflowSelection,
    summary: {
      removedEdgeIDs: [...removedEdgeIDs],
      removedNodeIDs: [nodeID],
      removedTransitionGroupIDs: transitionGroupDifference(draft, nextDraft),
    },
    warnings: [],
  };
}

export function createWorkflowNodeGroupFromNode(
  draft: DraftWorkflowDefinition,
  input: CreateWorkflowNodeGroupInput,
): WorkflowEditorGraphMutationResult {
  const node = draft.nodes.find((item) => item.id === input.nodeID);
  if (node === undefined) {
    return unchanged(draft, workflowEditorGraphMutationWarnings.nodeNotFound);
  }
  if (node.kind !== "agent") {
    return unchanged(draft, workflowEditorGraphMutationWarnings.nodeGroupRequiresAgent);
  }
  if (node.groupID.length > 0) {
    return unchanged(draft, workflowEditorGraphMutationWarnings.nodeGroupRequiresUngroupedNode);
  }
  const groupName = input.groupName ?? `${node.name} parallel`;
  const groupKey = input.groupKey ?? uniqueWorkflowModelKey(groupName, draft.nodeGroups.map((group) => group.key));
  const joinKey = uniqueWorkflowModelKey(`${groupKey}_join`, draft.nodes.map((item) => item.key));
  const group = {
    id: input.groupID,
    key: groupKey,
    name: groupName,
    nodeIDs: [node.id, input.joinNodeID],
    sortOrder: draft.nodeGroups.length * 100,
    workflowID: draft.workflow.id,
  };
  const joinNode: DraftWorkflowNode = {
    groupID: group.id,
    groupKey: group.key,
    id: input.joinNodeID,
    inputFields: [],
    joinInputProviders: [],
    key: joinKey,
    kind: "join",
    name: `${node.name} join`,
    promptTemplate: "",
    subagentRole: "",
    workflowID: draft.workflow.id,
  };
  const nodes = draft.nodes.map((item) =>
    item.id === node.id ? { ...item, groupID: group.id, groupKey: group.key } : item,
  );
  return {
    draft: { ...draft, nodeGroups: [...draft.nodeGroups, group], nodes: [...nodes, joinNode] },
    nextSelection: { groupID: group.id, kind: "group" },
    summary: emptySummary,
    warnings: [],
  };
}

export function addWorkflowNodeToGroup(
  draft: DraftWorkflowDefinition,
  input: AddWorkflowNodeToGroupInput,
): WorkflowEditorGraphMutationResult {
  const node = draft.nodes.find((item) => item.id === input.nodeID);
  if (node === undefined) {
    return unchanged(draft, workflowEditorGraphMutationWarnings.nodeNotFound);
  }
  const group = draft.nodeGroups.find((item) => item.id === input.groupID);
  if (group === undefined) {
    return unchanged(draft, workflowEditorGraphMutationWarnings.nodeGroupNotFound);
  }
  if (node.kind !== "agent") {
    return unchanged(draft, workflowEditorGraphMutationWarnings.nodeGroupRequiresAgentMembership);
  }
  if (node.groupID.length > 0 && node.groupID !== group.id) {
    return unchanged(draft, workflowEditorGraphMutationWarnings.nodeGroupRequiresUngroupedNode);
  }
  const nodes = draft.nodes.map((item) =>
    item.id === node.id ? { ...item, groupID: group.id, groupKey: group.key } : item,
  );
  const membershipDraft = {
    ...draft,
    nodeGroups: refreshNodeGroupMembership(draft.nodeGroups, nodes),
    nodes,
  };
  const nextDraft =
    input.inferredTopologyIDs === undefined
      ? membershipDraft
      : inferNodeGroupV1Topology(membershipDraft, node.id, input.inferredTopologyIDs);
  if (nextDraft === null) {
    return unchanged(draft, workflowEditorGraphMutationWarnings.nodeGroupTopologyInferenceFailed);
  }
  return {
    draft: nextDraft,
    nextSelection: { groupID: group.id, kind: "group" },
    summary: emptySummary,
    warnings: [],
  };
}

export function removeWorkflowNodeFromGroup(
  draft: DraftWorkflowDefinition,
  nodeID: string,
): WorkflowEditorGraphMutationResult {
  const node = draft.nodes.find((item) => item.id === nodeID);
  if (node === undefined) {
    return unchanged(draft, workflowEditorGraphMutationWarnings.nodeNotFound);
  }
  if (node.groupID.length === 0) {
    return unchanged(draft, workflowEditorGraphMutationWarnings.nodeGroupNotFound);
  }
  if (node.kind !== "agent") {
    return unchanged(draft, workflowEditorGraphMutationWarnings.nodeGroupRequiresAgentMembership);
  }
  const groupID = node.groupID;
  const ungroupedNodes = draft.nodes.map((item) =>
    item.id === nodeID ? { ...item, groupID: "", groupKey: "" } : item,
  );
  const remainingBranches = ungroupedNodes.filter(
    (item) => item.groupID === groupID && item.kind === "agent",
  );
  if (remainingBranches.length > 1) {
    return {
      draft: {
        ...draft,
        nodeGroups: refreshNodeGroupMembership(draft.nodeGroups, ungroupedNodes),
        nodes: ungroupedNodes,
      },
      nextSelection: { nodeID, kind: "node" },
      summary: emptySummary,
      warnings: [],
    };
  }
  return dissolveWorkflowNodeGroup(draft, groupID, { nodeID, kind: "node" });
}

export function deleteWorkflowNodeGroup(
  draft: DraftWorkflowDefinition,
  groupID: string,
): WorkflowEditorGraphMutationResult {
  const group = draft.nodeGroups.find((item) => item.id === groupID);
  if (group === undefined) {
    return unchanged(draft, workflowEditorGraphMutationWarnings.nodeGroupNotFound);
  }
  return dissolveWorkflowNodeGroup(draft, group.id, workflowSelection);
}

function dissolveWorkflowNodeGroup(
  draft: DraftWorkflowDefinition,
  groupID: string,
  nextSelection: WorkflowEditorGraphMutationResult["nextSelection"],
): WorkflowEditorGraphMutationResult {
  const ungroupedNodes = draft.nodes.map((node) =>
    node.groupID === groupID && node.kind !== "join" ? { ...node, groupID: "", groupKey: "" } : node,
  );
  const joinIDs = new Set(
    ungroupedNodes.filter((item) => item.groupID === groupID && item.kind === "join").map((item) => item.id),
  );
  const removedJoinTransitionGroupIDs = new Set(
    draft.transitionGroups.filter((group) => joinIDs.has(group.sourceNodeID)).map((group) => group.id),
  );
  const removedEdgeIDs = draft.edges
    .filter((edge) => joinIDs.has(edge.targetNodeID) || removedJoinTransitionGroupIDs.has(edge.transitionGroupID))
    .map((edge) => edge.id);
  const afterJoinDeletes =
    joinIDs.size === 0 ? { ...draft, nodes: ungroupedNodes } : deleteNodeIDsInternal({ ...draft, nodes: ungroupedNodes }, joinIDs);
  return {
    draft: {
      ...afterJoinDeletes,
      nodeGroups: afterJoinDeletes.nodeGroups.filter((group) => group.id !== groupID),
    },
    nextSelection,
    summary: {
      removedEdgeIDs,
      removedNodeIDs: [...joinIDs],
      removedTransitionGroupIDs: transitionGroupDifference(draft, afterJoinDeletes),
    },
    warnings: [],
  };
}

function defaultNodeName(kind: WorkflowNode["kind"]): string {
  return kind === "terminal" ? "New terminal" : "New agent";
}
