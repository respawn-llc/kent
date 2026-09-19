import type {
  ChatGoalFact,
  ChatMainView,
  ChatMainViewRead,
  ChatRuntimeActivity,
  ChatTranscriptMessage,
  ChatTranscriptPayloadByKind,
  PendingPrompt,
} from "@/api";
import { chatExecutionTarget, chatRuntimeActivity, goalFactFromTranscript, orderPendingPrompts } from "@/api";

export type ChatAuthorityTuple = ChatMainView["version"];
type CompactionFeedback =
  | Readonly<{ kind: "completed" | "idle" }>
  | Readonly<{ kind: "failed"; diagnostic: ChatTranscriptPayloadByKind["compaction_status"]["Diagnostic"] }>;
export type ChatProjectionHostEffect =
  | Readonly<{ kind: "compaction"; feedback: CompactionFeedback }>
  | Readonly<{ kind: "connection-replaced"; replacement: ChatTranscriptPayloadByKind["connection_replaced"] }>
  | Readonly<{ kind: "pending-work-hydrated"; sessionID: string }>
  | Readonly<{ kind: "pending-work-changed" }>
  | Readonly<{
      kind: "pending-work-restored";
      restoration: ChatTranscriptPayloadByKind["pending_work_restored"];
    }>
  | Readonly<{
      kind: "human-input-interrupted";
      items: ChatTranscriptPayloadByKind["human_input_interrupted"]["Items"];
    }>
  | Readonly<{
      kind: "worktree-transition-outcome";
      outcome: ChatTranscriptPayloadByKind["worktree_transition_outcome"];
    }>;
type PendingMetadata = Readonly<{
  sessionIdentity?: ChatTranscriptPayloadByKind["session_identity"];
  sessionStatus?: Omit<ChatTranscriptPayloadByKind["session_status"], "CompactionCount">;
  contextUsage?: ChatTranscriptPayloadByKind["context_usage"] | null;
  compactionCount?: number;
  runtime?: Readonly<{ version: ChatAuthorityTuple; activity: ChatRuntimeActivity }>;
}>;
export type ChatProjectionState = Readonly<{
  pendingPrompts: readonly PendingPrompt[];
  view: ChatMainView | null;
  metadataRevision: number;
  pendingMetadata: PendingMetadata | null;
}>;
export type ChatProjectionInput =
  | Readonly<{ kind: "prompts-replaced"; prompts: readonly PendingPrompt[] }>
  | Readonly<{ kind: "prompts-resolved"; toolCallIDs: ReadonlySet<string> }>
  | Readonly<{
      kind: "authoritative-read";
      read: ChatMainViewRead;
      metadataRevisionAtStart: number;
      goalGenerationAtStart: number;
      currentGoalGeneration: number;
    }>
  | Readonly<{ kind: "hydration"; hydration: ChatTranscriptPayloadByKind["hydration"] }>
  | Readonly<{ kind: "event"; event: ChatTranscriptMessage }>;
export type ChatProjectionResult = Readonly<{
  state: ChatProjectionState;
  goalFact: ChatGoalFact | null;
  effects: readonly ChatProjectionHostEffect[];
}>;

export function emptyChatProjectionState(): ChatProjectionState {
  return { view: null, metadataRevision: 0, pendingMetadata: null, pendingPrompts: [] };
}

export function reduceChatProjection(
  state: ChatProjectionState,
  input: ChatProjectionInput,
): ChatProjectionResult {
  switch (input.kind) {
    case "prompts-replaced":
      return result({ ...state, pendingPrompts: orderPendingPrompts(input.prompts) });
    case "prompts-resolved":
      return result({
        ...state,
        pendingPrompts: state.pendingPrompts.filter((prompt) => !input.toolCallIDs.has(prompt.toolCallID)),
      });
    case "authoritative-read":
      return admitRead(state, input);
    case "hydration":
      return admitHydration(state, input.hydration);
    case "event":
      return admitEvent(state, input.event);
  }
}

