import { ApiClient } from "./client";
import { ContractError, RpcError, TransportError } from "./errors";
import { FakeRpcTransport } from "@/test-support/api";
import { create } from "@app/server-api-contract";
import { ChatService, QueueRequestSchema } from "@app/server-api-contract/gen/kent/api/chat/chat_pb";
import {
  AgentPreparationCategory,
  InitialChatSettingsSchema,
  SupervisorValue,
} from "@app/server-api-contract/gen/kent/api/chat_settings/chat_settings_pb";
import {
  BackgroundShellOutputMode,
  CacheWarningMode,
  CompactionMode,
  ModelVerbosity,
  SessionLaunchService,
  SessionPlanResultSchema,
  SettingsSchema,
  SourceReportSchema,
  ShellPostprocessingMode,
  SleepPreventionMode,
  ToolID,
  WorkflowCompletionMode,
} from "@app/server-api-contract/gen/kent/api/session_launch/session_launch_pb";

import { requireProjectAttachment } from "./chatAttachment";
import { ChatOperationError } from "./chatErrors";

const sessionID = "123e4567-e89b-42d3-a456-426614174000";
const target = {
  projectID: "project-1",
  workspace: { workspaceID: "workspace-1" },
  sessionID,
} as const;

function runtimePlanResult(planSessionID: string) {
  const settings = create(SettingsSchema, {
    model: "gpt-5",
    thinkingLevel: "medium",
    modelVerbosity: ModelVerbosity.MEDIUM,
    systemPromptFiles: [],
    modelCapabilities: { supportsReasoningEffort: true, supportsVisionInputs: false },
    theme: "auto",
    notificationMethod: "off",
    toolPreambles: true,
    priorityRequestMode: false,
    debug: false,
    serverHost: "127.0.0.1",
    serverPort: 53082,
    webSearch: "off",
    providerOverride: "openai",
    providerIdentifier: "openai",
    openaiBaseUrl: "",
    providerCapabilities: {
      providerId: "openai",
      supportsResponsesApi: true,
      supportsResponsesCompact: true,
      supportsRequestInputTokenCount: false,
      supportsPromptCacheKey: true,
      supportsNativeWebSearch: false,
      supportsReasoningEncrypted: false,
      supportsServerSideContextEdit: false,
      supportsProviderVerbosity: true,
      isOpenaiFirstParty: true,
    },
    store: true,
    allowNonCwdEdits: false,
    modelContextWindow: 100000,
    contextCompactionThresholdTokens: 80000,
    preSubmitCompactionLeadTokens: 1000,
    minimumExecToBgSeconds: 1,
    compactionMode: CompactionMode.LOCAL,
    enabledTools: [{ toolId: ToolID.TOOL_ID_EXEC_COMMAND, enabled: true }],
    skillToggles: [{ key: "example", value: true }],
    timeouts: { modelRequestSeconds: 30 },
    shellOutputMaxChars: 10000,
    bgShellsOutput: BackgroundShellOutputMode.DEFAULT,
    shell: { postprocessingMode: ShellPostprocessingMode.BUILTIN },
    cacheWarningMode: CacheWarningMode.DEFAULT,
    worktrees: { baseDir: "", setupScript: "", setupTimeoutSeconds: 0 },
    workflow: {
      completionMode: WorkflowCompletionMode.AUTO,
      concurrency: 1,
      maxInvalidCompletionAttempts: 1,
      useRequiredToolCalls: false,
      subagents: false,
    },
    reviewer: {
      frequency: "off",
      model: "gpt-5",
      thinkingLevel: "medium",
      modelVerbosity: ModelVerbosity.MEDIUM,
      providerOverride: "openai",
      openaiBaseUrl: "",
      modelCapabilities: { supportsReasoningEffort: true, supportsVisionInputs: false },
      providerCapabilities: {
        providerId: "openai",
        supportsResponsesApi: true,
        supportsResponsesCompact: true,
        supportsRequestInputTokenCount: false,
        supportsPromptCacheKey: true,
        supportsNativeWebSearch: false,
        supportsReasoningEncrypted: false,
        supportsServerSideContextEdit: false,
        supportsProviderVerbosity: true,
        isOpenaiFirstParty: true,
      },
      modelContextWindow: 100000,
      auth: "",
      systemPromptFile: "",
      timeoutSeconds: 30,
      verboseOutput: false,
    },
    subagents: [],
    maxSubagentDepth: 0,
    preventSleep: SleepPreventionMode.ACTIVE,
  });
  return create(SessionPlanResultSchema, {
    outcome: {
      case: "success",
      value: {
        plan: {
          sessionId: planSessionID,
          activeSettings: settings,
          enabledToolIds: [ToolID.TOOL_ID_EXEC_COMMAND],
          modelContractLocked: false,
          questionsEnabled: true,
          autoCompactionEnabled: true,
          thinkingOverrideExplicit: false,
          source: create(SourceReportSchema, { sources: [] }),
        },
        warnings: [],
      },
    },
  });
}

