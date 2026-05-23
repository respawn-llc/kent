import { MarkerType, Position, type Edge, type Node } from "@xyflow/react";
import ELK from "elkjs/lib/elk.bundled.js";
import type { ElkExtendedEdge, ElkNode } from "elkjs/lib/elk-api";

import type { WorkflowDefinition, WorkflowValidation } from "../../api";
import { visibleWorkflowGraphEdgeModels } from "./workflowGraphEdges";

export type WorkflowGraphNodeData = Readonly<{
  [key: string]: unknown;
  kind: string;
  label: string;
  role: string;
  hasError: boolean;
}>;

export type WorkflowGraphGroupData = Readonly<{
  [key: string]: unknown;
  kind: "group";
  label: string;
  empty: boolean;
  hasError: boolean;
}>;

export type WorkflowGraphEdgeData = Readonly<{
  [key: string]: unknown;
  label: string;
  hasError: boolean;
  routePoints: readonly WorkflowGraphPoint[];
}>;

export type WorkflowGraphPoint = Readonly<{ x: number; y: number }>;

export type WorkflowGraphNode = Node<WorkflowGraphNodeData | WorkflowGraphGroupData>;
export type WorkflowGraphWorkflowNode = Node<WorkflowGraphNodeData>;
export type WorkflowGraphGroupNode = Node<WorkflowGraphGroupData>;
export type WorkflowGraphEdge = Edge<WorkflowGraphEdgeData>;

export type WorkflowGraphLayout = Readonly<{
  nodes: readonly WorkflowGraphNode[];
  edges: readonly WorkflowGraphEdge[];
}>;

type ValidationMarkers = ReturnType<typeof validationMarkers>;
type NodeLayoutOffset = Readonly<{ x: number; y: number }>;

const elk = new ELK();
const workflowNodeWidth = 220;
const workflowNodeHeight = 92;
const emptyGroupWidth = 260;
const emptyGroupHeight = 140;

export async function layoutWorkflowGraph(
  definition: WorkflowDefinition,
  validation: WorkflowValidation,
): Promise<WorkflowGraphLayout> {
  const errorMarkers = validationMarkers(validation);
  const transitionGroupsByID = new Map(definition.transitionGroups.map((group) => [group.id, group]));
  const visibleNodes = definition.nodes.filter(isVisibleWorkflowGraphNode);
  const edgeModels = visibleWorkflowGraphEdgeModels(definition, transitionGroupsByID, errorMarkers);
  const groupedNodeIDs = new Set(
    visibleNodes.map((node) => node.groupID).filter((groupID) => groupID.length > 0),
  );
  const emptyGroups = definition.nodeGroups.filter((group) => !groupedNodeIDs.has(group.id));
  const children: ElkNode[] = [
    ...visibleNodes.map((node) => elkWorkflowNode(node.id)),
    ...emptyGroups.map((group) => ({
      id: groupNodeID(group.id),
      width: emptyGroupWidth,
      height: emptyGroupHeight,
    })),
  ];
  const graph: ElkNode = {
    id: "workflow-root",
    children,
    edges: edgeModels.map((edge) => edge.elk),
    layoutOptions: {
      "elk.algorithm": "layered",
      "elk.direction": "RIGHT",
      "elk.edgeRouting": "ORTHOGONAL",
      "elk.spacing.edgeEdge": "28",
      "elk.spacing.edgeNode": "44",
      "elk.spacing.nodeNode": "80",
      "elk.layered.spacing.edgeEdgeBetweenLayers": "28",
      "elk.layered.spacing.edgeNodeBetweenLayers": "56",
      "elk.layered.spacing.nodeNodeBetweenLayers": "120",
    },
  };
  const result = await elk.layout(graph);
  const nodes = flattenNodes(result, definition, errorMarkers);
  const edgeLayoutByID = new Map((result.edges ?? []).map((edge) => [edge.id, edge]));
  const edges = edgeModels.map(
    (model): WorkflowGraphEdge => ({
      id: model.id,
      source: model.sourceNodeID,
      target: model.targetNodeID,
      type: "workflow",
      markerEnd: { type: MarkerType.ArrowClosed },
      data: {
        label: model.label,
        hasError: model.hasError,
        routePoints: edgeRoutePoints(edgeLayoutByID.get(model.id)),
      },
    }),
  );
  return { nodes, edges };
}

