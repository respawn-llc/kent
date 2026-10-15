import { MutationObserver, type QueryClient } from "@tanstack/react-query";
import { useAtomMount, useAtomSet } from "@effect/atom-react";
import * as Effect from "effect/Effect";
import * as Atom from "effect/reactivity/Atom";
import type { TFunction } from "i18next";

import {
  type ChatSettingsTarget,
  type ChatMutationTarget,
  type InitialChatSettings,
  type ChatNotAcceptedReason,
  type PendingWorkItemID,
  parsePendingWorkItemID,
  type ChatRuntimeActivity,
  type ChatInputMutationResult,
} from "@/api";
import { mutationPendingAtom, queryAtom, type AppServices } from "@/app-facade";
import { showStatusToast } from "@/ui";
import { chatOperationFailureMessage } from "./chatSettingsPresentation";
import {
  composerRequestOptions,
  type ComposerDraftViewModel,
  type ComposerTextRestoration,
} from "./ComposerDraftViewModel";
import {
  dispatchComposerCommand,
  resolveComposerCommand,
  type ComposerCommand,
  type PreparedComposerCommand,
  type ComposerCommandResolution,
  type ComposerCommandResult,
} from "./composerCommands";
import type { OutgoingInputPresentation } from "./OutgoingInputPresentation";

export type ComposerSubmission<Kind extends ChatSettingsTarget["kind"] = ChatSettingsTarget["kind"]> =
  | Readonly<{ kind: "loading" }>
  | Readonly<{ kind: "failed"; error: unknown }>
  | (Kind extends "new_chat"
      ? Readonly<{ kind: "ready"; initialSettings: InitialChatSettings }>
      : Readonly<{ kind: "ready" }>);
type CompletedInput = Exclude<ComposerCommandResult, { kind: "local" }>;
type SubmissionCallbacks = Readonly<{
  restore(input: ComposerTextRestoration): void;
  accepted(sessionID: string, submittedText: string): void;
  failed(): void;
  delivered?(result: CompletedInput, target: ChatMutationTarget): void | Promise<void>;
  prepared?(queueItemID: PendingWorkItemID): void;
  rejected?(queueItemID: PendingWorkItemID): void;
}>;
type Request = SubmissionCallbacks &
  Readonly<{
    target: ChatMutationTarget;
    original: string | null;
    submittedText: string;
    intent: "send" | "queue";
    command: PreparedComposerCommand;
    resolveOutgoing(outcome: ChatInputMutationResult["outcome"] | null): boolean;
  }>;
export type ComposerInputActivation = SubmissionCallbacks &
  Readonly<{
    intent: "send" | "queue";
    selectedToken: string | null;
    commands: readonly ComposerCommand[];
    source: "editor" | "compact-button";
    activity?: ChatRuntimeActivity;
  }>;