function transcriptHydrationPayload() {
  return {
    SessionIdentity: {
      SessionID: sessionID,
      SessionName: null,
      ConversationFreshness: 0,
      ExecutionTarget: null,
    },
    SessionStatus: {
      ReviewerFrequency: "off",
      ReviewerEnabled: false,
      AutoCompactionEnabled: true,
      QuestionsEnabled: true,
      FastModeAvailable: false,
      FastModeEnabled: false,
      ThinkingLevel: "medium",
      CompactionMode: "local",
      CompactionCount: 0,
      PreviousSessionID: null,
      ParentAgentSessionID: null,
      NavigationTargetSessionID: null,
      Workflow: null,
    },
    RuntimeReadModelUpdate: {
      Version: { Epoch: "epoch-1", Generation: 1, Sequence: 1 },
      Activity: {
        State: "registered_idle",
        ActiveStep: null,
        Reviewer: "inactive",
        QueueAccepting: true,
        DiagnosticRecovery: false,
      },
    },
    TailSegment: {
      OlderCursor: null,
      HasMoreAbove: false,
      Entries: [],
    },
    ActiveAssistant: {
      StepID: sessionID,
      StreamID: sessionID,
      Text: " ",
      Phase: "commentary",
    },
    ActiveThinkingStatus: null,
    ActiveReasoningTraces: null,
    ActiveStep: null,
    ActiveCompaction: null,
    InFlightTools: null,
    PendingPrompts: null,
    BackgroundActivities: null,
    ContextUsage: null,
    GoalStatus: null,
  };
}

