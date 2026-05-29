import type { WorkflowGraphNode } from "./workflowGraphLayout";

export type NodeLayoutOffset = Readonly<{ x: number; y: number }>;

export type WorkflowGraphNodeRect = Readonly<{
  groupID: string;
  height: number;
  kind: string;
  width: number;
  x: number;
  y: number;
}>;

export const workflowNodeWidth = 220;
export const workflowNodeHeight = 92;
export const workflowJoinNodeSize = 56;
export const workflowJoinGroupGap = 80;
export const emptyGroupWidth = 260;
export const emptyGroupHeight = 140;

export function graphNodeWidth(node: WorkflowGraphNode): number {
  return positiveFiniteNumber(node.style?.width, workflowNodeWidth);
}

export function graphNodeHeight(node: WorkflowGraphNode): number {
  return positiveFiniteNumber(node.style?.height, workflowNodeHeight);
}

function positiveFiniteNumber(value: unknown, fallback: number): number {
  if (typeof value !== "number" && typeof value !== "string") {
    return fallback;
  }
  const parsed = typeof value === "number" ? value : Number.parseFloat(value);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}
