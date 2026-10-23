import { useEffect, useRef, useState, type ReactElement } from "react";
import { useTranslation } from "react-i18next";
import { useAtomValue } from "@effect/atom-react";
import type * as Atom from "effect/reactivity/Atom";
import { useQueryClient } from "@tanstack/react-query";
import { AnimatePresence } from "motion/react";

import type { ChatApi, ChatGoalFact, ChatGoalSetResult, ChatSessionTarget } from "@/api";
import { ContractError, errorMessage } from "@/api";
import { recoverOrThrowDebugFailure, useAppServices, useStatusController } from "@/app-facade";
import { AnimatedReveal, ErrorState, LoadingState, Spinner } from "@/ui";
import { type NewChatGoalBinding } from "./goalBinding";
import { GoalMarkdownField } from "./GoalMarkdownField";
import { GoalActions, GoalMetadata, GoalSaveButton } from "./GoalSidebarParts";
import { createGoalSidebarViewModel, useGoalSidebarActions } from "./GoalSidebarViewModel";
import {
  goalErrorMessage,
  goalSetDiagnosticNotificationID,
  reportGoalSetDiagnostic,
} from "./goalNotifications";

export type GoalSidebarApi = Pick<
  ChatApi,
  "setGoal" | "subscribeGoal" | "pauseGoal" | "resumeGoal" | "clearGoal"
>;
export type GoalSidebarInput =
  | Readonly<{ kind: "session"; api: GoalSidebarApi; target: ChatSessionTarget }>
  | Readonly<{
      kind: "new_chat";
      api: GoalSidebarApi;
      binding: NewChatGoalBinding;
      setGoal(objective: string): Promise<ChatGoalSetResult>;
    }>;

export function GoalSidebarPage({ input }: Readonly<{ input: GoalSidebarInput }>) {
  if (input.kind === "new_chat") {
    return <NewChatGoalSidebar api={input.api} binding={input.binding} setGoal={input.setGoal} />;
  }
  return <ExactGoalSidebar api={input.api} target={input.target} />;
}

function NewChatGoalSidebar({
  api,
  binding,
  setGoal,
}: Readonly<{
  api: GoalSidebarApi;
  binding: NewChatGoalBinding;
  setGoal(objective: string): Promise<ChatGoalSetResult>;
}>) {
  const snapshot = useAtomValue(binding.state);
  const { t } = useTranslation();
  const { push } = useStatusController();
  const { logger } = useAppServices();
  const mounted = useRef(false);
  const [draft, setDraft] = useState("");
  const [editing, setEditing] = useState(true);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  if (snapshot.kind === "resolved_session") {
    return <ExactGoalSidebar api={api} initialDraft={draft} target={snapshot.target} />;
  }

  const unavailable = snapshot.availability === "agent_capability_missing";
  const canSave = draft.trim().length > 0 && snapshot.ready && !snapshot.pending && !unavailable;
  const save = () => {
    if (!canSave) return;
    setEditing(false);
    void setGoal(draft)
      .then((result) => {
        if (result.outcome.kind === "rejected") {
          const message = goalErrorMessage(result.outcome.error.detail, t);
          push({ id: "goal-set-rejected", title: t("chat.goal.setFailed"), body: message, tone: "danger" });
          return;
        }
        setDraft("");
        if (result.outcome.diagnostic !== null) {
          reportGoalSetDiagnostic(push, t, result.outcome.diagnostic, goalSetDiagnosticNotificationID());
        }
      })
      .catch((cause: unknown) => {
        const nextError = cause instanceof Error ? cause : new Error(errorMessage(cause));
        const recover = () => {
          if (mounted.current) setEditing(true);
          push({
            id: "goal-set-failed",
            title: t("chat.goal.setFailed"),
            body: nextError.message,
            tone: "danger",
          });
        };
        if (cause instanceof ContractError) {
          void recoverOrThrowDebugFailure({
            context: { operation: "Goal Set" },
            error: cause,
            logger,
            message: "Goal Set response was malformed.",
            recover,
          });
          return;
        }
        recover();
      });
  };

  return (
    <div className="flex min-h-0 flex-col gap-[var(--space-3)]">
      <div className="min-h-0 shrink-0">
        <GoalMarkdownField
          editing={editing}
          floatingAction={
            snapshot.pending ? (
              <Spinner size="sm" testID="goal-mutation-pending" />
            ) : draft.trim().length === 0 ? undefined : (
              <GoalSaveButton
                disabled={!canSave}
                label={
                  !snapshot.ready
                    ? t("chatComposer.loadingSettings")
                    : unavailable
                      ? t("chat.goal.unavailableForAgent")
                      : t("chat.goal.save")
                }
                onClick={save}
                pending={snapshot.pending}
              />
            )
          }
          expanded
          onChange={setDraft}
          onEdit={() => {
            setEditing(true);
          }}
          onEditingChange={setEditing}
          onExpand={() => undefined}
          submitIntent={{ available: canSave, onSubmitIntent: save }}
          value={draft}
        />
      </div>
    </div>
  );
}

