import type { ElkExtendedEdge, ElkNode } from "elkjs/lib/elk-api";

import {
  graphNodeWidth,
  type NodeLayoutOffset,
  type WorkflowGraphEndpointPort,
  type WorkflowGraphNodeRect,
} from "./workflowGraphLayoutGeometry";
import type { WorkflowGraphNode, WorkflowGraphPoint } from "./workflowGraphLayout";

export function layoutEdgeByID(root: ElkNode): ReadonlyMap<string, ElkExtendedEdge> {
  const out = new Map<string, ElkExtendedEdge>();
  function visit(node: ElkNode): void {
    for (const edge of node.edges ?? []) {
      out.set(edge.id, edge);
    }
    for (const child of node.children ?? []) {
      visit(child);
    }
  }
  visit(root);
  return out;
}

export function absoluteLayoutOffsetByID(root: ElkNode): ReadonlyMap<string, NodeLayoutOffset> {
  const out = new Map<string, NodeLayoutOffset>([[root.id, { x: 0, y: 0 }]]);
  function visit(node: ElkNode, parentOffset: NodeLayoutOffset): void {
    for (const child of node.children ?? []) {
      const childOffset = {
        x: parentOffset.x + (child.x ?? 0),
        y: parentOffset.y + (child.y ?? 0),
      };
      out.set(child.id, childOffset);
      visit(child, childOffset);
    }
  }
  visit(root, { x: 0, y: 0 });
  return out;
}

export function workflowGraphEdgeRoutePoints(
  model: Readonly<{
    sourceNodeID: string;
    sourcePort: WorkflowGraphEndpointPort;
    targetNodeID: string;
    targetPort: WorkflowGraphEndpointPort;
  }>,
  edge: ElkExtendedEdge | undefined,
  options: Readonly<{
    alignedJoinNodeIDs: ReadonlySet<string>;
    containerOffsetByID: ReadonlyMap<string, NodeLayoutOffset>;
    groupNodeByGroupID: ReadonlyMap<string, WorkflowGraphNode>;
    rectByNodeID: ReadonlyMap<string, WorkflowGraphNodeRect>;
  }>,
): readonly WorkflowGraphPoint[] {
  const source = options.rectByNodeID.get(model.sourceNodeID);
  const target = options.rectByNodeID.get(model.targetNodeID);
  const routedPoints = edgeRoutePoints(edge, options.containerOffsetByID);
  if (source === undefined || target === undefined) {
    return routedPoints;
  }
  const sourceAligned = options.alignedJoinNodeIDs.has(model.sourceNodeID);
  const targetAligned = options.alignedJoinNodeIDs.has(model.targetNodeID);
  if (isBranchToAlignedJoin(source, target, targetAligned)) {
    return branchJoinEdgeRoutePoints(model, source, target, options.groupNodeByGroupID);
  }
  return adjustAlignedJoinEndpointRoutePoints(model, routedPoints, source, target, { sourceAligned, targetAligned });
}

function edgeRoutePoints(
  edge: ElkExtendedEdge | undefined,
  containerOffsetByID: ReadonlyMap<string, NodeLayoutOffset>,
): readonly WorkflowGraphPoint[] {
  if (edge === undefined) {
    return [];
  }
  const section = edge.sections?.[0];
  if (section === undefined) {
    return [];
  }
  const offset =
    edge.container === undefined ? { x: 0, y: 0 } : containerOffsetByID.get(edge.container) ?? { x: 0, y: 0 };
  return [section.startPoint, ...(section.bendPoints ?? []), section.endPoint].map((point) => ({
    x: point.x + offset.x,
    y: point.y + offset.y,
  }));
}

function isBranchToAlignedJoin(
  source: WorkflowGraphNodeRect,
  target: WorkflowGraphNodeRect,
  targetAligned: boolean,
): boolean {
  return targetAligned && target.kind === "join" && source.groupID.length > 0 && source.groupID === target.groupID;
}

function branchJoinEdgeRoutePoints(
  model: Readonly<{ sourcePort: WorkflowGraphEndpointPort; targetPort: WorkflowGraphEndpointPort }>,
  source: WorkflowGraphNodeRect,
  target: WorkflowGraphNodeRect,
  groupNodeByGroupID: ReadonlyMap<string, WorkflowGraphNode>,
): readonly WorkflowGraphPoint[] {
  const start = centeredSourcePoint(source);
  const end = joinCenterTargetPoint(target);
  const groupNode = groupNodeByGroupID.get(target.groupID);
  const groupRight = groupNode === undefined ? source.x + source.width : groupNode.position.x + graphNodeWidth(groupNode);
  const busX = groupRight + Math.max(24, (target.x - groupRight) / 2);
  return compactRoutePoints([start, { x: busX, y: start.y }, { x: busX, y: end.y }, end]);
}

function adjustAlignedJoinEndpointRoutePoints(
  model: Readonly<{ sourcePort: WorkflowGraphEndpointPort; targetPort: WorkflowGraphEndpointPort }>,
  points: readonly WorkflowGraphPoint[],
  source: WorkflowGraphNodeRect,
  target: WorkflowGraphNodeRect,
  flags: Readonly<{ sourceAligned: boolean; targetAligned: boolean }>,
): readonly WorkflowGraphPoint[] {
  if (points.length < 2 || (!flags.sourceAligned && !flags.targetAligned)) {
    return points;
  }
  const adjusted = [...points];
  if (flags.sourceAligned) {
    adjusted[0] = centeredSourcePoint(source);
  }
  if (flags.targetAligned) {
    adjusted[adjusted.length - 1] = joinCenterTargetPoint(target);
  }
  return compactRoutePoints(adjusted);
}

function sourceHandlePoint(rect: WorkflowGraphNodeRect, port: WorkflowGraphEndpointPort): WorkflowGraphPoint {
  return { x: rect.x + rect.width, y: rect.y + port.y };
}

function targetHandlePoint(rect: WorkflowGraphNodeRect, port: WorkflowGraphEndpointPort): WorkflowGraphPoint {
  return { x: rect.x, y: rect.y + port.y };
}

function centeredSourcePoint(rect: WorkflowGraphNodeRect): WorkflowGraphPoint {
  return { x: rect.x + rect.width, y: rect.y + rect.height / 2 };
}

function joinCenterTargetPoint(rect: WorkflowGraphNodeRect): WorkflowGraphPoint {
  return { x: rect.x, y: rect.y + rect.height / 2 };
}

function compactRoutePoints(points: readonly WorkflowGraphPoint[]): readonly WorkflowGraphPoint[] {
  return points.filter((point, index) => {
    const previous = points[index - 1];
    const next = points[index + 1];
    return (
      previous === undefined ||
      next === undefined ||
      (previous.x - point.x) * (next.y - point.y) !== (previous.y - point.y) * (next.x - point.x)
    );
  });
}
