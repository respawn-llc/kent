import { useCallback, useEffect, useRef, useState, useSyncExternalStore, type ReactElement } from "react";
import { useTranslation } from "react-i18next";

import type { ChatApi, ChatGoalFact, ChatGoalMutationResult, ChatGoalStatus, ChatSessionTarget } from "@/api";
import { ChatOperationError, ContractError, errorMessage } from "@/api";
import { recoverOrThrowDebugFailure, useAppServices, useStatusController } from "@/app-facade";
import { Button, ErrorState, LoadingState } from "@/ui";
import { type NewChatGoalBinding } from "./goalBinding";
import {
  deriveGoalSidebarState,
  localDraftForFact,
  reconcileDraftState,
  type DraftState,
  type GoalMutationIntent,
  type GoalObservationState,
} from "./goalSidebarState";
import { GoalMarkdownField } from "./GoalMarkdownField";
import { GoalActions, GoalMetadata, GoalSaveButton, goalLifecycleAction } from "./GoalSidebarParts";
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
    }>;

export function GoalSidebarPage({ input }: Readonly<{ input: GoalSidebarInput }>) {
  if (input.kind === "new_chat") {
    return <NewChatGoalSidebar api={input.api} binding={input.binding} />;
  }
  return <ExactGoalSidebar api={input.api} target={input.target} />;
}

