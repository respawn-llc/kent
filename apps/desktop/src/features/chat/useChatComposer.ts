import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useAtomValue, useAtomSet } from "@effect/atom-react";
import * as Atom from "effect/reactivity/Atom";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import { useAppServices } from "@/app-facade";
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
import { useComposerHistoryActions } from "./ComposerHistoryViewModel";
import type { ChatComposerViewModel } from "./ChatComposerViewModel";
import type { QuerySnapshot } from "@/app-facade";
import type { QueryObserverResult } from "@tanstack/react-query";
import type {
  ChatApi,
  ChatMutationTarget,
  ChatTranscriptPayloadByKind,
  PendingWorkItemID,
  ChatRuntimeActivity,
} from "@/api";
import { pendingWorkItemIDFromProto } from "@/api";
import { SharedTextMotion, showStatusToast } from "@/ui";

export type { ComposerSubmission } from "./ComposerInputViewModel";
type DraftState = Readonly<{ kind: "loading" | "ready" }> | Readonly<{ kind: "failed"; error: unknown }>;
export type ChatComposerOptions = Readonly<{
  model: ChatComposerViewModel;
  prepared?(queueItemID: PendingWorkItemID): void;
  rejected?(queueItemID: PendingWorkItemID): void;
  onDeliveredSession?(
    result: Exclude<ComposerCommandResult, { kind: "local" }>,
    target: ChatMutationTarget,
  ): void | Promise<void>;
  commands?: readonly ComposerCommand[];
  catalog?: QuerySnapshot<QueryObserverResult<Awaited<ReturnType<ChatApi["getCommandCatalog"]>>>>;
  retryCatalog?(): void;
}>;
const noCommands: readonly ComposerCommand[] = [];

export function useChatComposer(options: ChatComposerOptions) {
  const [messageMotion] = useState(() => new SharedTextMotion());
  useEffect(
    () => () => {
      messageMotion.dispose();
    },
    [messageMotion],
  );
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
  const historyActions = useComposerHistoryActions(model.history);
  const inputPending = useAtomValue(model.input.pending);
  const outgoing = useAtomValue(model.outgoing.items);
  const heldReadonly = useAtomValue(model.outgoing.heldReadonly);
  const canPrepare = useAtomValue(model.outgoing.canPrepare);
  const observeOutgoing = useAtomSet(model.outgoing.observe);
  const observeCommits = useAtomSet(model.outgoing.observeCommits);
  const authoritativePendingItems = useAtomValue(model.outgoing.pendingItems);
  const restriction = useMemo(
    () => Atom.make((get) => get(model.draft.interactionRestricted) || get(model.outgoing.heldReadonly)),
    [model],
  );
  const interactionRestricted = useAtomValue(restriction);
  const text = useAtomValue(model.draft.text);
  const draftValue = useAtomValue(model.draft.value);
  const historySelection = useAtomValue(model.draft.selection);
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
      if (interactionRestricted) return;
      restoreAction({ text: incoming, direction });
    },
    [restoreAction, interactionRestricted],
  );
  const flush = draftActions.flush;
  const flushDraft = useCallback(async () => flush(undefined), [flush]);
  const pendingWork = useComposerPendingWork(model.pending, restoreAction);
  const pending = { ...pendingWork, items: authoritativePendingItems };
  const observation = useMemo(
    () => ({
      ...pending.observation,
      onUserInputCommitted: (inputIDs: readonly string[]) => {
        observeCommits(inputIDs.map(pendingWorkItemIDFromProto));
      },
      onConnectionReplaced: (replacement: ChatTranscriptPayloadByKind["connection_replaced"]) => {
        showStatusToast({
          id: "connection-replaced",
          tone: "info",
          title: t("chatComposer.connectionReplaced"),
          body: t("chatComposer.connectionReplacedBody", {
            previous: replacement.PreviousID,
            current: replacement.CurrentID,
          }),
        });
      },
      ...(target?.kind === "session" ? chatCompactionFeedback(services, target) : {}),
    }),
    [pending.observation, services, target, t, observeCommits],
  );
  const [picker, setPicker] = useState<
    Readonly<{ kind: "dismissed" }> | Readonly<{ kind: "selecting"; token: string | null }>
  >({ kind: "selecting", token: null });
  const suggestions =
    picker.kind === "dismissed" || historySelection !== null ? [] : composerSuggestions(text, commands);
  const pickerOpen = commandPickerOpen(picker.kind, suggestions, text, options.catalog);
  const selectedCommand =
    (picker.kind === "selecting"
      ? suggestions.find((command) => command.token === picker.token)
      : undefined) ??
    suggestions[0] ??
    null;
  const saveDraft = draftActions.save;
  useEffect(() => {
    if (draft.kind !== "ready") return;
    const timer = setTimeout(() => {
      saveDraft(draftValue);
    }, 300);
    return () => {
      clearTimeout(timer);
    };
  }, [saveDraft, draft.kind, draftValue]);
  const submissionReady = submissionEnabled({ interactionRestricted, canPrepare, draft, submission });
  const canSubmit = submissionReady && text.trim().length > 0;
  function submit(
    intent: "send" | "queue",
    source: "editor" | "compact-button" = "editor",
    activity?: ChatRuntimeActivity,
  ) {
    if (interactionRestricted) return;
    inputActions.submit({
      ...(options.prepared === undefined ? {} : { prepared: options.prepared }),
      ...(options.rejected === undefined ? {} : { rejected: options.rejected }),
      intent,
      source,
      commands,
      selectedToken: selectedCommand?.token ?? null,
      ...(activity === undefined ? {} : { activity }),
      restore: (input) => {
        if (mounted.current) restoreAction(input);
      },
      accepted: (_, submittedText) => {
        if (mounted.current) {
          pending.refresh();
          historyActions.append(submittedText);
        }
      },
      failed: () => {
        if (mounted.current) draftActions.resume(undefined);
      },
      ...(onDeliveredSession === undefined
        ? {}
        : {
            delivered: async (
              result: Exclude<ComposerCommandResult, { kind: "local" }>,
              target: ChatMutationTarget,
            ) => {
              if (mounted.current) return onDeliveredSession(result, target);
            },
          }),
    });
    if (source === "editor") setPicker({ kind: "selecting", token: null });
  }
  return {
    messageMotion,
    target,
    text,
    draft,
    submission,
    canSubmit,
    submissionReady,
    inputPending,
    outgoing,
    heldReadonly,
    canPrepare,
    observeOutgoing,
    interactionRestricted,
    interactionRestriction: restriction,
    navigateHistory: historyActions.navigate,
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
      if (interactionRestricted) return;
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
    flushDraft,
    restore,
  };
}

function submissionEnabled(
  input: Readonly<{
    interactionRestricted: boolean;
    canPrepare: boolean;
    draft: DraftState;
    submission: { kind: string };
  }>,
) {
  return (
    !input.interactionRestricted &&
    input.canPrepare &&
    input.draft.kind === "ready" &&
    input.submission.kind === "ready"
  );
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
