import { MutationObserver, QueryObserver, skipToken, type QueryClient } from "@tanstack/react-query";
import * as Effect from "effect/Effect";
import * as Atom from "effect/unstable/reactivity/Atom";
import type { TFunction } from "i18next";

import {
  errorMessage,
  type ChatSessionTarget,
  type ChatSettingsTarget,
  type PendingWork,
  type PendingWorkIdentity,
} from "@/api";
import { mutationPendingAtom, queryAtom, type AppServices } from "@/app-facade";
import { showStatusToast } from "@/ui";
import {
  composerReadOptions,
  composerRequestOptions,
  type ComposerTextRestoration,
} from "./ComposerDraftViewModel";

type DiscardRequest = Readonly<{
  target: ChatSessionTarget;
  item: PendingWorkIdentity;
  restore(input: ComposerTextRestoration): void;
  refresh(): void;
}>;
export function createComposerPendingViewModel({
  services,
  client,
  target,
  t,
}: Readonly<{
  services: AppServices;
  client: QueryClient;
  target: Atom.Atom<ChatSettingsTarget>;
  t: TFunction;
}>) {
  const owner = crypto.randomUUID();
  const scope = Atom.make<string>(crypto.randomUUID());
  const report = (error: unknown) => {
    showStatusToast({
      id: "chat-pending-work",
      tone: "danger",
      title: t("chatComposer.pendingFailed"),
      body: errorMessage(error),
    });
  };
  const current = Atom.make((get) => {
    const selected = get(target);
    const identity = get(scope);
    const observer = new QueryObserver<PendingWork, Error>(client, {
      ...composerReadOptions,
      staleTime: 0,
      queryKey: [
        "chat-composer-pending",
        owner,
        identity,
        selected.kind === "session" ? selected.sessionID : null,
      ],
      queryFn:
        selected.kind === "session" ? async () => services.api.chat.listPendingWork(selected) : skipToken,
    });
    return { observer, read: queryAtom(observer) };
  });
  const read = Atom.make((get) => get(get(current).read));
  const refresh = Atom.fn<undefined>()(
    (_, get) =>
      Effect.gen(function* () {
        if (get(target).kind !== "session") return;
        yield* Effect.tryPromise(async () => client.fetchQuery(get(current).observer.options)).pipe(
          Effect.ignore,
        );
      }),
    { concurrent: true },
  );
  const hydrate = Atom.fn<undefined>()(
    (_, get) =>
      Effect.sync(() => {
        if (get(target).kind === "session") get.set(scope, crypto.randomUUID());
      }),
    { concurrent: true },
  );
  const stopKey = ["chat-composer-stop", owner];
  const stopObserver = new MutationObserver(client, {
    ...composerRequestOptions,
    mutationKey: stopKey,
    mutationFn: async (session: ChatSessionTarget) => services.api.chat.stop(session),
    onError: report,
  });
  const stopping = queryAtom(stopObserver);
  const stopPending = mutationPendingAtom(client, { mutationKey: stopKey });
  const stop = Atom.fn<{ onSettled: () => void }>()(
    ({ onSettled }, get) =>
      Effect.gen(function* () {
        const selected = get(target);
        if (selected.kind !== "session") return;
        yield* Effect.tryPromise(async () => stopObserver.mutate(selected, { onSettled })).pipe(
          Effect.ignore,
        );
      }),
    { concurrent: true },
  );
  const discardKey = ["chat-composer-discard", owner];
  const discardOptions = {
    ...composerRequestOptions,
    mutationKey: discardKey,
    mutationFn: async (input: DiscardRequest) =>
      services.api.chat.removePendingWork(input.target, input.item),
    onSuccess: (
      restoration: Awaited<ReturnType<AppServices["api"]["chat"]["removePendingWork"]>>,
      input: DiscardRequest,
    ) => {
      input.restore({ text: restoration.canonicalInput, direction: "append" });
      input.refresh();
    },
    onError: report,
  };
  const discardObserver = new MutationObserver(client, discardOptions);
  const discarding = queryAtom(discardObserver);
  const discardPending = Atom.family((identity: string) =>
    mutationPendingAtom(client, { mutationKey: [...discardKey, identity] }),
  );
  const discard = Atom.fn<Omit<DiscardRequest, "target">>()(
    (input, get) =>
      Effect.gen(function* () {
        const selected = get(target);
        if (selected.kind !== "session") return;
        discardObserver.setOptions({
          ...discardOptions,
          mutationKey: [...discardKey, input.item.toJSONValue()],
        });
        yield* Effect.tryPromise(async () => discardObserver.mutate({ ...input, target: selected })).pipe(
          Effect.ignore,
        );
      }),
    { concurrent: true },
  );
  return {
    read,
    refresh,
    hydrate,
    stop,
    stopping,
    stopPending,
    discard,
    discarding,
    discardPending,
  } as const;
}
export type ComposerPendingViewModel = ReturnType<typeof createComposerPendingViewModel>;
