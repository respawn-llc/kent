import { MutationObserver, type QueryClient } from "@tanstack/react-query";
import { useAtomMount, useAtomSet } from "@effect/atom-react";
import * as Atom from "effect/reactivity/Atom";
import * as AsyncResult from "effect/reactivity/AsyncResult";
import * as Effect from "effect/Effect";
import * as Stream from "effect/Stream";
import * as Cause from "effect/Cause";
import type { TFunction } from "i18next";

import {
  ChatOperationError,
  ContractError,
  errorMessage,
  type ChatSessionTarget,
  type ChatGoalStreamObservation,
} from "@/api";
import { queryAtom, recoverOrThrowDebugFailure, type AppServices, type StatusController } from "@/app-facade";
import type { GoalSidebarApi } from "./GoalSidebar";
import {
  deriveGoalSidebarState,
  localDraftForFact,
  reconcileDraftState,
  type DraftState,
  type GoalMutationIntent,
  type GoalObservationState,
} from "./goalSidebarState";
import { goalLifecycleAction } from "./GoalSidebarParts";
import {
  goalErrorMessage,
  goalSetDiagnosticNotificationID,
  reportGoalSetDiagnostic,
} from "./goalNotifications";

type Editor = Readonly<{ draft: DraftState; editing: boolean; expanded: boolean }>;
type Action =
  | Readonly<{ kind: "draft"; value: string }>
  | Readonly<{ kind: "editing" | "expanded"; value: boolean }>
  | Readonly<{ kind: "save" | "lifecycle" | "clear" }>
  | Readonly<{ kind: "retry" }>;

function observationFailure(error: Error, sequence: number | null): GoalObservationState {
  return { kind: sequence === null ? "error" : "loading_retryable", error, fact: null };
}

function validSequence(
  previous: number | null,
  event: Extract<ChatGoalStreamObservation, { kind: "event" }>["event"],
) {
  return previous === null
    ? event.kind === "hydration" && event.sequence === 1
    : event.kind === "update" && event.sequence === previous + 1;
}

