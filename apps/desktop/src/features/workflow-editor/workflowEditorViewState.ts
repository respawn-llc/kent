import type { QueryObserverResult } from "@tanstack/react-query";
import type { QuerySnapshot } from "@/app-facade";
import type * as Atom from "effect/unstable/reactivity/Atom";

import type { WorkflowValidation } from "@/api";
import type { WorkflowGraphLayout } from "./workflowGraphLayout";
import type { WorkflowEditorViewModel } from "./WorkflowEditorViewModel";

type WorkflowEditorData = Atom.Type<WorkflowEditorViewModel["data"]>;
type LayoutResult = QuerySnapshot<QueryObserverResult<WorkflowGraphLayout>>;

export type WorkflowEditorViewState =
  | Readonly<{ kind: "loading" }>
  | Readonly<{ error: Error; kind: "linkError" }>
  | Readonly<{ kind: "unlinked" }>
  | Readonly<{ error: Error; kind: "loadError" }>
  | Readonly<{ graph: WorkflowGraphLayout; kind: "ready"; validation: WorkflowValidation }>;

export function workflowEditorViewState(
  data: WorkflowEditorData,
  layoutQuery: LayoutResult,
  projectedGraph: WorkflowGraphLayout | undefined,
): WorkflowEditorViewState {
  if (isLinkGateLoading(data)) {
    return { kind: "loading" };
  }
  if (data.linksQuery.isError) {
    return { error: data.linksQuery.error, kind: "linkError" };
  }
  if (!data.linked) {
    return { kind: "unlinked" };
  }
  if (isGraphLoading(data, layoutQuery)) {
    return { kind: "loading" };
  }
  const loadError = workflowEditorLoadError(data, layoutQuery);
  if (loadError !== null) {
    return { error: loadError, kind: "loadError" };
  }
  if (projectedGraph === undefined || data.validationQuery.data === undefined) {
    return { kind: "loading" };
  }
  return { graph: projectedGraph, kind: "ready", validation: data.validationQuery.data };
}

function isLinkGateLoading(data: WorkflowEditorData): boolean {
  return data.projectContext && data.linksQuery.isPending;
}

function isGraphLoading(data: WorkflowEditorData, layoutQuery: LayoutResult): boolean {
  return data.workflowQuery.isPending || data.validationQuery.isPending || layoutQuery.isPending;
}

function workflowEditorLoadError(data: WorkflowEditorData, layoutQuery: LayoutResult): Error | null {
  return data.workflowQuery.error ?? data.validationQuery.error ?? layoutQuery.error ?? null;
}
