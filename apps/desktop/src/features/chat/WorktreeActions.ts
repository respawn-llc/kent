import { useAtomMount, useAtomSet } from "@effect/atom-react";
import { MutationObserver, type QueryClient } from "@tanstack/react-query";
import * as Effect from "effect/Effect";
import * as Atom from "effect/unstable/reactivity/Atom";
import type { TFunction } from "i18next";

import { type ApiService, type WorktreeSwitch } from "@/api";
import { queryAtom, type SidebarPageNavigator, type StatusController } from "@/app-facade";
import { worktreeErrorMessage } from "./worktreeErrorMessage";

export function createWorktreeActions({
  client,
  api,
  sessionID,
  navigator,
  push,
  t,
  refreshOpenWorktreeList,
}: Readonly<{
  client: QueryClient;
  api: ApiService;
  sessionID: string;
  navigator: SidebarPageNavigator;
  push: StatusController["push"];
  t: TFunction;
  refreshOpenWorktreeList(sessionID: string): void;
}>) {
  const observer = new MutationObserver(client, {
    mutationFn: async (operation: WorktreeSwitch) => api.switchWorktree(sessionID, operation),
    retry: false,
    networkMode: "always",
    onSuccess: () => {
      navigator.close();
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
  const switchWorktree = Atom.fn<WorktreeSwitch>()(
    (operation) =>
      Effect.gen(function* () {
        if (observer.getCurrentResult().isPending) return;
        yield* Effect.tryPromise(async () => observer.mutate(operation)).pipe(Effect.ignore);
      }),
    { concurrent: true },
  );
  const submitSwitch = (operation: WorktreeSwitch) => {
    void observer.mutate(operation).catch(() => undefined);
  };
  return { switching, switchWorktree, submitSwitch, refreshOpenWorktreeList };
}

export function useWorktreeActions(model: ReturnType<typeof createWorktreeActions>) {
  useAtomMount(model.switching);
  return { switchWorktree: useAtomSet(model.switchWorktree, { mode: "value" }) };
}
