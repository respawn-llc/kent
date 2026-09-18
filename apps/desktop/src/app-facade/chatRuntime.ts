import { queryOptions, type QueryClient } from "@tanstack/react-query";

import type {
  ChatApi,
  ChatGoalFact,
  ChatGoalProjection,
  ChatMainView,
  ChatMainViewRead,
  ChatSessionTarget,
  ChatTranscriptMessage,
  ChatTranscriptPayloadByKind,
  PendingPrompt,
} from "@/api";
import type { AppLogger } from "./logging";
import { recoverOrThrowDebugFailure } from "./debugFailure";
import { ChatTranscriptHost } from "./chatTranscriptHost";
import { ChatTranscriptObservation, type ChatTranscriptObservationState } from "./chatTranscriptObservation";
import {
  emptyChatProjectionState,
  reduceChatProjection,
  type ChatProjectionHostEffect,
  type ChatProjectionInput,
  type ChatProjectionState,
} from "./chatRuntimeProjection";
import { queryKeys } from "./queryKeys";

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
  onHumanInputInterrupted?(items: ChatTranscriptPayloadByKind["human_input_interrupted"]["Items"]): void;
  onPendingWorkHydrated?(sessionID: string): void;
  onPendingWorkChanged?(): void;
  onPendingWorkRestored?(restoration: ChatTranscriptPayloadByKind["pending_work_restored"]): void;
  onWorktreeTransitionOutcome?(outcome: ChatTranscriptPayloadByKind["worktree_transition_outcome"]): void;
  onTranscriptError?(error: Error): void;
  onManualCompactionCompleted?(): void;
  onManualCompactionFailed?(diagnostic: ChatTranscriptPayloadByKind["compaction_status"]["Diagnostic"]): void;
}>;
export type ChatRuntimeOwnerSnapshot = Readonly<{
  activeProcessCount: number;
  pendingPrompts: readonly PendingPrompt[];
  goal: ChatGoalProjection;
  observation: ChatTranscriptObservationState;
  transcript: ChatTranscriptHost["snapshot"];
  transcriptPresentationUpdate: ChatTranscriptHost["presentationUpdate"];
  jumpPending: boolean;
  disposed: boolean;
}>;

export const chatMainViewQueryOptions = (
  api: ChatRuntimeApi,
  target: ChatSessionTarget,
  admit: (read: ChatMainViewRead) => ChatMainView,
) =>
  queryOptions<ChatMainView>({
    queryKey: queryKeys.chatMainView(target.sessionID),
    queryFn: async () => {
      const read = await api.getMainView(target);
      return admit(read);
    },
    enabled: false,
    staleTime: 0,
    retry: false,
    refetchOnMount: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });

export class ChatRuntimeOwner {
  readonly transcript: ChatTranscriptHost;
  readonly #api: ChatRuntimeApi;
  readonly #target: ChatSessionTarget;
  readonly #queryClient: QueryClient;
  readonly #host: ChatRuntimeHost;
  readonly #queryKey: ReturnType<typeof queryKeys.chatMainView>;
  readonly #listeners = new Set<() => void>();
  #observation: ChatTranscriptObservation | null = null;
  #projection: ChatProjectionState = emptyChatProjectionState();
  #goal: ChatGoalProjection = { kind: "unobserved" };
  #goalGeneration = 0;
  #snapshotCache: ChatRuntimeOwnerSnapshot;
  #mainViewToken = 0;
  #mountGeneration = 0;
  #started = false;
  #disposed = false;
  #pendingCompactionNotice = false;

  constructor(
    api: ChatRuntimeApi,
    target: ChatSessionTarget,
    queryClient: QueryClient,
    host: ChatRuntimeHost,
  ) {
    this.#api = api;
    this.#target = target;
    this.#queryClient = queryClient;
    this.#host = host;
    this.#queryKey = queryKeys.chatMainView(target.sessionID);
    this.transcript = new ChatTranscriptHost(api, target, {
      onContractFailure: (error) => {
        this.#observation?.rejectIntegrity(error);
      },
      onOpeningFailure: (error) => this.#host.onTranscriptError?.(error),
      onScratchRehydration: () => {
        this.#observation?.rehydrateAfterCompaction();
      },
    });
    this.#snapshotCache = this.#projectSnapshot();
    this.transcript.subscribe(() => {
      this.#notify();
    });
  }

