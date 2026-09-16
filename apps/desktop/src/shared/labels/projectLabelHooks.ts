import { MutationObserver, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { useMemo } from "react";

import type { ProjectLabel, ProjectLabelCatalog } from "@/api";
import { queryAction, queryKeys, useAppServices, useQueryAction, type AppServices } from "@/app-facade";
import { useProjectLabelData } from "./projectLabelContext";
import type { ProjectLabelEffects } from "./labelEventEffects";
import type { ProjectLabelFilterController } from "./projectLabelFilter";
import { pruneDeletedLabelFromExistingCaches } from "./taskLabelCache";

type ProjectLabelReorderContext = Readonly<{
  previous: ProjectLabelCatalog;
  projected: ProjectLabelCatalog;
}>;

type Completion<A> = Readonly<{
  onSuccess?: (value: A) => void;
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
  const { api } = useAppServices();
  const { effects, projectID } = useProjectLabelData();
  const queryClient = useQueryClient();
  const model = useMemo(
    () => createLabelActions({ api, effects, projectID, queryClient }, labelID),
    [api, effects, projectID, queryClient, labelID],
  );
  return { rename: useQueryAction(model.rename), delete: useQueryAction(model.delete) };
}

function createCatalogActions({ api, effects, projectID, queryClient }: CatalogActionContext) {
  const queryKey = queryKeys.projectLabels(projectID);
  const cancelCatalog = async (): Promise<void> => {
    await queryClient.cancelQueries({ queryKey, exact: true }, { revert: false, silent: true });
  };
  return {
    create: queryAction(
      new MutationObserver(queryClient, {
        mutationFn: async (input: Completion<ProjectLabel> & Readonly<{ name: string }>) =>
          api.createProjectLabel(projectID, input.name),
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
          await cancelCatalog();
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
        Completion<ProjectLabelCatalog> & Readonly<{ labelIDs: readonly string[] }>,
        ProjectLabelReorderContext
      >(queryClient, {
        mutationFn: async ({ labelIDs }) => api.reorderProjectLabels(projectID, labelIDs),
        async onMutate({ labelIDs }) {
          await cancelCatalog();
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
          await cancelCatalog();
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

function createLabelActions({ api, effects, projectID, queryClient }: CatalogActionContext, labelID: string) {
  const queryKey = queryKeys.projectLabels(projectID);
  const cancelCatalog = async () =>
    queryClient.cancelQueries({ queryKey, exact: true }, { revert: false, silent: true });
  return {
    rename: queryAction(
      new MutationObserver(queryClient, {
        mutationFn: async (input: Completion<ProjectLabel> & Readonly<{ name: string }>) =>
          api.renameProjectLabel(projectID, labelID, input.name),
        async onSuccess(label, input) {
          await cancelCatalog();
          patchRenamedLabel(queryClient, queryKey, label);
          effects.scheduleCatalogRefresh();
          input.onSuccess?.(label);
        },
      }),
    ),
    delete: queryAction(
      new MutationObserver<string, unknown, Completion<string>>(queryClient, {
        mutationFn: async () => api.deleteProjectLabel(projectID, labelID),
        async onSuccess(deleted, input) {
          await cancelCatalog();
          pruneDeletedLabelFromExistingCaches(queryClient, projectID, deleted);
          effects.scheduleDeleteRefresh();
          input.onSuccess?.(deleted);
        },
      }),
    ),
  } as const;
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