function admitRead(
  state: ChatProjectionState,
  input: Extract<ChatProjectionInput, { kind: "authoritative-read" }>,
): ChatProjectionResult {
  const current = state.view;
  const activity = admitAuthorityActivity(current, input.read.mainView);
  const metadataCurrent = state.metadataRevision === input.metadataRevisionAtStart;
  let view =
    current === null
      ? input.read.mainView
      : metadataCurrent
        ? { ...input.read.mainView, version: activity.version, activity: activity.activity }
        : { ...current, version: activity.version, activity: activity.activity };
  if (state.pendingMetadata !== null) view = applyPendingMetadata(view, state.pendingMetadata);
  return {
    state: { ...state, view, pendingMetadata: null },
    goalFact: input.goalGenerationAtStart === input.currentGoalGeneration ? input.read.goal : null,
    effects: [],
  };
}

function admitHydration(
  state: ChatProjectionState,
  hydration: ChatTranscriptPayloadByKind["hydration"],
): ChatProjectionResult {
  const metadata: PendingMetadata = {
    sessionIdentity: hydration.SessionIdentity,
    ...statusMetadata(hydration.SessionStatus),
    contextUsage: hydration.ContextUsage,
    runtime: {
      version: runtimeVersion(hydration.RuntimeReadModelUpdate.Version),
      activity: chatRuntimeActivity(hydration.RuntimeReadModelUpdate.Activity),
    },
  };
  const next = {
    ...admitMetadata(state, metadata),
    pendingPrompts: orderPendingPrompts(
      hydration.PendingPrompts.flatMap((update) => (update.state === "pending" ? [update.prompt] : [])),
    ),
  };
  return {
    state: next,
    goalFact: hydration.GoalStatus === null ? null : goalFactFromTranscript(hydration.GoalStatus),
    effects: [
      { kind: "pending-work-hydrated", sessionID: hydration.SessionIdentity.SessionID },
      ...idleEffects(next),
    ],
  };
}

function admitEvent(state: ChatProjectionState, event: ChatTranscriptMessage): ChatProjectionResult {
  if (event.kind === "pending_work_changed")
    return result(state, { effects: [{ kind: "pending-work-changed" }] });
  if (event.kind === "pending_work_restored")
    return result(state, { effects: [{ kind: "pending-work-restored", restoration: event.payload }] });
  if (event.kind === "prompt") return admitPrompt(state, event.payload);
  if (event.kind === "runtime_read_model_update") return admitIncrementalRuntime(state, event.payload);
  if (event.kind === "session_identity") return metadataResult(state, { sessionIdentity: event.payload });
  if (event.kind === "session_status") return metadataResult(state, statusMetadata(event.payload));
  if (event.kind === "context_usage") return metadataResult(state, { contextUsage: event.payload });
  if (event.kind === "compaction_status") return admitCompaction(state, event.payload);
  if (event.kind === "goal_status") return result(state, { goalFact: goalFactFromTranscript(event.payload) });
  if (event.kind === "human_input_interrupted") {
    return result(state, {
      effects: [{ kind: "human-input-interrupted", items: event.payload.Items }],
    });
  }
  if (event.kind === "worktree_transition_outcome") {
    return result(state, {
      effects: [{ kind: "worktree-transition-outcome", outcome: event.payload }],
    });
  }
  if (event.kind === "connection_replaced") {
    return result(state, { effects: [{ kind: "connection-replaced", replacement: event.payload }] });
  }
  return result(state);
}

function admitCompaction(
  state: ChatProjectionState,
  status: ChatTranscriptPayloadByKind["compaction_status"],
): ChatProjectionResult {
  const next = status.State === "completed" ? admitMetadata(state, { compactionCount: status.Count }) : state;
  if (status.Mode !== "manual" || status.RequestID == null || status.State === "started") return result(next);
  const feedback: CompactionFeedback =
    status.State === "completed" ? { kind: "completed" } : { kind: "failed", diagnostic: status.Diagnostic };
  return result(next, { effects: [{ kind: "compaction", feedback }] });
}