  get snapshot(): ChatRuntimeOwnerSnapshot {
    return this.#snapshotCache;
  }

  subscribe(listener: () => void): () => void {
    if (this.#disposed) return () => undefined;
    this.#listeners.add(listener);
    return () => this.#listeners.delete(listener);
  }

  mainViewOptions() {
    return chatMainViewQueryOptions(this.#api, this.#target, (read) =>
      this.#admitRead(this.#mainViewToken, this.#projection.metadataRevision, this.#goalGeneration, read),
    );
  }

  start(): void {
    if (this.#started || this.#disposed) return;
    this.#started = true;
    void this.forceMainViewRead();
    this.transcript.open();
    this.#observation = new ChatTranscriptObservation(this.#api, this.#target, {
      onHydration: (kind, hydration) => {
        const admission = this.transcript.hydration(kind, hydration);
        if (admission.kind === "rejected") throw admission.error;
        this.#admitProjection({ kind: "hydration", hydration });
      },
      onEvent: (event) => {
        this.#admitTranscriptEvent(event);
      },
      onIntegrityFailure: (error) => {
        void recoverOrThrowDebugFailure({
          context: { sessionID: this.#target.sessionID },
          error,
          logger: this.#host.logger,
          message: "Transcript observation violated its integrity contract.",
          recover: () => this.#host.onTranscriptError?.(error),
        });
      },
      onObservationLoss: () => {
        this.#pendingCompactionNotice = false;
        this.transcript.observationLost();
      },
      onError: (error) => this.#host.onTranscriptError?.(error),
      onStateChange: () => {
        this.#notify();
      },
    });
    this.#observation.start();
  }

  mount(): () => void {
    const generation = ++this.#mountGeneration;
    this.start();
    return () => {
      queueMicrotask(() => {
        if (generation === this.#mountGeneration) void this.dispose();
      });
    };
  }

  async forceMainViewRead(): Promise<void> {
    if (this.#disposed) return;
    const token = ++this.#mainViewToken;
    await this.#queryClient.cancelQueries(
      { queryKey: this.#queryKey, exact: true },
      { revert: true, silent: true },
    );
    if (token !== this.#mainViewToken) return;
    this.#startMainViewRead(token);
  }

  async retryMainView(): Promise<void> {
    return this.forceMainViewRead();
  }

  retryTranscriptObservation(): void {
    this.#observation?.retry();
  }

  replacePendingPrompts(prompts: readonly PendingPrompt[]): void {
    if (!this.#disposed) this.#admitProjection({ kind: "prompts-replaced", prompts });
  }

  resolvePendingPrompts(toolCallIDs: ReadonlySet<string>): void {
    if (!this.#disposed) this.#admitProjection({ kind: "prompts-resolved", toolCallIDs });
  }

  async dispose(): Promise<void> {
    if (this.#disposed) return;
    this.#disposed = true;
    this.#pendingCompactionNotice = false;
    this.#mainViewToken++;
    this.#observation?.close();
    this.#observation = null;
    this.transcript.dispose();
    await this.#queryClient.cancelQueries(
      { queryKey: this.#queryKey, exact: true },
      { revert: true, silent: true },
    );
    this.#notify();
    this.#listeners.clear();
  }

  #admitRead(
    token: number,
    metadataRevisionAtStart: number,
    goalGenerationAtStart: number,
    read: ChatMainViewRead,
  ): ChatMainView {
    if (this.#disposed || token !== this.#mainViewToken) return read.mainView;
    const admitted = reduceChatProjection(this.#currentProjection(), {
      kind: "authoritative-read",
      read,
      metadataRevisionAtStart,
      goalGenerationAtStart,
      currentGoalGeneration: this.#goalGeneration,
    });
    this.#storeProjectionMetadata(admitted.state);
    if (admitted.goalFact !== null) {
      this.#admitGoal(admitted.goalFact);
      this.#notify();
    }
    this.#applyHostEffects(admitted.effects);
    return admitted.state.view ?? read.mainView;
  }

  #startMainViewRead(token: number): void {
    if (this.#disposed || token !== this.#mainViewToken) return;
    const metadataRevisionAtStart = this.#projection.metadataRevision;
    const goalGenerationAtStart = this.#goalGeneration;
    const options = chatMainViewQueryOptions(this.#api, this.#target, (read) =>
      this.#admitRead(token, metadataRevisionAtStart, goalGenerationAtStart, read),
    );
    void this.#queryClient.fetchQuery(options).catch(() => undefined);
  }

  #admitTranscriptEvent(event: Exclude<ChatTranscriptMessage, { kind: "hydration" }>): void {
    if (this.#disposed) return;
    const admission = this.transcript.event(event);
    if (admission.kind === "rejected") throw admission.error;
    this.#admitProjection({ kind: "event", event });
  }

  #admitProjection(input: Exclude<ChatProjectionInput, { kind: "authoritative-read" }>): void {
    const current = this.#currentProjection();
    const admitted = reduceChatProjection(current, input);
    this.#storeProjectionMetadata(admitted.state);
    if (admitted.state.view !== current.view && admitted.state.view !== null) {
      this.#queryClient.setQueryData(this.#queryKey, admitted.state.view);
    }
    if (admitted.goalFact !== null) {
      this.#admitGoal(admitted.goalFact);
      this.#notify();
    }
    this.#applyHostEffects(admitted.effects);
    if (
      admitted.state.pendingPrompts !== current.pendingPrompts ||
      admitted.state.activeProcesses !== current.activeProcesses
    )
      this.#notify();
  }

  #currentProjection(): ChatProjectionState {
    return {
      ...this.#projection,
      view: this.#queryClient.getQueryData<ChatMainView>(this.#queryKey) ?? null,
    };
  }

  #storeProjectionMetadata(state: ChatProjectionState): void {
    this.#projection = {
      view: null,
      metadataRevision: state.metadataRevision,
      pendingMetadata: state.pendingMetadata,
      pendingPrompts: state.pendingPrompts,
      activeProcesses: state.activeProcesses,
    };
  }

  #applyHostEffects(effects: readonly ChatProjectionHostEffect[]): void {
    for (const effect of effects) {
      if (effect.kind === "compaction") {
        this.#applyCompactionFeedback(effect.feedback);
      } else if (effect.kind === "human-input-interrupted") {
        this.#host.onHumanInputInterrupted?.(effect.items);
      } else if (effect.kind === "worktree-transition-outcome") {
        this.#host.onWorktreeTransitionOutcome?.(effect.outcome);
      } else if (effect.kind === "pending-work-hydrated") {
        this.#host.onPendingWorkHydrated?.(effect.sessionID);
      } else if (effect.kind === "pending-work-changed") {
        this.#host.onPendingWorkChanged?.();
      } else {
        this.#host.onPendingWorkRestored?.(effect.restoration);
      }
    }
  }

  #applyCompactionFeedback(
    feedback: Extract<ChatProjectionHostEffect, { kind: "compaction" }>["feedback"],
  ): void {
    if (feedback.kind === "completed") this.#pendingCompactionNotice = true;
    else if (feedback.kind === "failed") this.#host.onManualCompactionFailed?.(feedback.diagnostic);
    else if (this.#pendingCompactionNotice) {
      this.#pendingCompactionNotice = false;
      this.#host.onManualCompactionCompleted?.();
    }
  }

  #notify(): void {
    this.#snapshotCache = this.#projectSnapshot();
    for (const listener of this.#listeners) listener();
  }

  #projectSnapshot(): ChatRuntimeOwnerSnapshot {
    return {
      activeProcessCount: this.#projection.activeProcesses.size,
      pendingPrompts: this.#projection.pendingPrompts,
      goal: this.#goal,
      observation: this.#observation?.state ?? { kind: "loading" },
      transcript: this.transcript.snapshot,
      transcriptPresentationUpdate: this.transcript.presentationUpdate,
      jumpPending: this.transcript.jumpPending,
      disposed: this.#disposed,
    };
  }

  #admitGoal(fact: ChatGoalFact): void {
    this.#goal = { kind: "observed", value: fact };
    this.#goalGeneration++;
  }
}
