import type { ElkExtendedEdge } from "elkjs/lib/elk-api";

import type { WorkflowDefinition } from "../../api";

export type WorkflowGraphEdgeErrorMarkers = Readonly<{
  edgeIDs: ReadonlySet<string>;
  transitionGroupIDs: ReadonlySet<string>;
}>;

export type WorkflowGraphEdgeModel = Readonly<{
  contextMode: string;
  edgeID: string;
  hasError: boolean;
  id: string;
  label: string;
  sourceNodeID: string;
  targetNodeID: string;
  transitionGroupID: string;
  elk: ElkExtendedEdge;
}>;

export function visibleWorkflowGraphEdgeModels(
  definition: WorkflowDefinition,
  transitionGroupsByID: ReadonlyMap<string, WorkflowDefinition["transitionGroups"][number]>,
  errorMarkers: WorkflowGraphEdgeErrorMarkers,
): readonly WorkflowGraphEdgeModel[] {
  const models: WorkflowGraphEdgeModel[] = [];
  for (const edge of definition.edges) {
    const group = transitionGroupsByID.get(edge.transitionGroupID);
    if (group === undefined) {
      continue;
    }
    models.push(
      workflowGraphEdgeModel({
        contextMode: edge.contextMode,
        edgeID: edge.id,
        hasError: edgeHasError(edge, group, errorMarkers),
        label: edgeLabel(edge.key, group, definition.edges),
        sourceNodeID: group.sourceNodeID,
        targetNodeID: edge.targetNodeID,
        transitionGroupID: group.id,
      }),
    );
  }
  return models;
}

function workflowGraphEdgeModel(
  input: Readonly<{
    contextMode: string;
    edgeID: string;
    hasError: boolean;
    label: string;
    sourceNodeID: string;
    targetNodeID: string;
    transitionGroupID: string;
  }>,
): WorkflowGraphEdgeModel {
  return {
    contextMode: input.contextMode,
    edgeID: input.edgeID,
    hasError: input.hasError,
    id: input.edgeID,
    label: input.label,
    sourceNodeID: input.sourceNodeID,
    targetNodeID: input.targetNodeID,
    transitionGroupID: input.transitionGroupID,
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