export function createComposerInputViewModel({
  services,
  client,
  target,
  submission,
  draft,
  outgoing,
  t,
}: Readonly<{
  services: AppServices;
  client: QueryClient;
  target: Atom.Atom<ChatSettingsTarget | null>;
  submission: Atom.Atom<ComposerSubmission>;
  draft: ComposerDraftViewModel;
  outgoing: OutgoingInputPresentation;
  t: TFunction;
}>) {
  const key = ["chat-composer-input", crypto.randomUUID()];
  const observer = new MutationObserver(client, {
    ...composerRequestOptions,
    mutationKey: key,
    mutationFn: async (input: Request) =>
      dispatchComposerCommand(services.api.chat, input.target, input.command, input.intent),
    onSuccess: async (result, input) => {
      if (input.command.kind === "input" && !("kind" in result)) {
        if (result.outcome.kind === "accepted" && !("disposition" in result.outcome))
          throw new Error("Ordinary input acceptance requires an admission disposition.");
        const confirmed = input.resolveOutgoing(result.outcome);
        if (confirmed && result.outcome.kind === "not_accepted") return;
      }
      if (input.command.kind === "input" && !("kind" in result) && result.outcome.kind === "not_accepted")
        input.rejected?.(input.command.queueItemID);
      if (observer.hasListeners()) await complete(result, input, t);
    },
    onError: (error, input) => {
      if (input.resolveOutgoing(null)) return;
      if (input.command.kind === "input") input.rejected?.(input.command.queueItemID);
      if (observer.hasListeners()) {
        if (input.command.kind !== "input" && input.original !== null)
          input.restore({ text: input.original, direction: "append" });
        input.failed();
      }
      showStatusToast({
        id: "chat-composer-input",
        tone: "danger",
        title: t("chatComposer.submitFailed"),
        body: chatOperationFailureMessage(t, error, "input"),
      });
    },
  });
  const requestResult = queryAtom(observer);
  const result = Atom.make((get) => {
    get.mount(outgoing.completion);
    return get(requestResult);
  });
  const pending = mutationPendingAtom(client, { mutationKey: key });
  const admit = Effect.fnUntraced(function* (
    input: ComposerInputActivation,
    get: Atom.FnContext,
    command: Exclude<ComposerCommandResolution, { kind: "unknown-prompt" }>,
    original: string | null,
  ) {
    if (command.kind === "unavailable") {
      yield* get.setResult(draft.edit, "");
      if (command.draft === "restore" && original !== null)
        input.restore({ text: original, direction: "append" });
      command.notify();
      return null;
    }
    if (command.kind === "input" && !get(outgoing.canPrepare)) {
      showStatusToast({
        id: "chat-composer-input",
        tone: "danger",
        title: t("chatComposer.submitFailed"),
        body: t("chatComposer.rejections.pending_work_capacity"),
      });
      return null;
    }
    return prepareSubmission(command, input);
  });
  const submit = Atom.fn<ComposerInputActivation>()(
    (input, get) =>
      Effect.gen(function* () {
        const ready = get(submission);
        if (
          !canActivate({
            restricted: get(draft.interactionRestricted) || get(outgoing.heldReadonly),
            source: input.source,
            loaded: get(draft.read).isSuccess,
            ready,
          })
        )
          return;
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
        const prepared = yield* admit(input, get, command, original);
        if (prepared === null) return;
        const outgoingCompletion = get(outgoing.completion);
        if (prepared.kind === "input") {
          yield* get.setResult(draft.begin, undefined);
          const admitted = yield* get.setResult(outgoing.apply, {
            kind: "prepare",
            inputID: prepared.queueItemID,
            text,
            intent: input.intent,
            optimistic: predictsIdle(requestTarget, input.activity),
          });
          if (!admitted) return;
        } else if (original !== null) get.set(draft.submit, undefined);
        const request = Effect.tryPromise(async () =>
          observer.mutate({
            ...input,
            target: requestTarget,
            original,
            submittedText: text,
            command: prepared,
            resolveOutgoing: (outcome) => {
              if (!observer.hasListeners() || prepared.kind !== "input") return false;
              return outgoingCompletion.resolve(prepared.queueItemID, outcome);
            },
          }),
        ).pipe(Effect.ignore);
        if (prepared.kind === "input") {
          const release = Effect.gen(function* () {
            yield* Effect.sleep("2 seconds");
            outgoingCompletion.release(prepared.queueItemID);
          });
          yield* Effect.forkChild(release);
          yield* request;
        } else yield* request;
      }),
    { concurrent: true },
  );
  return { result, pending, submit } as const;
}

function canActivate(
  input: Readonly<{
    restricted: boolean;
    source: ComposerInputActivation["source"];
    loaded: boolean;
    ready: ComposerSubmission;
  }>,
) {
  return !input.restricted && (input.source !== "editor" || (input.loaded && input.ready.kind === "ready"));
}

function predictsIdle(target: ChatMutationTarget, activity: ChatRuntimeActivity | undefined) {
  return target.kind === "session" && activity?.state === "registered_idle";
}

function prepareSubmission(
  command: Exclude<ComposerCommandResolution, { kind: "unknown-prompt" | "unavailable" }>,
  callbacks: SubmissionCallbacks,
): PreparedComposerCommand {
  if (command.kind === "direct") return command;
  const queueItemID = parsePendingWorkItemID(crypto.randomUUID());
  callbacks.prepared?.(queueItemID);
  return { ...command, queueItemID };
}

function submissionText(input: ComposerInputActivation, draft: string) {
  return input.source === "compact-button"
    ? { text: "/compact", original: null, commandText: "/compact" }
    : { text: draft, original: draft, commandText: input.selectedToken ?? draft };
}

function mutationTarget(
  target: ChatSettingsTarget | null,
  submission: ComposerSubmission,
): ChatMutationTarget | null {
  if (target === null) return null;
  if (target.kind === "session") return target;
  if (submission.kind !== "ready") return null;
  if (!("initialSettings" in submission)) throw new Error("Ready New Chat requires initial settings.");
  return { ...target, initialSettings: submission.initialSettings };
}

async function complete(result: ComposerCommandResult, input: Request, t: TFunction) {
  if ("kind" in result) return;
  const delivers =
    input.target.kind === "new_chat" ||
    (input.command.kind === "input" && input.command.activation.kind === "command");
  if (result.outcome.kind === "accepted") {
    if (delivers) await input.delivered?.(result, input.target);
    input.accepted(result.sessionID, input.submittedText);
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
    if (input.command.kind !== "input" && input.original !== null)
      input.restore({ text: input.original, direction: "append" });
    if (delivers) await input.delivered?.(result, input.target);
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