function NewChatGoalSidebar({
  api,
  binding,
}: Readonly<{
  api: GoalSidebarApi;
  binding: NewChatGoalBinding;
}>) {
  const snapshot = useBindingSnapshot(binding);
  const { t } = useTranslation();
  const { push } = useStatusController();
  const { logger } = useAppServices();
  const mounted = useRef(false);
  const [draft, setDraft] = useState("");
  const [editing, setEditing] = useState(true);
  const [error, setError] = useState<Error | null>(null);

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
  const canSave = draft.trim().length > 0 && !snapshot.pending && !unavailable;
  const save = () => {
    if (!canSave) return;
    setError(null);
    setEditing(false);
    void binding
      .setGoal(draft)
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
          if (mounted.current) {
            setError(nextError);
            setEditing(true);
          }
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
    <div className="flex h-full min-h-0 flex-col gap-[var(--space-3)] p-[var(--space-4)]">
      <p className="m-0 text-sm leading-relaxed text-[var(--color-muted)]">{t("chat.goal.guidance")}</p>
      <div className="min-h-0 flex-1">
        <GoalMarkdownField
          editing={editing}
          error={error?.message}
          floatingAction={
            snapshot.pending || draft.trim().length === 0 ? undefined : (
              <GoalSaveButton
                disabled={!canSave}
                label={unavailable ? t("chat.goal.unavailableForAgent") : t("chat.goal.save")}
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
}: Readonly<{
  api: GoalSidebarApi;
  initialDraft?: string | undefined;
  target: ChatSessionTarget;
}>) {
  const model = useExactGoalSidebarModel({ api, initialDraft, target });

  if (model.observation.kind === "loading_retryable") {
    return (
      <LoadingState
        actions={
          <Button onClick={model.replaceObservation} variant="primary">
            {model.t("app.retry")}
          </Button>
        }
        body={model.observation.error.message}
        fullPage={false}
        title={model.t("chat.goal.loading")}
      />
    );
  }
  if (model.observation.kind === "loading" && model.fact === null) {
    return <LoadingState fullPage={false} title={model.t("chat.goal.loading")} />;
  }
  if (model.observation.kind === "error") {
    return (
      <ErrorState
        body={model.observation.error.message}
        fullPage={false}
        onRetry={model.replaceObservation}
        retryLabel={model.t("app.retry")}
        title={model.t("chat.goal.loadFailed")}
      />
    );
  }
  if (model.fact === null) return null;
  return <ExactGoalContent model={model} />;
}

type ExactGoalSidebarModel = Readonly<{
  observation: GoalObservationState;
  draftState: DraftState;
  editing: boolean;
  expanded: boolean;
  fact: ChatGoalFact | null;
  now: number;
  pendingIntent: GoalMutationIntent | null;
  displayedStatus: ChatGoalStatus | null;
  displayedCreatedAt: string | null;
  displayedObjective: string;
  dirty: boolean;
  unavailable: boolean;
  saveAvailable: boolean;
  actionsDisabled: boolean;
  error: Error | null;
  t: ReturnType<typeof useTranslation>["t"];
  replaceObservation: () => void;
  setDraft: (value: string) => void;
  setEditing: (value: boolean) => void;
  setExpanded: (value: boolean) => void;
  save: () => void;
  runLifecycle: () => void;
  clear: () => void;
}>;

type GoalMutationOperationResult = Readonly<{
  mutation: ChatGoalMutationResult;
  diagnostic: ChatOperationError | null;
}>;

type GoalMutationRunnerInput = Readonly<{
  actionsDisabled: boolean;
  currentFact: () => ChatGoalFact | null;
  draftState: DraftState;
  draftRevision: Readonly<{ current: number }>;
  editing: boolean;
  expanded: boolean;
  logger: ReturnType<typeof useAppServices>["logger"];
  mounted: Readonly<{ current: boolean }>;
  pendingIntent: GoalMutationIntent | null;
  push: ReturnType<typeof useStatusController>["push"];
  setError: (error: Error | null) => void;
  setExpanded: (expanded: boolean) => void;
  setEditing: (editing: boolean) => void;
  setLocalDraft: React.Dispatch<React.SetStateAction<DraftState>>;
  setPendingIntent: (intent: GoalMutationIntent | null) => void;
  t: ReturnType<typeof useTranslation>["t"];
}>;

function useGoalMutationRunner(input: GoalMutationRunnerInput) {
  return useCallback(
    async (
      intent: GoalMutationIntent,
      action: "set" | "pause" | "resume" | "reopen" | "clear",
      operation: () => Promise<GoalMutationOperationResult>,
    ) => {
      if (input.actionsDisabled || input.pendingIntent !== null || !input.mounted.current) {
        return;
      }
      const submittedDraftRevision = input.draftRevision.current;
      const previous = { draft: input.draftState.draft, editing: input.editing, expanded: input.expanded };
      input.setPendingIntent(intent);
      input.setError(null);
      if (intent.kind === "clear") {
        input.setLocalDraft({ base: "", draft: "" });
        input.setEditing(true);
        input.setExpanded(true);
      }
      try {
        const result = await operation();
        settleGoalMutation(input, action, submittedDraftRevision);
        if (action === "set" && result.diagnostic !== null) {
          reportGoalSetDiagnostic(input.push, input.t, result.diagnostic, goalSetDiagnosticNotificationID());
        }
      } catch (cause: unknown) {
        const failure = goalMutationFailure(cause, input.t);
        const recover = () => {
          restoreGoalMutation(input, action, previous, failure);
        };
        if (cause instanceof ContractError) {
          void recoverOrThrowDebugFailure({
            context: { operation: `Goal ${action}` },
            error: cause,
            logger: input.logger,
            message: "Goal mutation response was malformed.",
            recover,
          });
        } else {
          recover();
        }
        reportGoalMutationFailure(input, action, failure);
      }
    },
    [input],
  );
}

function settleGoalMutation(
  input: GoalMutationRunnerInput,
  action: "set" | "pause" | "resume" | "reopen" | "clear",
  submittedDraftRevision: number,
): void {
  if (!input.mounted.current) return;
  input.setPendingIntent(null);
  input.setError(null);
  const fact = input.currentFact();
  const draftChangedAfterSubmit = input.draftRevision.current !== submittedDraftRevision;
  input.setLocalDraft((current) =>
    (action === "set" || action === "clear") && !draftChangedAfterSubmit
      ? localDraftForFact(fact)
      : reconcileDraftState(current, fact, null),
  );
}

function restoreGoalMutation(
  input: GoalMutationRunnerInput,
  action: "set" | "pause" | "resume" | "reopen" | "clear",
  previous: Readonly<{ draft: string; editing: boolean; expanded: boolean }>,
  failure: Error,
): void {
  if (!input.mounted.current) return;
  input.setPendingIntent(null);
  input.setLocalDraft((currentDraft) => ({ ...currentDraft, draft: previous.draft }));
  if (action === "set") {
    input.setEditing(true);
    input.setExpanded(true);
  } else {
    input.setEditing(previous.editing);
    input.setExpanded(previous.expanded);
  }
  input.setError(failure);
}

function reportGoalMutationFailure(
  input: GoalMutationRunnerInput,
  action: "set" | "pause" | "resume" | "reopen" | "clear",
  failure: Error,
): void {
  input.push({
    id: `goal-${action}-failed`,
    title: action === "clear" ? input.t("chat.goal.clearFailed") : input.t("chat.goal.mutationFailed"),
    body: failure.message,
    tone: "danger",
  });
}

function goalMutationFailure(cause: unknown, t: ReturnType<typeof useTranslation>["t"]): Error {
  if (cause instanceof ChatOperationError) return new Error(goalErrorMessage(cause.detail, t));
  if (cause instanceof Error) return cause;
  return new Error(errorMessage(cause));
}

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

function useExactGoalSidebarModel(
  input: Readonly<{
    api: GoalSidebarApi;
    initialDraft: string;
    target: ChatSessionTarget;
  }>,
): ExactGoalSidebarModel {
  const { api, initialDraft, target } = input;
  const { t } = useTranslation();
  const observation = useGoalObservation(api, target);
  const [localDraft, setLocalDraft] = useState<DraftState>({ base: "", draft: initialDraft });
  const draftRevision = useRef(0);
  const [pendingIntent, setPendingIntent] = useState<GoalMutationIntent | null>(null);
  const latestFact = useRef<ChatGoalFact | null>(observation.fact);
  const { push } = useStatusController();
  const { logger } = useAppServices();
  const mounted = useRef(false);
  const [editing, setEditing] = useState(false);
  const [expanded, setExpanded] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  useEffect(() => {
    latestFact.current = observation.fact;
  }, [observation.fact]);

  const reconciledDraftState = reconcileDraftState(localDraft, observation.fact, pendingIntent);
  const derived = deriveGoalSidebarState(observation.observation, pendingIntent, reconciledDraftState);
  const {
    actionsDisabled,
    dirty,
    displayedCreatedAt,
    displayedObjective,
    displayedStatus,
    draftState,
    fact,
    saveAvailable,
    unavailable,
  } = derived;
  const now = useGoalMinuteClock(fact);
  const creationMode = fact?.goal === null;

  const runMutation = useGoalMutationRunner({
    actionsDisabled,
    currentFact: () => latestFact.current,
    draftRevision,
    draftState,
    editing: (creationMode && pendingIntent === null) || editing,
    expanded: (creationMode && pendingIntent === null) || expanded,
    logger,
    mounted,
    pendingIntent,
    push,
    setError,
    setExpanded,
    setEditing,
    setLocalDraft,
    setPendingIntent,
    t,
  });

  const save = () => {
    if (actionsDisabled) return;
    setEditing(false);
    void runMutation(
      {
        kind: "goal",
        preview: { objective: draftState.draft, status: "active" },
      },
      "set",
      async () => {
        const result = await api.setGoal({ kind: "session", sessionID: target.sessionID }, draftState.draft);
        if (result.outcome.kind === "rejected") throw result.outcome.error;
        return { mutation: result.outcome.mutation, diagnostic: result.outcome.diagnostic };
      },
    );
  };

  const lifecycle = goalLifecycleAction(displayedStatus);
  const runLifecycle = () => {
    void runMutation(
      {
        kind: "goal",
        preview: {
          objective: fact?.goal?.objective ?? draftState.draft,
          status: lifecycle === "pause" ? "paused" : "active",
        },
      },
      lifecycle,
      lifecycle === "pause"
        ? async () => ({ mutation: await api.pauseGoal(target), diagnostic: null })
        : async () => ({ mutation: await api.resumeGoal(target), diagnostic: null }),
    );
  };

  const clear = () => {
    void runMutation({ kind: "clear" }, "clear", async () => ({
      mutation: await api.clearGoal(target),
      diagnostic: null,
    }));
  };
  const setDraft = useCallback(
    (value: string) => {
      draftRevision.current += 1;
      setLocalDraft((current) => {
        const synchronized = reconcileDraftState(current, observation.fact, pendingIntent);
        return { ...synchronized, draft: value };
      });
    },
    [observation.fact, pendingIntent],
  );

  return {
    actionsDisabled,
    clear,
    dirty,
    draftState,
    displayedCreatedAt,
    displayedObjective,
    displayedStatus,
    editing: (creationMode && pendingIntent === null) || editing,
    error,
    expanded: (creationMode && pendingIntent === null) || expanded,
    fact,
    now,
    observation: observation.observation,
    pendingIntent,
    replaceObservation: observation.replace,
    runLifecycle,
    save,
    saveAvailable,
    setDraft,
    setEditing,
    setExpanded,
    t,
    unavailable,
  };
}

function useGoalObservation(
  api: GoalSidebarApi,
  target: ChatSessionTarget,
): Readonly<{
  fact: ChatGoalFact | null;
  observation: GoalObservationState;
  replace: () => void;
}> {
  const [attempt, setAttempt] = useState(0);
  const [observation, setObservation] = useState<GoalObservationState>({
    kind: "loading",
    fact: null,
  });
  const replace = useCallback(() => {
    setObservation((current) => ({ kind: "loading", fact: current.fact }));
    setAttempt((current) => current + 1);
  }, []);

  useEffect(() => {
    let live = true;
    let hasObserved = false;
    let nextSequence = 0;
    let subscription: ReturnType<GoalSidebarApi["subscribeGoal"]> | null = null;
    const fail = (error: Error) => {
      if (!live) return;
      if (hasObserved) {
        live = false;
        subscription?.close();
        subscription = null;
        setObservation({ kind: "loading_retryable", error, fact: null });
        return;
      }
      live = false;
      subscription?.close();
      subscription = null;
      setObservation({ kind: "error", error, fact: null });
    };

    subscription = api.subscribeGoal(target, {
      onOpen: () => {
        if (!live) return;
        nextSequence = 0;
        setObservation((current) => ({ kind: "loading", fact: current.fact }));
      },
      onEvent: (event) => {
        if (!live) return;
        if (!validGoalObservationSequence(nextSequence, event.sequence, event.kind)) {
          fail(new ContractError("Goal observation sequence is not continuous."));
          return;
        }
        nextSequence = event.sequence;
        hasObserved = true;
        setObservation({ kind: "observed", fact: event.fact });
      },
      onComplete: (code, message) => {
        fail(
          new ContractError(
            code === 0
              ? "Goal observation completed unexpectedly."
              : `Goal observation completed with code ${code.toString()}: ${message}`,
          ),
        );
      },
      onError: fail,
    });

    return () => {
      live = false;
      subscription?.close();
      subscription = null;
    };
  }, [api, attempt, target]);

  return { fact: observation.fact, observation, replace };
}

function validGoalObservationSequence(
  previous: number,
  sequence: number,
  kind: "hydration" | "update",
): boolean {
  if (previous === 0) return kind === "hydration" && sequence === 1;
  return kind === "update" && sequence === previous + 1;
}

function ExactGoalContent({ model }: Readonly<{ model: ExactGoalSidebarModel }>) {
  const { fact } = model;
  if (fact === null) return null;
  const showActions = model.displayedStatus !== null;
  return (
    <div className="flex h-full min-h-0 flex-col gap-[var(--space-3)] p-[var(--space-4)]">
      {!showActions ? (
        <p className="m-0 text-sm leading-relaxed text-[var(--color-muted)]">
          {model.t("chat.goal.guidance")}
        </p>
      ) : null}
      <div className="min-h-0 flex-1">
        <GoalMarkdownField
          editing={model.editing}
          error={model.error?.message}
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
      {showActions ? (
        <>
          <GoalMetadata createdAt={model.displayedCreatedAt} now={model.now} status={model.displayedStatus} />
          <GoalActions model={model} />
        </>
      ) : null}
    </div>
  );
}

function renderSaveAction(model: ExactGoalSidebarModel): ReactElement | undefined {
  if (!model.dirty || model.draftState.draft.trim().length === 0) return undefined;
  if (
    model.pendingIntent?.kind === "goal" &&
    model.pendingIntent.preview.objective === model.draftState.draft
  ) {
    return undefined;
  }
  return (
    <GoalSaveButton
      disabled={!model.saveAvailable || model.unavailable}
      label={model.unavailable ? model.t("chat.goal.unavailableForAgent") : model.t("chat.goal.save")}
      onClick={model.save}
      pending={false}
    />
  );
}

function useBindingSnapshot(binding: NewChatGoalBinding) {
  const subscribe = useCallback((listener: () => void) => binding.subscribe(listener), [binding]);
  const getSnapshot = useCallback(() => binding.snapshot, [binding]);
  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}
