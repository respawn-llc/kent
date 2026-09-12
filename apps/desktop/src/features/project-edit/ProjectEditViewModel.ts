import {
  InfiniteQueryObserver,
  MutationObserver,
  QueryObserver,
  type QueryClient,
} from "@tanstack/react-query";
import * as Effect from "effect/Effect";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Stream from "effect/Stream";
import type { TFunction } from "i18next";
import { useAtomMount, useAtomSet } from "@effect/atom-react";

import { errorMessage, type WorkspaceCatalogRow } from "@/api";
import {
  completeProjectDeletion,
  projectWorkspaceChanges,
  queryAtom,
  queryKeys,
  workspaceCatalogInfiniteQueryOptions,
  type AppServices,
  type SidebarPageNavigator,
  type StatusController,
} from "@/app-facade";
import { projectKeyErrors, projectNameErrors } from "./ProjectEditUtils";
import {
  invalidateProjectEditQueries,
  invalidateProjectWorkspaceOwners,
  projectDeleteMutationOptions,
} from "./useProjectEditData";

const explicitRequestOptions = { retry: false, networkMode: "always" } as const;
const explicitReadOptions = {
  ...explicitRequestOptions,
  refetchOnReconnect: false,
  refetchOnWindowFocus: false,
} as const;

export function createProjectEditViewModel({
  services,
  client,
  projectID,
  t,
  push,
  navigator,
}: Readonly<{
  services: AppServices;
  client: QueryClient;
  projectID: string;
  t: TFunction;
  push: StatusController["push"];
  navigator?: SidebarPageNavigator | undefined;
}>) {
  const workspaceChanges = Atom.make(
    projectWorkspaceChanges(services.nativeBridge.projectWorkspace, projectID).pipe(
      Stream.runForEach(() =>
        Effect.tryPromise({
          try: async () => invalidateProjectWorkspaceOwners(client, projectID),
          catch: (cause) => ({ _tag: "WorkspaceRefreshError" as const, cause }),
        }),
      ),
      Effect.catch((error) =>
        Effect.sync(() => {
          push({
            id: "project-edit-workspace-observation-error",
            tone: "danger",
            title: t("projectEdit.title"),
            body: errorMessage(error.cause),
          });
        }),
      ),
    ),
  );
  const observer = new QueryObserver(client, {
    queryKey: queryKeys.projectEdit(projectID),
    queryFn: async () => services.api.getProjectEdit(projectID),
    ...explicitReadOptions,
  });
  const metadata = queryAtom(observer);
  const catalogObserver = new InfiniteQueryObserver(client, {
    ...workspaceCatalogInfiniteQueryOptions(services.api, projectID),
    ...explicitReadOptions,
  });
  const catalog = queryAtom(catalogObserver);
  const workspaces = Atom.make(
    (get) =>
      get(catalog).data?.pages.flatMap((page) =>
        page.workspaces.map((workspace, index) => ({
          occurrenceKey: `${page.offset.toString()}:${index.toString()}`,
          workspace,
        })),
      ) ?? [],
  );
  const nextPage = Atom.fn(() => Effect.promise(async () => catalogObserver.fetchNextPage()), {
    concurrent: true,
  });
  const previousPage = Atom.fn(() => Effect.promise(async () => catalogObserver.fetchPreviousPage()), {
    concurrent: true,
  });
  const retryCatalog = Atom.fn(() => Effect.promise(async () => catalogObserver.refetch()), {
    concurrent: true,
  });
  const name = Atom.make<string | null>(null);
  const key = Atom.make<string | null>(null);
  const saveObserver = new MutationObserver(client, {
    mutationFn: async (input: { displayName: string; projectKey: string | undefined }) =>
      services.api.updateProject(projectID, input.displayName, input.projectKey),
    ...explicitRequestOptions,
    onSuccess: async () => {
      await invalidateProjectEditQueries(client, projectID);
      push({
        id: "project-edit-saved",
        tone: "success",
        title: t("projectEdit.title"),
        body: t("projectEdit.projectSaved"),
      });
    },
    onError: (error) => {
      push({
        id: "project-edit-save-error",
        tone: "danger",
        title: t("projectEdit.title"),
        body: errorMessage(error),
      });
    },
  });
  const saving = queryAtom(saveObserver);
  const defaultObserver = new MutationObserver(client, {
    mutationFn: async (workspaceID: string) => services.api.setDefaultWorkspace(projectID, workspaceID),
    ...explicitRequestOptions,
    onSuccess: async () => {
      await invalidateProjectWorkspaceOwners(client, projectID);
      push({
        id: "project-edit-default-saved",
        tone: "success",
        title: t("projectEdit.title"),
        body: t("projectEdit.defaultWorkspaceSaved"),
      });
    },
    onError: (error) => {
      push({
        id: "project-edit-default-save-error",
        tone: "danger",
        title: t("projectEdit.title"),
        body: errorMessage(error),
      });
    },
  });
  const savingDefault = queryAtom(defaultObserver);
  const attachObserver = new MutationObserver(client, {
    mutationFn: async (path: string) => services.api.attachWorkspace(projectID, path),
    ...explicitRequestOptions,
    onSuccess: async (response) => {
      await invalidateProjectWorkspaceOwners(client, projectID);
      push({
        id: "project-edit-workspace-attached",
        tone: "success",
        title: t("projectEdit.title"),
        body: t(
          response.outcome === "already_attached"
            ? "projectEdit.workspaceAlreadyLinked"
            : "projectEdit.workspaceAttached",
        ),
      });
    },
    onError: (error) => {
      push({
        id: "project-edit-workspace-attach-error",
        tone: "danger",
        title: t("projectEdit.title"),
        body: errorMessage(error),
      });
    },
  });
  const attaching = queryAtom(attachObserver);
  const unlinkObserver = new MutationObserver(client, {
    mutationFn: async (input: { workspaceID: string; close(): void }) =>
      services.api.unlinkWorkspace(projectID, input.workspaceID),
    ...explicitRequestOptions,
    onSuccess: async (response, input) => {
      await invalidateProjectWorkspaceOwners(client, projectID);
      if (response.blockers.length === 0) {
        input.close();
        push({
          id: "project-edit-workspace-unlinked",
          tone: "success",
          title: t("projectEdit.title"),
          body: t("projectEdit.workspaceUnlinked"),
        });
      } else {
        push({
          id: "project-edit-workspace-unlink-blocked",
          tone: "danger",
          title: t("projectEdit.workspaceUnlinkBlocked"),
          body:
            response.blockers.map((blocker) => blocker.message).join("\n") ||
            t("projectEdit.workspaceUnlinkBlocked"),
        });
      }
    },
    onError: (error) => {
      push({
        id: "project-edit-workspace-unlink-error",
        tone: "danger",
        title: t("projectEdit.title"),
        body: errorMessage(error),
      });
    },
  });
  const unlinking = queryAtom(unlinkObserver);
  const deleteObserver = new MutationObserver(client, {
    ...projectDeleteMutationOptions(services.api, projectID),
    ...explicitRequestOptions,
    onSuccess: async (response, input: { close(): void; openHome: () => Promise<void> }) => {
      if (!response.deleted) {
        await invalidateProjectEditQueries(client, projectID);
        push({
          id: "project-delete-blocked",
          tone: "danger",
          title: t("projectEdit.deleteBlocked"),
          body: response.blockers.map((blocker) => blocker.message).join("\n"),
        });
        return;
      }
      input.close();
      const outcome = navigator?.close();
      await completeProjectDeletion({
        projectID,
        queryClient: client,
        navigateHome: outcome === "accepted" ? input.openHome : undefined,
        pushDeletedToast: () => {
          push({ id: "project-delete-deleted", tone: "success", title: t("projectEdit.deleteDeleted") });
        },
      });
    },
    onError: (error) => {
      push({
        id: "project-delete-error",
        tone: "danger",
        title: t("projectEdit.deleteTitle"),
        body: errorMessage(error),
      });
    },
  });
  const deleting = queryAtom(deleteObserver);
  const deleteProject = Atom.fn<{ close(): void; openHome: () => Promise<void> }>()(
    (input, get) =>
      Effect.gen(function* () {
        if (navigator === undefined || requestPending() || get(picker).waiting) return;
        yield* Effect.tryPromise(async () => deleteObserver.mutate(input)).pipe(Effect.ignore);
      }),
    { concurrent: true },
  );
  const unlink = Atom.fn<{ workspaceID: string; close(): void }>()(
    (input, get) =>
      Effect.gen(function* () {
        if (requestPending() || get(picker).waiting) return;
        yield* Effect.tryPromise(async () => unlinkObserver.mutate(input)).pipe(Effect.ignore);
      }),
    { concurrent: true },
  );
  const picker = Atom.fn(
    () =>
      Effect.tryPromise({
        try: async () =>
          services.nativeBridge.directories.selectDirectory({ title: t("projectEdit.chooseWorkspace") }),
        catch: (cause) => ({ _tag: "DirectorySelectionError" as const, cause }),
      }),
    { concurrent: true },
  );
  const chooseWorkspace = Atom.fn<undefined>()(
    (_, get) =>
      Effect.gen(function* () {
        if (requestPending() || get(picker).waiting) return;
        const selected = yield* get.setResult(picker, undefined);
        if (selected === null) return;
        yield* Effect.tryPromise(async () => attachObserver.mutate(selected.path)).pipe(Effect.ignore);
      }).pipe(
        Effect.catch((error) =>
          Effect.sync(() => {
            push({
              id: "project-edit-workspace-attach-error",
              tone: "danger",
              title: t("projectEdit.title"),
              body: errorMessage(error.cause),
            });
          }),
        ),
      ),
    { concurrent: true },
  );
  const requests = Atom.make((get) =>
    [get(saving), get(savingDefault), get(attaching), get(unlinking), get(deleting)].some(
      (result) => result.isPending,
    ),
  );
  const requestPending = () =>
    [saveObserver, defaultObserver, attachObserver, unlinkObserver, deleteObserver].some(
      (mutation) => mutation.getCurrentResult().isPending,
    );
  const makeDefault = Atom.fn<WorkspaceCatalogRow>()(
    (workspace, get) =>
      Effect.gen(function* () {
        if (workspace.isDefault || requestPending() || get(picker).waiting) return;
        yield* Effect.tryPromise(async () => defaultObserver.mutate(workspace.id)).pipe(Effect.ignore);
      }),
    { concurrent: true },
  );
  const drafts = Atom.make((get) => {
    const project = get(metadata).data;
    const nameDraft = get(name) ?? project?.displayName ?? "";
    const keyDraft = get(key) ?? project?.projectKey ?? "";
    const nameErrors = projectNameErrors(nameDraft, t);
    const keyChanged = project !== undefined && keyDraft !== project.projectKey;
    const keyErrors = keyChanged ? projectKeyErrors(keyDraft, t) : [];
    const dirty = project !== undefined && (nameDraft !== project.displayName || keyChanged);
    return {
      nameDraft,
      keyDraft,
      nameErrors,
      keyErrors,
      dirty,
      keyChanged,
    };
  });
  const state = Atom.make((get) => {
    const draft = get(drafts);
    const pending = get(requests) || get(picker).waiting;
    return {
      ...draft,
      pending,
      canSave: draft.dirty && draft.nameErrors.length === 0 && draft.keyErrors.length === 0 && !pending,
    };
  });
  const editName = Atom.fn<string>()(
    (value, get) =>
      Effect.sync(() => {
        get.set(name, value);
      }),
    {
      concurrent: true,
    },
  );
  const editKey = Atom.fn<string>()(
    (value, get) =>
      Effect.sync(() => {
        get.set(key, value);
      }),
    {
      concurrent: true,
    },
  );
  const save = Atom.fn<undefined>()(
    (_, get) =>
      Effect.gen(function* () {
        const draft = get(state);
        if (!draft.canSave || requestPending()) return;
        yield* Effect.tryPromise(async () =>
          saveObserver.mutate({
            displayName: draft.nameDraft,
            projectKey: draft.keyChanged ? draft.keyDraft : undefined,
          }),
        ).pipe(Effect.ignore);
      }),
    { concurrent: true },
  );
  const retryMetadata = Atom.fn(() => Effect.promise(async () => observer.refetch()), { concurrent: true });
  return {
    workspaceChanges,
    metadata,
    catalog,
    workspaces,
    nextPage,
    previousPage,
    retryCatalog,
    state,
    editName,
    editKey,
    save,
    makeDefault,
    chooseWorkspace,
    unlink,
    deleteProject,
    retryMetadata,
    requests,
  } as const;
}

export type ProjectEditViewModel = ReturnType<typeof createProjectEditViewModel>;

export function useProjectEditActions(model: ProjectEditViewModel) {
  useAtomMount(model.requests);
  return {
    editName: useAtomSet(model.editName),
    editKey: useAtomSet(model.editKey),
    save: useAtomSet(model.save),
    retryMetadata: useAtomSet(model.retryMetadata),
    retryCatalog: useAtomSet(model.retryCatalog),
    nextPage: useAtomSet(model.nextPage),
    previousPage: useAtomSet(model.previousPage),
    makeDefault: useAtomSet(model.makeDefault),
    chooseWorkspace: useAtomSet(model.chooseWorkspace),
    unlink: useAtomSet(model.unlink),
    deleteProject: useAtomSet(model.deleteProject),
  };
}
