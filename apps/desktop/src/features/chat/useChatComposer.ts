import { useCallback, useEffect, useMemo, useState } from "react";
import { useAtomValue } from "@effect/atom-react";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import type { ChatSettingsTarget } from "@/api";
import { useAppServices, useDebouncedText } from "@/app-facade";
import {
  compactCommand,
  composerSuggestions,
  type ComposerCommand,
  type ComposerCommandResult,
} from "./composerCommands";
import { useComposerPendingWork } from "./useComposerPendingWork";
import { createComposerDraftViewModel, useComposerDraftActions } from "./ComposerDraftViewModel";
import {
  createComposerInputViewModel,
  useComposerInputActions,
  type ComposerSubmission,
} from "./ComposerInputViewModel";
import { createComposerPendingViewModel } from "./ComposerPendingViewModel";
import { chatCompactionFeedback } from "./chatCompactionFeedback";

export type { ComposerSubmission } from "./ComposerInputViewModel";
type DraftState = Readonly<{ kind: "loading" | "ready" }> | Readonly<{ kind: "failed"; error: unknown }>;
export type ChatComposerOptions = Readonly<
  (
    | (Extract<ChatSettingsTarget, { kind: "session" }> & { submission?: ComposerSubmission<"session"> })
    | (Extract<ChatSettingsTarget, { kind: "new_chat" }> & { submission?: ComposerSubmission<"new_chat"> })
  ) & {
    onDeliveredSession?(result: Exclude<ComposerCommandResult, { kind: "local" }>): void;
    commands?: readonly ComposerCommand[];
  }
>;
const noCommands: readonly ComposerCommand[] = [];

export function useChatComposer(options: ChatComposerOptions) {
  const {
    submission = { kind: "loading" },
    onDeliveredSession,
    commands: additionalCommands = noCommands,
  } = options;
  const services = useAppServices();
  const client = useQueryClient();
  const { t } = useTranslation();
  const commands = [
    compactCommand(services.api.chat, client, t("chatComposer.context.compact")),
    ...additionalCommands,
  ];
  const [model] = useState(() => {
    const { projectID, workspace } = options;
    const target: ChatSettingsTarget =
      options.kind === "session"
        ? { kind: "session", projectID, workspace, sessionID: options.sessionID }
        : { kind: "new_chat", projectID, workspace };
    const draft = createComposerDraftViewModel({ services, client, target, t });
    const input = createComposerInputViewModel({ services, client, target, draft, t });
    const pending = createComposerPendingViewModel({ services, client, target, t });
    return { target, draft, input, pending } as const;
  });
  const draftActions = useComposerDraftActions(model.draft);
  const inputActions = useComposerInputActions(model.input);
  const inputPending = useAtomValue(model.input.pending);
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
      ...(model.target.kind === "session" ? chatCompactionFeedback(services, model.target) : {}),
    }),
    [pending.observation, services, model.target],
  );
  const [picker, setPicker] = useState<
    Readonly<{ kind: "dismissed" }> | Readonly<{ kind: "selecting"; token: string | null }>
  >({ kind: "selecting", token: null });
  const suggestions = picker.kind === "dismissed" ? [] : composerSuggestions(text, commands);
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
  const canSubmit = draft.kind === "ready" && submission.kind === "ready" && text.trim().length > 0;
  function submit(intent: "send" | "queue", source: "editor" | "compact-button" = "editor") {
    inputActions.submit({
      intent,
      source,
      submission,
      commands,
      selectedToken: selectedCommand?.token ?? null,
      restore: restoreAction,
      accepted: pending.returnedSession,
      ...(onDeliveredSession === undefined ? {} : { delivered: onDeliveredSession }),
    });
    if (source === "editor") setPicker({ kind: "selecting", token: null });
  }
  return {
    target: model.target,
    text,
    draft,
    submission,
    canSubmit,
    inputPending,
    submit,
    compact: () => {
      submit("send", "compact-button");
    },
    suggestions,
    selectedCommand,
    pending,
    observation,
    edit: (value: string) => {
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
    restore,
  };
}
