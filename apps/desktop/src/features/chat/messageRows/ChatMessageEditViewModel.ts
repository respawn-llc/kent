import { useAtomMount, useAtomSet } from "@effect/atom-react";
import { MutationObserver, type QueryClient } from "@tanstack/react-query";
import * as Effect from "effect/Effect";
import * as Atom from "effect/unstable/reactivity/Atom";
import type { TFunction } from "i18next";

import type { ChatApi, ChatForkEditInput, ChatSessionTarget } from "@/api";
import { queryAtom, type StatusController } from "@/app-facade";
import { settingsOperationFailureMessage } from "../chatSettingsPresentation";
import type { ChatUserMessageItem } from "./ChatUserMessage";

export type ChatMessageEditHandoff = Readonly<{ sessionID: string; draft: string }>;
export type ChatMessageEditActivation = Readonly<{
  item: ChatUserMessageItem;
  draft: string;
  onSuccess(handoff: ChatMessageEditHandoff): void;
}>;

export function createChatMessageEditViewModel({
  api,
  client,
  target,
  t,
  push,
}: Readonly<{
  api: ChatApi;
  client: QueryClient;
  target: ChatSessionTarget;
  t: TFunction;
  push: StatusController["push"];
}>) {
  const observer = new MutationObserver(client, {
    mutationFn: async (
      input: Readonly<{
        target: ChatSessionTarget;
        fork: ChatForkEditInput;
        onSuccess: ChatMessageEditActivation["onSuccess"];
      }>,
    ) => api.forkEdit(input.target, input.fork),
    retry: false,
    networkMode: "always",
    onSuccess: (sessionID, input) => {
      input.onSuccess({ sessionID, draft: input.fork.initialInput });
    },
    onError: (error) => {
      push({
        id: "chat-message-edit-failed",
        title: t("chatTranscript.editFailed"),
        body: settingsOperationFailureMessage(t, error),
        tone: "danger",
      });
    },
  });
  const request = queryAtom(observer);
  const activate = Atom.fn<ChatMessageEditActivation>()(
    (input) =>
      Effect.gen(function* () {
        const rollbackTargetID = input.item.value.RollbackTargetID;
        if (rollbackTargetID == null || observer.getCurrentResult().isPending) return;
        yield* Effect.tryPromise(async () =>
          observer.mutate({
            target,
            fork: { rollbackTargetID, initialInput: combineDraft(input.item.value.Text, input.draft) },
            onSuccess: input.onSuccess,
          }),
        ).pipe(Effect.ignore);
      }),
    { concurrent: true },
  );
  return { request, activate } as const;
}

export type ChatMessageEditViewModel = ReturnType<typeof createChatMessageEditViewModel>;

function combineDraft(original: string, draft: string): string {
  return draft === "" ? original : `${original}\n\n${draft}`;
}

export function useChatMessageEditActions(
  model: ChatMessageEditViewModel,
  serverMutationAvailability: "available" | "disconnected",
) {
  useAtomMount(model.request);
  const activate = useAtomSet(model.activate);
  return {
    activate(input: ChatMessageEditActivation) {
      if (serverMutationAvailability === "available") activate(input);
    },
  };
}
