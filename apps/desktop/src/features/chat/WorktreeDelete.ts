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
  const observer = new MutationObserver(client, {
    mutationFn: async (choice: WorktreeDeleteConfirmationChoice) => {
      const result = previewObserver.getCurrentResult();
      if (!result.isSuccess) throw new Error("Delete requires a completed preview");
      return api.deleteWorktree(sessionID, result.data, choice);
    },
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
      if (!observer.hasListeners()) {
        push({
          id: crypto.randomUUID(),
          tone: "danger",
          title: t("chat.worktree.delete"),
          body: worktreeErrorMessage(error, t),
        });
      } else if (error instanceof WorktreeError && error.detail.kind === "delete_precondition") {
        await freshFetchWorktreeDeletePreview(client, api, request).catch(() => undefined);
      }
    },
  });
  const deletion = queryAtom(observer);
  const confirm = Atom.fn<WorktreeDeleteConfirmationChoice>()(
    (choice) =>
      Effect.gen(function* () {
        if (observer.getCurrentResult().isPending || !previewObserver.getCurrentResult().isSuccess) return;
        yield* Effect.tryPromise(async () => observer.mutate(choice)).pipe(Effect.ignore);
      }),
    { concurrent: true },
  );
  return { preview, deletion, load, confirm };
}

export function useWorktreeDelete(model: ReturnType<typeof createWorktreeDelete>) {
  useAtomMount(model.preview);
  useAtomMount(model.deletion);
  useAtomMount(model.load);
  return { confirm: useAtomSet(model.confirm, { mode: "value" }) };
}
