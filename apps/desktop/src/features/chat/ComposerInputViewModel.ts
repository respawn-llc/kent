import { MutationObserver, type QueryClient } from "@tanstack/react-query";
import { useAtomMount, useAtomSet } from "@effect/atom-react";
import * as Effect from "effect/Effect";
import * as Atom from "effect/unstable/reactivity/Atom";
import type { TFunction } from "i18next";

import {
  errorMessage,
  type ChatSettingsTarget,
  type ChatMutationTarget,
  type InitialChatSettings,
  type ChatNotAcceptedReason,
} from "@/api";
import { mutationPendingAtom, queryAtom, type AppServices } from "@/app-facade";
import { showStatusToast } from "@/ui";
import {
  composerRequestOptions,
  type ComposerDraftViewModel,
  type ComposerTextRestoration,
} from "./ComposerDraftViewModel";
import {
  dispatchComposerCommand,
  resolveComposerCommand,
  type ComposerCommand,
  type ComposerCommandResolution,
  type ComposerCommandResult,
} from "./composerCommands";

export type ComposerSubmission<Kind extends ChatSettingsTarget["kind"] = ChatSettingsTarget["kind"]> =
  | Readonly<{ kind: "loading" }>
  | Readonly<{ kind: "failed"; error: unknown }>
  | (Kind extends "new_chat"
      ? Readonly<{ kind: "ready"; initialSettings: InitialChatSettings }>
      : Readonly<{ kind: "ready" }>);
type CompletedInput = Exclude<ComposerCommandResult, { kind: "local" }>;
type SubmissionCallbacks = Readonly<{
  restore(input: ComposerTextRestoration): void;
  accepted(sessionID: string): void;
  failed(): void;
  delivered?(result: CompletedInput): void;
}>;
type Request = SubmissionCallbacks &
  Readonly<{
    target: ChatMutationTarget;
    original: string | null;
    intent: "send" | "queue";
    command: Exclude<ComposerCommandResolution, { kind: "unknown-prompt" | "unavailable" }>;
  }>;
export type ComposerInputActivation = SubmissionCallbacks &
  Readonly<{
    intent: "send" | "queue";
    selectedToken: string | null;
    commands: readonly ComposerCommand[];
    source: "editor" | "compact-button";
  }>;

export function createComposerInputViewModel({
  services,
  client,
  target,
  submission,
  draft,
  t,
}: Readonly<{
  services: AppServices;
  client: QueryClient;
  target: Atom.Atom<ChatSettingsTarget>;
  submission: Atom.Atom<ComposerSubmission>;
  draft: ComposerDraftViewModel;
  t: TFunction;
}>) {
  const key = ["chat-composer-input", crypto.randomUUID()];
  const observer = new MutationObserver(client, {
    ...composerRequestOptions,
    mutationKey: key,
    mutationFn: async (input: Request) =>
      dispatchComposerCommand(services.api.chat, input.target, input.command, input.intent),
    onSuccess: (result, input) => {
      if (observer.hasListeners()) complete(result, input, t);
    },
    onError: (error, input) => {
      if (observer.hasListeners()) {
        if (input.original !== null) input.restore({ text: input.original, direction: "append" });
        input.failed();
      }
      showStatusToast({
        id: "chat-composer-input",
        tone: "danger",
        title: t("chatComposer.submitFailed"),
        body: errorMessage(error),
      });
    },
  });
  const result = queryAtom(observer);
  const pending = mutationPendingAtom(client, { mutationKey: key });
  const submit = Atom.fn<ComposerInputActivation>()(
    (input, get) =>
      Effect.gen(function* () {
        const ready = get(submission);
        if (input.source === "editor" && (!get(draft.read).isSuccess || ready.kind !== "ready")) return;
        const requestTarget = mutationTarget(get(target), ready);
        if (requestTarget === null) return;
        const { text, original, commandText } = submissionText(input, get(draft.text));
        if (text.trim().length === 0) return;
        const command = resolveComposerCommand(commandText, input.commands);
        if (command.kind === "unknown-prompt") {
          showStatusToast({
            id: "chat-composer-input",
            tone: "danger",
            title: t("chatComposer.submitFailed"),
            body: t("chatComposer.rejections.prompt_command_not_found"),
          });
          return;
        }
        if (command.kind === "unavailable") {
          get.set(draft.edit, "");
          command.notify();
          return;
        }
        if (original !== null) yield* get.setResult(draft.submit, undefined);
        yield* Effect.tryPromise(async () =>
          observer.mutate({ ...input, target: requestTarget, original, command }),
        ).pipe(Effect.ignore);
      }),
    { concurrent: true },
  );
  return { result, pending, submit } as const;
}

function submissionText(input: ComposerInputActivation, draft: string) {
  return input.source === "compact-button"
    ? { text: "/compact", original: null, commandText: "/compact" }
    : { text: draft, original: draft, commandText: input.selectedToken ?? draft };
}

function mutationTarget(
  target: ChatSettingsTarget,
  submission: ComposerSubmission,
): ChatMutationTarget | null {
  if (target.kind === "session") return target;
  if (submission.kind !== "ready") return null;
  if (!("initialSettings" in submission)) throw new Error("Ready New Chat requires initial settings.");
  return { ...target, initialSettings: submission.initialSettings };
}

function complete(result: ComposerCommandResult, input: Request, t: TFunction) {
  if ("kind" in result) return;
  if (result.outcome.kind === "accepted") {
    if (input.target.kind === "new_chat") input.delivered?.(result);
    input.accepted(result.sessionID);
    const diagnostic = result.outcome.diagnostic;
    if (diagnostic !== null)
      showStatusToast({
        id: "chat-composer-input",
        tone: "warning",
        title: t("chatComposer.acceptedDiagnostic"),
        body: [t(`chatComposer.diagnostics.${diagnostic.kind}`), diagnostic.operation, diagnostic.cause]
          .filter((value) => value !== null)
          .join("\n"),
      });
  } else {
    if (input.original !== null) input.restore({ text: input.original, direction: "append" });
    if (input.target.kind === "new_chat") input.delivered?.(result);
    showStatusToast({
      id: "chat-composer-input",
      tone: "danger",
      title: t("chatComposer.submitFailed"),
      body: [
        t(`chatComposer.rejections.${result.outcome.reason.kind}`),
        ...rejectionDetails(result.outcome.reason),
      ].join("\n"),
    });
  }
}
function rejectionDetails(reason: ChatNotAcceptedReason): readonly string[] {
  if ("command" in reason) return reason.command === null ? [] : [reason.command];
  if (reason.kind === "internal_failure")
    return [reason.operation, reason.cause].filter((value) => value !== null);
  return [];
}

export type ComposerInputViewModel = ReturnType<typeof createComposerInputViewModel>;
export function useComposerInputActions(model: ComposerInputViewModel) {
  useAtomMount(model.result);
  return { submit: useAtomSet(model.submit) };
}