function admitPrompt(
  state: ChatProjectionState,
  update: ChatTranscriptPayloadByKind["prompt"],
): ChatProjectionResult {
  if (update.state === "resolved") {
    return result({
      ...state,
      pendingPrompts: state.pendingPrompts.filter((prompt) => prompt.toolCallID !== update.toolCallID),
    });
  }
  const exists = state.pendingPrompts.some((prompt) => prompt.toolCallID === update.prompt.toolCallID);
  return result({
    ...state,
    pendingPrompts: orderPendingPrompts(
      exists
        ? state.pendingPrompts.map((prompt) =>
            prompt.toolCallID === update.prompt.toolCallID ? update.prompt : prompt,
          )
        : [...state.pendingPrompts, update.prompt],
    ),
  });
}

function admitIncrementalRuntime(
  state: ChatProjectionState,
  update: ChatTranscriptPayloadByKind["runtime_read_model_update"],
): ChatProjectionResult {
  const incoming = runtimeVersion(update.Version);
  const current = state.view?.version ?? state.pendingMetadata?.runtime?.version ?? null;
  if (current === null) {
    return runtimeResult(
      admitRuntime(state, {
        runtime: { version: incoming, activity: chatRuntimeActivity(update.Activity) },
      }),
    );
  }
  const comparison = compareAuthorityTuple(current, incoming);
  if (comparison === "newer-sequence" || comparison === "forward-authority") {
    return runtimeResult(
      admitRuntime(state, {
        runtime: { version: incoming, activity: chatRuntimeActivity(update.Activity) },
      }),
    );
  }
  return result(state);
}

function idleEffects(state: ChatProjectionState): readonly ChatProjectionHostEffect[] {
  const activity = state.view?.activity ?? state.pendingMetadata?.runtime?.activity;
  return activity?.state === "registered_idle" ? [{ kind: "compaction", feedback: { kind: "idle" } }] : [];
}

function runtimeResult(state: ChatProjectionState): ChatProjectionResult {
  return result(state, { effects: idleEffects(state) });
}

function admitRuntime(state: ChatProjectionState, runtime: PendingMetadata): ChatProjectionState {
  if (state.view === null) {
    return {
      ...state,
      pendingMetadata: mergeMetadata(state.pendingMetadata, runtime),
    };
  }
  return {
    ...state,
    view: applyPendingMetadata(state.view, runtime),
  };
}

function metadataResult(state: ChatProjectionState, metadata: PendingMetadata): ChatProjectionResult {
  return result(admitMetadata(state, metadata));
}

function admitMetadata(state: ChatProjectionState, metadata: PendingMetadata): ChatProjectionState {
  if (state.view === null) {
    return {
      ...state,
      view: null,
      metadataRevision: state.metadataRevision + 1,
      pendingMetadata: mergeMetadata(state.pendingMetadata, metadata),
    };
  }
  return {
    ...state,
    view: applyPendingMetadata(state.view, metadata),
    metadataRevision: state.metadataRevision + 1,
    pendingMetadata: null,
  };
}

function mergeMetadata(previous: PendingMetadata | null, next: PendingMetadata): PendingMetadata {
  return { ...(previous ?? {}), ...next };
}

function statusMetadata(status: ChatTranscriptPayloadByKind["session_status"]): PendingMetadata {
  const { CompactionCount: compactionCount, ...sessionStatus } = status;
  return { sessionStatus, compactionCount };
}

