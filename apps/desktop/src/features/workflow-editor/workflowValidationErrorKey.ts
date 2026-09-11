import type { WorkflowValidationError } from "@/api";

export function workflowValidationErrorKey(error: WorkflowValidationError, index: number): string {
  const reasonKey = error.details.reason === null ? "" : error.details.reason.toString();
  const entityID = [error.edgeID, error.nodeID, error.transitionGroupID, error.workflowID].find(
    (value): value is string => value !== null && value.length > 0,
  );
  if (entityID !== undefined) {
    return `${error.code}:${entityID}:${reasonKey}:${error.message}`;
  }
  return `${error.code}:${reasonKey}:${error.message}:${index.toString()}`;
}
