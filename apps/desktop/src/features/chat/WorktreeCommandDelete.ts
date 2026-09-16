import { useAtomMount, useAtomSet } from "@effect/atom-react";
import { MutationObserver } from "@tanstack/react-query";
import type { WorktreeDeletePreview } from "@/api";
import {
  createWorktreeSelectorRequest,
  disposeWorktreeDeletePreview,
  freshFetchWorktreeDeletePreview,
  queryAtom,
} from "@/app-facade";
import { createWorktreeDeletion } from "./WorktreeDelete";
import { worktreeErrorMessage } from "./worktreeErrorMessage";

export function createWorktreeCommandDelete({
  present,
  ...dependencies
}: Omit<Parameters<typeof createWorktreeDeletion>[0], "feedback"> &
  Readonly<{
    present(preview: WorktreeDeletePreview): void;
  }>) {
  const { client, api, sessionID, push, t } = dependencies;
  const deletion = createWorktreeDeletion({ ...dependencies, feedback: { kind: "dialog" } });
  const observer = new MutationObserver(client, {
    mutationFn: async (selector: string | null) => {
      const resolved =
        selector === null
          ? (await api.getWorktreeStatus(sessionID)).worktree?.recordedRoot
          : (await api.resolveWorktreeSelector(sessionID, selector)).worktree?.projection?.selector;
      if (resolved === undefined) throw new Error("Worktree read returned no target identity");
      const request = createWorktreeSelectorRequest(sessionID, resolved);
      try {
        return await freshFetchWorktreeDeletePreview(client, api, request);
      } finally {
        await disposeWorktreeDeletePreview(client, request);
      }
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
  const prepare = async (selector: string | null) => {
    if (observer.getCurrentResult().isPending) return;
    await observer.mutate(selector).catch(() => undefined);
  };
  return { preparation, prepare, deletion };
}

export function useWorktreeCommandDelete(model: ReturnType<typeof createWorktreeCommandDelete>) {
  useAtomMount(model.preparation);
  useAtomMount(model.deletion.deletion);
  return { confirm: useAtomSet(model.deletion.confirm, { mode: "value" }) };
}
