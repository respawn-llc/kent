import { useAtomMount, useAtomSet } from "@effect/atom-react";
import { MutationObserver, QueryObserver, type QueryClient } from "@tanstack/react-query";
import * as Effect from "effect/Effect";
import * as Atom from "effect/unstable/reactivity/Atom";
import type { TFunction } from "i18next";

import {
  BranchCleanupOutcomeKind,
  WorktreeError,
  type ApiService,
  type WorktreeDeleteConfirmationChoice,
  type WorktreeDeletePreview,
} from "@/api";
import {
  createWorktreeSelectorRequest,
  disposeWorktreeDeletePreview,
  freshFetchWorktreeDeletePreview,
  worktreeDeletePreviewQueryOptions,
  queryAtom,
  type StatusController,
} from "@/app-facade";
import { worktreeErrorMessage } from "./worktreeErrorMessage";

export function createWorktreeDelete({
  client,
  api,
  sessionID,
  selector,
  close,
  refreshOpenWorktreeList,
  push,
  t,
}: Readonly<{
  client: QueryClient;
  api: ApiService;
  sessionID: string;
  selector: string;
  close(): void;
  refreshOpenWorktreeList(sessionID: string): void;
  push: StatusController["push"];
  t: TFunction;
}>) {
  const request = createWorktreeSelectorRequest(sessionID, selector);
  const previewObserver = new QueryObserver(client, {
    ...worktreeDeletePreviewQueryOptions(api, request),
    enabled: false,
  });
  const preview = queryAtom(previewObserver);
  const load = Atom.make(
    Effect.gen(function* () {
      yield* Effect.addFinalizer(() =>
        Effect.promise(async () => disposeWorktreeDeletePreview(client, request)),
      );
      yield* Effect.tryPromise(async () => freshFetchWorktreeDeletePreview(client, api, request)).pipe(
        Effect.ignore,
      );
    }),
  );
  const mutation = createWorktreeDeletion({
    client,
    api,
    sessionID,
    close,
    refreshOpenWorktreeList,
    push,
    t,
    feedback: {
      kind: "inline",
      reload: async () => {
        await freshFetchWorktreeDeletePreview(client, api, request).catch(() => undefined);
      },
    },
  });
  const confirm = Atom.fn<WorktreeDeleteConfirmationChoice>()(
    (choice) =>
      Effect.gen(function* () {
        const result = previewObserver.getCurrentResult();
        if (!result.isSuccess) return;
        yield* Effect.promise(async () => mutation.submit({ preview: result.data, choice }));
      }),
    { concurrent: true },
  );
  return { preview, deletion: mutation.deletion, load, confirm };
}

type DeleteRequest = Readonly<{ preview: WorktreeDeletePreview; choice: WorktreeDeleteConfirmationChoice }>;

export function createWorktreeDeletion({
  client,
  api,
  sessionID,
  close,
  refreshOpenWorktreeList,
  push,
  t,
  feedback,
}: Omit<Parameters<typeof createWorktreeDelete>[0], "selector"> &
  Readonly<{
    feedback: Readonly<{ kind: "dialog" }> | Readonly<{ kind: "inline"; reload(): Promise<void> }>;
  }>) {
  const observer = new MutationObserver(client, {
    mutationFn: async ({ preview, choice }: DeleteRequest) => api.deleteWorktree(sessionID, preview, choice),
    retry: false,
    networkMode: "always",
    onSuccess: (result) => {
      if (observer.hasListeners()) close();
      refreshOpenWorktreeList(sessionID);
      const warnings: string[] = [];
      if (result.cleanup?.kind === BranchCleanupOutcomeKind.WORKTREE_BRANCH_CLEANUP_OUTCOME_RETAINED) {
        warnings.push(
          t("chat.worktree.branchRetained", {
            branch: result.cleanup.branchName,
          }),
        );
        if (result.cleanup.diagnostic !== undefined) warnings.push(result.cleanup.diagnostic);
      }
      if (result.leftoverRoot !== undefined)
        warnings.push(t("chat.worktree.rootRetained", { root: result.leftoverRoot }));
      if (warnings.length > 0)
        push({
          id: crypto.randomUUID(),
          tone: "warning",
          title: t("chat.worktree.delete"),
          body: warnings.join("\n"),
        });
    },
    onError: async (error) => {
      if (feedback.kind === "dialog" || !observer.hasListeners()) {
        if (feedback.kind === "dialog" && observer.hasListeners()) close();
        push({
          id: crypto.randomUUID(),
          tone: "danger",
          title: t("chat.worktree.delete"),
          body: worktreeErrorMessage(error, t),
        });
      } else if (error instanceof WorktreeError && error.detail.kind === "delete_precondition") {
        await feedback.reload();
      }
    },
  });
  const deletion = queryAtom(observer);
  const submit = async (request: DeleteRequest) => {
    if (observer.getCurrentResult().isPending) return;
    await observer.mutate(request).catch(() => undefined);
  };
  const confirm = Atom.fn<DeleteRequest>()((request) => Effect.promise(async () => submit(request)), {
    concurrent: true,
  });
  return { deletion, confirm, submit };
}

export function useWorktreeDelete(model: ReturnType<typeof createWorktreeDelete>) {
  useAtomMount(model.preview);
  useAtomMount(model.deletion);
  useAtomMount(model.load);
  return { confirm: useAtomSet(model.confirm, { mode: "value" }) };
}