function applyPendingMetadata(view: ChatMainView, metadata: PendingMetadata): ChatMainView {
  let next = view;
  if (metadata.sessionIdentity !== undefined) {
    const identity = metadata.sessionIdentity;
    next = {
      ...next,
      sessionID: identity.SessionID,
      sessionName: identity.SessionName,
      executionTarget:
        identity.ExecutionTarget === null
          ? next.executionTarget
          : chatExecutionTarget(identity.ExecutionTarget),
      status: {
        ...next.status,
        conversationFreshness: identity.ConversationFreshness,
      },
    };
  }
  if (metadata.sessionStatus !== undefined) {
    const status = metadata.sessionStatus;
    next = {
      ...next,
      status: {
        ...next.status,
        reviewerFrequency: status.ReviewerFrequency,
        reviewerEnabled: status.ReviewerEnabled,
        autoCompactionEnabled: status.AutoCompactionEnabled,
        questionsEnabled: status.QuestionsEnabled,
        fastModeAvailable: status.FastModeAvailable,
        fastModeEnabled: status.FastModeEnabled,
        previousSessionID: status.PreviousSessionID ?? null,
        parentAgentSessionID: status.ParentAgentSessionID ?? null,
        navigationTargetSessionID: status.NavigationTargetSessionID ?? null,
        thinkingLevel: status.ThinkingLevel,
        compactionMode: status.CompactionMode,
        workflowSession:
          status.Workflow === null
            ? null
            : { taskID: status.Workflow.TaskID, workflowID: status.Workflow.WorkflowID },
      },
    };
  }
  if ("contextUsage" in metadata) {
    next = {
      ...next,
      status: {
        ...next.status,
        contextUsage: projectedContextUsage(metadata.contextUsage),
      },
    };
  }
  if (metadata.compactionCount !== undefined)
    next = { ...next, status: { ...next.status, compactionCount: metadata.compactionCount } };
  if (metadata.runtime !== undefined) {
    const admitted = admitAuthorityActivity(next, {
      ...next,
      version: metadata.runtime.version,
      activity: metadata.runtime.activity,
    });
    next = { ...next, version: admitted.version, activity: admitted.activity };
  }
  return next;
}

function projectedContextUsage(
  usage: ChatTranscriptPayloadByKind["context_usage"] | null,
): ChatMainView["status"]["contextUsage"] {
  return usage === null
    ? { usedTokens: 0, windowTokens: 0, cacheHitPercent: 0, hasCacheHitPercentage: false }
    : {
        usedTokens: usage.UsedTokens,
        windowTokens: usage.WindowTokens,
        cacheHitPercent: usage.CacheHitPercent ?? 0,
        hasCacheHitPercentage: usage.CacheHitPercent !== null,
      };
}

function admitAuthorityActivity(
  current: ChatMainView | null,
  incoming: ChatMainView,
): Readonly<{ version: ChatAuthorityTuple; activity: ChatRuntimeActivity }> {
  if (current === null) return incoming;
  const comparison = compareAuthorityTuple(current.version, incoming.version);
  if (comparison === "older" || comparison === "equal") {
    return { version: current.version, activity: current.activity };
  }
  return { version: incoming.version, activity: incoming.activity };
}

export function compareAuthorityTuple(
  current: ChatAuthorityTuple,
  incoming: ChatAuthorityTuple,
): "older" | "equal" | "newer-sequence" | "forward-authority" {
  if (incoming.epoch !== current.epoch || incoming.generation > current.generation)
    return "forward-authority";
  if (incoming.generation < current.generation) return "older";
  if (incoming.sequence < current.sequence) return "older";
  if (incoming.sequence === current.sequence) return "equal";
  return "newer-sequence";
}

function runtimeVersion(
  input: ChatTranscriptPayloadByKind["runtime_read_model_update"]["Version"],
): ChatAuthorityTuple {
  return { epoch: input.Epoch, generation: input.Generation, sequence: input.Sequence };
}

function result(
  state: ChatProjectionState,
  partial: Partial<Omit<ChatProjectionResult, "state">> = {},
): ChatProjectionResult {
  return {
    state,
    goalFact: partial.goalFact ?? null,
    effects: partial.effects ?? [],
  };
}
