import { QueryObserver, type QueryClient } from "@tanstack/react-query";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";
import type { WorkflowDefinition, WorkflowValidation } from "@/api";
import { queryAtom, type AppServices } from "@/app-facade";
import {
  workflowDefinitionFromDraft,
  type WorkflowEditorDraftState,
  type WorkflowEditorDirtyState,
} from "./workflowEditorDraft";
import {
  workflowDraftDerivedWiringOptions,
  workflowDraftValidationOptions,
  workflowGraphLayoutOptions,
  workflowEditorReadOptions,
} from "./workflowEditorQueries";
import {
  mergeWorkflowValidations,
  workflowLayoutSnapshotAfterRender,
  type WorkflowLayoutSnapshot,
} from "./workflowEditorLayoutSnapshot";
import {
  isTopologyDirty,
  resolveCachedExecutionValidation,
  resolveCleanScopedValidations,
  resolveDraftDerivedWiring,
  resolveGraphValidation,
  resolveLayoutValidation,
  resolveProjectedGraph,
} from "./workflowEditorGraphProjection";
import type { WorkflowGraphLayout } from "./workflowGraphLayout";

export function createWorkflowEditorGraphModel({
  api,
  client,
  workflowID,
  state,
  saved,
}: Readonly<{
  api: AppServices["api"];
  client: QueryClient;
  workflowID: string;
  state: Atom.Atom<{
    draftState: WorkflowEditorDraftState | null;
    dirtyState: WorkflowEditorDirtyState;
  }>;
  saved: Atom.Atom<{
    definition: WorkflowDefinition | undefined;
    validation: WorkflowValidation | undefined;
  }>;
}>) {
  const validationObserver = new QueryObserver(client, {
    ...workflowDraftValidationOptions(api, workflowID, null, false),
    ...workflowEditorReadOptions,
  });
  const validation = queryAtom(validationObserver);
  const wiringObserver = new QueryObserver(client, {
    ...workflowDraftDerivedWiringOptions(api, workflowID, null, false),
    ...workflowEditorReadOptions,
  });
  const wiring = queryAtom(wiringObserver);
  const dependencies = Atom.make((get) => {
    const { draftState, dirtyState } = get(state);
    validationObserver.setOptions({
      ...workflowDraftValidationOptions(api, workflowID, draftState, dirtyState.graphDirty),
      ...workflowEditorReadOptions,
    });
    wiringObserver.setOptions({
      ...workflowDraftDerivedWiringOptions(api, workflowID, draftState, dirtyState.graphDirty),
      ...workflowEditorReadOptions,
    });
    return { validation: get(validation), wiring: get(wiring) };
  });
  const definition = Atom.make((get) => {
    const { draftState } = get(state);
    return draftState === null ? get(saved).definition : workflowDefinitionFromDraft(draftState.draft);
  });
  const cleanValidation = Atom.make((get) => {
    const draft = get(dependencies).validation.data?.draft ?? null;
    const execution = resolveCachedExecutionValidation(
      get(dependencies).validation.data?.execution,
      get(saved).validation,
    );
    return { draft, execution, merged: mergeWorkflowValidations(draft, execution) };
  });
  const snapshot = Atom.make<WorkflowLayoutSnapshot>({
    graphVersion: 0,
    layout: undefined,
    validation: null,
  });
  const layoutObserver = new QueryObserver<WorkflowGraphLayout>(client, {
    ...workflowGraphLayoutOptions(workflowID, undefined, null, null),
    ...workflowEditorReadOptions,
  });
  const layout = queryAtom(layoutObserver);
  const graph = Atom.make((get) => {
    const { draftState, dirtyState } = get(state);
    get.mount(snapshot);
    const current = get.once(snapshot);
    const clean = get(cleanValidation);
    const draftDefinition = get(definition);
    const reads = get(dependencies);
    const cleanScoped = resolveCleanScopedValidations({
      cachedDraftValidation: clean.draft,
      cachedExecutionValidation: clean.execution,
      cleanLayoutValidation: clean.merged,
      graphDirty: dirtyState.graphDirty,
    });
    const layoutValidation = resolveLayoutValidation({
      cleanLayoutValidation: clean.merged,
      graphDirty: dirtyState.graphDirty,
      snapshotValidation: current.validation,
      topologyDirty: isTopologyDirty(dirtyState.graphDirty, draftState, current.graphVersion),
    });
    layoutObserver.setOptions({
      ...workflowGraphLayoutOptions(
        workflowID,
        draftDefinition,
        draftState?.graphVersion ?? null,
        layoutValidation,
      ),
      ...workflowEditorReadOptions,
    });
    const layoutQuery = get(layout);
    const nextSnapshot = workflowLayoutSnapshotAfterRender(current, {
      cleanGraphVersion: draftState?.graphVersion ?? current.graphVersion,
      cleanValidation: cleanScoped.cleanValidationForSnapshot,
      layout: layoutQuery.data,
    });
    if (nextSnapshot !== current) get.set(snapshot, nextSnapshot);
    return {
      draftDefinition,
      draftValidation: cleanScoped.draftValidation,
      executionValidation: cleanScoped.executionValidation,
      draftDerivedWiring: resolveDraftDerivedWiring({
        draftDefinition,
        derivedWiringQueryData: reads.wiring.data,
        graphDirty: dirtyState.graphDirty,
        validationQueryWiring: reads.validation.data?.derivedWiring,
      }),
      layoutQuery,
      projectedGraph: resolveProjectedGraph({
        draftDefinition,
        graphDirty: dirtyState.graphDirty,
        graphValidation: resolveGraphValidation(dirtyState.graphDirty, clean.merged),
        layout: layoutQuery.data,
        snapshotLayout: current.layout,
      }),
    };
  });
  const retryLayout = Atom.fn(() => Effect.promise(async () => layoutObserver.refetch()), {
    concurrent: true,
  });
  return { graph, retryLayout } as const;
}
