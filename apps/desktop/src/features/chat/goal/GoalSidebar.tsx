import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
  type ReactElement,
} from "react";
import { useTranslation } from "react-i18next";

import type {
  ChatApi,
  ChatError,
  ChatGoalFact,
  ChatGoalMutationResult,
  ChatGoalStatus,
  ChatSessionTarget,
} from "@/api";
import { ChatOperationError, ContractError, errorMessage } from "@/api";
import {
  ChatGoalDestinationController,
  recoverOrThrowDebugFailure,
  useAppServices,
  useStatusController,
  type ChatGoalDestinationSnapshot,
  type ChatGoalMutationIntent,
} from "@/app-facade";
import { ErrorState, LoadingState } from "@/ui";
import { type NewChatGoalBinding } from "./goalBinding";
import { GoalMarkdownField } from "./GoalMarkdownField";
import { GoalActions, GoalMetadata, GoalSaveButton, goalLifecycleAction } from "./GoalSidebarParts";
export type GoalSidebarApi = Pick<
  ChatApi,
  "setGoal" | "subscribeGoal" | "pauseGoal" | "resumeGoal" | "clearGoal"
>;
export type GoalSidebarInput =
  | Readonly<{ kind: "session"; api: GoalSidebarApi; target: ChatSessionTarget }>
  | Readonly<{ kind: "new_chat"; api: GoalSidebarApi; binding: NewChatGoalBinding }>;

