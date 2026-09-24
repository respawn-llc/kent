import { useState } from "react";
import {
  InfiniteQueryObserver,
  QueryObserver,
  useQueryClient,
  type QueryClient,
  type QueryObserverResult,
} from "@tanstack/react-query";
import { useAtomMount, useAtomSet, useAtomValue } from "@effect/atom-react";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";
import * as Option from "effect/Option";
import type { TFunction } from "i18next";
import { errorMessage, type ApiService, type ProjectWorkspaceResult, type WorkspaceCatalogRow } from "@/api";
import {
  queryAtom,
  queryKeys,
  projectWorkspaceQueryOptions,
  workspaceCatalogInfiniteQueryOptions,
  type QuerySnapshot,
} from "@/app-facade";
import {
  projectWorkspaceSelectorProjection,
  updateWorkspaceSelection,
  type WorkspaceSelectionState,
} from "@/shared/workspaces";
import type { SelectFieldPaging } from "@/ui";

function createNewTaskWorkspaceModel(
  api: ApiService,
  client: QueryClient,
  projectID: string,
  initiatingID: string | undefined,
) {
  const catalogObserver = new InfiniteQueryObserver(
    client,
    workspaceCatalogInfiniteQueryOptions(api, projectID),
  );
  const initiatingObserver = new QueryObserver(
    client,
    projectWorkspaceQueryOptions(api, projectID, initiatingID),
  );
  const workspaces = queryAtom(catalogObserver);
  const initiatingWorkspace = queryAtom(initiatingObserver);
  const firstRetainedOffset = catalogObserver.getCurrentResult().data?.pages[0]?.offset;
  const initialize = Atom.make(
    Effect.promise(async () => {
      if (firstRetainedOffset !== undefined && firstRetainedOffset > 0) {
        await client.resetQueries({ exact: true, queryKey: queryKeys.projectWorkspaceCatalog(projectID) });
      }
    }),
  );
  const selection = Atom.writable(
    (get): WorkspaceSelectionState => {
      const catalog = get(workspaces);
      const initiating = get(initiatingWorkspace);
      let state = Option.getOrElse(get.self<WorkspaceSelectionState>(), (): WorkspaceSelectionState => ({
        catalog: { state: "pending" },
        initiating: initiatingID === undefined ? undefined : { state: "pending" },
        selection: { state: "uncommitted" },
      }));
      const defaultWorkspace = catalog.data?.pages[0]?.workspaces.find((row) => row.isDefault);
      if (
        defaultWorkspace !== undefined &&
        (state.catalog.state !== "loaded" || state.catalog.defaultWorkspace !== defaultWorkspace)
      ) {
        state = updateWorkspaceSelection(state, { type: "catalog-loaded", defaultWorkspace });
      }
      return resolveInitiatingSelection(state, initiating);
    },
    (get, state: WorkspaceSelectionState) => {
      get.setSelf(state);
    },
  );
  const projection = Atom.make((get) => {
    const state = get(selection);
    const catalog = get(workspaces);
    const selectedWorkspace = state.selection.state === "committed" ? state.selection.row : undefined;
    const workspaceProjection = projectWorkspaceSelectorProjection({
      catalogPages: catalog.data?.pages ?? [],
      initiatingRow: state.initiating?.state === "attached" ? state.initiating.row : undefined,
      selectedSnapshot: selectedWorkspace,
      catalogExhausted: catalog.data !== undefined && !catalog.hasNextPage,
    });
    return {
      workspaceSelection: state,
      selectedWorkspace,
      workspaceProjection,
      workspaceItems: workspaceProjection.rows,
    };
  });
  const select = Atom.fn<WorkspaceCatalogRow>()(
    (row, get) =>
      Effect.sync(() => {
        get.set(selection, updateWorkspaceSelection(get(selection), { type: "user-selected", row }));
      }),
    { concurrent: true },
  );
  const retryInitiating = Atom.fn(
    (_, get) =>
      Effect.gen(function* () {
        get.set(selection, updateWorkspaceSelection(get(selection), { type: "initiating-retry" }));
        yield* Effect.promise(async () => initiatingObserver.refetch());
      }),
    { concurrent: true },
  );
  const retryCatalog = Atom.fn(() => Effect.promise(async () => catalogObserver.refetch()), {
    concurrent: true,
  });
  const nextPage = Atom.fn(() => Effect.promise(async () => catalogObserver.fetchNextPage()), {
    concurrent: true,
  });
  return {
    initialize,
    workspaces,
    initiatingWorkspace,
    projection,
    select,
    retryInitiating,
    retryCatalog,
    nextPage,
  } as const;
}

function resolveInitiatingSelection(
  state: WorkspaceSelectionState,
  initiating: QuerySnapshot<QueryObserverResult<ProjectWorkspaceResult>>,
): WorkspaceSelectionState {
  const current = state.initiating;
  if (current === undefined) return state;
  if (initiating.data?.kind === "attached") {
    if (current.state === "attached" && current.row === initiating.data.workspace) return state;
    return updateWorkspaceSelection(state, { type: "initiating-attached", row: initiating.data.workspace });
  }
  if (initiating.data?.kind === "not_attached") {
    return current.state === "not_attached"
      ? state
      : updateWorkspaceSelection(state, { type: "initiating-not-attached" });
  }
  if (initiating.isError) {
    if (current.state === "failed" && current.error === initiating.error) return state;
    return updateWorkspaceSelection(state, { type: "initiating-failed", error: initiating.error });
  }
  return state;
}

export function useNewTaskWorkspaceCatalog(
  api: ApiService,
  projectID: string,
  initiatingID: string | undefined,
  t: TFunction,
) {
  const client = useQueryClient();
  const [model] = useState(() => createNewTaskWorkspaceModel(api, client, projectID, initiatingID));
  useAtomMount(model.initialize);
  const workspaces = useAtomValue(model.workspaces);
  const initiatingWorkspace = useAtomValue(model.initiatingWorkspace);
  const projection = useAtomValue(model.projection);
  const selectWorkspace = useAtomSet(model.select);
  const retryInitiatingWorkspace = useAtomSet(model.retryInitiating);
  const retryCatalog = useAtomSet(model.retryCatalog);
  const nextPage = useAtomSet(model.nextPage);
  const workspacePaging: SelectFieldPaging = {
    hasNextPage: workspaces.hasNextPage,
    initialBoundary: workspaces.isPending
      ? { state: "loading", label: t("states.loading") }
      : workspaces.isError && workspaces.data === undefined
        ? {
            state: "error",
            message: errorMessage(workspaces.error),
            retryLabel: t("app.retry"),
            onRetry: retryCatalog,
          }
        : undefined,
    loadKey: workspaces.data?.pages.at(-1)?.nextOffset?.toString(),
    nextBoundary: workspaces.isFetchingNextPage
      ? { state: "loading", label: t("app.loadingMore") }
      : workspaces.isFetchNextPageError
        ? {
            state: "error",
            message: errorMessage(workspaces.error),
            retryLabel: t("app.retry"),
            onRetry: nextPage,
          }
        : undefined,
    onLoadNext: nextPage,
  };
  return {
    ...projection,
    initiatingWorkspace,
    workspaces,
    workspacePaging,
    selectWorkspace,
    retryInitiatingWorkspace,
  };
}
