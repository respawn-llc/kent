import { useCallback, useEffect, useEffectEvent, useState } from "react";
import { useTranslation } from "react-i18next";

import {
  errorMessage,
  type ChatSettingsTarget,
  type ChatMutationTarget,
  type ChatNotAcceptedReason,
} from "@/api";
import {
  readBrowserStorage,
  recoverOrThrowDebugFailure,
  useAppServices,
  useConnectionSnapshot,
  useDebouncedText,
  writeBrowserStorage,
} from "@/app-facade";
import { showStatusToast } from "@/ui";
import { mergeComposerText } from "./composerText";
import {
  composerSuggestions,
  dispatchComposerCommand,
  resolveComposerCommand,
  type ComposerCommand,
  type ComposerCommandResult,
} from "./composerCommands";
import { useComposerPendingWork } from "./useComposerPendingWork";

type DraftState = Readonly<{ kind: "loading" | "ready" }> | Readonly<{ kind: "failed"; error: unknown }>;
const newChatDraftKey = "desktop.newChatDraft";

export type ComposerSubmission =
  | Readonly<{ kind: "loading" }>
  | Readonly<{ kind: "failed"; error: unknown }>
  | Readonly<{ kind: "ready"; target: ChatMutationTarget }>;
export type ChatComposerOptions = Readonly<{
  target: ChatSettingsTarget;
  submission?: ComposerSubmission;
  onDeliveredSession?(result: Exclude<ComposerCommandResult, { kind: "local" }>): void;
  commands?: readonly ComposerCommand[];
}>;
const noCommands: readonly ComposerCommand[] = [];

export function useChatComposer({
  target,
  submission = { kind: "loading" },
  onDeliveredSession,
  commands = noCommands,
}: ChatComposerOptions) {
  const { api, logger } = useAppServices();
  const { t } = useTranslation();
  const connection = useConnectionSnapshot();
  const [text, edit] = useState("");
  const restore = useCallback((incoming: string, direction: "append" | "prepend") => {
    edit((current) => mergeComposerText(current, incoming, direction));
  }, []);
  const pending = useComposerPendingWork(target, restore);
  const [draft, setDraft] = useState<DraftState>({ kind: "loading" });
  const [readAttempt, retryDraft] = useState(0);
  const [inputRequests, setInputRequests] = useState(0);
  const [localPersistence, setLocalPersistence] = useState<"editing" | "destination-owned">("editing");
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
  const notify = useEffectEvent((error: unknown) => {
    showStatusToast({
      id: "chat-composer-draft",
      tone: "danger",
      title: t("chatComposer.saveFailed"),
      body: errorMessage(error),
    });
  });
  const storageFailure = useEffectEvent(async (error: unknown) =>
    recoverOrThrowDebugFailure({
      error,
      logger,
      message: "New Chat draft storage failed.",
      context: {},
      recover: () => {
        /* New Chat storage is best-effort; the editor remains authoritative. */
      },
    }),
  );
  useEffect(() => {
    let disposed = false;
    async function load() {
      try {
        let saved: string;
        if (target.kind === "session") {
          saved = await api.chat.getDraft(target);
        } else {
          const result = readBrowserStorage("local", newChatDraftKey);
          if (!result.ok) {
            void storageFailure(result.error);
            saved = "";
          } else saved = result.value ?? "";
        }
        if (disposed) return;
        edit((current) => mergeComposerText(current, saved, "prepend"));
        setDraft({ kind: "ready" });
      } catch (error) {
        if (!disposed) setDraft({ kind: "failed", error });
      }
    }
    void load();
    return () => {
      disposed = true;
    };
  }, [api.chat, target, readAttempt]);
  useEffect(() => {
    if (draft.kind !== "ready" || debounced !== text || localPersistence === "destination-owned") return;
    if (target.kind === "session") {
      void api.chat.persistDraft(target, debounced).catch(notify);
    } else {
      const result = writeBrowserStorage("local", newChatDraftKey, debounced);
      if (!result.ok) void storageFailure(result.error);
    }
  }, [api.chat, target, draft.kind, debounced, text, localPersistence]);
  const canSubmit =
    connection.phase === "connected" &&
    draft.kind === "ready" &&
    submission.kind === "ready" &&
    text.trim().length > 0;
  async function submit(intent: "send" | "queue") {
    if (!canSubmit) return;
    const submitted = text;
    const requestTarget = submission.target;
    const resolved = resolveComposerCommand(selectedCommand?.token ?? submitted, commands);
    if (resolved.kind === "unknown-prompt") {
      showStatusToast({
        id: "chat-composer-input",
        tone: "danger",
        title: t("chatComposer.submitFailed"),
        body: t("chatComposer.rejections.prompt_command_not_found"),
      });
      return;
    }
    if (requestTarget.kind === "new_chat") setLocalPersistence("destination-owned");
    edit("");
    setPicker({ kind: "selecting", token: null });
    setInputRequests((count) => count + 1);
    let result: ComposerCommandResult;
    try {
      result = await dispatchComposerCommand(api.chat, requestTarget, resolved, intent);
    } catch (error) {
      edit((current) => mergeComposerText(current, submitted, "append"));
      showStatusToast({
        id: "chat-composer-input",
        tone: "danger",
        title: t("chatComposer.submitFailed"),
        body: errorMessage(error),
      });
      return;
    } finally {
      setInputRequests((count) => count - 1);
    }
    if ("kind" in result) return;
    if (result.outcome.kind === "accepted") {
      pending.refresh({ ...requestTarget, sessionID: result.sessionID });
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
      edit((current) => mergeComposerText(current, submitted, "append"));
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
    if (requestTarget.kind === "new_chat") onDeliveredSession?.(result);
  }
  return {
    target,
    text,
    draft,
    submission,
    canSubmit,
    inputRequests,
    submit,
    suggestions,
    selectedCommand,
    pending,
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
    retryDraft: () => {
      setDraft({ kind: "loading" });
      retryDraft((attempt) => attempt + 1);
    },
    restore,
  };
}

function rejectionDetails(reason: ChatNotAcceptedReason): readonly string[] {
  if ("command" in reason) return reason.command === null ? [] : [reason.command];
  if (reason.kind === "internal_failure")
    return [reason.operation, reason.cause].filter((value) => value !== null);
  return [];
}
