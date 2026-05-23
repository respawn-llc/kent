import type { ElkExtendedEdge } from "elkjs/lib/elk-api";

import type { WorkflowDefinition } from "../../api";

export type WorkflowGraphEdgeErrorMarkers = Readonly<{
  edgeIDs: ReadonlySet<string>;
  transitionGroupIDs: ReadonlySet<string>;
}>;

export type WorkflowGraphEdgeModel = Readonly<{
  id: string;
  sourceNodeID: string;
  targetNodeID: string;
  label: string;
  hasError: boolean;
  elk: ElkExtendedEdge;
}>;

type CollapsedJoinTarget = Readonly<{
  edgeIDs: readonly string[];
  hasError: boolean;
  targetNodeID: string;
}>;

export function visibleWorkflowGraphEdgeModels(
  definition: WorkflowDefinition,
  transitionGroupsByID: ReadonlyMap<string, WorkflowDefinition["transitionGroups"][number]>,
  errorMarkers: WorkflowGraphEdgeErrorMarkers,
): readonly WorkflowGraphEdgeModel[] {
  const nodesByID = new Map(definition.nodes.map((node) => [node.id, node]));
  const outgoingEdgesBySourceNodeID = outgoingEdgesBySourceNodeIDMap(definition, transitionGroupsByID);
  const models: WorkflowGraphEdgeModel[] = [];
  for (const edge of definition.edges) {
    const group = transitionGroupsByID.get(edge.transitionGroupID);
    if (group === undefined) {
      continue;
    }
    const sourceNode = nodesByID.get(group.sourceNodeID);
    const targetNode = nodesByID.get(edge.targetNodeID);
    if (sourceNode?.kind === "join") {
      continue;
    }
    if (targetNode?.kind === "join") {
      const collapsedTargets = collapsedJoinTargets({
        currentNodeID: targetNode.id,
        errorMarkers,
        nodesByID,
        outgoingEdgesBySourceNodeID,
        transitionGroupsByID,
        visitedJoinNodeIDs: new Set([targetNode.id]),
      });
      for (const collapsedTarget of collapsedTargets) {
        models.push(
          workflowGraphEdgeModel({
            edgeID: [edge.id, ...collapsedTarget.edgeIDs].join(":"),
            sourceNodeID: group.sourceNodeID,
            targetNodeID: collapsedTarget.targetNodeID,
            label: edgeLabel(edge.key, group, definition.edges),
            hasError: edgeHasError(edge, group, errorMarkers) || collapsedTarget.hasError,
          }),
        );
      }
      continue;
    }
    models.push(
      workflowGraphEdgeModel({
        edgeID: edge.id,
        sourceNodeID: group.sourceNodeID,
        targetNodeID: edge.targetNodeID,
        label: edgeLabel(edge.key, group, definition.edges),
        hasError: edgeHasError(edge, group, errorMarkers),
      }),
    );
  }
  return models;
}

function collapsedJoinTargets({
  currentNodeID,
  errorMarkers,
  nodesByID,
  outgoingEdgesBySourceNodeID,
  transitionGroupsByID,
  visitedJoinNodeIDs,
}: Readonly<{
  currentNodeID: string;
  errorMarkers: WorkflowGraphEdgeErrorMarkers;
  nodesByID: ReadonlyMap<string, WorkflowDefinition["nodes"][number]>;
  outgoingEdgesBySourceNodeID: ReadonlyMap<string, readonly WorkflowDefinition["edges"][number][]>;
  transitionGroupsByID: ReadonlyMap<string, WorkflowDefinition["transitionGroups"][number]>;
  visitedJoinNodeIDs: ReadonlySet<string>;
}>): readonly CollapsedJoinTarget[] {
  const targets: CollapsedJoinTarget[] = [];
  for (const edge of outgoingEdgesBySourceNodeID.get(currentNodeID) ?? []) {
    const group = transitionGroupsByID.get(edge.transitionGroupID);
    if (group === undefined) {
      continue;
    }
    const edgeError = edgeHasError(edge, group, errorMarkers);
    const targetNode = nodesByID.get(edge.targetNodeID);
    if (targetNode?.kind !== "join") {
      targets.push({ edgeIDs: [edge.id], hasError: edgeError, targetNodeID: edge.targetNodeID });
      continue;
    }
    if (visitedJoinNodeIDs.has(targetNode.id)) {
      continue;
    }
    const nextVisitedJoinNodeIDs = new Set(visitedJoinNodeIDs);
    nextVisitedJoinNodeIDs.add(targetNode.id);
    for (const target of collapsedJoinTargets({
      currentNodeID: targetNode.id,
      errorMarkers,
      nodesByID,
      outgoingEdgesBySourceNodeID,
      transitionGroupsByID,
      visitedJoinNodeIDs: nextVisitedJoinNodeIDs,
    })) {
      targets.push({
        edgeIDs: [edge.id, ...target.edgeIDs],
        hasError: edgeError || target.hasError,
        targetNodeID: target.targetNodeID,
      });
    }
  }
  return targets;
}

function outgoingEdgesBySourceNodeIDMap(
  definition: WorkflowDefinition,
  transitionGroupsByID: ReadonlyMap<string, WorkflowDefinition["transitionGroups"][number]>,
): ReadonlyMap<string, readonly WorkflowDefinition["edges"][number][]> {
  const out = new Map<string, WorkflowDefinition["edges"][number][]>();
  for (const edge of definition.edges) {
    const group = transitionGroupsByID.get(edge.transitionGroupID);
    if (group === undefined) {
      continue;
    }
    const edges = out.get(group.sourceNodeID) ?? [];
    edges.push(edge);
    out.set(group.sourceNodeID, edges);
  }
  return out;
}

function workflowGraphEdgeModel(
  input: Readonly<{
    edgeID: string;
    sourceNodeID: string;
    targetNodeID: string;
    label: string;
    hasError: boolean;
  }>,
): WorkflowGraphEdgeModel {
  return {
    id: input.edgeID,
    sourceNodeID: input.sourceNodeID,
    targetNodeID: input.targetNodeID,
    label: input.label,
    hasError: input.hasError,
    elk: {
      id: input.edgeID,
      sources: [input.sourceNodeID],
      targets: [input.targetNodeID],
    },
  };
}

function edgeHasError(
  edge: WorkflowDefinition["edges"][number],
  group: WorkflowDefinition["transitionGroups"][number],
  errorMarkers: WorkflowGraphEdgeErrorMarkers,
): boolean {
  return errorMarkers.edgeIDs.has(edge.id) || errorMarkers.transitionGroupIDs.has(group.id);
}

function edgeLabel(
  edgeKey: string,
  group: WorkflowDefinition["transitionGroups"][number],
  edges: WorkflowDefinition["edges"],
): string {
  const base = group.name.length > 0 ? group.name : group.transitionID;
  const groupEdgeCount = edges.filter((edge) => edge.transitionGroupID === group.id).length;
  return groupEdgeCount > 1 ? `${base} / ${edgeKey}` : base;
}
