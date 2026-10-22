import type { TFunction } from "i18next";
import type { WorkflowGraphSavePreview, WorkflowGraphValidationResults } from "@/api";
import type { AppServices } from "@/app-facade";
import {
  workflowEditorDirtyState,
  workflowEditorDraftGraph,
  workflowEditorDraftMetadata,
  type WorkflowEditorDraftState,
} from "./workflowEditorDraft";
import { confirmationFromImpact } from "./workflowEditorGraphMutationPlanning";

type WorkflowSaveOutcome =
  | Readonly<{
      kind: "draftInvalid";
      version: number;
      results: WorkflowGraphValidationResults;
      message: string;
    }>
  | Readonly<{ kind: "confirmationRequired"; preview: WorkflowGraphSavePreview }>
  | Readonly<{ kind: "blocked"; blockers: readonly string[] }>
  | Readonly<{
      kind: "savedBlocked";
      preview: WorkflowGraphSavePreview | null;
      blockers: readonly string[];
    }>
  | Readonly<{ kind: "saved"; definition: WorkflowEditorDraftState["source"] }>;

export function workflowSavePresentation(outcome: WorkflowSaveOutcome | undefined): Readonly<{
  blockers: readonly string[];
  validation: { version: number; results: WorkflowGraphValidationResults } | null;
  preview: WorkflowGraphSavePreview | null;
}> {
  switch (outcome?.kind) {
    case "draftInvalid":
      return {
        blockers: [outcome.message],
        validation: { version: outcome.version, results: outcome.results },
        preview: null,
      };
    case "confirmationRequired":
      return { blockers: [], validation: null, preview: outcome.preview };
    case "blocked":
      return { blockers: outcome.blockers, validation: null, preview: null };
    case "savedBlocked":
      return { blockers: outcome.blockers, validation: null, preview: outcome.preview };
    case "saved":
    case undefined:
      return { blockers: [], validation: null, preview: null };
  }
}

export async function runWorkflowSave({
  api,
  confirmedPreview,
  draftState,
  t,
  workflowID,
}: Readonly<{
  api: AppServices["api"];
  confirmedPreview: WorkflowGraphSavePreview | undefined;
  draftState: WorkflowEditorDraftState;
  t: TFunction;
  workflowID: string;
}>): Promise<WorkflowSaveOutcome> {
  const latestDirty = workflowEditorDirtyState(draftState);
  const graph = workflowEditorDraftGraph(draftState);
  const metadata = latestDirty.metadataDirty ? workflowEditorDraftMetadata(draftState) : undefined;
  if (latestDirty.graphDirty) {
    const draftInvalid = await validateDraftBeforeSave({ api, draftState, graph, t, workflowID });
    if (draftInvalid !== null) return draftInvalid;
  }
  const preview =
    confirmedPreview ??
    (await api.previewWorkflowGraphSave({
      expectedVersion: draftState.source.workflow.version,
      graph,
      metadata,
      workflowID,
    }));
  const previewOutcome = previewSaveOutcome(preview, confirmedPreview);
  if (previewOutcome !== null) return previewOutcome;
  const saved = await api.saveWorkflowGraph({
    expectedVersion: draftState.source.workflow.version,
    graph,
    metadata,
    workflowID,
    confirmation:
      confirmedPreview === undefined ? undefined : confirmationFromImpact(confirmedPreview.impact),
  });
  if (!saved.saved || saved.definition === null) {
    return {
      kind: "savedBlocked",
      blockers: saved.blockers.map((blocker) => blocker.message),
      preview: saved.confirmationRequired ? saved : null,
    };
  }
  return { kind: "saved", definition: saved.definition };
}

async function validateDraftBeforeSave({
  api,
  draftState,
  graph,
  t,
  workflowID,
}: Readonly<{
  api: AppServices["api"];
  draftState: WorkflowEditorDraftState;
  graph: ReturnType<typeof workflowEditorDraftGraph>;
  t: TFunction;
  workflowID: string;
}>): Promise<Extract<WorkflowSaveOutcome, { kind: "draftInvalid" }> | null> {
  const validation = await api.validateWorkflowGraphDraft({
    graph,
    metadata: workflowEditorDraftMetadata(draftState),
    modes: ["draft", "execution"],
    workflowID,
  });
  if (validation.draft?.valid === true) return null;
  return {
    kind: "draftInvalid",
    message: t("workflowEditor.draftValidationBlocksSave"),
    results: validation,
    version: draftState.version,
  };
}

function previewSaveOutcome(
  preview: WorkflowGraphSavePreview,
  confirmedPreview: WorkflowGraphSavePreview | undefined,
): Extract<WorkflowSaveOutcome, { kind: "confirmationRequired" | "blocked" }> | null {
  const actionableBlockers = preview.blockers.filter((blocker) => blocker.code !== "confirmation_required");
  if (confirmedPreview === undefined && preview.confirmationRequired && actionableBlockers.length === 0) {
    return { kind: "confirmationRequired", preview };
  }
  if (actionableBlockers.length > 0) {
    return { kind: "blocked", blockers: actionableBlockers.map((blocker) => blocker.message) };
  }
  return null;
}
