import { useAtomMount, useAtomSet } from "@effect/atom-react";
import { MutationObserver, type QueryClient } from "@tanstack/react-query";
import * as Effect from "effect/Effect";
import * as Atom from "effect/unstable/reactivity/Atom";
import type { TFunction } from "i18next";

import { type ApiService, type WorktreeSwitch } from "@/api";
import { queryAtom, type StatusController } from "@/app-facade";
import { worktreeErrorMessage } from "./worktreeErrorMessage";

export function createWorktreeActions({
  client,
  api,
  sessionID,
  onAccepted,
  push,
  t,
  refreshOpenWorktreeList,
}: Readonly<{
  client: QueryClient;
  api: ApiService;
  sessionID: string;
  onAccepted(): void;
  push: StatusController["push"];
  t: TFunction;
  refreshOpenWorktreeList(sessionID: string): void;
}>) {
  const observer = new MutationObserver(client, {
    mutationFn: async (operation: WorktreeSwitchRequest) => {
      if (operation.kind === "selector") {
        const resolution = await api.resolveWorktreeSelector(sessionID, operation.selector);
        return api.switchWorktree(sessionID, { kind: "resolved", resolution });
      }
      return api.switchWorktree(sessionID, operation);
    },
    retry: false,
    networkMode: "always",
    onSuccess: () => {
      onAccepted();
    },
    onError: (error) => {
      refreshOpenWorktreeList(sessionID);
      push({
        id: crypto.randomUUID(),
        tone: "danger",
        title: t("chat.worktree.switch"),
        body: worktreeErrorMessage(error, t),
      });
    },
  });
  const switching = queryAtom(observer);
  const submitSwitch = async (operation: WorktreeSwitchRequest) => {
    if (observer.getCurrentResult().isPending) return;
    await observer.mutate(operation).catch(() => undefined);
  };
  const switchWorktree = Atom.fn<WorktreeSwitchRequest>()(
    (operation) => Effect.promise(async () => submitSwitch(operation)),
    { concurrent: true },
  );
  return { switching, switchWorktree, submitSwitch, refreshOpenWorktreeList };
}

type WorktreeSwitchRequest =
  WorktreeSwitch | Readonly<{ kind: "selector"; selector: string }> | Readonly<{ kind: "leave" }>;

export function useWorktreeActions(model: ReturnType<typeof createWorktreeActions>) {
  useAtomMount(model.switching);
  return { switchWorktree: useAtomSet(model.switchWorktree, { mode: "value" }) };
}
