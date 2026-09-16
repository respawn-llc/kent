import { useCallback, useEffect, useRef, useState } from "react";
import { useAtomValue } from "@effect/atom-react";
import { useDebouncedText } from "@/app-facade";
import { composerSuggestions, type ComposerCommand, type ComposerCommandResult } from "./composerCommands";
import { useComposerPendingWork } from "./useComposerPendingWork";
import { useComposerDraftActions } from "./ComposerDraftViewModel";
import { useComposerInputActions } from "./ComposerInputViewModel";
import type { ChatComposerViewModel } from "./ChatComposerViewModel";

export type { ComposerSubmission } from "./ComposerInputViewModel";
type DraftState = Readonly<{ kind: "loading" | "ready" }> | Readonly<{ kind: "failed"; error: unknown }>;
export type ChatComposerOptions = Readonly<{
  model: ChatComposerViewModel;
  onDeliveredSession?(result: Exclude<ComposerCommandResult, { kind: "local" }>): void;
  commands?: readonly ComposerCommand[];
}>;
const noCommands: readonly ComposerCommand[] = [];

export function useChatComposer(options: ChatComposerOptions) {
  const { model, onDeliveredSession, commands = noCommands } = options;
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
  const canSubmit =
    !navigationPending && draft.kind === "ready" && submission.kind === "ready" && text.trim().length > 0;
  function submit(intent: "send" | "queue") {
    if (navigationPending) return;
    inputActions.submit({
      intent,
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
            delivered: (result: Exclude<ComposerCommandResult, { kind: "local" }>) => {
              if (mounted.current) onDeliveredSession(result);
            },
          }),
    });
    setPicker({ kind: "selecting", token: null });
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
    suggestions,
    selectedCommand,
    pending,
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