function edgeRoutePoints(edge: ElkExtendedEdge | undefined): readonly WorkflowGraphPoint[] {
  const section = edge?.sections?.[0];
  if (section === undefined) {
    return [];
  }
  return [section.startPoint, ...(section.bendPoints ?? []), section.endPoint];
}

function elkWorkflowNode(id: string): ElkNode {
  return { id, width: workflowNodeWidth, height: workflowNodeHeight };
}

function flattenNodes(
  result: ElkNode,
  definition: WorkflowDefinition,
  errorMarkers: ValidationMarkers,
): WorkflowGraphNode[] {
  const workflowNodesByID = new Map(
    definition.nodes.filter(isVisibleWorkflowGraphNode).map((node) => [node.id, node]),
  );
  const layoutByID = new Map((result.children ?? []).map((node) => [node.id, node]));
  const groupLayout = workflowGroupNodes(definition, layoutByID, errorMarkers);
  const out: WorkflowGraphNode[] = [...groupLayout.nodes];
  for (const child of result.children ?? []) {
    if (child.id.startsWith("workflow-group-")) {
      continue;
    }
    const node = workflowNodesByID.get(child.id);
    if (node !== undefined) {
      out.push(
        workflowNode(child, node, {
          errorMarkers,
          offset: groupLayout.memberOffsetByNodeID.get(node.id),
          parentID: groupLayout.memberParentIDByNodeID.get(node.id),
        }),
      );
    }
  }
  return out;
}

function workflowGroupNodes(
  definition: WorkflowDefinition,
  layoutByID: ReadonlyMap<string, ElkNode>,
  errorMarkers: ValidationMarkers,
): Readonly<{
  memberOffsetByNodeID: ReadonlyMap<string, NodeLayoutOffset>;
  memberParentIDByNodeID: ReadonlyMap<string, string>;
  nodes: WorkflowGraphNode[];
}> {
  const nodes: WorkflowGraphNode[] = [];
  const memberParentIDByNodeID = new Map<string, string>();
  const memberOffsetByNodeID = new Map<string, NodeLayoutOffset>();
  for (const group of definition.nodeGroups) {
    const members = groupMembers(definition, layoutByID, group.id);
    const groupNode =
      members.length === 0
        ? emptyGroupNode(group, layoutByID.get(groupNodeID(group.id)), errorMarkers)
        : populatedGroupNode(group, members, errorMarkers);
    nodes.push(groupNode.node);
    for (const nodeID of groupNode.memberNodeIDs) {
      memberParentIDByNodeID.set(nodeID, groupNode.node.id);
      memberOffsetByNodeID.set(nodeID, groupNode.offset);
    }
  }
  return { memberOffsetByNodeID, memberParentIDByNodeID, nodes };
}

function groupMembers(
  definition: WorkflowDefinition,
  layoutByID: ReadonlyMap<string, ElkNode>,
  groupID: string,
): readonly ElkNode[] {
  return definition.nodes
    .filter((node) => node.groupID === groupID && isVisibleWorkflowGraphNode(node))
    .map((node) => layoutByID.get(node.id))
    .filter((node): node is ElkNode => node !== undefined);
}

