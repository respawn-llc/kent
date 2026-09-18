import {
  MutationObserver,
  matchMutation,
  useQueryClient,
  type QueryClient,
  type MutationKey,
  type MutationOptions,
} from "@tanstack/react-query";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { useContext, useMemo } from "react";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";

import type { ProjectLabel, ProjectLabelCatalog } from "@/api";
import { queryAction, queryKeys, useAppServices, useQueryAction, type AppServices } from "@/app-facade";
import { LabelActionScopeContext, useProjectLabelData } from "./projectLabelContext";
import type { ProjectLabelEffects } from "./labelEventEffects";
import type { ProjectLabelFilterController } from "./projectLabelFilter";
import { pruneDeletedLabelFromExistingCaches } from "./taskLabelCache";

type ProjectLabelReorderContext = Readonly<{
  previous: ProjectLabelCatalog;
  projected: ProjectLabelCatalog;
}>;

type Completion<A> = Readonly<{
  onSuccess?: (value: A) => void;
}>;
type CreateInput = Completion<ProjectLabel> &
  Readonly<{
    name: string;
    onStart?: () => void;
    onSettled?: () => void;
    onError?: (error: unknown) => void;
  }>;
type CatalogActionContext = Readonly<{
  api: AppServices["api"];
  effects: ProjectLabelEffects;
  projectID: string;
  queryClient: QueryClient;
}>;

export function useProjectLabelCatalog() {
  const model = useProjectLabelData();
  const catalog = useAtomValue(model.catalog);
  const refetch = useAtomSet(model.refresh, { mode: "value" });
  return { ...catalog, refetch };
}

export function useProjectLabelCatalogMutations() {
  const { api } = useAppServices();
  const { effects, projectID } = useProjectLabelData();
  const queryClient = useQueryClient();
  const model = useMemo(
    () => createCatalogActions({ api, effects, projectID, queryClient }),
    [api, effects, projectID, queryClient],
  );
  return { create: useQueryAction(model.create), reorder: useQueryAction(model.reorder) };
}

export function useProjectLabelActions(labelID: string) {
  const scope = useContext(LabelActionScopeContext);
  if (scope === null) throw new Error("Label actions require their chooser scope.");
  const { api } = useAppServices();
  const { effects, projectID } = useProjectLabelData();
  const queryClient = useQueryClient();
  const model = useMemo(
    () => createLabelActions({ api, effects, projectID, queryClient }, { labelID, scope }),
    [api, effects, projectID, queryClient, labelID, scope],
  );
  return { rename: useLabelAction(model.rename), delete: useLabelAction(model.delete) };
}

function createCatalogActions({ api, effects, projectID, queryClient }: CatalogActionContext) {
  const queryKey = queryKeys.projectLabels(projectID);
  return {
    create: queryAction(
      new MutationObserver(queryClient, {
        mutationFn: async (input: CreateInput) => api.createProjectLabel(projectID, input.name),
        onMutate(input) {
          input.onStart?.();
        },
        onSettled(_data, _error, input) {
          input.onSettled?.();
        },
        onError(error, input) {
          input.onError?.(error);
        },
        async onSuccess(label, input) {
          await cancelCatalog(queryClient, queryKey);
          patchCreatedLabel(queryClient, queryKey, label);
          effects.scheduleCatalogRefresh();
          input.onSuccess?.(label);
        },
      }),
    ),
    reorder: queryAction(
      new MutationObserver<
        ProjectLabelCatalog,
        unknown,
        Readonly<{ labelIDs: readonly string[]; onError?: (error: unknown) => void }>,
        ProjectLabelReorderContext
      >(queryClient, {
        mutationFn: async ({ labelIDs }) => api.reorderProjectLabels(projectID, labelIDs),
        async onMutate({ labelIDs }) {
          await cancelCatalog(queryClient, queryKey);
          const previous = queryClient.getQueryData<ProjectLabelCatalog>(queryKey);
          if (previous === undefined) {
            throw new Error(
              `Cannot reorder Project labels before the catalog for Project ${projectID} is loaded.`,
            );
          }
          const projected = projectCatalogPermutation(previous, labelIDs);
          queryClient.setQueryData(queryKey, projected);
          return { previous, projected };
        },
        onError(error, input, context) {
          if (
            context !== undefined &&
            catalogsStructurallyEqual(
              queryClient.getQueryData<ProjectLabelCatalog>(queryKey),
              context.projected,
            )
          ) {
            queryClient.setQueryData(queryKey, context.previous);
          }
          input.onError?.(error);
        },
        async onSuccess(catalog) {
          await cancelCatalog(queryClient, queryKey);
          if (catalog.projectID !== projectID) {
            throw new Error(
              `Project label reorder returned ${catalog.projectID} while serving ${projectID}.`,
            );
          }
          queryClient.setQueryData(queryKey, catalog);
          effects.scheduleReorderRefresh();
        },
      }),
    ),
  } as const;
}

