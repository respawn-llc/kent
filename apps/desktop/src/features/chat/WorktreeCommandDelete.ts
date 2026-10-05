import { useAtomMount, useAtomSet } from "@effect/atom-react";
import { MutationObserver } from "@tanstack/react-query";
import type { WorktreeDeletePreview } from "@/api";
import { mutationPendingAtom, queryAtom } from "@/app-facade";
import { createWorktreeDeletion } from "./WorktreeDelete";
import { worktreeErrorMessage } from "./worktreeErrorMessage";

export function createWorktreeCommandDelete({
  present,
  ...dependencies
}: Omit<Parameters<typeof createWorktreeDeletion>[0], "close"> &
  Readonly<{
    present(preview: WorktreeDeletePreview): void;
  }>) {
  const { client, api, sessionID, push, t } = dependencies;
  const deletion = createWorktreeDeletion(dependencies);
  const mutationKey = ["worktree-command-preview", sessionID, crypto.randomUUID()];
  const observer = new MutationObserver(client, {
    mutationKey,
    mutationFn: async (selector: string | null) => {
      const resolved =
        selector === null
          ? (await api.getWorktreeStatus(sessionID)).worktree?.recordedRoot
          : (await api.resolveWorktreeSelector(sessionID, selector)).worktree?.projection?.selector;
      if (resolved === undefined) throw new Error("Worktree read returned no target identity");
      return api.previewWorktreeDelete(sessionID, resolved);
    },
    retry: false,
    networkMode: "always",
    onSuccess: (preview) => {
      if (observer.hasListeners()) present(preview);
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
  const preparation = queryAtom(observer);
  const requestPending = mutationPendingAtom(client, { mutationKey });
  const prepare = async (selector: string | null) => {
    await observer.mutate(selector).catch(() => undefined);
  };
  return { preparation, requestPending, prepare, deletion };
}

export function useWorktreeCommandDelete(model: ReturnType<typeof createWorktreeCommandDelete>) {
  useAtomMount(model.preparation);
  useAtomMount(model.deletion.deletion);
  return { confirm: useAtomSet(model.deletion.confirm, { mode: "value" }) };
}
