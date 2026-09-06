import { queryOptions, type QueryClient } from "@tanstack/react-query";

import type {
  ChatApi,
  ChatGoalProjection,
  ChatMainView,
  ChatMainViewRead,
  ChatSessionTarget,
  ChatTranscriptPage,
  ChatTranscriptMessage,
  ChatTranscriptPayloadByKind,
} from "@/api";
import { ChatGoalProjectionSource } from "./chatGoalDestination";
import type { ChatTranscriptSink } from "./chatTranscriptHost";
import { ChatTranscriptObservation, type ChatTranscriptObservationState } from "./chatTranscriptObservation";
import {
  emptyChatProjectionState,
  reduceChatProjection,
  type ChatAuthorityTuple,
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
export type ChatTranscriptPageInput =
  Readonly<{ kind: "newest" }> | Readonly<{ kind: "older" | "newer"; cursor: number }>;
export type ChatRuntimeApi = Pick<ChatApi, "getMainView" | "getTranscriptPage" | "subscribeTranscript">;
export type ChatRuntimeHost = Readonly<{
  transcript: ChatTranscriptSink;
  onHumanInputInterrupted?(items: ChatTranscriptPayloadByKind["human_input_interrupted"]["Items"]): void;
  onWorktreeTransitionOutcome?(outcome: ChatTranscriptPayloadByKind["worktree_transition_outcome"]): void;
  onTranscriptError?(error: Error): void;
}>;
export type ChatRuntimeOwnerSnapshot = Readonly<{
  goal: ChatGoalProjection;
  observation: ChatTranscriptObservationState;
  disposed: boolean;
}>;
interface ActiveMainViewRead {
  token: number;
  requestedTuple: ChatAuthorityTuple | null;
  laterTuple: ChatAuthorityTuple | null;
  followUpUsed: boolean;
}

export const chatMainViewQueryOptions = (
  api: ChatRuntimeApi,
  target: ChatSessionTarget,
  admit: (read: ChatMainViewRead) => ChatMainView,
) =>
  queryOptions({
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

export async function executeChatTranscriptPage(
  api: ChatRuntimeApi,
  target: ChatSessionTarget,
  input: ChatTranscriptPageInput,
): Promise<ChatTranscriptPage> {
  switch (input.kind) {
    case "newest":
      return api.getTranscriptPage(target);
    case "older":
    case "newer":
      return api.getTranscriptPage(target, {
        direction: input.kind,
        value: input.cursor,
      });
  }
}

export class ChatRuntimeOwner {
  readonly goal = new ChatGoalProjectionSource();
  readonly #api: ChatRuntimeApi;
  readonly #target: ChatSessionTarget;
  readonly #queryClient: QueryClient;
  readonly #host: ChatRuntimeHost;
  readonly #queryKey: ReturnType<typeof queryKeys.chatMainView>;
  readonly #listeners = new Set<() => void>();
  #observation: ChatTranscriptObservation | null = null;
  #projection: ChatProjectionState = emptyChatProjectionState();
  #snapshotCache: ChatRuntimeOwnerSnapshot;
  #mainViewToken = 0;
  #activeMainViewRead: ActiveMainViewRead | null = null;
  #started = false;
  #disposed = false;

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
    this.#snapshotCache = this.#projectSnapshot();
    this.goal.subscribe(() => {
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
      this.#admitRead(this.#mainViewToken, this.#projection.metadataRevision, this.goal.generation, read),
    );
  }

  start(): void {
    if (this.#started || this.#disposed) return;
    this.#started = true;
    void this.forceMainViewRead();
    this.#startOpeningPage();
    this.#observation = new ChatTranscriptObservation(this.#api, this.#target, {
      onHydration: (kind, hydration) => {
        this.#host.transcript.hydration(kind, hydration);
        this.#admitProjection({ kind: "hydration", hydration });
      },
      onEvent: (event) => {
        this.#admitTranscriptEvent(event);
      },
      onRecoveryBegin: () => {
        this.#host.transcript.recoveryStarted();
      },
      onForceMainViewRead: () => {
        void this.forceMainViewRead();
      },
      onError: (error) => this.#host.onTranscriptError?.(error),
      onStateChange: () => {
        this.#notify();
      },
    });
    this.#observation.start();
  }

  async forceMainViewRead(): Promise<void> {
    if (this.#disposed) return;
    const token = ++this.#mainViewToken;
    this.#activeMainViewRead = null;
    await this.#queryClient.cancelQueries(
      { queryKey: this.#queryKey, exact: true },
      { revert: true, silent: true },
    );
    if (token !== this.#mainViewToken) return;
    this.#startMainViewRead(token, null, false);
  }

  async retryMainView(): Promise<void> {
    return this.forceMainViewRead();
  }

  retryTranscriptObservation(): void {
    this.#observation?.retry();
  }

  recoverTranscriptContinuity(): void {
    this.#observation?.recoverContinuity();
  }

  controlReconnected(): void {
    this.#observation?.replaceForReconnect();
  }

  requireMainViewAuthority(tuple: ChatAuthorityTuple): void {
    if (this.#disposed) return;
    const active = this.#activeMainViewRead;
    if (active !== null && active.token === this.#mainViewToken) {
      active.laterTuple = coalesceAuthorityTuple(active.laterTuple, tuple);
      return;
    }
    this.#startMainViewRead(this.#mainViewToken, tuple, false);
  }

  async dispose(): Promise<void> {
    if (this.#disposed) return;
    this.#disposed = true;
    this.#mainViewToken++;
    this.#activeMainViewRead = null;
    this.#observation?.close();
    this.#observation = null;
    this.#host.transcript.dispose();
    this.goal.dispose();
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
      currentGoalGeneration: this.goal.generation,
    });
    this.#storeProjectionMetadata(admitted.state);
    if (admitted.goalFact !== null) this.goal.admit(admitted.goalFact);
    this.#applyHostEffects(admitted.effects);
    return admitted.state.view ?? read.mainView;
  }

  #startMainViewRead(token: number, requestedTuple: ChatAuthorityTuple | null, followUpUsed: boolean): void {
    if (this.#disposed || token !== this.#mainViewToken || this.#activeMainViewRead !== null) return;
    const active: ActiveMainViewRead = {
      token,
      requestedTuple,
      laterTuple: null,
      followUpUsed,
    };
    this.#activeMainViewRead = active;
    const metadataRevisionAtStart = this.#projection.metadataRevision;
    const goalGenerationAtStart = this.goal.generation;
    const options = chatMainViewQueryOptions(this.#api, this.#target, (read) =>
      this.#admitRead(token, metadataRevisionAtStart, goalGenerationAtStart, read),
    );
    void this.#queryClient.fetchQuery(options).then(
      (view) => {
        this.#mainViewReadSucceeded(active, view.version);
      },
      () => {
        this.#mainViewReadFailed(active);
      },
    );
  }

  #mainViewReadSucceeded(active: ActiveMainViewRead, version: ChatAuthorityTuple): void {
    if (this.#activeMainViewRead !== active || active.token !== this.#mainViewToken) return;
    this.#activeMainViewRead = null;
    const demand = coalesceAuthorityTuple(active.requestedTuple, active.laterTuple);
    if (demand === null || authorityTupleSatisfies(version, demand)) return;
    if (active.followUpUsed) {
      if (active.laterTuple !== null) {
        this.#startMainViewRead(active.token, active.laterTuple, false);
      }
      return;
    }
    this.#startMainViewRead(active.token, demand, true);
  }

  #mainViewReadFailed(active: ActiveMainViewRead): void {
    if (this.#activeMainViewRead !== active || active.token !== this.#mainViewToken) return;
    this.#activeMainViewRead = null;
    if (active.laterTuple !== null) {
      this.#startMainViewRead(active.token, active.laterTuple, false);
    }
  }

  #admitTranscriptEvent(event: Exclude<ChatTranscriptMessage, { kind: "hydration" }>): void {
    if (this.#disposed) return;
    this.#host.transcript.event(event);
    this.#admitProjection({ kind: "event", event });
  }

  #admitProjection(input: Exclude<ChatProjectionInput, { kind: "authoritative-read" }>): void {
    const current = this.#currentProjection();
    const admitted = reduceChatProjection(current, input);
    this.#storeProjectionMetadata(admitted.state);
    if (admitted.state.view !== current.view && admitted.state.view !== null) {
      this.#queryClient.setQueryData(this.#queryKey, admitted.state.view);
    }
    if (admitted.goalFact !== null) this.goal.admit(admitted.goalFact);
    this.#applyHostEffects(admitted.effects);
    if (admitted.requiredAuthority !== null) {
      this.requireMainViewAuthority(admitted.requiredAuthority);
    }
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
    };
  }

  #startOpeningPage(): void {
    this.#host.transcript.openingStarted();
    void executeChatTranscriptPage(this.#api, this.#target, { kind: "newest" }).then(
      (page) => {
        this.#host.transcript.openingSucceeded(page);
      },
      (error: unknown) => {
        this.#host.transcript.openingFailed(
          error instanceof Error ? error : new Error("Transcript page failed."),
        );
      },
    );
  }

  #applyHostEffects(effects: readonly ChatProjectionHostEffect[]): void {
    for (const effect of effects) {
      if (effect.kind === "human-input-interrupted") {
        this.#host.onHumanInputInterrupted?.(effect.items);
      } else {
        this.#host.onWorktreeTransitionOutcome?.(effect.outcome);
      }
    }
  }

  #notify(): void {
    this.#snapshotCache = this.#projectSnapshot();
    for (const listener of this.#listeners) listener();
  }

  #projectSnapshot(): ChatRuntimeOwnerSnapshot {
    return {
      goal: this.goal.snapshot,
      observation: this.#observation?.state ?? { kind: "loading" },
      disposed: this.#disposed,
    };
  }
}

function coalesceAuthorityTuple(
  current: ChatAuthorityTuple | null,
  incoming: ChatAuthorityTuple | null,
): ChatAuthorityTuple | null {
  if (incoming === null) return current;
  if (current?.epoch !== incoming.epoch) return incoming;
  if (incoming.generation > current.generation) return incoming;
  if (incoming.generation < current.generation) return current;
  return incoming.sequence > current.sequence ? incoming : current;
}

function authorityTupleSatisfies(current: ChatAuthorityTuple, demand: ChatAuthorityTuple): boolean {
  if (current.epoch !== demand.epoch) return false;
  if (current.generation > demand.generation) return true;
  return current.generation === demand.generation && current.sequence >= demand.sequence;
}
