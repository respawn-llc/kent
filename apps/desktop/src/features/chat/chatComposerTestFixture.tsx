import { useLayoutEffect, useState } from "react";
import * as Atom from "effect/unstable/reactivity/Atom";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import type { ChatSettingsTarget } from "@/api";
import { useAppServices } from "@/app-facade";
import { createChatComposerViewModel } from "@/features/chat/ChatComposerViewModel";
import {
  useChatComposer as useComposer,
  type ChatComposerOptions,
  type ComposerSubmission,
} from "@/features/chat/useChatComposer";

export function useChatComposer(
  options: ChatSettingsTarget & Omit<ChatComposerOptions, "model"> & { submission?: ComposerSubmission },
) {
  const services = useAppServices();
  const client = useQueryClient();
  const { t } = useTranslation();
  const [target] = useState(() =>
    Atom.make<ChatSettingsTarget>(
      options.kind === "session"
        ? { kind: "session", projectID: options.projectID, sessionID: options.sessionID }
        : { kind: "new_chat", projectID: options.projectID, workspace: options.workspace },
    ),
  );
  const [submission] = useState(() =>
    Atom.make<ComposerSubmission>(options.submission ?? { kind: "loading" }),
  );
  const setSubmission = useAtomSet(submission);
  const state = options.submission?.kind ?? "loading";
  const settings =
    options.submission?.kind === "ready" && "initialSettings" in options.submission
      ? options.submission.initialSettings
      : null;
  const error = options.submission?.kind === "failed" ? options.submission.error : null;
  useLayoutEffect(() => {
    setSubmission(
      state === "ready"
        ? settings === null
          ? { kind: "ready" }
          : { kind: "ready", initialSettings: settings }
        : state === "failed"
          ? { kind: "failed", error }
          : { kind: "loading" },
    );
  }, [state, settings, error, setSubmission]);
  const [model] = useState(() =>
    createChatComposerViewModel({ services, client, t, target, submission, opening: options }),
  );
  return {
    ...useComposer({ ...options, model }),
    navigateDraft: useAtomSet(model.draft.navigate, { mode: "promise" }),
    replaceHistory: useAtomSet(model.draft.reindexHistory),
    draftPair: useAtomValue(model.draft.value),
  };
}
