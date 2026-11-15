import { useAtomMount, useAtomSet } from "@effect/atom-react";
import { MutationObserver, QueryObserver, type QueryClient } from "@tanstack/react-query";
import * as Effect from "effect/Effect";
import * as Atom from "effect/reactivity/Atom";
import type { TFunction } from "i18next";

import { type ApiService, type WorktreeDeleteConfirmationChoice, type WorktreeDeletePreview } from "@/api";
import {
  createWorktreeSelectorRequest,
  disposeWorktreeDeletePreview,
  freshFetchWorktreeDeletePreview,
  worktreeDeletePreviewQueryOptions,
  queryAtom,
  mutationPendingAtom,
  type StatusController,
} from "@/app-facade";
import { branchCleanupWarnings } from "@/shared/workspaces";
import { worktreeErrorMessage } from "./worktreeErrorMessage";

export function createWorktreeDelete({
  client,
  api,
  sessionID,
  refreshOpenWorktreeList,
  push,
  t,
}: Readonly<{
  client: QueryClient;
  api: ApiService;
  sessionID: string;
  refreshOpenWorktreeList(sessionID: string): void | Promise<void>;
  push: StatusController["push"];
  t: TFunction;
}>) {
  const selector = Atom.make<string | null>(null);
  const selection = Atom.make((get) => {
    const selected = get(selector);
    if (selected === null) return null;
    const request = createWorktreeSelectorRequest(sessionID, selected);
    const observer = new QueryObserver(client, {
      ...worktreeDeletePreviewQueryOptions(api, request),
      enabled: false,
    });
    return {
      preview: queryAtom(observer),
      load: Atom.make((loadGet) =>
        Effect.gen(function* () {
          yield* Effect.addFinalizer(() =>
            Effect.promise(async () => disposeWorktreeDeletePreview(client, request)),
          );
          yield* Effect.tryPromise(async () => freshFetchWorktreeDeletePreview(client, api, request)).pipe(
            Effect.catch((error) =>
              Effect.sync(() => {
                push({
                  id: crypto.randomUUID(),
                  tone: "danger",
                  title: t("chat.worktree.delete"),
                  body: worktreeErrorMessage(error, t),
                });
                loadGet.set(selector, null);
              }),
            ),
          );
        }),
      ),
    };
  });
  const preview = Atom.make((get) => {
    const current = get(selection);
    return current === null ? null : get(current.preview);
  });
  const load = Atom.make((get) => {
    const current = get(selection);
    if (current !== null) get(current.load);
  });
  const mutation = createWorktreeDeletion({
    client,
    api,
    sessionID,
    refreshOpenWorktreeList,
    push,
    t,
  });
  const confirm = Atom.fn<WorktreeDeleteConfirmationChoice>()(
    (choice, get) =>
      Effect.gen(function* () {
        const result = get(preview);
        if (result === null || !result.isSuccess || get(mutation.requestPending)) return;
        get.set(selector, null);
        yield* Effect.promise(async () => mutation.submit({ preview: result.data, choice }));
      }),
    { concurrent: true },
  );
  const busySelectors = Atom.make((get): ReadonlySet<string> => {
    const busy = new Set<string>();
    const deletion = get(mutation.deletion);
    if (deletion.isPending) busy.add(deletion.variables.preview.deletionSelector);
    const selected = get(selector);
    if (get(preview)?.isPending === true && selected !== null) busy.add(selected);
    return busy;
  });
  return { selector, preview, deletion: mutation.deletion, busySelectors, load, confirm };
}

type DeleteRequest = Readonly<{ preview: WorktreeDeletePreview; choice: WorktreeDeleteConfirmationChoice }>;

export function createWorktreeDeletion({
  client,
  api,
  sessionID,
  refreshOpenWorktreeList,
  push,
  t,
}: Parameters<typeof createWorktreeDelete>[0]) {
  const mutationKey = ["worktree-delete", sessionID, crypto.randomUUID()];
  const observer = new MutationObserver(client, {
    mutationKey,
    mutationFn: async ({ preview, choice }: DeleteRequest) => api.deleteWorktree(sessionID, preview, choice),
    retry: false,
    networkMode: "always",
    onSuccess: async (result) => {
      const warnings = branchCleanupWarnings(result.cleanup, t);
      if (result.leftoverRoot !== undefined)
        warnings.push(t("chat.worktree.rootRetained", { root: result.leftoverRoot }));
      if (warnings.length > 0)
        push({
          id: crypto.randomUUID(),
          tone: "warning",
          title: t("chat.worktree.delete"),
          body: warnings.join("\n"),
        });
      await refreshOpenWorktreeList(sessionID);
    },
    onError: (error) => {
      push({
        id: crypto.randomUUID(),
        tone: "danger",
        title: t("chat.worktree.delete"),
        body: worktreeErrorMessage(error, t),
      });
    },
  });
  const deletion = queryAtom(observer);
  const requestPending = mutationPendingAtom(client, { mutationKey });
  const submit = async (request: DeleteRequest) => {
    await observer.mutate(request).catch(() => undefined);
  };
  const confirm = Atom.fn<DeleteRequest>()((request) => Effect.promise(async () => submit(request)), {
    concurrent: true,
  });
  return { deletion, requestPending, confirm, submit };
}

export function useWorktreeDelete(model: ReturnType<typeof createWorktreeDelete>) {
  useAtomMount(model.preview);
  useAtomMount(model.deletion);
  useAtomMount(model.load);
  return {
    select: useAtomSet(model.selector, { mode: "value" }),
    confirm: useAtomSet(model.confirm, { mode: "value" }),
  };
}
