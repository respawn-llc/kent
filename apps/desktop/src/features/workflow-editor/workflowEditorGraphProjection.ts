import {
  emptyWorkflowDerivedWiring,
  type WorkflowDefinition,
  type WorkflowDerivedWiring,
  type WorkflowValidation,
} from "@/api";
import type { WorkflowEditorDraftState } from "./workflowEditorDraft";
import { workflowGraphLayoutWithDraftProjection, type WorkflowGraphLayout } from "./workflowGraphLayout";
import { emptyWorkflowValidation } from "./workflowEditorLayoutSnapshot";

export function resolveCachedExecutionValidation(
  draftExecution: WorkflowValidation | null | undefined,
  fallbackExecution: WorkflowValidation | undefined,
): WorkflowValidation | null {
  return draftExecution ?? fallbackExecution ?? null;
}

export function resolveCleanScopedValidations({
  cachedDraftValidation,
  cachedExecutionValidation,
  cleanLayoutValidation,
  graphDirty,
}: Readonly<{
  cachedDraftValidation: WorkflowValidation | null;
  cachedExecutionValidation: WorkflowValidation | null;
  cleanLayoutValidation: WorkflowValidation | null;
  graphDirty: boolean;
}>): Readonly<{
  draftValidation: WorkflowValidation | null;
  executionValidation: WorkflowValidation | null;
  cleanValidationForSnapshot: WorkflowValidation | null;
}> {
  if (graphDirty) {
    return {
      cleanValidationForSnapshot: null,
      draftValidation: null,
      executionValidation: emptyWorkflowValidation,
    };
  }
  return {
    cleanValidationForSnapshot: cleanLayoutValidation,
    draftValidation: cachedDraftValidation,
    executionValidation: cachedExecutionValidation,
  };
}

export function resolveGraphValidation(
  graphDirty: boolean,
  cleanLayoutValidation: WorkflowValidation | null,
): WorkflowValidation {
  if (graphDirty) return emptyWorkflowValidation;
  return cleanLayoutValidation ?? emptyWorkflowValidation;
}

export function resolveProjectedGraph({
  draftDefinition,
  graphDirty,
  graphValidation,
  layout,
  snapshotLayout,
}: Readonly<{
  draftDefinition: WorkflowDefinition | undefined;
  graphDirty: boolean;
  graphValidation: WorkflowValidation;
  layout: WorkflowGraphLayout | undefined;
  snapshotLayout: WorkflowGraphLayout | undefined;
}>): WorkflowGraphLayout | undefined {
  const resolvedLayout = layout ?? (graphDirty ? snapshotLayout : undefined);
  if (resolvedLayout === undefined || draftDefinition === undefined) return resolvedLayout;
  return workflowGraphLayoutWithDraftProjection(resolvedLayout, draftDefinition, graphValidation);
}

export function isTopologyDirty(
  graphDirty: boolean,
  draftState: WorkflowEditorDraftState | null,
  snapshotGraphVersion: number,
): boolean {
  return graphDirty && draftState !== null && draftState.graphVersion !== snapshotGraphVersion;
}

export function resolveDraftDerivedWiring({
  draftDefinition,
  derivedWiringQueryData,
  graphDirty,
  validationQueryWiring,
}: Readonly<{
  draftDefinition: WorkflowDefinition | undefined;
  derivedWiringQueryData: WorkflowDerivedWiring | undefined;
  graphDirty: boolean;
  validationQueryWiring: WorkflowDerivedWiring | undefined;
}>): WorkflowDerivedWiring {
  const active = graphDirty ? (derivedWiringQueryData ?? validationQueryWiring) : validationQueryWiring;
  return active ?? draftDefinition?.derivedWiring ?? emptyWorkflowDerivedWiring;
}

export function resolveLayoutValidation({
  cleanLayoutValidation,
  graphDirty,
  snapshotValidation,
  topologyDirty,
}: Readonly<{
  cleanLayoutValidation: WorkflowValidation | null;
  graphDirty: boolean;
  snapshotValidation: WorkflowValidation | null;
  topologyDirty: boolean;
}>): WorkflowValidation | null {
  if (!graphDirty) return cleanLayoutValidation;
  if (topologyDirty) return emptyWorkflowValidation;
  return snapshotValidation ?? emptyWorkflowValidation;
}
