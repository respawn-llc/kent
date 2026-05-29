import type { WorkflowEditorCascadeSummary } from "./workflowEditorGraphMutations";

export type WorkflowDeleteConfirmationCounts = Readonly<{
  nodeCount: number;
  edgeCount: number;
  transitionGroupCount: number;
}>;

export type WorkflowDeleteConfirmationWindowTarget = Readonly<{
  counts: WorkflowDeleteConfirmationCounts;
  requestID: string;
}>;

export function workflowDeleteConfirmationCountsFromSummary(
  summary: WorkflowEditorCascadeSummary,
): WorkflowDeleteConfirmationCounts {
  return {
    edgeCount: summary.removedEdgeIDs.length,
    nodeCount: summary.removedNodeIDs.length,
    transitionGroupCount: summary.removedTransitionGroupIDs.length,
  };
}

export function workflowDeleteConfirmationWindowOptions({
  counts,
  requestID,
  title,
}: WorkflowDeleteConfirmationWindowTarget & Readonly<{ title: string }>) {
  return {
    initialHeight: 260,
    initialWidth: 420,
    label: `workflow-delete-${requestID}`,
    params: {
      edgeCount: counts.edgeCount.toString(),
      nodeCount: counts.nodeCount.toString(),
      requestID,
      transitionGroupCount: counts.transitionGroupCount.toString(),
    },
    route: "/native-dialog/workflow-delete-confirm",
    title,
  };
}

export function workflowDeleteConfirmationWindowTargetFromSearch(search: Readonly<{
  edgeCount: string;
  nodeCount: string;
  requestID: string;
  transitionGroupCount: string;
}>): WorkflowDeleteConfirmationWindowTarget {
  return {
    counts: {
      edgeCount: parseSearchCount(search.edgeCount),
      nodeCount: parseSearchCount(search.nodeCount),
      transitionGroupCount: parseSearchCount(search.transitionGroupCount),
    },
    requestID: search.requestID,
  };
}

function parseSearchCount(value: string): number {
  const count = Number(value);
  if (!Number.isSafeInteger(count) || count < 0) {
    return 0;
  }
  return count;
}