export function GoalSidebarPage({ input }: Readonly<{ input: GoalSidebarInput }>) {
  if (input.kind === "new_chat") {
    return <NewChatGoalSidebar api={input.api} binding={input.binding} />;
  }
  return <ExactGoalSidebar api={input.api} target={input.target} />;
}
function NewChatGoalSidebar({
  api,
  binding,
}: Readonly<{ api: GoalSidebarApi; binding: NewChatGoalBinding }>) {
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
    const diagnosticID = goalSetDiagnosticNotificationID();
    void binding
      .setGoal(draft)
      .then((result) => {
        if (result.outcome.kind === "rejected") {
          const message = goalErrorMessage(result.outcome.error.detail, t);
          push({ id: "goal-set-rejected", title: t("chat.goal.setFailed"), body: message, tone: "danger" });
          return;
        }
        if (result.outcome.diagnostic !== null) {
          reportGoalSetDiagnostic(push, t, result.outcome.diagnostic, diagnosticID);
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
}: Readonly<{ api: GoalSidebarApi; initialDraft?: string; target: ChatSessionTarget }>) {
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

type DraftState = Readonly<{ base: string; draft: string }>;
type GoalSidebarDerivedState = Readonly<{
  fact: ChatGoalFact | null;
  pendingIntent: ChatGoalMutationIntent | null;
  draftState: DraftState;
  displayedFact: ChatGoalFact | null;
  displayedStatus: ChatGoalStatus | null;
  displayedCreatedAt: string | null;
  displayedObjective: string;
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
  logger: ReturnType<typeof useAppServices>["logger"];
  push: ReturnType<typeof useStatusController>["push"];
  setError: (error: Error | null) => void;
  setExpanded: (expanded: boolean) => void;
  setEditing: (editing: boolean) => void;
  setLocalDraft: React.Dispatch<React.SetStateAction<DraftState>>;
  t: ReturnType<typeof useTranslation>["t"];
}>;
type GoalMutationOperationResult = Readonly<{
  mutation: ChatGoalMutationResult;
  diagnostic: ChatOperationError | null;
}>;

function useGoalMutationRunner(input: GoalMutationRunnerInput) {
  return useCallback(
    async (
      intent: ChatGoalMutationIntent,
      action: "set" | "pause" | "resume" | "reopen" | "clear",
      operation: () => Promise<GoalMutationOperationResult>,
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
        if (input.controller.succeed(handle, result.mutation)) {
          input.setError(null);
        }
        if (action === "set" && result.diagnostic !== null) {
          reportGoalSetDiagnostic(input.push, input.t, result.diagnostic, goalSetDiagnosticNotificationID());
        }
      } catch (cause: unknown) {
        const current = input.controller.fail(handle);
        const failure = goalMutationFailure(cause, input.t);
        const recover = () => {
          if (current) {
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
          input.push({
            id: `goal-${action}-failed`,
            title:
              action === "clear" ? input.t("chat.goal.clearFailed") : input.t("chat.goal.mutationFailed"),
            body: failure.message,
            tone: "danger",
          });
        };
        if (cause instanceof ContractError) {
          void recoverOrThrowDebugFailure({
            context: { operation: `Goal ${action}` },
            error: cause,
            logger: input.logger,
            message: "Goal mutation response was malformed.",
            recover,
          });
          return;
        }
        recover();
      }
    },
    [input],
  );
}

function goalMutationFailure(cause: unknown, t: ReturnType<typeof useTranslation>["t"]): Error {
  if (cause instanceof ChatOperationError) return new Error(goalErrorMessage(cause.detail, t));
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
  const presentation = goalPresentation(fact, pendingIntent, draftState.draft);
  const displayedFact = fact;
  const displayedStatus = presentation.status;
  const dirty = draftState.draft !== draftState.base;
  const unavailable = goalUnavailable(fact);
  const saveAvailable = dirty && nonBlank(draftState.draft) && pendingIntent === null;
  const actionsDisabled = pendingIntent !== null;
  return {
    actionsDisabled,
    dirty,
    displayedFact,
    displayedCreatedAt: presentation.createdAt,
    displayedObjective: presentation.objective,
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

function goalPresentation(
  fact: ChatGoalFact | null,
  pendingIntent: ChatGoalMutationIntent | null,
  draft: string,
): Readonly<{ objective: string; status: ChatGoalStatus | null; createdAt: string | null }> {
  if (pendingIntent?.kind === "clear") {
    return { objective: "", status: null, createdAt: null };
  }
  if (pendingIntent?.kind === "goal") {
    return pendingGoalPresentation(fact, pendingIntent, draft);
  }
  return { objective: draft, status: fact?.goal?.status ?? null, createdAt: fact?.goal?.createdAt ?? null };
}

function pendingGoalPresentation(
  fact: ChatGoalFact | null,
  pendingIntent: Extract<ChatGoalMutationIntent, { kind: "goal" }>,
  draft: string,
): Readonly<{ objective: string; status: ChatGoalStatus; createdAt: string | null }> {
  const authoritativeGoal = fact?.goal;
  const objective = pendingIntent.preview.objective === draft ? pendingIntent.preview.objective : draft;
  if (authoritativeGoal === undefined || authoritativeGoal === null) {
    return { objective, status: pendingIntent.preview.status, createdAt: null };
  }
  return {
    objective,
    status: pendingIntent.preview.status,
    createdAt:
      authoritativeGoal.objective === pendingIntent.preview.objective ? authoritativeGoal.createdAt : null,
  };
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
  api: GoalSidebarApi,
  initialDraft: string,
  target: ChatSessionTarget,
): ExactGoalSidebarModel {
  const { t } = useTranslation();
  const [localDraft, setLocalDraft] = useState<DraftState>({ base: "", draft: initialDraft });
  const controller = useMemo(() => new ChatGoalDestinationController(api, target), [api, target]);
  const reconcileSnapshot = useCallback(() => {
    setLocalDraft((current) => {
      const snapshot = controller.snapshot;
      const next = reconcileDraftState(current, observedGoalFact(snapshot), pendingGoalIntent(snapshot));
      return next.base === current.base && next.draft === current.draft ? current : next;
    });
  }, [controller]);
  const snapshot = useControllerSnapshot(controller, reconcileSnapshot);
  const { push } = useStatusController();
  const { logger } = useAppServices();
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
    displayedCreatedAt,
    displayedFact,
    displayedObjective,
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
    editing: (creationMode && pendingIntent === null) || editing,
    expanded: (creationMode && pendingIntent === null) || expanded,
    logger,
    push,
    setError,
    setExpanded,
    setEditing,
    setLocalDraft,
    t,
  });

  const save = () => {
    if (actionsDisabled) return;
    setEditing(false);
    const intent: ChatGoalMutationIntent = {
      kind: "goal",
      preview: { objective: draftState.draft, status: "active" },
    };
    void runMutation(intent, "set", async () => {
      const result = await api.setGoal({ kind: "session", sessionID: target.sessionID }, draftState.draft);
      if (result.outcome.kind === "rejected") {
        throw result.outcome.error;
      }
      return { mutation: result.outcome.mutation, diagnostic: result.outcome.diagnostic };
    });
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
    displayedCreatedAt,
    editing: (creationMode && pendingIntent === null) || editing,
    error,
    expanded: (creationMode && pendingIntent === null) || expanded,
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
    displayedObjective,
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
  if (pendingIntent?.kind === "goal" && fact?.goal === null) {
    return localDraft;
  }
  if (fact === null) {
    return localDraft.draft === localDraft.base ? { base: "", draft: "" } : { ...localDraft, base: "" };
  }
  const nextBase = fact.goal?.objective ?? "";
  return localDraft.draft === localDraft.base
    ? { base: nextBase, draft: nextBase }
    : { ...localDraft, base: nextBase };
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
      pending={
        model.pendingIntent?.kind === "goal" &&
        model.pendingIntent.preview.objective === model.draftState.draft
      }
    />
  );
}

function useBindingSnapshot(binding: NewChatGoalBinding) {
  const subscribe = useCallback((listener: () => void) => binding.subscribe(listener), [binding]);
  const getSnapshot = useCallback(() => binding.snapshot, [binding]);
  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}

function useControllerSnapshot(controller: ChatGoalDestinationController, onSnapshot: () => void) {
  const [snapshot, setSnapshot] = useState(controller.snapshot);
  useEffect(
    () =>
      controller.subscribe(() => {
        onSnapshot();
        setSnapshot(controller.snapshot);
      }),
    [controller, onSnapshot],
  );
  return snapshot;
}

function goalErrorMessage(error: ChatError, t: ReturnType<typeof useTranslation>["t"]): string {
  switch (error.kind) {
    case "runtime_unavailable":
      return t("chatSettings.errors.runtimeUnavailable");
    case "internal_failure":
      return error.cause ?? t("chatSettings.errors.internalFailure");
    case "unknown":
      return t("chatSettings.errors.unknown", { code: error.code });
    default:
      throw new Error("Unexpected Goal Set error detail.");
  }
}

type StatusPush = ReturnType<typeof useStatusController>["push"];

function goalSetDiagnosticNotificationID(): string {
  return `goal-set-diagnostic:${crypto.randomUUID()}`;
}

function reportGoalSetDiagnostic(
  push: StatusPush,
  t: ReturnType<typeof useTranslation>["t"],
  diagnostic: ChatOperationError,
  id: string,
): void {
  push({
    id,
    title: t("chat.goal.savedWithWarning"),
    body: goalErrorMessage(diagnostic.detail, t),
    tone: "warning",
  });
}