describe("Desktop Chat read client", () => {
  it("accepts a dormant Main View with authoritative Goal absence", async () => {
    const hydration = transcriptHydrationPayload();
    const { Workflow: workflow, ...sessionStatus } = hydration.SessionStatus;
    expect(workflow).toBeNull();
    const transport = new FakeRpcTransport([
      {
        method: "session.getMainView",
        result: {
          MainView: {
            Version: hydration.RuntimeReadModelUpdate.Version,
            Status: {
              ...sessionStatus,
              ConversationFreshness: hydration.SessionIdentity.ConversationFreshness,
              LastCommittedAssistantFinalAnswer: null,
              ContextUsage: {
                UsedTokens: 0,
                WindowTokens: 100_000,
                CacheHitPercent: 0,
                HasCacheHitPercentage: false,
              },
              Goal: {
                Goal: null,
                Availability: "available",
                Suspended: false,
              },
              WorkflowSession: null,
            },
            Session: {
              SessionID: sessionID,
              SessionName: "",
              AgentRole: null,
              ConversationFreshness: 0,
              ExecutionTarget: {
                WorkspaceID: "workspace-1",
                WorkspaceName: "Workspace",
                WorkspaceRoot: "/workspace",
                WorkspaceAvailability: "available",
                Worktree: null,
                CwdRelpath: ".",
                EffectiveWorkdir: "/workspace",
              },
            },
            Activity: {
              State: "unavailable",
              ActiveStep: null,
              Reviewer: "inactive",
              QueueAccepting: false,
              DiagnosticRecovery: false,
            },
          },
        },
      },
    ]);

    await expect(new ApiClient(transport).chat.getMainView(target)).resolves.toMatchObject({
      mainView: { sessionID, activity: { state: "unavailable" } },
      goal: { goal: null, availability: "available" },
    });
  });

  it("reads Main View and Context and validates runtime activation identity", async () => {
    const transport = new FakeRpcTransport([
      {
        method: "session.getMainView",
        result: {
          MainView: {
            Version: { Epoch: "epoch-1", Generation: 1, Sequence: 1 },
            Status: {
              ReviewerFrequency: "off",
              ReviewerEnabled: false,
              AutoCompactionEnabled: true,
              QuestionsEnabled: true,
              FastModeAvailable: false,
              FastModeEnabled: false,
              ConversationFreshness: 0,
              PreviousSessionID: null,
              ParentAgentSessionID: null,
              NavigationTargetSessionID: null,
              LastCommittedAssistantFinalAnswer: null,
              ThinkingLevel: "medium",
              CompactionMode: "local",
              ContextUsage: {
                UsedTokens: 4,
                WindowTokens: 100,
                CacheHitPercent: 0,
                HasCacheHitPercentage: false,
              },
              CompactionCount: 0,
              Goal: null,
              WorkflowSession: null,
            },
            Session: {
              SessionID: sessionID,
              SessionName: "",
              AgentRole: null,
              ConversationFreshness: 0,
              ExecutionTarget: {
                WorkspaceID: "",
                WorkspaceName: "Workspace",
                WorkspaceRoot: "/workspace",
                WorkspaceAvailability: "unlinked",
                Worktree: null,
                CwdRelpath: ".",
                EffectiveWorkdir: "/workspace",
              },
            },
            Activity: {
              State: "registered_idle",
              ActiveStep: null,
              Reviewer: "inactive",
              QueueAccepting: true,
              DiagnosticRecovery: false,
            },
          },
        },
      },
      {
        method: "chat.context.get",
        result: {
          context: {
            context_window_tokens: 100,
            used_tokens: 4,
            remaining_tokens: 96,
            automatic_threshold_tokens: 80,
            auto_compaction_enabled: true,
            compaction_mode: "local",
            completed_compaction_count: 0,
            compaction_running: false,
            manual_compact_available: true,
          },
        },
      },
    ]);
    const client = new ApiClient(transport);

    await expect(client.chat.getMainView(target)).resolves.toMatchObject({
      mainView: {
        sessionID,
        sessionName: null,
        executionTarget: { workspaceID: "", workspaceAvailability: "unlinked" },
      },
      goal: { goal: null, availability: null },
    });
    await expect(client.chat.getContext(target)).resolves.toMatchObject({
      contextWindowTokens: 100,
      remainingTokens: 96,
    });
    const activationTransport = new FakeRpcTransport([
      { descriptor: SessionLaunchService.method.plan, result: runtimePlanResult(sessionID) },
      {
        method: "session.runtime.activate",
        result: { attachment: { session_id: sessionID, generation: 7 } },
      },
      { method: "session.runtime.release", result: { released: true, active: false } },
    ]);
    const activationClient = new ApiClient(activationTransport);
    await expect(activationClient.chat.activateRuntime(target)).resolves.toEqual({
      sessionID,
      generation: 7,
    });
    await expect(activationClient.chat.releaseRuntime({ sessionID, generation: 7 })).resolves.toEqual({
      released: true,
      active: false,
    });

    const mismatchedPlanClient = new ApiClient(
      new FakeRpcTransport([
        {
          descriptor: SessionLaunchService.method.plan,
          result: runtimePlanResult("223e4567-e89b-42d3-a456-426614174000"),
        },
      ]),
    );
    await expect(mismatchedPlanClient.chat.activateRuntime(target)).rejects.toBeInstanceOf(ContractError);
  });

  it("converts Goal inspection, authoritative mutation, and ordered observation", async () => {
    const now = "2026-09-04T10:00:00Z";
    const transport = new FakeRpcTransport([
      {
        method: "runtime.goal.show",
        result: {
          goal: { id: "goal-1", objective: "ship", status: "active", created_at: now, updated_at: now },
          availability: "available",
        },
      },
      {
        method: "runtime.goal.pause",
        result: {
          result: {
            kind: "authoritative_goal",
            goal: {
              id: "goal-1",
              objective: "ship",
              status: "paused",
              created_at: now,
              updated_at: now,
            },
            availability: null,
          },
        },
      },
    ]);
    const client = new ApiClient(transport);

    await expect(client.chat.getGoal(target)).resolves.toEqual({
      goal: { id: "goal-1", objective: "ship", status: "active", createdAt: now, updatedAt: now },
      availability: "available",
    });
    await expect(client.chat.mutateGoal(target, { kind: "pause" })).resolves.toEqual({
      kind: "authoritative_goal",
      fact: {
        goal: {
          id: "goal-1",
          objective: "ship",
          status: "paused",
          createdAt: now,
          updatedAt: now,
        },
        availability: null,
      },
    });

    const observations: unknown[] = [];
    const observationErrors: Error[] = [];
    client.chat.subscribeGoal(target, {
      onEvent: (observation) => observations.push(observation),
      onComplete: () => undefined,
      onError: (error) => observationErrors.push(error),
    });
    expect(transport.chatSubscriptionStarts[0]?.establishmentTimeoutMs).toBeUndefined();
    transport.emit("goal.observation", {
      observation: {
        sequence: 1,
        kind: "hydration",
        status: { goal: null, availability: "available" },
      },
    });
    expect(observations).toEqual([
      {
        sequence: 1,
        kind: "hydration",
        fact: { goal: null, availability: "available" },
      },
    ]);
    transport.fail(
      "goal.observe",
      new RpcError({ code: -32000, message: "Session unavailable", method: "goal.observe" }),
    );
    transport.fail("goal.observe", new TransportError("Subscription socket closed."));
    expect(observationErrors).toHaveLength(1);
    expect(observationErrors[0]).toBeInstanceOf(RpcError);
  });

  it("reads bounded transcript pages in both cursor directions", async () => {
    const client = new ApiClient(
      new FakeRpcTransport([
        {
          method: "session.getTranscriptPage",
          handler: (_params, callIndex) => ({
            transcript: {
              SessionID: sessionID,
              SessionName: "",
              ConversationFreshness: callIndex,
              OlderCursor: 11,
              HasMoreAbove: true,
              NewerCursor: 29,
              HasMoreBelow: false,
              LatestRollbackCandidate: { user_message_seq: 7, candidate_page_end_byte: 100 },
              Entries: [
                {
                  Visibility: "ongoing",
                  Integrity: 0,
                  Kind: "assistant",
                  Locator: { event_sequence: 7, row_ordinal: 1 },
                  User: null,
                  Assistant: {
                    StepID: sessionID,
                    StreamID: null,
                    Text: "done",
                    CondensedText: null,
                    Phase: "final_answer",
                    committed_at_unix_ms: null,
                  },
                  Tool: null,
                  ReasoningTrace: null,
                  Notice: null,
                  ReviewerFeedback: null,
                  ReviewerError: null,
                },
              ],
            },
          }),
        },
      ]),
    );

    await expect(
      client.chat.getTranscriptPage(target, { direction: "older", value: 41 }),
    ).resolves.toMatchObject({
      sessionID,
      sessionName: null,
      olderCursor: 11,
      newerCursor: 29,
      entries: [{ Kind: "assistant", Locator: { event_sequence: 7, row_ordinal: 1 } }],
    });
    await expect(
      client.chat.getTranscriptPage(target, { direction: "newer", value: 29 }),
    ).resolves.toMatchObject({
      sessionID,
      conversationFreshness: 1,
      hasMoreBelow: false,
    });
  });

  it("delivers transcript hydration and live events with recoverable contract errors and typed completion", async () => {
    const events: unknown[] = [];
    const errors: Error[] = [];
    const transportLosses: unknown[] = [];
    const completions: unknown[] = [];
    const transport = new FakeRpcTransport([]);
    const client = new ApiClient(transport);
    client.chat.subscribeTranscript(target, {
      onEvent: (event) => events.push(event),
      onComplete: (completion) => completions.push(completion),
      onError: (error) => errors.push(error),
      onTransportLoss: () => transportLosses.push({}),
    });
    expect(transport.chatSubscriptionStarts[0]?.establishmentTimeoutMs).toBeNull();
    transport.fail("session.subscribeTranscript", new TransportError("isolated socket loss"));
    expect(errors).toEqual([]);
    expect(transportLosses).toHaveLength(1);

    transport.emit("session.transcript", {
      message: { sequence: 1, kind: "hydration", payload: transcriptHydrationPayload() },
    });
    transport.emit("session.transcript", {
      message: {
        sequence: 2,
        kind: "session_identity",
        payload: {
          SessionID: "223e4567-e89b-42d3-a456-426614174000",
          SessionName: null,
          ConversationFreshness: 0,
          ExecutionTarget: null,
        },
      },
    });
    transport.emit("session.transcript", {
      message: {
        sequence: 3,
        kind: "session_identity",
        payload: { SessionID: sessionID, SessionName: null, ConversationFreshness: 0, ExecutionTarget: null },
      },
    });
    transport.complete("session.subscribeTranscript", 17, "subscriber overflow", "subscriber_overflow");

    expect(events).toHaveLength(2);
    expect(events[0]).toMatchObject({ sequence: 1, kind: "hydration" });
    expect(events[1]).toMatchObject({ sequence: 3, kind: "session_identity" });
    expect(errors).toHaveLength(1);
    expect(errors[0]).toBeInstanceOf(ContractError);
    expect(completions).toEqual([
      { code: 17, message: "subscriber overflow", reason: "subscriber_overflow" },
    ]);
  });

  it("rejects hydration whose older-history boundary has no cursor", () => {
    const errors: Error[] = [];
    const transport = new FakeRpcTransport([]);
    const client = new ApiClient(transport);
    client.chat.subscribeTranscript(target, {
      onEvent: () => undefined,
      onComplete: () => undefined,
      onError: (error) => errors.push(error),
    });
    const payload = transcriptHydrationPayload();
    payload.TailSegment.HasMoreAbove = true;

    transport.emit("session.transcript", {
      message: { sequence: 1, kind: "hydration", payload },
    });

    expect(errors).toHaveLength(1);
    expect(errors[0]).toBeInstanceOf(ContractError);
  });

  it("rejects hydration whose tail entries are absent", () => {
    const errors: Error[] = [];
    const transport = new FakeRpcTransport([]);
    const client = new ApiClient(transport);
    client.chat.subscribeTranscript(target, {
      onEvent: () => undefined,
      onComplete: () => undefined,
      onError: (error) => errors.push(error),
    });
    const validPayload = transcriptHydrationPayload();
    const payload = {
      ...validPayload,
      TailSegment: { ...validPayload.TailSegment, Entries: null },
    };

    transport.emit("session.transcript", {
      message: { sequence: 1, kind: "hydration", payload },
    });

    expect(errors).toHaveLength(1);
    expect(errors[0]).toBeInstanceOf(ContractError);
  });
});