function ExactGoalSidebar({
  api,
  initialDraft = "",
  target,
}: Readonly<{ api: GoalSidebarApi; initialDraft?: string | undefined; target: ChatSessionTarget }>) {
  const { t } = useTranslation();
  const { logger } = useAppServices();
  const { push } = useStatusController();
  const client = useQueryClient();
  const [owner] = useState(() =>
    createGoalSidebarViewModel({ api, initialDraft, target, client, logger, push, t }),
  );
  const state = useAtomValue(owner.state);
  const act = useGoalSidebarActions(owner);
  const now = useGoalMinuteClock(state.fact);
  const model = {
    ...state,
    t,
    now,
    replaceObservation: () => {
      act({ kind: "retry" });
    },
    setDraft: (value: string) => {
      act({ kind: "draft", value });
    },
    setEditing: (value: boolean) => {
      act({ kind: "editing", value });
    },
    setExpanded: (value: boolean) => {
      act({ kind: "expanded", value });
    },
    save: () => {
      act({ kind: "save" });
    },
    runLifecycle: () => {
      act({ kind: "lifecycle" });
    },
    clear: () => {
      act({ kind: "clear" });
    },
  };
  if (state.observation.kind === "loading" && state.fact === null) {
    return <LoadingState surface="plain" title={t("chat.goal.loading")} />;
  }
  if (state.observation.kind === "error" || state.observation.kind === "loading_retryable") {
    return (
      <ErrorState
        surface="plain"
        body={state.observation.error.message}
        onRetry={model.replaceObservation}
        retryLabel={t("app.retry")}
        title={t("chat.goal.loadFailed")}
      />
    );
  }
  if (state.fact === null) return null;
  return <ExactGoalContent model={model} />;
}

type ExactGoalSidebarModel = Atom.Type<ReturnType<typeof createGoalSidebarViewModel>["state"]> &
  Readonly<{
    t: ReturnType<typeof useTranslation>["t"];
    now: number;
    setDraft(value: string): void;
    setEditing(value: boolean): void;
    setExpanded(value: boolean): void;
    save(): void;
    runLifecycle(): void;
    clear(): void;
  }>;

function useGoalMinuteClock(fact: ChatGoalFact | null): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (fact?.goal === null || fact?.goal === undefined) return undefined;
    const timer = window.setInterval(() => {
      setNow(Date.now());
    }, 60_000);
    return () => {
      window.clearInterval(timer);
    };
  }, [fact?.goal]);
  return now;
}

function ExactGoalContent({ model }: Readonly<{ model: ExactGoalSidebarModel }>) {
  if (model.fact === null) return null;
  return (
    <div className="flex min-h-0 flex-col">
      <div className="min-h-0 shrink-0">
        <GoalMarkdownField
          editing={model.editing}
          floatingAction={renderSaveAction(model)}
          expanded={model.expanded}
          onChange={model.setDraft}
          onEdit={() => {
            model.setEditing(true);
            model.setExpanded(true);
          }}
          onEditingChange={model.setEditing}
          onExpand={() => {
            model.setExpanded(true);
          }}
          submitIntent={{ available: model.saveAvailable && !model.unavailable, onSubmitIntent: model.save }}
          value={model.displayedObjective}
        />
      </div>
      <AnimatePresence initial={false}>
        {model.displayedStatus !== null ? (
          <AnimatedReveal key="metadata" variant="fade">
            <div className="flex flex-col gap-[var(--space-3)] pt-[var(--space-3)]">
              <GoalMetadata
                createdAt={model.displayedCreatedAt}
                now={model.now}
                status={model.displayedStatus}
              />
              <GoalActions model={model} />
            </div>
          </AnimatedReveal>
        ) : null}
      </AnimatePresence>
    </div>
  );
}

function renderSaveAction(model: ExactGoalSidebarModel): ReactElement | undefined {
  if (model.pendingIntent?.kind === "set" || model.pendingIntent?.kind === "clear") {
    return <Spinner size="sm" testID="goal-mutation-pending" />;
  }
  if (!model.dirty || model.draftState.draft.trim().length === 0) return undefined;
  return (
    <GoalSaveButton
      disabled={!model.saveAvailable || model.unavailable}
      label={model.unavailable ? model.t("chat.goal.unavailableForAgent") : model.t("chat.goal.save")}
      onClick={model.save}
      pending={false}
    />
  );
}
