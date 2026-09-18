import { QueryObserver, skipToken, type QueryClient } from "@tanstack/react-query";
import { useAtomMount, useAtomSet } from "@effect/atom-react";
import * as Effect from "effect/Effect";
import * as Atom from "effect/unstable/reactivity/Atom";
import type { TFunction } from "i18next";

import { errorMessage, type ChatSettingsTarget } from "@/api";
import { queryAtom, type AppServices } from "@/app-facade";
import { showStatusToast } from "@/ui";
import {
  composerReadOptions,
  type ComposerDraftViewModel,
  type ComposerHistoryMovement,
} from "./ComposerDraftViewModel";

const historyLimit = 100;

export function createComposerHistoryViewModel({
  services,
  client,
  target,
  draft,
  pending,
  t,
}: Readonly<{
  services: AppServices;
  client: QueryClient;
  target: Atom.Atom<ChatSettingsTarget | null>;
  draft: ComposerDraftViewModel;
  pending: Atom.Atom<boolean>;
  t: TFunction;
}>) {
  const owner = crypto.randomUUID();
  const current = Atom.make((get) => {
    const selected = get(target);
    const key = ["chat-composer-history", owner, selected?.kind === "session" ? selected.sessionID : null];
    const observer = new QueryObserver<readonly string[], Error>(client, {
      ...composerReadOptions,
      queryKey: key,
      staleTime: Infinity,
      refetchOnMount: "always",
      queryFn:
        selected?.kind === "session"
          ? async () => {
              try {
                const entries = await services.api.chat.getPromptHistory(selected);
                if (observer.hasListeners())
                  get.set(draft.reindexHistory, {
                    kind: "replace",
                    entries,
                    previous: observer.getCurrentResult().data ?? [],
                  });
                return entries;
              } catch (error) {
                if (observer.hasListeners())
                  showStatusToast({
                    id: `chat-composer-history-${owner}`,
                    tone: "danger",
                    title: t("chatComposer.historyFailed"),
                    body: errorMessage(error),
                    actionLabel: t("app.retry"),
                    onAction: () => {
                      void observer.refetch();
                    },
                  });
                throw error;
              }
            }
          : skipToken,
    });
    return { observer, key, read: queryAtom(observer) };
  });
  const read = Atom.make((get) => get(get(current).read));
  const navigate = Atom.fn<-1 | 1>()((direction, get) =>
    Effect.gen(function* (): Effect.fn.Return<ComposerHistoryMovement> {
      const result = get(current).observer.getCurrentResult();
      if (result.isFetching || get(pending) || get(draft.navigationPending) || result.data === undefined)
        return { kind: "none" };
      return yield* get.setResult(draft.navigate, { direction, entries: result.data });
    }),
  );
  const append = Atom.fn<string>()(
    (input, get) =>
      Effect.sync(() => {
        const text = input.trim();
        if (text.length === 0 || get(target)?.kind !== "session") return;
        const { observer, key } = get(current);
        const entries = observer.getCurrentResult().data ?? [];
        const removed = Math.max(0, entries.length + 1 - historyLimit);
        get.set(draft.reindexHistory, { kind: "trim", removed });
        client.setQueryData<readonly string[]>(key, [...entries.slice(removed), text]);
      }),
    { concurrent: true },
  );
  return { read, navigate, append } as const;
}

export type ComposerHistoryViewModel = ReturnType<typeof createComposerHistoryViewModel>;

export function useComposerHistoryActions(model: ComposerHistoryViewModel) {
  useAtomMount(model.read);
  return {
    navigate: useAtomSet(model.navigate, { mode: "promise" }),
    append: useAtomSet(model.append),
  };
}