function emptyGroupNode(
  group: WorkflowDefinition["nodeGroups"][number],
  layout: ElkNode | undefined,
  errorMarkers: ValidationMarkers,
): Readonly<{ memberNodeIDs: readonly string[]; node: WorkflowGraphNode; offset: NodeLayoutOffset }> {
  return {
    memberNodeIDs: [],
    node: {
      id: groupNodeID(group.id),
      type: "workflowGroup",
      position: { x: layout?.x ?? 0, y: layout?.y ?? 0 },
      selectable: false,
      draggable: false,
      data: {
        kind: "group",
        label: group.name || group.key,
        empty: true,
        hasError: errorMarkers.relatedIDs.has(group.id),
      },
      style: { width: layout?.width ?? emptyGroupWidth, height: layout?.height ?? emptyGroupHeight },
    },
    offset: { x: 0, y: 0 },
  };
}

function populatedGroupNode(
  group: WorkflowDefinition["nodeGroups"][number],
  members: readonly ElkNode[],
  errorMarkers: ValidationMarkers,
): Readonly<{ memberNodeIDs: readonly string[]; node: WorkflowGraphNode; offset: NodeLayoutOffset }> {
  const bounds = groupBounds(members);
  return {
    memberNodeIDs: members.map((member) => member.id),
    node: {
      id: groupNodeID(group.id),
      type: "workflowGroup",
      position: { x: bounds.minX, y: bounds.minY },
      selectable: false,
      draggable: false,
      data: {
        kind: "group",
        label: group.name || group.key,
        empty: false,
        hasError: errorMarkers.relatedIDs.has(group.id),
      },
      style: { width: bounds.maxX - bounds.minX, height: bounds.maxY - bounds.minY },
    },
    offset: { x: bounds.minX, y: bounds.minY },
  };
}

function groupBounds(members: readonly ElkNode[]) {
  return {
    minX: Math.min(...members.map((member) => member.x ?? 0)) - 24,
    minY: Math.min(...members.map((member) => member.y ?? 0)) - 56,
    maxX: Math.max(...members.map((member) => (member.x ?? 0) + (member.width ?? workflowNodeWidth))) + 24,
    maxY: Math.max(...members.map((member) => (member.y ?? 0) + (member.height ?? workflowNodeHeight))) + 24,
  };
}

function workflowNode(
  layoutNode: ElkNode,
  node: WorkflowDefinition["nodes"][number],
  options: Readonly<{
    errorMarkers: ValidationMarkers;
    offset: NodeLayoutOffset | undefined;
    parentID: string | undefined;
  }>,
): WorkflowGraphNode {
  const offset = options.offset ?? { x: 0, y: 0 };
  return {
    id: node.id,
    type: "workflowNode",
    ...(options.parentID === undefined ? {} : parentNodeOptions(options.parentID)),
    position: { x: (layoutNode.x ?? 0) - offset.x, y: (layoutNode.y ?? 0) - offset.y },
    sourcePosition: Position.Right,
    targetPosition: Position.Left,
    draggable: false,
    data: {
      kind: node.kind,
      label: node.name,
      role: node.subagentRole,
      hasError: options.errorMarkers.nodeIDs.has(node.id) || options.errorMarkers.relatedIDs.has(node.id),
    },
    style: { width: layoutNode.width ?? workflowNodeWidth, height: layoutNode.height ?? workflowNodeHeight },
  };
}

function parentNodeOptions(parentId: string): Pick<WorkflowGraphNode, "extent" | "parentId"> {
  return { extent: "parent", parentId };
}

function groupNodeID(id: string): string {
  return `workflow-group-${id}`;
}

function isVisibleWorkflowGraphNode(node: WorkflowDefinition["nodes"][number]): boolean {
  return node.kind !== "join";
}

function validationMarkers(validation: WorkflowValidation) {
  return {
    edgeIDs: new Set(validation.errors.map((error) => error.edgeID).filter(nonEmpty)),
    nodeIDs: new Set(validation.errors.map((error) => error.nodeID).filter(nonEmpty)),
    relatedIDs: new Set(validation.errors.flatMap((error) => error.relatedIDs).filter(nonEmpty)),
    transitionGroupIDs: new Set(validation.errors.map((error) => error.transitionGroupID).filter(nonEmpty)),
  };
}

function nonEmpty(value: string): boolean {
  return value.length > 0;
}
