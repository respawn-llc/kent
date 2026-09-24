import { useAtomMount, useAtomSet, useAtomRefresh } from "@effect/atom-react";
import { MutationObserver, QueryObserver, type QueryClient } from "@tanstack/react-query";
import type { TFunction } from "i18next";
import type { WorkflowGraphSavePreview } from "@/api";
import * as Effect from "effect/Effect";
import * as Atom from "effect/unstable/reactivity/Atom";
import { queryAtom, queryKeys, type AppServices, type StatusController } from "@/app-facade";
import {
  initializeWorkflowEditorDraft,
  workflowEditorDirtyState,
  workflowEditorDraftReducer,
  type WorkflowEditorDraftAction,
  type WorkflowEditorDraftState,
} from "./workflowEditorDraft";
import { createWorkflowEditorGraphModel } from "./WorkflowEditorGraphModel";
import { runWorkflowSave, workflowSavePresentation } from "./workflowEditorSave";
import { workflowSaveConfirmationPreviewKey, workflowEditorReadOptions } from "./workflowEditorQueries";
import { workflowEditorObservations } from "./workflowEditorObservation";
import { graphEditWarningTranslationKey } from "./workflowEditorGraphMutationPlanning";

export function createWorkflowEditorViewModel({
  services,
  client,
  workflowID,
  projectID,
  t,
  push,
}: Readonly<{
  services: AppServices;
  client: QueryClient;
  workflowID: string;
  projectID: string | null;
  t: TFunction;
  push: StatusController["push"];
}>) {
  const linksObserver = new QueryObserver(client, {
    queryKey: queryKeys.projectWorkflowLinks(projectID),
    queryFn: async () => {
      if (projectID === null) throw new Error("Project links requested without a Project.");
      return services.api.listProjectWorkflowLinks(projectID);
    },
    enabled: projectID !== null,
    ...workflowEditorReadOptions,
  });
  const links = queryAtom(linksObserver);
  const workflowOptions = (enabled: boolean) => ({
    queryKey: queryKeys.workflowDefinition(workflowID),
    queryFn: async () => services.api.getWorkflow(workflowID),
    enabled,
    ...workflowEditorReadOptions,
  });
  const validationOptions = (enabled: boolean) => ({
    queryKey: queryKeys.workflowValidation(workflowID, "execution"),
    queryFn: async () => services.api.validateWorkflow(workflowID, "execution"),
    enabled,
    ...workflowEditorReadOptions,
  });
  const workflowObserver = new QueryObserver(client, workflowOptions(projectID === null));
  const validationObserver = new QueryObserver(client, validationOptions(projectID === null));
  const workflow = queryAtom(workflowObserver);
  const validation = queryAtom(validationObserver);
  const data = Atom.make((get) => {
    const linksQuery = get(links);
    const activeLink = linksQuery.data?.find(
      (link) => link.projectID === projectID && link.workflowID === workflowID,
    );
    const linked = projectID === null || activeLink !== undefined;
    workflowObserver.setOptions(workflowOptions(linked));
    validationObserver.setOptions(validationOptions(linked));
    return {
      linksQuery,
      activeLink,
      linked,
      projectContext: projectID !== null,
      workflowQuery: get(workflow),
      validationQuery: get(validation),
    };
  });
  const draft = Atom.make<WorkflowEditorDraftState | null>(null);
  const lifecycle = Atom.make((get) => {
    const source = get(data).workflowQuery.data;
    const current = get(draft);
    if (source === undefined) return;
    if (current === null) {
      get.set(draft, initializeWorkflowEditorDraft(source));
    } else if (source.workflow.version !== current.source.workflow.version) {
      if (!workflowEditorDirtyState(current).dirty) {
        get.set(draft, workflowEditorDraftReducer(current, { type: "reset", source }));
      } else if (
        current.conflict?.workflow.version !== source.workflow.version &&
        current.acknowledgedConflictVersion !== source.workflow.version
      ) {
        get.set(draft, workflowEditorDraftReducer(current, { type: "conflict", source }));
      }
    }
  });
  const state = Atom.make((get) => {
    get(lifecycle);
    const draftState = get(draft);
    return {
      draftState,
      dirtyState:
        draftState === null
          ? { dirty: false, graphDirty: false, metadataDirty: false }
          : workflowEditorDirtyState(draftState),
    };
  });
  const edit = Atom.fn<WorkflowEditorDraftAction>()(
    (action, get) =>
      Effect.sync(() => {
        const current = get(draft);
        if (current === null) return;
        const next = workflowEditorDraftReducer(current, action);
        get.set(draft, next);
        const warning = next.lastTopologyMutation?.warnings[0];
        if (warning !== undefined) {
          push({
            body: t(graphEditWarningTranslationKey(warning)),
            id: `workflow-graph-edit-warning-${next.version.toString()}`,
            title: t("workflowEditor.graphEditBlockedTitle"),
            tone: "warning",
          });
        }
      }),
    { concurrent: true },
  );
  const discard = Atom.fn<undefined>()(
    (_, get) =>
      Effect.sync(() => {
        const source = workflowObserver.getCurrentResult().data;
        if (source !== undefined) get.set(draft, initializeWorkflowEditorDraft(source));
      }),
    { concurrent: true },
  );
  const retryLoad = Atom.fn(
    () =>
      Effect.promise(async () => {
        await Promise.all([workflowObserver.refetch(), validationObserver.refetch()]);
      }),
    { concurrent: true },
  );
  const retryLinks = Atom.fn(() => Effect.promise(async () => linksObserver.refetch()), { concurrent: true });
  const { graph, retryLayout } = createWorkflowEditorGraphModel({
    api: services.api,
    client,
    workflowID,
    state,
    saved: Atom.make((get) => ({
      definition: get(data).workflowQuery.data,
      validation: get(data).validationQuery.data,
    })),
  });
  const saveObserver = new MutationObserver(client, {
    mutationFn: async (input: {
      draftState: WorkflowEditorDraftState;
      confirmedPreview: WorkflowGraphSavePreview | undefined;
    }) => runWorkflowSave({ ...input, api: services.api, t, workflowID }),
    retry: false,
    networkMode: "always",
    onSuccess: async (outcome) => {
      if (outcome.kind !== "saved") return;
      await Promise.all([
        client.invalidateQueries({ queryKey: queryKeys.workflowDefinition(workflowID) }),
        client.invalidateQueries({ queryKey: queryKeys.workflowValidation(workflowID, "execution") }),
        client.invalidateQueries({ queryKey: queryKeys.allWorkflows }),
        ...(projectID === null
          ? []
          : [
              client.invalidateQueries({ queryKey: queryKeys.boardWorkflowRoot(projectID, workflowID) }),
              client.invalidateQueries({
                queryKey: queryKeys.boardNodeCardsWorkflowRoot(projectID, workflowID),
              }),
            ]),
      ]);
    },
  });
  const saving = queryAtom(saveObserver);
  const dismissedConfirmation = Atom.make<string | null>(null);
  const saveState = Atom.make((get) => {
    const result = get(saving);
    const outcome = workflowSavePresentation(result.data);
    const current = get(state).draftState;
    const input = result.variables?.draftState;
    const key = input === undefined ? null : workflowSaveConfirmationPreviewKey(input);
    return {
      saving: result.isPending,
      error: result.error,
      blockers: outcome.blockers,
      validation:
        outcome.validation !== null && current?.version === outcome.validation.version
          ? outcome.validation.results
          : null,
      confirmationPreview:
        current !== null &&
        key === workflowSaveConfirmationPreviewKey(current) &&
        key !== get(dismissedConfirmation)
          ? outcome.preview
          : null,
    };
  });
  const dismissConfirmation = Atom.fn<undefined>()(
    (_, get) =>
      Effect.sync(() => {
        const current = get(state).draftState;
        get.set(dismissedConfirmation, current === null ? null : workflowSaveConfirmationPreviewKey(current));
      }),
    { concurrent: true },
  );
  const save = Atom.fn<WorkflowGraphSavePreview | undefined>()(
    (confirmedPreview, get) =>
      Effect.gen(function* () {
        const current = get(state);
        const submitted = current.draftState;
        if (submitted === null || !current.dirtyState.dirty || saveObserver.getCurrentResult().isPending)
          return;
        if (confirmedPreview !== undefined && confirmedPreview !== get(saveState).confirmationPreview) return;
        get.set(dismissedConfirmation, null);
        const outcome = yield* Effect.tryPromise(async () =>
          saveObserver.mutate({
            draftState: submitted,
            confirmedPreview,
          }),
        );
        if (outcome.kind === "saved") {
          get.set(draft, initializeWorkflowEditorDraft(outcome.definition));
        }
      }).pipe(Effect.ignore),
    { concurrent: true },
  );
  const observations = workflowEditorObservations({
    api: services.api,
    client,
    workflowID,
    projectID,
    t,
    push,
  });
  return {
    data,
    state,
    lifecycle,
    graph,
    edit,
    discard,
    retryLoad,
    retryLayout,
    retryLinks,
    saving,
    saveState,
    save,
    dismissConfirmation,
    workflowObservation: observations.workflow,
    projectObservation: observations.project,
  } as const;
}

export type WorkflowEditorViewModel = ReturnType<typeof createWorkflowEditorViewModel>;

export function useWorkflowEditorActions(model: WorkflowEditorViewModel) {
  useAtomMount(model.lifecycle);
  useAtomMount(model.saving);
  useAtomMount(model.workflowObservation);
  useAtomMount(model.projectObservation);
  return {
    edit: useAtomSet(model.edit),
    discard: useAtomSet(model.discard),
    retryLoad: useAtomSet(model.retryLoad),
    retryLinks: useAtomSet(model.retryLinks),
    save: useAtomSet(model.save),
    dismissConfirmation: useAtomSet(model.dismissConfirmation),
    retryWorkflowObservation: useAtomRefresh(model.workflowObservation),
    retryProjectObservation: useAtomRefresh(model.projectObservation),
  };
}

export function useWorkflowGraphEditorActions(model: WorkflowEditorViewModel) {
  useAtomMount(model.graph);
  return {
    ...useWorkflowEditorActions(model),
    retryLayout: useAtomSet(model.retryLayout),
  };
}
