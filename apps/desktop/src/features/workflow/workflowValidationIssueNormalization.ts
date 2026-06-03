import type { WorkflowValidationError } from "../../api";

export function normalizeWorkflowValidationErrors(
  errors: readonly WorkflowValidationError[],
): readonly WorkflowValidationError[] {
  const byIdentity = new Map<string, WorkflowValidationError>();
  for (const error of errors) {
    byIdentity.set(workflowValidationErrorIdentity(error), error);
  }
  return [...byIdentity.values()];
}

function workflowValidationErrorIdentity(error: WorkflowValidationError): string {
  return [
    error.code,
    error.message,
    error.workflowID,
    error.nodeID,
    error.transitionGroupID,
    error.edgeID,
    error.details.fieldName,
    error.details.inputName,
    error.details.placeholder,
    error.details.providerEdgeID,
    [...error.relatedIDs].sort().join(","),
  ].join("\u0000");
}
