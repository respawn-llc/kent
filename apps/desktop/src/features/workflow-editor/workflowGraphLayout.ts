import { MarkerType, type Edge, type Node } from "@xyflow/react";
import ELK from "elkjs/lib/elk.bundled.js";
import type { ElkNode } from "elkjs/lib/elk-api";

import type { WorkflowDefinition, WorkflowValidation } from "../../api";
import { workflowEdgeColor } from "./workflowGraphColors";
import { visibleWorkflowGraphEdgeModels } from "./workflowGraphEdges";
import {
  absoluteLayoutOffsetByID,
  layoutEdgeByID,
  workflowGraphEdgeRoutePoints,
} from "./workflowGraphLayoutEdgeRoutes";
import {
  emptyGroupHeight,
  emptyGroupWidth,
  workflowJoinNodeSize,
  workflowNodeHeight,
  workflowNodeWidth,
} from "./workflowGraphLayoutGeometry";
import { isWorkflowGroupIslandMember, layoutWorkflowGraphNodes } from "./workflowGraphLayoutNodes";

export type WorkflowGraphNodeData = Readonly<{
  [key: string]: unknown;
  entityID: string;
  entityKind: "node";
  groupID: string;
  key: string;
  kind: string;
  label: string;
  role: string;
  hasError: boolean;
}>;

export type WorkflowGraphGroupData = Readonly<{
  [key: string]: unknown;
  entityID: string;
  entityKind: "group";
  kind: "group";
  label: string;
  empty: boolean;
  hasError: boolean;
}>;

export type WorkflowGraphEdgeData = Readonly<{
  [key: string]: unknown;
  contextMode: string;
  entityID: string;
  entityKind: "edge";
  label: string;
  hasError: boolean;
  routePoints: readonly WorkflowGraphPoint[];
  transitionGroupID: string;
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

const elk = new ELK();

export async function layoutWorkflowGraph(
  definition: WorkflowDefinition,
  validation: WorkflowValidation,
): Promise<WorkflowGraphLayout> {
  const errorMarkers = validationMarkers(validation);
  const transitionGroupsByID = new Map(definition.transitionGroups.map((group) => [group.id, group]));
  const visibleNodes = definition.nodes;
  const edgeModels = visibleWorkflowGraphEdgeModels(definition, transitionGroupsByID, errorMarkers);
  const groupedNodeIDs = new Set(visibleNodes.filter(isWorkflowGroupIslandMember).map((node) => node.id));
  const populatedGroupIDs = new Set(
    visibleNodes.filter(isWorkflowGroupIslandMember).map((node) => node.groupID),
  );
  const emptyGroups = definition.nodeGroups.filter((group) => !populatedGroupIDs.has(group.id));
  const children: ElkNode[] = [
    ...visibleNodes.filter((node) => !groupedNodeIDs.has(node.id)).map((node) => elkWorkflowNode(node)),
    ...definition.nodeGroups
      .filter((group) => populatedGroupIDs.has(group.id))
      .map((group) => elkWorkflowGroupNode(group, visibleNodes)),
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
      "elk.hierarchyHandling": "INCLUDE_CHILDREN",
    },
  };
  const result = await elk.layout(graph);
  const nodeLayout = layoutWorkflowGraphNodes(result, definition, errorMarkers);
  const edgeLayoutByID = layoutEdgeByID(result);
  const containerOffsetByID = absoluteLayoutOffsetByID(result);
  const edges = edgeModels.map(
    (model): WorkflowGraphEdge => ({
      id: model.id,
      source: model.sourceNodeID,
      target: model.targetNodeID,
      type: "workflow",
      markerEnd: { color: workflowEdgeColor(model.contextMode, model.hasError), type: MarkerType.ArrowClosed },
      data: {
        label: model.label,
        contextMode: model.contextMode,
        entityID: model.edgeID,
        entityKind: "edge",
        hasError: model.hasError,
        routePoints: workflowGraphEdgeRoutePoints(model, edgeLayoutByID.get(model.id), {
          containerOffsetByID,
          groupNodeByGroupID: nodeLayout.groupNodeByGroupID,
          rectByNodeID: nodeLayout.rectByNodeID,
          alignedJoinNodeIDs: nodeLayout.alignedJoinNodeIDs,
        }),
        transitionGroupID: model.transitionGroupID,
      },
    }),
  );
  return { nodes: nodeLayout.nodes, edges };
}

function elkWorkflowNode(node: WorkflowDefinition["nodes"][number]): ElkNode {
  return {
    id: node.id,
    width: node.kind === "join" ? workflowJoinNodeSize : workflowNodeWidth,
    height: node.kind === "join" ? workflowJoinNodeSize : workflowNodeHeight,
  };
}

function elkWorkflowGroupNode(
  group: WorkflowDefinition["nodeGroups"][number],
  nodes: readonly WorkflowDefinition["nodes"][number][],
): ElkNode {
  return {
    id: groupNodeID(group.id),
    children: nodes
      .filter((node) => node.groupID === group.id && isWorkflowGroupIslandMember(node))
      .map((node) => elkWorkflowNode(node)),
    layoutOptions: {
      "elk.algorithm": "layered",
      "elk.direction": "RIGHT",
      "elk.padding": "[top=56,left=24,bottom=24,right=24]",
      "elk.spacing.edgeNode": "44",
      "elk.spacing.nodeNode": "80",
      "elk.layered.spacing.nodeNodeBetweenLayers": "120",
    },
  };
}

function groupNodeID(id: string): string {
  return `workflow-group-${id}`;
}

function validationMarkers(validation: WorkflowValidation) {
  return {
    edgeIDs: new Set(
      validation.errors.flatMap((error) => [error.edgeID, error.details.providerEdgeID]).filter(nonEmpty),
    ),
    nodeIDs: new Set(validation.errors.map((error) => error.nodeID).filter(nonEmpty)),
    relatedIDs: new Set(validation.errors.flatMap((error) => error.relatedIDs).filter(nonEmpty)),
    transitionGroupIDs: new Set(validation.errors.map((error) => error.transitionGroupID).filter(nonEmpty)),
  };
}

function nonEmpty(value: string): boolean {
  return value.length > 0;
}
