import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useAtomValue } from "@effect/atom-react";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import { useAppServices, useDebouncedText } from "@/app-facade";
import {
  compactCommand,
  composerSuggestions,
  tokenizeComposerCommand,
  type ComposerCommand,
  type ComposerCommandResult,
} from "./composerCommands";
import { useComposerPendingWork } from "./useComposerPendingWork";
import { chatCompactionFeedback } from "./chatCompactionFeedback";
import { useComposerDraftActions } from "./ComposerDraftViewModel";
import { useComposerInputActions } from "./ComposerInputViewModel";
import type { ChatComposerViewModel } from "./ChatComposerViewModel";
import type { QuerySnapshot } from "@/app-facade";
import type { QueryObserverResult } from "@tanstack/react-query";
import type { ChatApi, ChatMutationTarget } from "@/api";

export type { ComposerSubmission } from "./ComposerInputViewModel";
type DraftState = Readonly<{ kind: "loading" | "ready" }> | Readonly<{ kind: "failed"; error: unknown }>;
export type ChatComposerOptions = Readonly<{
  model: ChatComposerViewModel;
  onDeliveredSession?(
    result: Exclude<ComposerCommandResult, { kind: "local" }>,
    target: ChatMutationTarget,
  ): void;
  commands?: readonly ComposerCommand[];
  catalog?: QuerySnapshot<QueryObserverResult<Awaited<ReturnType<ChatApi["getCommandCatalog"]>>>>;
  retryCatalog?(): void;
}>;
const noCommands: readonly ComposerCommand[] = [];

export function useChatComposer(options: ChatComposerOptions) {
  const { model, onDeliveredSession, commands: additionalCommands = noCommands } = options;
  const services = useAppServices();
  const client = useQueryClient();
  const { t } = useTranslation();
  const commands = [
    compactCommand(services.api.chat, client, t("chatComposer.context.compact")),
    ...additionalCommands,
  ];
  const submission = useAtomValue(model.submission);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const target = useAtomValue(model.target);
  const draftActions = useComposerDraftActions(model.draft);
  const inputActions = useComposerInputActions(model.input);
  const inputPending = useAtomValue(model.input.pending);
  const navigationPending = useAtomValue(model.draft.navigationPending);
  const text = useAtomValue(model.draft.text);
  const draftRead = useAtomValue(model.draft.read);
  const draft: DraftState = draftRead.isSuccess
    ? { kind: "ready" }
    : draftRead.isError
      ? { kind: "failed", error: draftRead.error }
      : { kind: "loading" };
  const edit = draftActions.edit;
  const restoreAction = draftActions.restore;
  const restore = useCallback(
    (incoming: string, direction: "append" | "prepend") => {
      restoreAction({ text: incoming, direction });
    },
    [restoreAction],
  );
  const pending = useComposerPendingWork(model.pending, restoreAction);
  const observation = useMemo(
    () => ({
      ...pending.observation,
      ...(target.kind === "session" ? chatCompactionFeedback(services, target) : {}),
    }),
    [pending.observation, services, target],
  );
  const [picker, setPicker] = useState<
    Readonly<{ kind: "dismissed" }> | Readonly<{ kind: "selecting"; token: string | null }>
  >({ kind: "selecting", token: null });
  const suggestions = picker.kind === "dismissed" ? [] : composerSuggestions(text, commands);
  const pickerOpen = commandPickerOpen(picker.kind, suggestions, text, options.catalog);
  const selectedCommand =
    (picker.kind === "selecting"
      ? suggestions.find((command) => command.token === picker.token)
      : undefined) ??
    suggestions[0] ??
    null;
  const debounced = useDebouncedText(text, 300);
  const saveDraft = draftActions.save;
  useEffect(() => {
    if (draft.kind === "ready" && debounced === text) saveDraft(debounced);
  }, [saveDraft, draft.kind, debounced, text]);
  const canSubmit =
    !navigationPending && draft.kind === "ready" && submission.kind === "ready" && text.trim().length > 0;
  function submit(intent: "send" | "queue", source: "editor" | "compact-button" = "editor") {
    if (navigationPending) return;
    inputActions.submit({
      intent,
      source,
      commands,
      selectedToken: selectedCommand?.token ?? null,
      restore: (input) => {
        if (mounted.current) restoreAction(input);
      },
      accepted: () => {
        if (mounted.current) pending.refresh();
      },
      failed: () => {
        if (mounted.current) draftActions.resume(undefined);
      },
      ...(onDeliveredSession === undefined
        ? {}
        : {
            delivered: (
              result: Exclude<ComposerCommandResult, { kind: "local" }>,
              target: ChatMutationTarget,
            ) => {
              if (mounted.current) onDeliveredSession(result, target);
            },
          }),
    });
    if (source === "editor") setPicker({ kind: "selecting", token: null });
  }
  return {
    target,
    text,
    draft,
    submission,
    canSubmit,
    inputPending,
    navigationPending,
    submit,
    compact: () => {
      submit("send", "compact-button");
    },
    suggestions,
    pickerOpen,
    catalog: options.catalog,
    retryCatalog: options.retryCatalog,
    selectedCommand,
    pending,
    observation,
    edit: (value: string) => {
      if (navigationPending) return;
      edit(value);
      setPicker({ kind: "selecting", token: null });
    },
    dismissPicker: () => {
      setPicker({ kind: "dismissed" });
    },
    selectCommand: (token: string) => {
      setPicker({ kind: "selecting", token });
    },
    moveCommand: (direction: -1 | 1) => {
      const index = suggestions.findIndex((command) => command.token === selectedCommand?.token);
      const next = suggestions[(index + direction + suggestions.length) % suggestions.length];
      if (next !== undefined) setPicker({ kind: "selecting", token: next.token });
    },
    retryDraft: draftActions.retry,
    adoptDraft: draftActions.adopt,
    beginFirstAction: draftActions.begin,
    resumeNewChat: draftActions.resume,
    flushDraft: async () => draftActions.flush(undefined),
    restore,
  };
}

function commandPickerOpen(
  mode: "dismissed" | "selecting",
  suggestions: readonly ComposerCommand[],
  text: string,
  catalog: ChatComposerOptions["catalog"],
): boolean {
  if (mode === "dismissed") return false;
  if (suggestions.length > 0) return true;
  const invocation = tokenizeComposerCommand(text);
  return (
    invocation.token.startsWith("/") &&
    invocation.separatorWhitespace.length === 0 &&
    (catalog?.isFetching === true || catalog?.isError === true)
  );
}
