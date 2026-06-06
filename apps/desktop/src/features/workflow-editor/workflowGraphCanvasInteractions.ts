import type { Connection, Edge, Node } from "@xyflow/react";

import type { WorkflowGraphSelection } from "./workflowGraphSelection";
import type {
  WorkflowGraphEdgeData,
  WorkflowGraphGroupData,
  WorkflowGraphEdge,
  WorkflowGraphNode,
  WorkflowGraphNodeData,
} from "./workflowGraphLayout";
import { isInspectableWorkflowNodeKind } from "./workflowGraphNodeKinds";

export type WorkflowGraphReconnectEndpoint = "source" | "target";

export type WorkflowGraphReconnectEdgeInput =
  | Readonly<{ edgeID: string; endpoint: "source"; sourceNodeID: string }>
  | Readonly<{ edgeID: string; endpoint: "target"; targetNodeID: string }>;

export function connectWorkflowGraphNodes(
  connection: Connection,
  onConnectNodes: ((sourceNodeID: string, targetNodeID: string) => void) | undefined,
): void {
  if (connection.source === null || connection.target === null) {
    return;
  }
  onConnectNodes?.(connection.source, connection.target);
}

export function reconnectWorkflowGraphEdge(
  edge: Edge,
  connection: Connection,
  endpoint: WorkflowGraphReconnectEndpoint | null | undefined,
  onReconnectEdge: ((input: WorkflowGraphReconnectEdgeInput) => void) | undefined,
): void {
  if (onReconnectEdge === undefined) {
    return;
  }
  const edgeID = workflowGraphEdgeID(edge);
  const resolvedEndpoint = endpoint ?? inferReconnectEndpoint(edge, connection);
  if (edgeID === null || resolvedEndpoint === null) {
    return;
  }
  if (resolvedEndpoint === "source") {
    if (connection.source === null) {
      return;
    }
    onReconnectEdge({ edgeID, endpoint: "source", sourceNodeID: connection.source });
    return;
  }
  if (connection.target === null) {
    return;
  }
  onReconnectEdge({ edgeID, endpoint: "target", targetNodeID: connection.target });
}

export function selectionFromNode(node: Node): WorkflowGraphSelection | null {
  const { data } = node;
  if (isWorkflowGraphGroupData(data)) {
    return { groupID: data.entityID, kind: "group" };
  }
  if (isWorkflowGraphNodeData(data)) {
    return { kind: "node", nodeID: data.entityID };
  }
  return null;
}

export function selectionFromEdge(edge: Edge): WorkflowGraphSelection | null {
  const { data } = edge;
  if (isWorkflowGraphEdgeData(data)) {
    return { edgeID: data.entityID, kind: "edge" };
  }
  return null;
}

export function groupIDFromPoint(x: number, y: number): string | null {
  const directGroupID = groupIDFromElement(document.elementFromPoint(x, y));
  if (directGroupID !== null) {
    return directGroupID;
  }
  return groupIDFromBounds(x, y);
}

export function inspectNode(
  node: Node,
  onGroupInspect: (groupID: string) => void,
  onNodeInspect: (nodeID: string) => void,
): void {
  const { data } = node;
  if (isWorkflowGraphGroupData(data)) {
    onGroupInspect(data.entityID);
    return;
  }
  if (isWorkflowGraphNodeData(data)) {
    if (!isInspectableWorkflowNodeKind(data.kind)) {
      return;
    }
    onNodeInspect(data.entityID);
  }
}

export function inspectEdge(edge: Edge, onEdgeInspect: (edgeID: string) => void): void {
  const { data } = edge;
  if (isWorkflowGraphEdgeData(data)) {
    onEdgeInspect(data.entityID);
  }
}

export function workflowGraphSelectionExists(
  selection: WorkflowGraphSelection,
  nodes: readonly WorkflowGraphNode[],
  edges: readonly WorkflowGraphEdge[],
): boolean {
  if (selection.kind === "edge") {
    return edges.some((edge) => edge.data?.entityID === selection.edgeID);
  }
  return nodes.some((node) => {
    if (selection.kind === "group") {
      return node.data.entityKind === "group" && node.data.entityID === selection.groupID;
    }
    return node.data.entityKind === "node" && node.data.entityID === selection.nodeID;
  });
}

export function isFormTarget(target: EventTarget | null): boolean {
  return target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement || target instanceof HTMLSelectElement;
}

function isWorkflowGraphNodeData(data: Node["data"]): data is WorkflowGraphNodeData {
  return data.entityKind === "node" && typeof data.entityID === "string";
}

function isWorkflowGraphGroupData(data: Node["data"]): data is WorkflowGraphGroupData {
  return data.entityKind === "group" && typeof data.entityID === "string";
}

function isWorkflowGraphEdgeData(data: Edge["data"]): data is WorkflowGraphEdgeData {
  return data?.entityKind === "edge" && typeof data.entityID === "string";
}

function workflowGraphEdgeID(edge: Edge): string | null {
  return isWorkflowGraphEdgeData(edge.data) ? edge.data.entityID : edge.id;
}

function inferReconnectEndpoint(
  edge: Edge,
  connection: Connection,
): WorkflowGraphReconnectEndpoint | null {
  if (connection.source !== null && connection.source !== edge.source) {
    return "source";
  }
  if (connection.target !== null && connection.target !== edge.target) {
    return "target";
  }
  return null;
}

function groupIDFromElement(element: Element | null): string | null {
  const group = element?.closest("[data-workflow-group-id]");
  return group instanceof HTMLElement ? group.dataset.workflowGroupId ?? null : null;
}

function groupIDFromBounds(x: number, y: number): string | null {
  const candidates = Array.from(document.querySelectorAll<HTMLElement>("[data-workflow-group-id]"))
    .map((group) => {
      const bounds = group.getBoundingClientRect();
      return {
        bounds,
        groupID: group.dataset.workflowGroupId ?? "",
        area: bounds.width * bounds.height,
      };
    })
    .filter(
      (candidate) =>
        candidate.groupID.length > 0 &&
        x >= candidate.bounds.left &&
        x <= candidate.bounds.right &&
        y >= candidate.bounds.top &&
        y <= candidate.bounds.bottom,
    )
    .sort((left, right) => left.area - right.area);
  return candidates[0]?.groupID ?? null;
}
