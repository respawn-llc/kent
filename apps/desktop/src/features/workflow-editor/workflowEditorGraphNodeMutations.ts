import type { WorkflowNode } from "../../api";
import type { DraftWorkflowDefinition, DraftWorkflowNode } from "./workflowEditorDraft";
import { uniqueWorkflowModelKey } from "./workflowEditorGraphKeys";
import {
  deleteNodeIDsInternal,
  refreshNodeGroupMembership,
  removeEdgesInternal,
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
    outputFields: [],
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
  if (node.groupID.length > 0) {
    const groupedBranchesAfterDelete = draft.nodes.filter(
      (item) => item.groupID === node.groupID && item.kind === "agent" && item.id !== nodeID,
    );
    if (node.kind === "join") {
      return dissolveWorkflowNodeGroup(draft, node.groupID, workflowSelection);
    }
    if (node.kind === "agent" && groupedBranchesAfterDelete.length < 2) {
      const afterNodeDelete = deleteNodeIDsInternal(draft, new Set([nodeID]));
      const dissolved = dissolveWorkflowNodeGroup(
        afterNodeDelete,
        node.groupID,
        workflowSelection,
        groupedBranchesAfterDelete[0]?.id,
      );
      return { ...dissolved, summary: removedGraphRows(draft, dissolved.draft) };
    }
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
    outputFields: [],
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
  const warnings =
    nextDraft === null ? [workflowEditorGraphMutationWarnings.nodeGroupTopologyInferenceFailed] : [];
  return {
    draft: nextDraft ?? membershipDraft,
    nextSelection: { groupID: group.id, kind: "group" },
    summary: emptySummary,
    warnings,
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
  return dissolveWorkflowNodeGroup(draft, groupID, { nodeID, kind: "node" }, remainingBranches[0]?.id);
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
  preferredPreservedBranchID?: string,
): WorkflowEditorGraphMutationResult {
  const branchIDs = orderedNodeGroupBranchIDs(draft, groupID);
  const preservedBranchID =
    preferredPreservedBranchID !== undefined && branchIDs.includes(preferredPreservedBranchID)
      ? preferredPreservedBranchID
      : branchIDs[0];
  const ungroupedNodes = draft.nodes.map((node) =>
    node.groupID === groupID && node.kind !== "join" ? { ...node, groupID: "", groupKey: "" } : node,
  );
  const joinIDs = new Set(
    ungroupedNodes.filter((item) => item.groupID === groupID && item.kind === "join").map((item) => item.id),
  );
  const rewiredDraft =
    preservedBranchID === undefined
      ? draft
      : {
          ...draft,
          transitionGroups: draft.transitionGroups.map((group) =>
            joinIDs.has(group.sourceNodeID) ? { ...group, sourceNodeID: preservedBranchID } : group,
          ),
        };
  const fanoutEdgeIDs = nodeGroupFanoutEdgeIDsToRemove(rewiredDraft, new Set(branchIDs), preservedBranchID);
  const beforeJoinDeletes =
    fanoutEdgeIDs.length === 0 ? rewiredDraft : removeEdgesInternal(rewiredDraft, new Set(fanoutEdgeIDs));
  const afterJoinDeletes =
    joinIDs.size === 0 ? { ...beforeJoinDeletes, nodes: ungroupedNodes } : deleteNodeIDsInternal({ ...beforeJoinDeletes, nodes: ungroupedNodes }, joinIDs);
  const nextDraft = {
    ...afterJoinDeletes,
    nodeGroups: afterJoinDeletes.nodeGroups.filter((group) => group.id !== groupID),
  };
  const remainingEdgeIDs = new Set(nextDraft.edges.map((edge) => edge.id));
  return {
    draft: nextDraft,
    nextSelection,
    summary: {
      removedEdgeIDs: draft.edges.filter((edge) => !remainingEdgeIDs.has(edge.id)).map((edge) => edge.id),
      removedNodeIDs: [...joinIDs],
      removedTransitionGroupIDs: transitionGroupDifference(draft, nextDraft),
    },
    warnings: [],
  };
}

function orderedNodeGroupBranchIDs(draft: DraftWorkflowDefinition, groupID: string): readonly string[] {
  const group = draft.nodeGroups.find((item) => item.id === groupID);
  const branchIDs = new Set(
    draft.nodes.filter((node) => node.groupID === groupID && node.kind === "agent").map((node) => node.id),
  );
  const ordered = group?.nodeIDs.filter((nodeID) => branchIDs.has(nodeID)) ?? [];
  return ordered.length > 0 ? ordered : [...branchIDs];
}

function nodeGroupFanoutEdgeIDsToRemove(
  draft: DraftWorkflowDefinition,
  branchIDs: ReadonlySet<string>,
  preservedBranchID: string | undefined,
): readonly string[] {
  const edgeIDs: string[] = [];
  for (const group of draft.transitionGroups) {
    const edges = draft.edges.filter((edge) => edge.transitionGroupID === group.id);
    const branchTargetCount = edges.filter((edge) => branchIDs.has(edge.targetNodeID)).length;
    if (branchTargetCount < 2) {
      continue;
    }
    edgeIDs.push(
      ...edges
        .filter((edge) => branchIDs.has(edge.targetNodeID) && edge.targetNodeID !== preservedBranchID)
        .map((edge) => edge.id),
    );
  }
  return edgeIDs;
}

function removedGraphRows(
  before: DraftWorkflowDefinition,
  after: DraftWorkflowDefinition,
): WorkflowEditorGraphMutationResult["summary"] {
  const remainingNodeIDs = new Set(after.nodes.map((node) => node.id));
  const remainingEdgeIDs = new Set(after.edges.map((edge) => edge.id));
  return {
    removedEdgeIDs: before.edges.filter((edge) => !remainingEdgeIDs.has(edge.id)).map((edge) => edge.id),
    removedNodeIDs: before.nodes.filter((node) => !remainingNodeIDs.has(node.id)).map((node) => node.id),
    removedTransitionGroupIDs: transitionGroupDifference(before, after),
  };
}

function defaultNodeName(kind: WorkflowNode["kind"]): string {
  return kind === "terminal" ? "New terminal" : "New agent";
}
