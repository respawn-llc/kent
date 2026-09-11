import { Check, CircleDot, Pause, PauseCircle, Play, RotateCcw, Save, Trash2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import type {
  ChatApi,
  ChatGoalError,
  ChatGoalFact,
  ChatGoalMutationResult,
  ChatGoalStatus,
  ChatSessionTarget,
} from "@/api";
import { errorMessage } from "@/api";
import {
  ChatGoalDestinationController,
  useStatusController,
  type ChatGoalDestinationSnapshot,
  type ChatGoalMutationIntent,
} from "@/app-facade";
import {
  Button,
  DisabledInteractionGuard,
  ErrorState,
  IslandSurface,
  LoadingState,
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/ui";
import { cx } from "@/ui";
import { type NewChatGoalBinding } from "./goalBinding";
import { formatGoalAge } from "./goalFormat";
import { GoalMarkdownField } from "./GoalMarkdownField";
export type GoalSidebarInput =
  | Readonly<{ kind: "session"; api: ChatApi; target: ChatSessionTarget }>
  | Readonly<{ kind: "new_chat"; api: ChatApi; binding: NewChatGoalBinding }>;

export function GoalSidebarPage({ input }: Readonly<{ input: GoalSidebarInput }>) {
  if (input.kind === "new_chat") {
    return <NewChatGoalSidebar api={input.api} binding={input.binding} />;
  }
  return <ExactGoalSidebar api={input.api} target={input.target} />;
}
function NewChatGoalSidebar({ api, binding }: Readonly<{ api: ChatApi; binding: NewChatGoalBinding }>) {
  const snapshot = useBindingSnapshot(binding);
  const { t } = useTranslation();
  const { push } = useStatusController();
  const [draft, setDraft] = useState("");
  const [editing, setEditing] = useState(true);
  const [error, setError] = useState<Error | null>(null);

  if (snapshot.kind === "resolved_session") {
    return <ExactGoalSidebar api={api} initialDraft={draft} target={snapshot.target} />;
  }

  const unavailable = snapshot.availability === "agent_capability_missing";
  const canSave = draft.trim().length > 0 && !snapshot.pending && !unavailable;
  const save = () => {
    setError(null);
    void binding
      .setGoal(draft)
      .then((result) => {
        if (result.outcome.kind === "rejected") {
          const message = goalErrorMessage(result.outcome.error, t);
          push({ id: "goal-set-rejected", title: t("chat.goal.setFailed"), body: message, tone: "danger" });
        }
        if (result.diagnostic !== null) {
          push({
            id: "goal-set-diagnostic",
            title: t("chat.goal.setFailed"),
            body: goalErrorMessage(result.diagnostic, t),
            tone: "danger",
          });
        }
      })
      .catch((cause: unknown) => {
        const nextError = cause instanceof Error ? cause : new Error(errorMessage(cause));
        setError(nextError);
        setEditing(true);
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
            draft.trim().length === 0
              ? undefined
              : saveButton({
                  disabled: !canSave,
                  label: unavailable ? t("chat.goal.unavailableForAgent") : t("chat.goal.save"),
                  onClick: save,
                  pending: snapshot.pending,
                })
          }
          expanded
          onChange={setDraft}
          onEdit={() => {
            setEditing(true);
          }}
          onEditingChange={setEditing}
          onExpand={() => undefined}
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
}: Readonly<{ api: ChatApi; initialDraft?: string; target: ChatSessionTarget }>) {
  const model = useExactGoalSidebarModel(api, initialDraft, target);

  if (model.snapshot.observation.kind === "loading") {
    return <LoadingState fullPage={false} title={model.t("chat.goal.loading")} />;
  }
  if (model.snapshot.observation.kind === "error") {
    return (
      <ErrorState
        body={model.snapshot.observation.error.message}
        fullPage={false}
        onRetry={model.replaceObservation}
        retryLabel={model.t("app.retry")}
        title={model.t("chat.goal.loadFailed")}
      />
    );
  }
  if (model.snapshot.observation.kind === "disposed" || model.fact === null) {
    return null;
  }
  return <ExactGoalContent model={model} />;
}
type ExactGoalSidebarModel = Readonly<{
  snapshot: ChatGoalDestinationSnapshot;
  draftState: DraftState;
  editing: boolean;
  expanded: boolean;
  fact: ChatGoalFact | null;
  now: number;
  pendingIntent: ChatGoalMutationIntent | null;
  displayedStatus: ChatGoalStatus | null;
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

type DraftState = Readonly<{ base: string; draft: string }>;
type GoalSidebarDerivedState = Readonly<{
  fact: ChatGoalFact | null;
  pendingIntent: ChatGoalMutationIntent | null;
  draftState: DraftState;
  displayedFact: ChatGoalFact | null;
  displayedStatus: ChatGoalStatus | null;
  dirty: boolean;
  unavailable: boolean;
  saveAvailable: boolean;
  actionsDisabled: boolean;
}>;

type GoalMutationRunnerInput = Readonly<{
  actionsDisabled: boolean;
  controller: ChatGoalDestinationController;
  draftState: DraftState;
  editing: boolean;
  expanded: boolean;
  push: ReturnType<typeof useStatusController>["push"];
  setError: (error: Error | null) => void;
  setExpanded: (expanded: boolean) => void;
  setEditing: (editing: boolean) => void;
  setLocalDraft: React.Dispatch<React.SetStateAction<DraftState>>;
  t: ReturnType<typeof useTranslation>["t"];
}>;
function useGoalMutationRunner(input: GoalMutationRunnerInput) {
  return useCallback(
    async (
      intent: ChatGoalMutationIntent,
      action: "set" | "pause" | "resume" | "reopen" | "clear",
      operation: () => Promise<ChatGoalMutationResult>,
    ) => {
      if (input.actionsDisabled) return;
      const previous = { draft: input.draftState.draft, editing: input.editing, expanded: input.expanded };
      input.setError(null);
      if (intent.kind === "clear") {
        input.setLocalDraft({ base: "", draft: "" });
        input.setEditing(true);
        input.setExpanded(true);
      }
      const handle = input.controller.begin(intent);
      try {
        const result = await operation();
        input.controller.succeed(handle, result);
        input.setError(null);
      } catch (cause: unknown) {
        input.controller.fail(handle);
        input.setLocalDraft((current) => ({ ...current, draft: previous.draft }));
        input.setEditing(previous.editing);
        input.setExpanded(previous.expanded);
        const failure = goalMutationFailure(cause, input.t);
        input.setError(failure);
        input.push({
          id: `goal-${action}-failed`,
          title: action === "clear" ? input.t("chat.goal.clearFailed") : input.t("chat.goal.mutationFailed"),
          body: failure.message,
          tone: "danger",
        });
      }
    },
    [input],
  );
}

function goalMutationFailure(cause: unknown, t: ReturnType<typeof useTranslation>["t"]): Error {
  if (cause instanceof GoalDomainError) return new Error(goalErrorMessage(cause.error, t));
  if (cause instanceof Error) return cause;
  return new Error(errorMessage(cause));
}

function deriveGoalSidebarState(
  snapshot: ChatGoalDestinationSnapshot,
  localDraft: DraftState,
): GoalSidebarDerivedState {
  const fact = observedGoalFact(snapshot);
  const pendingIntent = pendingGoalIntent(snapshot);
  const draftState = reconcileDraftState(localDraft, fact, pendingIntent);
  const displayedFact = displayedGoalFact(fact, pendingIntent);
  const displayedStatus = displayedGoalStatus(fact, pendingIntent);
  const dirty = draftState.draft !== draftState.base;
  const unavailable = goalUnavailable(fact);
  const saveAvailable = dirty && nonBlank(draftState.draft) && pendingIntent === null;
  const actionsDisabled = pendingIntent !== null;
  return {
    actionsDisabled,
    dirty,
    displayedFact,
    displayedStatus,
    draftState,
    fact,
    pendingIntent,
    saveAvailable,
    unavailable,
  };
}

function observedGoalFact(snapshot: ChatGoalDestinationSnapshot): ChatGoalFact | null {
  return snapshot.authority.kind === "observed" ? snapshot.authority.value : null;
}

function pendingGoalIntent(snapshot: ChatGoalDestinationSnapshot): ChatGoalMutationIntent | null {
  return snapshot.presentation.kind === "unresolved" ? snapshot.presentation.intent : null;
}

function displayedGoalFact(
  fact: ChatGoalFact | null,
  pendingIntent: ChatGoalMutationIntent | null,
): ChatGoalFact | null {
  return pendingIntent?.kind === "clear" && fact !== null ? { ...fact, goal: null } : fact;
}

function displayedGoalStatus(
  fact: ChatGoalFact | null,
  pendingIntent: ChatGoalMutationIntent | null,
): ChatGoalStatus | null {
  return pendingIntent?.kind === "goal" ? pendingIntent.preview.status : (fact?.goal?.status ?? null);
}

function goalUnavailable(fact: ChatGoalFact | null): boolean {
  return fact?.availability === "agent_capability_missing";
}

function nonBlank(value: string): boolean {
  return value.trim().length > 0;
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
  api: ChatApi,
  initialDraft: string,
  target: ChatSessionTarget,
): ExactGoalSidebarModel {
  const { t } = useTranslation();
  const controller = useMemo(() => new ChatGoalDestinationController(api, target), [api, target]);
  const snapshot = useControllerSnapshot(controller);
  const { push } = useStatusController();
  const [localDraft, setLocalDraft] = useState<DraftState>({ base: "", draft: initialDraft });
  const [editing, setEditing] = useState(false);
  const [expanded, setExpanded] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  useEffect(() => {
    controller.start();
    return () => {
      controller.dispose();
    };
  }, [controller]);

  const derived = deriveGoalSidebarState(snapshot, localDraft);
  const {
    actionsDisabled,
    dirty,
    displayedFact,
    displayedStatus,
    draftState,
    fact,
    pendingIntent,
    saveAvailable,
    unavailable,
  } = derived;
  const now = useGoalMinuteClock(fact);
  const creationMode = displayedFact?.goal === null;

  const runMutation = useGoalMutationRunner({
    actionsDisabled,
    controller,
    draftState,
    editing: creationMode || editing,
    expanded: creationMode || expanded,
    push,
    setError,
    setExpanded,
    setEditing,
    setLocalDraft,
    t,
  });

  const save = () => {
    const intent: ChatGoalMutationIntent = {
      kind: "goal",
      preview: { objective: draftState.draft, status: "active" },
    };
    void runMutation(intent, "set", async () => {
      const result = await api.setGoal({ kind: "session", sessionID: target.sessionID }, draftState.draft);
      if (result.outcome.kind === "rejected") {
        throw new GoalDomainError(result.outcome.error);
      }
      if (result.diagnostic !== null) {
        push({
          id: "goal-set-diagnostic",
          title: t("chat.goal.setFailed"),
          body: goalErrorMessage(result.diagnostic, t),
          tone: "danger",
        });
      }
      return result.outcome.mutation;
    });
  };

  const lifecycle = goalLifecycleAction(displayedStatus);
  const runLifecycle = () => {
    void runMutation(
      {
        kind: "goal",
        preview: { objective: draftState.draft, status: lifecycle === "pause" ? "paused" : "active" },
      },
      lifecycle,
      lifecycle === "pause" ? async () => api.pauseGoal(target) : async () => api.resumeGoal(target),
    );
  };

  const clear = () => {
    void runMutation({ kind: "clear" }, "clear", async () => api.clearGoal(target));
  };
  const setDraft = useCallback((value: string) => {
    setLocalDraft((current) => ({ ...current, draft: value }));
  }, []);
  const replaceObservation = useCallback(() => {
    controller.replaceObservation();
  }, [controller]);

  return {
    actionsDisabled,
    clear,
    dirty,
    draftState,
    editing: creationMode || editing,
    error,
    expanded: creationMode || expanded,
    fact: displayedFact,
    now,
    pendingIntent,
    replaceObservation,
    runLifecycle,
    save,
    saveAvailable,
    setDraft,
    setEditing,
    setExpanded,
    t,
    unavailable,
    displayedStatus,
    snapshot,
  };
}

function reconcileDraftState(
  localDraft: DraftState,
  fact: ChatGoalFact | null,
  pendingIntent: ChatGoalMutationIntent | null,
): DraftState {
  if (pendingIntent?.kind === "clear") {
    return { base: "", draft: "" };
  }
  if (fact === null) {
    return localDraft.draft === localDraft.base ? { base: "", draft: "" } : { ...localDraft, base: "" };
  }
  const nextBase = fact.goal?.objective ?? "";
  return localDraft.draft === localDraft.base
    ? { base: nextBase, draft: nextBase }
    : { ...localDraft, base: nextBase };
}

type GoalLifecycleAction = "pause" | "resume" | "reopen";

function goalLifecycleAction(status: ChatGoalStatus | null): GoalLifecycleAction {
  if (status === "active") return "pause";
  if (status === "paused") return "resume";
  return "reopen";
}

function ExactGoalContent({ model }: Readonly<{ model: ExactGoalSidebarModel }>) {
  const { fact } = model;
  if (fact === null) return null;
  const visibleGoal = fact.goal;
  const showActions = visibleGoal !== null;
  return (
    <div className="flex h-full min-h-0 flex-col gap-[var(--space-3)] p-[var(--space-4)]">
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
          value={model.draftState.draft}
        />
      </div>
      {showActions ? (
        <>
          <GoalMetadata fact={fact} now={model.now} />
          <GoalActions model={model} />
        </>
      ) : null}
    </div>
  );
}

function renderSaveAction(model: ExactGoalSidebarModel) {
  if (!model.dirty || model.draftState.draft.trim().length === 0) return undefined;
  return saveButton({
    disabled: !model.saveAvailable || model.unavailable,
    label: model.unavailable ? model.t("chat.goal.unavailableForAgent") : model.t("chat.goal.save"),
    onClick: model.save,
    pending:
      model.pendingIntent?.kind === "goal" &&
      model.pendingIntent.preview.objective === model.draftState.draft,
  });
}

function GoalActions({ model }: Readonly<{ model: ExactGoalSidebarModel }>) {
  const lifecycle = goalLifecycleAction(model.displayedStatus);
  const lifecycleUnavailable = lifecycle !== "pause" && model.unavailable;
  const lifecycleButton = (
    <Button
      disabled={model.actionsDisabled || lifecycleUnavailable}
      onClick={model.runLifecycle}
      variant={lifecycle === "pause" ? "secondary" : lifecycle === "resume" ? "primary" : "primary-outline"}
    >
      {lifecycle === "pause" ? (
        <Pause size={15} />
      ) : lifecycle === "resume" ? (
        <Play size={15} />
      ) : (
        <RotateCcw size={15} />
      )}
      {model.t(`chat.goal.${lifecycle}`)}
    </Button>
  );
  return (
    <div className="flex flex-wrap items-center gap-[var(--space-2)]" data-testid="goal-actions">
      <DisabledInteractionGuard
        disabled={lifecycleUnavailable}
        reason={model.t("chat.goal.unavailableForAgent")}
      >
        {lifecycleButton}
      </DisabledInteractionGuard>
      <Button disabled={model.actionsDisabled} onClick={model.clear} variant="danger">
        <Trash2 size={15} />
        {model.t("chat.goal.clear")}
      </Button>
    </div>
  );
}

function GoalMetadata({ fact, now }: Readonly<{ fact: ChatGoalFact; now: number }>) {
  const { t } = useTranslation();
  const goal = fact.goal;
  if (goal === null) return null;
  const icon =
    goal.status === "active" ? (
      <CircleDot size={16} />
    ) : goal.status === "paused" ? (
      <PauseCircle size={16} />
    ) : (
      <Check size={16} />
    );
  return (
    <IslandSurface
      aria-label={t(`chat.goal.${goal.status}`)}
      className="grid gap-[var(--space-1)] p-[var(--space-3)]"
      level={1}
    >
      <div
        className={cx(
          "flex items-center gap-[var(--space-2)] font-medium",
          goal.status === "active"
            ? "text-[var(--color-primary)]"
            : goal.status === "paused"
              ? "text-[var(--color-warning)]"
              : "text-[var(--color-success)]",
        )}
      >
        {icon}
        <span>{t(`chat.goal.${goal.status}`)}</span>
      </div>
      <span className="text-sm text-[var(--color-muted)]">
        {t("chat.goal.setAt", {
          date: new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(
            new Date(goal.createdAt),
          ),
          age: formatGoalAge(goal.createdAt, now),
        })}
      </span>
    </IslandSurface>
  );
}

function saveButton({
  disabled,
  label,
  onClick,
  pending,
}: Readonly<{
  disabled: boolean;
  label: string;
  onClick: () => void;
  pending: boolean;
}>) {
  const button = (
    <Button
      aria-label={label}
      data-testid="goal-save"
      disabled={disabled || pending}
      onClick={onClick}
      size="icon"
      variant="primary"
    >
      <Save aria-hidden="true" size={16} />
    </Button>
  );
  return disabled ? (
    <TooltipProvider delayDuration={0}>
      <Tooltip>
        <TooltipTrigger asChild>{button}</TooltipTrigger>
        <TooltipContent>{label}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  ) : (
    button
  );
}

function useBindingSnapshot(binding: NewChatGoalBinding) {
  const [snapshot, setSnapshot] = useState(binding.snapshot);
  useEffect(
    () =>
      binding.subscribe(() => {
        setSnapshot(binding.snapshot);
      }),
    [binding],
  );
  return snapshot;
}

function useControllerSnapshot(controller: ChatGoalDestinationController) {
  const [snapshot, setSnapshot] = useState(controller.snapshot);
  useEffect(
    () =>
      controller.subscribe(() => {
        setSnapshot(controller.snapshot);
      }),
    [controller],
  );
  return snapshot;
}

function goalErrorMessage(error: ChatGoalError, t: ReturnType<typeof useTranslation>["t"]): string {
  switch (error.kind) {
    case "runtime_unavailable":
      return t("chatSettings.errors.runtimeUnavailable");
    case "internal_failure":
      return error.cause ?? t("chatSettings.errors.internalFailure");
    case "unknown":
      return t("chatSettings.errors.unknown", { code: error.code });
    default:
      throw new Error("Unknown Goal error kind.");
  }
}

class GoalDomainError extends Error {
  constructor(readonly error: ChatGoalError) {
    super(error.kind);
  }
}
