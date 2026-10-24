import { queryOptions, QueryObserver, type QueryClient } from "@tanstack/react-query";
import * as Atom from "effect/reactivity/Atom";
import * as Effect from "effect/Effect";
import * as Stream from "effect/Stream";

import type {
  ChatApi,
  ChatGoalProjection,
  ChatMainView,
  ChatMainViewRead,
  ChatSessionTarget,
  ChatTranscriptPayloadByKind,
  PendingPrompt,
} from "@/api";
import { ContractError } from "@/api";
import type { AppLogger } from "./logging";
import { recoverOrThrowDebugFailure } from "./debugFailure";
import { createChatTranscriptModel } from "./chatTranscriptHost";
import { observeChatTranscript, type ChatTranscriptObservationState } from "./chatTranscriptObservation";
import {
  emptyChatProjectionState,
  reduceChatProjection,
  type ChatProjectionHostEffect,
  type ChatProjectionInput,
  type ChatProjectionState,
} from "./chatRuntimeProjection";
import { queryKeys } from "./queryKeys";
import { queryAtom } from "./queryAtom";

export {
  compareAuthorityTuple,
  emptyChatProjectionState,
  reduceChatProjection,
} from "./chatRuntimeProjection";
export type {
  ChatAuthorityTuple,
  ChatProjectionHostEffect,
  ChatProjectionInput,
  ChatProjectionResult,
  ChatProjectionState,
} from "./chatRuntimeProjection";
export type ChatRuntimeApi = Pick<ChatApi, "getMainView" | "getTranscriptPage" | "subscribeTranscript">;
export type ChatRuntimeHost = Readonly<{
  logger: AppLogger;
  onConnectionReplaced?(replacement: ChatTranscriptPayloadByKind["connection_replaced"]): void;
  onHumanInputInterrupted?(items: ChatTranscriptPayloadByKind["human_input_interrupted"]["Items"]): void;
  onPendingWorkHydrated?(sessionID: string): void;
  onPendingWorkChanged?(): void;
  onPendingWorkRestored?(restoration: ChatTranscriptPayloadByKind["pending_work_restored"]): void;
  onUserInputCommitted?(inputIDs: readonly string[]): void;
  onWorktreeTransitionOutcome?(outcome: ChatTranscriptPayloadByKind["worktree_transition_outcome"]): void;
  onTranscriptError?(error: Error): void;
  onManualCompactionCompleted?(): void;
  onManualCompactionFailed?(diagnostic: ChatTranscriptPayloadByKind["compaction_status"]["Diagnostic"]): void;
}>;

export const chatMainViewQueryOptions = (
  api: ChatRuntimeApi,
  target: ChatSessionTarget,
  admit: (read: ChatMainViewRead) => ChatMainView,
) =>
  queryOptions<ChatMainView>({
    queryKey: queryKeys.chatMainView(target.sessionID),
    queryFn: async () => admit(await api.getMainView(target)),
    enabled: false,
    staleTime: 0,
    retry: false,
    refetchOnMount: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });

export function createChatRuntimeModel(
  api: ChatRuntimeApi,
  target: ChatSessionTarget,
  client: QueryClient,
  host: ChatRuntimeHost,
) {
  const transcript = createChatTranscriptModel(api, target, (error) => host.onTranscriptError?.(error));
  const projection = Atom.make(emptyChatProjectionState());
  const goal = Atom.make<ChatGoalProjection>({ kind: "unobserved" });
  const observationState = Atom.make<ChatTranscriptObservationState>({ kind: "loading" });
  const hydrationKind = Atom.make<"initial" | "scratch">("initial");
  const queryKey = queryKeys.chatMainView(target.sessionID);

  const scope = Atom.make((get) => {
    get.mount(projection);
    get.mount(goal);
    get.mount(observationState);
    get.mount(transcript.failure);
    get.mount(transcript.replacement);
    let goalGeneration = 0;
    let pendingCompactionNotice = false;
    const currentProjection = (): ChatProjectionState => ({
      ...get.once(projection),
      view: client.getQueryData<ChatMainView>(queryKey) ?? null,
    });
    const applyHostEffects = (effects: readonly ChatProjectionHostEffect[]) => {
      for (const effect of effects) {
        if (effect.kind === "compaction") {
          applyCompaction(effect.feedback);
          continue;
        }
        applyHostEffect(host, effect);
      }
    };
    const applyCompaction = (
      feedback: Extract<ChatProjectionHostEffect, { kind: "compaction" }>["feedback"],
    ) => {
      if (feedback.kind === "completed") pendingCompactionNotice = true;
      else if (feedback.kind === "failed") host.onManualCompactionFailed?.(feedback.diagnostic);
      else if (pendingCompactionNotice) {
        pendingCompactionNotice = false;
        host.onManualCompactionCompleted?.();
      }
    };
    const admit = (input: ChatProjectionInput) => {
      const current = currentProjection();
      const admitted = reduceChatProjection(current, input);
      get.set(projection, { ...admitted.state, view: null });
      if (admitted.goalFact !== null) {
        get.set(goal, { kind: "observed", value: admitted.goalFact });
        goalGeneration++;
      }
      applyHostEffects(admitted.effects);
      if (
        input.kind !== "authoritative-read" &&
        admitted.state.view !== current.view &&
        admitted.state.view !== null
      )
        client.setQueryData(queryKey, admitted.state.view);
      return admitted.state.view;
    };
    const observer = new QueryObserver(client, {
      ...chatMainViewQueryOptions(api, target, (read) => read.mainView),
      queryFn: async ({ signal }) => {
        const metadataRevisionAtStart = get.once(projection).metadataRevision;
        const goalGenerationAtStart = goalGeneration;
        const read = await api.getMainView(target);
        if (signal.aborted) return read.mainView;
        return (
          admit({
            kind: "authoritative-read",
            read,
            metadataRevisionAtStart,
            goalGenerationAtStart,
            currentGoalGeneration: goalGeneration,
          }) ?? read.mainView
        );
      },
    });
    const failure = Effect.fnUntraced(function* (error: Error) {
      pendingCompactionNotice = false;
      get.set(observationState, { kind: "error", error });
      yield* get.once(transcript.scope).apply({ kind: "observation-loss" }).pipe(Effect.orDie);
      if (error instanceof ContractError) {
        yield* Effect.promise(async () =>
          recoverOrThrowDebugFailure({
            context: { sessionID: target.sessionID },
            error,
            logger: host.logger,
            message: "Transcript observation violated its integrity contract.",
            recover: () => host.onTranscriptError?.(error),
          }),
        );
      } else host.onTranscriptError?.(error);
    });
    const replaceObservation = () => {
      get.set(observationState, { kind: "loading" });
      get.set(hydrationKind, "scratch");
      get.refresh(observation);
    };
    get.set(transcript.failure, (error) =>
      Effect.gen(function* () {
        get.set(observationState, { kind: "error", error });
        get.refresh(observation);
        yield* failure(error);
      }),
    );
    get.set(transcript.replacement, replaceObservation);
    get.addFinalizer(() => {
      observer.destroy();
    });
    return { observer, metadata: queryAtom(observer), admit, failure, replaceObservation };
  });
  const metadata = Atom.make((get) => get(get(scope).metadata));
  const observation = Atom.make(
    (get) => {
      const owner = get(scope);
      if (get.once(observationState).kind === "error") return Effect.succeed(null);
      const kind = get.once(hydrationKind);
      return observeChatTranscript(api, target, kind).pipe(
        Stream.runForEach((delivery) =>
          Effect.gen(function* () {
            if (delivery.kind === "hydration") {
              yield* transcript.receive(get, delivery.hydrationKind, delivery.hydration);
              owner.admit({ kind: "hydration", hydration: delivery.hydration });
              for (const row of delivery.hydration.TailSegment.Entries) {
                if (row.Kind === "user" && row.User !== null)
                  host.onUserInputCommitted?.(row.User.QueueItemIDs);
              }
              get.set(observationState, { kind: "observing" });
            } else {
              yield* transcript.event(get, delivery.event, () => {
                owner.admit({ kind: "event", event: delivery.event });
                if (
                  delivery.event.kind === "committed_row" &&
                  delivery.event.payload.Kind === "user" &&
                  delivery.event.payload.User !== null
                )
                  host.onUserInputCommitted?.(delivery.event.payload.User.QueueItemIDs);
              });
            }
          }),
        ),
        Effect.catch((failure) => owner.failure(failure.error)),
        Effect.as(null),
      );
    },
    { initialValue: null },
  );
  const forceMainViewRead = Atom.fn(
    (_, get) =>
      Effect.gen(function* () {
        const { observer } = get(scope);
        yield* Effect.promise(async () =>
          client.cancelQueries({ queryKey, exact: true }, { revert: true, silent: true }),
        );
        yield* Effect.promise(async () => observer.refetch());
      }),
    { concurrent: true },
  );
  const retryTranscriptObservation = Atom.fn(
    (_, get) =>
      Effect.sync(() => {
        if (get(observationState).kind === "error") get(scope).replaceObservation();
      }),
    { concurrent: true },
  );
  const resolvePendingPrompts = Atom.fn<ReadonlySet<string>>()(
    (toolCallIDs, get) =>
      Effect.sync(() => {
        get(scope).admit({ kind: "prompts-resolved", toolCallIDs });
      }),
    { concurrent: true },
  );
  const replacePendingPrompts = Atom.fn<readonly PendingPrompt[]>()(
    (prompts, get) =>
      Effect.sync(() => {
        get(scope).admit({ kind: "prompts-replaced", prompts });
      }),
    { concurrent: true },
  );
  const snapshot = Atom.make((get) => ({
    activeProcessCount: get(projection).activeProcesses.size,
    pendingPrompts: get(projection).pendingPrompts,
    goal: get(goal),
    observation: get(observationState),
    transcript: get(transcript.snapshot),
    transcriptPresentationUpdate: get(transcript.presentation),
    jumpPending: get(transcript.jumpPending),
  }));
  const mount = Atom.make((get) => {
    get.mount(scope);
    get.mount(metadata);
    get.mount(transcript.mount);
    get.mount(observation);
    get.mount(forceMainViewRead);
    get.mount(retryTranscriptObservation);
    get.mount(resolvePendingPrompts);
    get.mount(replacePendingPrompts);
    get.set(forceMainViewRead, undefined);
    return null;
  });
  const diagnostics = Atom.make(
    (get) => {
      const failure = [get(observation), ...get(transcript.outcomes)].find(
        (result) => result._tag === "Failure",
      );
      return failure === undefined ? Effect.succeed(null) : Effect.failCause(failure.cause);
    },
    { initialValue: null },
  );
  return {
    snapshot,
    metadata,
    mount,
    diagnostics,
    forceMainViewRead,
    retryTranscriptObservation,
    resolvePendingPrompts,
    replacePendingPrompts,
    transcript: { dispatch: transcript.dispatch, jump: transcript.jump },
  } as const;
}

