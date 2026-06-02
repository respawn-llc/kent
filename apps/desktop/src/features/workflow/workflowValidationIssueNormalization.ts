import type { WorkflowValidationError } from "../../api";

export function normalizeWorkflowValidationErrors(
  errors: readonly WorkflowValidationError[],
): readonly WorkflowValidationError[] {
  const byIdentity = new Map<string, WorkflowValidationError>();
  for (const error of errors) {
    const identity = workflowValidationErrorIdentity(error);
    const existing = byIdentity.get(identity);
    if (existing === undefined || shouldReplaceValidationError(existing, error)) {
      byIdentity.delete(identity);
      byIdentity.set(identity, error);
    }
  }
  return [...byIdentity.values()];
}

function shouldReplaceValidationError(
  existing: WorkflowValidationError,
  candidate: WorkflowValidationError,
): boolean {
  // Derived-wiring diagnostics currently arrive as the scoped, more specific wording for the same identity.
  return candidate.message.length >= existing.message.length;
}

function workflowValidationErrorIdentity(error: WorkflowValidationError): string {
  return [
    error.code,
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
