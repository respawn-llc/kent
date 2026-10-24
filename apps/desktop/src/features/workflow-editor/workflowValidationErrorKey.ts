import type { WorkflowValidationError } from "@/api";

export function workflowValidationErrorKey(error: WorkflowValidationError, index: number): string {
  const entityID = [error.edgeID, error.nodeID, error.transitionGroupID, error.workflowID].find(
    (value): value is string => value !== null,
  );
  if (entityID !== undefined) {
    return `${error.code}:${entityID}:${error.message}`;
  }
  return `${error.code}:${error.message}:${index.toString()}`;
}