export function createGoalSidebarViewModel({
  api,
  target,
  initialDraft,
  client,
  logger,
  push,
  t,
}: Readonly<{
  api: GoalSidebarApi;
  target: ChatSessionTarget;
  initialDraft: string;
  client: QueryClient;
  logger: AppServices["logger"];
  push: StatusController["push"];
  t: TFunction;
}>) {
  const loading: GoalObservationState = { kind: "loading", fact: null };
  const source = Atom.make(
    Stream.suspend(() => {
      let sequence: number | null = null;
      return api
        .subscribeGoal(target, async () => logger.reportObservationOverflow("goal"))
        .pipe(
          Stream.map((value): GoalObservationState => {
            switch (value.kind) {
              case "open":
                sequence = null;
                return loading;
              case "event": {
                const event = value.event;
                if (!validSequence(sequence, event))
                  return observationFailure(
                    new ContractError("Goal observation sequence is not continuous."),
                    sequence,
                  );
                sequence = event.sequence;
                return { kind: "observed", fact: event.fact };
              }
              case "error":
                return observationFailure(value.error, sequence);
              case "complete":
                return observationFailure(
                  new ContractError(
                    value.code === null
                      ? "Goal observation completed unexpectedly."
                      : `Goal observation completed with code ${value.code.toString()}: ${String(value.message)}`,
                  ),
                  sequence,
                );
            }
          }),
          Stream.takeUntil((value) => value.kind === "error" || value.kind === "loading_retryable"),
          Stream.prepend([loading]),
        );
    }),
    { initialValue: loading },
  );
  const observation = Atom.make((get) => {
    const result = get(source);
    if (AsyncResult.isFailure(result)) return observationFailure(new Error(Cause.pretty(result.cause)), null);
    return AsyncResult.getOrElse(result, () => loading);
  });
  const editorOwner = Atom.make((get) => {
    const editor = Atom.make<Editor>({
      draft: { base: "", draft: initialDraft },
      editing: false,
      expanded: false,
    });
    // Concurrent action contexts expire on the next action. Settlement belongs
    // to this destination lifetime, not the last draft or focus action.
    return {
      editor,
      failed(intent: GoalMutationIntent, submitted: Editor, previous: Editor) {
        const latest = get.once(editor);
        const restored = latest.draft === submitted.draft ? previous : latest;
        if (intent.kind === "set") get.set(editor, { ...restored, editing: true, expanded: true });
        else if (restored === previous) get.set(editor, previous);
      },
      succeeded(intent: GoalMutationIntent, submitted: Editor) {
        const latest = get.once(editor);
        const fact = get.once(observation).fact;
        get.set(editor, {
          ...latest,
          draft:
            (intent.kind === "set" || intent.kind === "clear") && latest.draft === submitted.draft
              ? localDraftForFact(fact)
              : reconcileDraftState(latest.draft, fact, null),
        });
      },
    };
  });
  const mutation = new MutationObserver(client, {
    retry: false,
    networkMode: "always",
    mutationFn: async (intent: GoalMutationIntent) => {
      switch (intent.kind) {
        case "set": {
          const result = await api.setGoal(
            { kind: "session", sessionID: target.sessionID },
            intent.preview.objective,
          );
          if (result.outcome.kind === "rejected") throw result.outcome.error;
          return result.outcome.diagnostic;
        }
        case "clear":
          await api.clearGoal(target);
          return null;
        case "pause":
          await api.pauseGoal(target);
          return null;
        case "resume":
        case "reopen":
          await api.resumeGoal(target);
          return null;
      }
    },
    onSuccess: (diagnostic) => {
      if (diagnostic !== null)
        reportGoalSetDiagnostic(push, t, diagnostic, goalSetDiagnosticNotificationID());
    },
    onError: async (cause, intent) => {
      const failure =
        cause instanceof ChatOperationError ? goalErrorMessage(cause.detail, t) : errorMessage(cause);
      push({
        id: `goal-${intent.kind}-failed`,
        title: t(intent.kind === "clear" ? "chat.goal.clearFailed" : "chat.goal.mutationFailed"),
        body: failure,
        tone: "danger",
      });
      if (cause instanceof ContractError) {
        await recoverOrThrowDebugFailure({
          context: { operation: `Goal ${intent.kind}` },
          error: cause,
          logger,
          message: "Goal mutation response was malformed.",
          recover: () => undefined,
        });
      }
    },
  });
  const requests = queryAtom(mutation);
  const state = Atom.make((get) => {
    const observed = get(observation);
    const request = get(requests);
    const pendingIntent = request.isPending ? request.variables : null;
    const local = get(get(editorOwner).editor);
    const derived = deriveGoalSidebarState(
      observed,
      pendingIntent,
      reconcileDraftState(local.draft, observed.fact, pendingIntent),
    );
    const creating = derived.fact?.goal === null && pendingIntent === null;
    return {
      ...derived,
      observation: observed,
      editing: creating || local.editing,
      expanded: creating || local.expanded,
    };
  });
  const action = Atom.fn<Action>()(
    (input, get) => {
      const current = get(state);
      const owner = get(editorOwner);
      const local = get(owner.editor);
      switch (input.kind) {
        case "draft":
          return Effect.sync(() => {
            get.set(owner.editor, { ...local, draft: { ...current.draftState, draft: input.value } });
          });
        case "editing":
        case "expanded":
          return Effect.sync(() => {
            get.set(owner.editor, { ...local, [input.kind]: input.value });
          });
        case "retry":
          return Effect.sync(() => {
            get.refresh(source);
          });
        case "save":
        case "lifecycle":
        case "clear":
          break;
      }
      return Effect.gen(function* () {
        if (mutation.getCurrentResult().isPending || current.fact === null) return;
        if (input.kind === "save" && (!current.saveAvailable || current.unavailable)) return;
        const intent = mutationIntent(input.kind, current);
        const previous: Editor = {
          draft: current.draftState,
          editing: current.editing,
          expanded: current.expanded,
        };
        const submitted: Editor =
          intent.kind === "clear"
            ? { draft: { base: "", draft: "" }, editing: true, expanded: true }
            : { ...previous, editing: intent.kind === "set" ? false : previous.editing };
        get.set(owner.editor, submitted);
        yield* Effect.tryPromise(async () => mutation.mutate(intent)).pipe(
          Effect.match({
            onFailure: () => {
              owner.failed(intent, submitted, previous);
            },
            onSuccess: () => {
              owner.succeeded(intent, submitted);
            },
          }),
        );
      });
    },
    { concurrent: true },
  );
  return { state, requests, action } as const;
}

export function useGoalSidebarActions(model: ReturnType<typeof createGoalSidebarViewModel>) {
  useAtomMount(model.requests);
  return useAtomSet(model.action);
}

function mutationIntent(
  kind: "save" | "lifecycle" | "clear",
  state: Atom.Type<ReturnType<typeof createGoalSidebarViewModel>["state"]>,
): GoalMutationIntent {
  if (kind === "clear") return { kind: "clear" };
  const lifecycle = goalLifecycleAction(state.displayedStatus);
  return {
    kind: kind === "save" ? "set" : lifecycle,
    preview: {
      objective:
        kind === "save" ? state.draftState.draft : (state.fact?.goal?.objective ?? state.draftState.draft),
      status: kind === "lifecycle" && lifecycle === "pause" ? "paused" : "active",
    },
  };
}
