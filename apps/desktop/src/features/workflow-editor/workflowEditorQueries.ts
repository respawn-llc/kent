import { useQuery } from "@tanstack/react-query";

import type { WorkflowDefinition, WorkflowValidation } from "@/api";
import type { AppServices } from "@/app-facade";
import { queryKeys } from "@/app-facade";
import { useAppServices } from "@/app-facade";
import {
  workflowEditorDraftGraph,
  workflowEditorDraftMetadata,
  type WorkflowEditorDraftState,
} from "./workflowEditorDraft";
import { layoutWorkflowGraph, type WorkflowGraphLayout } from "./workflowGraphLayout";

export const workflowEditorReadOptions = {
  retry: false,
  networkMode: "always",
  refetchOnReconnect: false,
  refetchOnWindowFocus: false,
} as const;

export function workflowSaveConfirmationPreviewKey(state: WorkflowEditorDraftState): string {
  const observedRemoteVersion = state.conflict?.workflow.version ?? state.acknowledgedConflictVersion;
  return JSON.stringify([
    state.source.workflow.id,
    state.source.workflow.version.toString(),
    observedRemoteVersion,
    state.version.toString(),
  ]);
}

export function workflowDraftValidationOptions(
  api: AppServices["api"],
  workflowID: string,
  draftState: WorkflowEditorDraftState | null,
  graphDirty: boolean,
) {
  // Metadata-only edits (display name/description) do not bump graphVersion, so
  // the server's draft validation of the workflow name would keep reusing the
  // stale staleTime:Infinity result. Vary the key by the draft metadata too.
  const metadataSignature =
    draftState === null ? null : JSON.stringify(workflowEditorDraftMetadata(draftState));
  return {
    queryKey: queryKeys.workflowDraftValidation(
      workflowID,
      draftState?.source.workflow.version ?? null,
      draftState?.graphVersion ?? null,
      metadataSignature,
    ),
    queryFn: async () => {
      if (draftState === null) {
        throw new Error("Workflow draft validation requested before draft is initialized.");
      }
      return api.validateWorkflowGraphDraft({
        graph: workflowEditorDraftGraph(draftState),
        metadata: workflowEditorDraftMetadata(draftState),
        modes: ["draft", "execution"],
        workflowID,
      });
    },
    enabled: draftState !== null && !graphDirty,
    staleTime: Infinity,
  };
}

export function workflowDraftDerivedWiringOptions(
  api: AppServices["api"],
  workflowID: string,
  draftState: WorkflowEditorDraftState | null,
  graphDirty: boolean,
) {
  // Keyed on the draft graph content, not graphVersion: wiring-relevant edits
  // (edge parameters and derived wiring) intentionally leave graphVersion
  // unchanged, so a version key would serve stale wiring for exactly those
  // edits. Only stringify while the query is enabled (graph dirty) to avoid the
  // cost on every clean-state render. Derive-wiring is cheap (no validation),
  // so refetching on graph content changes is acceptable.
  const graphSignature =
    draftState !== null && graphDirty ? JSON.stringify(workflowEditorDraftGraph(draftState)) : null;
  return {
    queryKey: queryKeys.workflowDraftDerivedWiring(
      workflowID,
      draftState?.source.workflow.version ?? null,
      graphSignature,
    ),
    queryFn: async () => {
      if (draftState === null) {
        throw new Error("Workflow draft derived wiring requested before draft is initialized.");
      }
      return api.deriveWorkflowGraphWiring({
        graph: workflowEditorDraftGraph(draftState),
        workflowID,
      });
    },
    enabled: draftState !== null && graphDirty,
    staleTime: Infinity,
  };
}

export function useWorkflowScriptPathValidationQuery(workflowID: string, nodeID: string, scriptPath: string) {
  const { api } = useAppServices();
  return useQuery({
    queryKey: queryKeys.workflowScriptPathValidation(workflowID, nodeID, scriptPath),
    queryFn: async () =>
      api.validateWorkflowScriptPath({
        nodeID,
        scriptPath,
        workflowID,
      }),
    enabled: workflowID.length > 0 && nodeID.length > 0,
    refetchInterval: scriptPathValidationRefreshMs,
    staleTime: scriptPathValidationStaleMs,
  });
}

const scriptPathValidationStaleMs = 5_000;
const scriptPathValidationRefreshMs = 5_000;

export function workflowGraphLayoutOptions(
  workflowID: string,
  definition: WorkflowDefinition | undefined,
  draftVersion: number | null,
  validation: WorkflowValidation | null,
) {
  return {
    queryKey: queryKeys.workflowGraphLayout(
      workflowID,
      definition === undefined || draftVersion === null
        ? null
        : definition.workflow.version * 100_000 + draftVersion,
      validation?.valid ?? null,
      validation?.errors ?? null,
    ),
    queryFn: async () => {
      if (definition === undefined || validation === null || draftVersion === null) {
        throw new Error("Workflow graph layout requested before workflow definition and validation loaded.");
      }
      return layoutWorkflowGraph(definition, validation);
    },
    enabled: definition !== undefined && validation !== null && draftVersion !== null,
    placeholderData: (previous: WorkflowGraphLayout | undefined) => previous,
  };
}
