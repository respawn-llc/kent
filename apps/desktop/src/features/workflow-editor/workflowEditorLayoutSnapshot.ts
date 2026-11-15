import { type WorkflowValidation } from "@/api";

export type WorkflowLayoutSnapshot = Readonly<{
  graphVersion: number;
  validation: WorkflowValidation | null;
}>;

export const emptyWorkflowValidation: WorkflowValidation = { errors: [], valid: true };

export function mergeWorkflowValidations(
  draft: WorkflowValidation | null,
  execution: WorkflowValidation | null,
): WorkflowValidation | null {
  if (draft === null) {
    return execution;
  }
  if (execution === null) {
    return draft;
  }
  return {
    valid: draft.valid && execution.valid,
    errors: [...draft.errors, ...execution.errors],
  };
}

export function workflowLayoutSnapshotAfterRender(
  current: WorkflowLayoutSnapshot,
  update: Readonly<{
    cleanGraphVersion: number;
    cleanValidation: WorkflowValidation | null;
  }>,
): WorkflowLayoutSnapshot {
  const graphVersion = update.cleanValidation === null ? current.graphVersion : update.cleanGraphVersion;
  const validation = update.cleanValidation ?? current.validation;
  return graphVersion === current.graphVersion && validation === current.validation
    ? current
    : { graphVersion, validation };
}