describe("Desktop Chat mutation adapter", () => {
  const queueItemID = "223e4567-e89b-42d3-a456-426614174000";
  const compactionRequestID = "323e4567-e89b-42d3-a456-426614174000";
  const sessionTarget = {
    kind: "session",
    projectID: "project-1",
    workspace: { workspaceID: "workspace-1" },
    sessionID,
  } as const;
  const newChatTarget = {
    kind: "new_chat",
    projectID: "project-1",
    workspace: { workspaceRoot: "/workspace" },
    initialSettings: {
      agentRole: "default",
      supervisor: "edits",
      thinking: "high",
      fast: true,
      questionsEnabled: false,
      autoCompactionEnabled: true,
    },
  } as const;

  it("constructs representative targets, exact lexical requests, and New Chat rejection", async () => {
    const transport = new FakeRpcTransport([
      {
        descriptor: ChatService.method.steer,
        result: create(ChatService.method.steer.output, {
          outcome: {
            case: "success",
            value: {
              session: { sessionId: sessionID },
              outcome: { case: "accepted", value: { queueItem: { id: queueItemID } } },
            },
          },
        }),
      },
      {
        descriptor: ChatService.method.queue,
        result: create(ChatService.method.queue.output, {
          outcome: {
            case: "success",
            value: {
              session: { sessionId: sessionID },
              outcome: { case: "accepted", value: { queueItem: { id: queueItemID } } },
            },
          },
        }),
      },
      {
        descriptor: ChatService.method.compact,
        result: create(ChatService.method.compact.output, {
          outcome: {
            case: "success",
            value: {
              session: { sessionId: sessionID },
              outcome: { case: "notAccepted", value: { reason: { case: "tooSoon", value: {} } } },
            },
          },
        }),
      },
    ]);
    const chat = new ApiClient(transport).chat;

    await chat.steer(sessionTarget, { kind: "text", text: "continue" });
    await chat.queue(newChatTarget, {
      kind: "command",
      catalogIdentity: "builtin:review",
      token: "/review",
      separatorWhitespace: "\t",
      arguments: "working tree",
    });
    const rejected = await chat.compact(newChatTarget, {
      token: "/compact",
      separatorWhitespace: " \t",
      rawGuidance: " keep   decisions ",
    });
    expect(rejected).toEqual({
      sessionID,
      outcome: { kind: "not_accepted", reason: { kind: "too_soon" } },
    });

    expect(transport.attachedProjectDescriptorCalls.map(({ request }) => request)).toMatchObject([
      {
        target: { target: { case: "session", value: { sessionId: sessionID } } },
        activation: { input: { case: "text", value: "continue" } },
      },
      {
        target: {
          target: {
            case: "newChat",
            value: {
              projectId: "project-1",
              workspaceId: "workspace-1",
            },
          },
        },
        activation: {
          input: {
            case: "command",
            value: {
              catalogIdentity: "builtin:review",
              token: "/review",
              separatorWhitespace: "\t",
              arguments: "working tree",
            },
          },
        },
      },
      {
        invocation: {
          token: "/compact",
          separatorWhitespace: " \t",
          rawGuidance: " keep   decisions ",
        },
      },
    ]);
    expect(transport.attachedProjectDescriptorCalls.map(({ request }) => request)).toContainEqual(
      create(QueueRequestSchema, {
        target: {
          target: {
            case: "newChat",
            value: {
              projectId: "project-1",
              workspaceId: "workspace-1",
              initialSettings: create(InitialChatSettingsSchema, {
                agentRole: "default",
                supervisor: SupervisorValue.AFTER_EDITS,
                thinking: "high",
                fast: true,
                questionsEnabled: false,
                autoCompactionEnabled: true,
              }),
            },
          },
        },
        activation: {
          input: {
            case: "command",
            value: {
              catalogIdentity: "builtin:review",
              token: "/review",
              separatorWhitespace: "\t",
              arguments: "working tree",
            },
          },
        },
      }),
    );
  });

  it("projects accepted identities, typed diagnostics, and shared errors", async () => {
    const transport = new FakeRpcTransport([
      {
        descriptor: ChatService.method.queue,
        result: create(ChatService.method.queue.output, {
          outcome: {
            case: "success",
            value: {
              session: { sessionId: sessionID },
              outcome: {
                case: "accepted",
                value: {
                  queueItem: { id: queueItemID },
                  diagnostic: {
                    detail: {
                      case: "promptHistoryFailure",
                      value: { operation: "history.record", cause: "disk full" },
                    },
                  },
                },
              },
            },
          },
        }),
      },
      {
        descriptor: ChatService.method.compact,
        result: create(ChatService.method.compact.output, {
          outcome: {
            case: "success",
            value: {
              session: { sessionId: sessionID },
              outcome: {
                case: "accepted",
                value: { request: { id: compactionRequestID } },
              },
            },
          },
        }),
      },
    ]);
    const chat = new ApiClient(transport).chat;

    const queued = await chat.queue(sessionTarget, { kind: "text", text: "continue" });
    if (queued.outcome.kind !== "accepted") throw new Error("Expected accepted Queue fixture.");
    expect(queued.outcome.queueItemID.toJSONValue()).toBe(queueItemID);
    expect(queued.outcome.diagnostic).toMatchObject({ kind: "prompt_history_failure" });

    const compacted = await chat.compact(sessionTarget, {
      token: "/compact",
      separatorWhitespace: "",
      rawGuidance: "",
    });
    if (compacted.outcome.kind !== "accepted") throw new Error("Expected accepted compaction fixture.");
    expect(compacted.outcome.requestID.toJSONValue()).toBe(compactionRequestID);

    const failingChat = new ApiClient(
      new FakeRpcTransport([
        {
          descriptor: ChatService.method.queue,
          result: create(ChatService.method.queue.output, {
            outcome: {
              case: "error",
              value: {
                code: "chat_settings_agent_preparation",
                detail: {
                  case: "chatSettingsAgentPreparation",
                  value: {
                    agent: "reviewer",
                    category: AgentPreparationCategory.PROVIDER_UNAVAILABLE,
                  },
                },
              },
            },
          }),
        },
      ]),
    ).chat;
    const error = await failingChat
      .queue(sessionTarget, { kind: "text", text: "continue" })
      .catch((cause: unknown) => cause);
    expect(error).toBeInstanceOf(ChatOperationError);
    expect(error).toMatchObject({
      detail: {
        kind: "agent_preparation",
        agent: "reviewer",
        category: "provider_unavailable",
      },
    });
  });

  it("rejects mismatched attachments, returned Sessions, and malformed identities", async () => {
    expect(() =>
      requireProjectAttachment(
        {
          projectID: "other-project",
          workspaceID: "workspace-1",
          workspaceRoot: "/workspace",
          workspaceSelection: { kind: "workspaceID", workspaceID: "workspace-1" },
        },
        sessionTarget,
      ),
    ).toThrow(ContractError);

    for (const testCase of [
      {
        returnedSessionID: "423e4567-e89b-42d3-a456-426614174000",
        returnedQueueItemID: queueItemID,
      },
      { returnedSessionID: sessionID, returnedQueueItemID: "not-a-queue-item-id" },
    ]) {
      const transport = new FakeRpcTransport([
        {
          descriptor: ChatService.method.steer,
          result: create(ChatService.method.steer.output, {
            outcome: {
              case: "success",
              value: {
                session: { sessionId: testCase.returnedSessionID },
                outcome: {
                  case: "accepted",
                  value: { queueItem: { id: testCase.returnedQueueItemID } },
                },
              },
            },
          }),
        },
      ]);
      await expect(
        new ApiClient(transport).chat.steer(sessionTarget, { kind: "text", text: "continue" }),
      ).rejects.toBeInstanceOf(Error);
    }
  });
});