export type ChatRuntimeOwner = ReturnType<typeof createChatRuntimeModel>;
export type ChatRuntimeOwnerSnapshot = Atom.Type<ChatRuntimeOwner["snapshot"]>;

function applyHostEffect(
  host: ChatRuntimeHost,
  effect: Exclude<ChatProjectionHostEffect, { kind: "compaction" }>,
) {
  if (
    effect.kind === "pending-work-hydrated" ||
    effect.kind === "pending-work-changed" ||
    effect.kind === "pending-work-restored"
  ) {
    applyPendingWork(host, effect);
    return;
  }
  switch (effect.kind) {
    case "human-input-interrupted":
      host.onHumanInputInterrupted?.(effect.items);
      break;
    case "worktree-transition-outcome":
      host.onWorktreeTransitionOutcome?.(effect.outcome);
      break;
    case "connection-replaced":
      host.onConnectionReplaced?.(effect.replacement);
      break;
  }
}

function applyPendingWork(
  host: ChatRuntimeHost,
  effect: Extract<
    ChatProjectionHostEffect,
    { kind: "pending-work-hydrated" | "pending-work-changed" | "pending-work-restored" }
  >,
) {
  switch (effect.kind) {
    case "pending-work-hydrated":
      host.onPendingWorkHydrated?.(effect.sessionID);
      break;
    case "pending-work-changed":
      host.onPendingWorkChanged?.();
      break;
    case "pending-work-restored":
      host.onPendingWorkRestored?.(effect.restoration);
      break;
  }
}