function createLabelActions(
  { api, effects, projectID, queryClient }: CatalogActionContext,
  { labelID, scope }: Readonly<{ labelID: string; scope: string }>,
) {
  const queryKey = queryKeys.projectLabels(projectID);
  return {
    rename: labelAction(queryClient, ["label-action", projectID, scope, labelID, "rename"], {
      mutationFn: async (input: Completion<ProjectLabel> & Readonly<{ name: string }>) =>
        api.renameProjectLabel(projectID, labelID, input.name),
      async onSuccess(label, input) {
        await cancelCatalog(queryClient, queryKey);
        patchRenamedLabel(queryClient, queryKey, label);
        effects.scheduleCatalogRefresh();
        input.onSuccess?.(label);
      },
    }),
    delete: labelAction<string, Completion<string>>(
      queryClient,
      ["label-action", projectID, scope, labelID, "delete"],
      {
        mutationFn: async () => api.deleteProjectLabel(projectID, labelID),
        async onSuccess(deleted, input) {
          await cancelCatalog(queryClient, queryKey);
          pruneDeletedLabelFromExistingCaches(queryClient, projectID, deleted);
          effects.scheduleDeleteRefresh();
          input.onSuccess?.(deleted);
        },
      },
    ),
  } as const;
}

function labelAction<A, V>(
  client: QueryClient,
  mutationKey: MutationKey,
  options: MutationOptions<A, unknown, V>,
) {
  const cache = client.getMutationCache();
  const filters = { mutationKey, exact: true };
  const current = () => cache.findAll(filters).at(-1)?.state ?? null;
  const request = Atom.make((get) => {
    get.addFinalizer(
      cache.subscribe((event) => {
        if (event.mutation !== undefined && matchMutation(filters, event.mutation)) get.setSelf(current());
      }),
    );
    return current();
  });
  const submit = Atom.fn<V>()(
    (input) =>
      Effect.gen(function* () {
        if (client.isMutating(filters) > 0) return;
        yield* Effect.tryPromise(async () =>
          cache.build(client, { ...options, mutationKey }).execute(input),
        ).pipe(Effect.ignore);
      }),
    { concurrent: true },
  );
  const reset = Atom.fn(() =>
    Effect.sync(() => {
      if (client.isMutating(filters) > 0) return;
      for (const mutation of cache.findAll(filters)) cache.remove(mutation);
    }),
  );
  return { request, submit, reset } as const;
}

function useLabelAction<A, V>(model: ReturnType<typeof labelAction<A, V>>) {
  const request = useAtomValue(model.request);
  return {
    isPending: request?.status === "pending",
    isError: request?.status === "error",
    error: request === null ? null : request.error,
    submit: useAtomSet(model.submit, { mode: "value" }),
    reset: useAtomSet(model.reset, { mode: "value" }),
  };
}

export function useProjectLabelFilter(): ProjectLabelFilterController {
  const { filter } = useProjectLabelData();
  const value = useAtomValue(filter.state);
  const dispatch = useAtomSet(filter.dispatch, { mode: "value" });
  return { ...value, dispatch };
}

export function useProjectLabelEffects(): ProjectLabelEffects {
  return useProjectLabelData().effects;
}

async function cancelCatalog(queryClient: QueryClient, queryKey: ReturnType<typeof queryKeys.projectLabels>) {
  await queryClient.cancelQueries({ queryKey, exact: true }, { revert: false, silent: true });
}

function patchCreatedLabel(
  queryClient: ReturnType<typeof useQueryClient>,
  queryKey: ReturnType<typeof queryKeys.projectLabels>,
  label: ProjectLabel,
): void {
  queryClient.setQueryData<ProjectLabelCatalog>(queryKey, (catalog) =>
    catalog === undefined
      ? undefined
      : {
          ...catalog,
          labels: [label, ...catalog.labels.filter((candidate) => candidate.id !== label.id)],
        },
  );
}

function patchRenamedLabel(
  queryClient: ReturnType<typeof useQueryClient>,
  queryKey: ReturnType<typeof queryKeys.projectLabels>,
  label: ProjectLabel,
): void {
  queryClient.setQueryData<ProjectLabelCatalog>(queryKey, (catalog) =>
    catalog === undefined
      ? undefined
      : {
          ...catalog,
          labels: catalog.labels.map((candidate) => (candidate.id === label.id ? label : candidate)),
        },
  );
}

function projectCatalogPermutation(
  catalog: ProjectLabelCatalog,
  labelIDs: readonly string[],
): ProjectLabelCatalog {
  if (labelIDs.length !== catalog.labels.length) {
    throw new Error(
      `Project label reorder must contain every label in Project ${catalog.projectID} exactly once.`,
    );
  }
  const labelsByID = new Map(catalog.labels.map((label) => [label.id, label]));
  const labels = labelIDs.map((labelID) => {
    const label = labelsByID.get(labelID);
    if (label === undefined) {
      throw new Error(
        `Project label reorder contains an unknown or duplicate label ${labelID} in Project ${catalog.projectID}.`,
      );
    }
    labelsByID.delete(labelID);
    return label;
  });
  if (labelsByID.size !== 0) {
    throw new Error(
      `Project label reorder must contain every label in Project ${catalog.projectID} exactly once.`,
    );
  }
  return { ...catalog, labels };
}

function catalogsStructurallyEqual(
  left: ProjectLabelCatalog | undefined,
  right: ProjectLabelCatalog,
): boolean {
  return (
    left?.projectID === right.projectID &&
    left.labels.length === right.labels.length &&
    left.labels.every((label, index) => {
      const candidate = right.labels[index];
      return label.id === candidate?.id && label.name === candidate.name;
    })
  );
}
