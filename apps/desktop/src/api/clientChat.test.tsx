import { observeTranscript, observeGoal } from "@/test-support/chat-runtime";
import { describe, expect, it } from "bun:test";
import { StreamFailureCode } from "@app/server-api-contract/gen/kent/api/shared/foundation_pb";
import { unexpectedProjectOverflow } from "@/test-support/api";
import { ApiClient } from "./client";
import { pendingWorkItemIDFromProto } from "./pendingWork";
import { ContractError, RpcError, TransportError } from "./errors";
import { FakeRpcTransport } from "@/test-support/api";
import { create, encode } from "@app/server-api-contract";
import { ReadService as SessionReadService } from "@app/server-api-contract/gen/kent/api/session/session_pb";
import {
  ChatContextService,
  CompactionMode as ContextCompactionMode,
} from "@app/server-api-contract/gen/kent/api/chat_context/chat_context_pb";
import * as R from "@app/server-api-contract/gen/kent/api/runtime/runtime_pb";
import * as T from "@app/server-api-contract/gen/kent/api/transcript/transcript_pb";
import { ProjectAvailability } from "@app/server-api-contract/gen/kent/api/project/project_pb";
import { SessionCategory } from "@app/server-api-contract/gen/kent/api/project/session_catalog_pb";
import { TranscriptCloseReason } from "@app/server-api-contract/gen/kent/api/shared/foundation_pb";
import {
  ChatService,
  InputAdmissionDisposition,
  QueueRequestSchema,
} from "@app/server-api-contract/gen/kent/api/chat/chat_pb";
import {
  SessionRuntimeService,
  SessionRuntimeActivateResultSchema,
} from "@app/server-api-contract/gen/kent/api/session_launch/session_lifecycle_pb";
import {
  AgentPreparationCategory,
  InitialChatSettingsSchema,
  SupervisorValue,
} from "@app/server-api-contract/gen/kent/api/chat_settings/chat_settings_pb";
import {
  GoalAvailability,
  GoalClearResultSchema,
  GoalMutationResultKind,
  GoalPauseResultSchema,
  GoalService,
  GoalSetResultSchema,
  GoalStatus,
} from "@app/server-api-contract/gen/kent/api/runtime/runtime_pb";
import {
  BackgroundShellOutputMode,
  CacheWarningMode,
  CompactionMode,
  ModelVerbosity,
  SessionLaunchService,
  SessionPlanResultSchema,
  SessionRuntimeAgentSelectionSchema,
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
  sessionID,
} as const;

function runtimePlanResult(planSessionID: string) {
  const settings = create(SettingsSchema, {
    model: "gpt-6-sol",
    thinkingLevel: "medium",
    modelVerbosity: ModelVerbosity.MEDIUM,
    modelCapabilities: { supportsReasoningEffort: true, supportsVisionInputs: false },
    theme: "auto",
    notificationMethod: "off",
    toolPreambles: true,
    priorityRequestMode: false,
    debug: false,
    serverHost: "127.0.0.1",
    serverPort: 53082,
    webSearch: "off",
    connection: "test",
    providerIdentifier: "openai",
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
      model: "gpt-6-sol",
      thinkingLevel: "medium",
      modelVerbosity: ModelVerbosity.MEDIUM,
      connection: "test",
      modelCapabilities: { supportsReasoningEffort: true, supportsVisionInputs: false },
      modelContextWindow: 100000,
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
          connectionScope: {},
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
  return create(T.HydrationSchema, {
    sessionIdentity: {
      category: SessionCategory.MAIN,
      sessionId: sessionID,
      conversationFreshness: R.ConversationFreshness.FRESH,
    },
    sessionStatus: {
      reviewerFrequency: "off",
      autoCompactionEnabled: true,
      questionsEnabled: true,
      thinkingLevel: "medium",
      compactionMode: "local",
    },
    runtimeReadModelUpdate: {
      version: { epoch: sessionID, generation: 1n, sequence: 1n },
      activity: {
        state: R.ActivityState.RUNTIME_ACTIVITY_RUNNING,
        reviewer: R.ReviewerActivity.INACTIVE,
        queueAccepting: true,
        activeStep: {
          stepId: sessionID,
          activeKind: R.ActivityActiveKind.RUNTIME_ACTIVITY_ACTIVE_KIND_USER_TURN,
        },
      },
    },
    tailSegment: {},
    activeAssistants: [
      {
        stepId: sessionID,
        streamId: sessionID,
        text: " ",
        phase: T.AssistantPhase.COMMENTARY,
      },
    ],
    activeStep: {
      stepId: sessionID,
      activeKind: R.ActivityActiveKind.RUNTIME_ACTIVITY_ACTIVE_KIND_USER_TURN,
      lifecycle: T.StepLifecycle.STARTED,
      status: T.RunStatus.RUNNING,
    },
  });
}

function mainViewResult(state: R.ActivityState, withGoal: boolean) {
  return create(SessionReadService.method.getMainView.output, {
    outcome: {
      case: "success",
      value: {
        mainView: {
          version: { epoch: sessionID, generation: 1n, sequence: 1n },
          status: {
            reviewerFrequency: "off",
            autoCompactionEnabled: true,
            questionsEnabled: true,
            conversationFreshness: R.ConversationFreshness.FRESH,
            thinkingLevel: "medium",
            compactionMode: "local",
            contextUsage: { usedTokens: 4, windowTokens: 100 },
            ...(withGoal ? { goal: { availability: R.GoalAvailability.AVAILABLE } } : {}),
          },
          session: {
            category: SessionCategory.MAIN,
            sessionId: sessionID,
            conversationFreshness: R.ConversationFreshness.FRESH,
            executionTarget: {
              workspaceName: "Workspace",
              workspaceRoot: "/workspace",
              workspaceAvailability: ProjectAvailability.UNLINKED,
              cwdRelpath: ".",
              effectiveWorkdir: "/workspace",
            },
          },
          activity: {
            state,
            reviewer: R.ReviewerActivity.INACTIVE,
            queueAccepting: state !== R.ActivityState.RUNTIME_ACTIVITY_UNAVAILABLE,
          },
        },
      },
    },
  });
}

describe("Desktop Chat read client", () => {
  it("uses the generated dedicated Goal Set operation for exact and New Chat targets", async () => {
    const createdAt = { seconds: 10n, nanos: 0 };
    const transport = new FakeRpcTransport([
      {
        descriptor: GoalService.method.set,
        resultFactory: (_request, callIndex) =>
          create(GoalSetResultSchema, {
            outcome: {
              case: "success",
              value: {
                session: {
                  sessionId: callIndex === 0 ? sessionID : "223e4567-e89b-42d3-a456-426614174000",
                },
                outcome: {
                  case: "mutation",
                  value: {
                    kind: GoalMutationResultKind.AUTHORITATIVE_GOAL,
                    goal: {
                      id: "goal-1",
                      objective: callIndex === 0 ? "exact Goal" : "New Chat Goal",
                      status: GoalStatus.RUNTIME_GOAL_STATUS_ACTIVE,
                      createdAt,
                      updatedAt: createdAt,
                    },
                    availability: GoalAvailability.AVAILABLE,
                  },
                },
              },
            },
          }),
      },
    ]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);

    expect(client.chat.setGoal({ kind: "session", sessionID }, "exact Goal")).resolves.toMatchObject({
      sessionID,
      outcome: { kind: "mutation", mutation: { kind: "authoritative_goal" } },
    });
    expect(
      client.chat.setGoal(
        {
          kind: "new_chat",
          projectID: "project-1",
          workspaceID: "workspace-1",
          initialSettings: {
            agentRole: "default",
            supervisor: "off",
            thinking: "medium",
            fast: false,
            questionsEnabled: true,
            autoCompactionEnabled: true,
          },
          initialInputDraft: "composer draft",
        },
        "New Chat Goal",
      ),
    ).resolves.toMatchObject({
      sessionID: "223e4567-e89b-42d3-a456-426614174000",
    });
    expect(transport.descriptorCalls).toHaveLength(2);
    expect(transport.attachedProjectDescriptorCalls).toHaveLength(1);
    expect(transport.attachedProjectDescriptorCalls[0]?.request).toMatchObject({
      target: {
        target: {
          case: "newChat",
          value: {
            projectId: "project-1",
            workspaceId: "workspace-1",
            initialSettings: { agentRole: "default" },
          },
        },
      },
      executionPolicy: 1,
      initialInputDraft: "composer draft",
    });
  });

  it("rejects Goal mutation dispositions that are illegal for the invoked method", async () => {
    const transport = new FakeRpcTransport([
      {
        descriptor: GoalService.method.set,
        result: create(GoalSetResultSchema, {
          outcome: {
            case: "success",
            value: {
              session: { sessionId: sessionID },
              outcome: {
                case: "mutation",
                value: { kind: GoalMutationResultKind.AUTHORITATIVE_CLEAR },
              },
            },
          },
        }),
      },
      {
        descriptor: GoalService.method.pause,
        result: create(GoalPauseResultSchema, {
          outcome: {
            case: "success",
            value: { kind: GoalMutationResultKind.AUTHORITATIVE_CLEAR },
          },
        }),
      },
      {
        descriptor: GoalService.method.clear,
        result: create(GoalClearResultSchema, {
          outcome: {
            case: "success",
            value: {
              kind: GoalMutationResultKind.AUTHORITATIVE_GOAL,
              goal: {
                id: "goal-1",
                objective: "ship",
                status: GoalStatus.RUNTIME_GOAL_STATUS_ACTIVE,
                createdAt: { seconds: 1n, nanos: 0 },
                updatedAt: { seconds: 1n, nanos: 0 },
              },
            },
          },
        }),
      },
    ]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);

    expect(client.chat.setGoal({ kind: "session", sessionID }, "ship")).rejects.toBeInstanceOf(ContractError);
    expect(client.chat.pauseGoal(target)).rejects.toBeInstanceOf(ContractError);
    expect(client.chat.clearGoal(target)).rejects.toBeInstanceOf(ContractError);
  });

  it("accepts a dormant Main View with authoritative Goal absence", async () => {
    const transport = new FakeRpcTransport([
      {
        descriptor: SessionReadService.method.getMainView,
        result: mainViewResult(R.ActivityState.RUNTIME_ACTIVITY_UNAVAILABLE, true),
      },
    ]);

    expect(
      new ApiClient(transport, unexpectedProjectOverflow).chat.getMainView(target),
    ).resolves.toMatchObject({
      mainView: { sessionID, activity: { state: "unavailable" } },
      goal: { goal: null, availability: "available" },
    });
  });

  it("reads Main View and Context and validates runtime activation identity", async () => {
    const transport = new FakeRpcTransport([
      {
        descriptor: SessionReadService.method.getMainView,
        result: mainViewResult(R.ActivityState.RUNTIME_ACTIVITY_REGISTERED_IDLE, false),
      },
      {
        descriptor: ChatContextService.method.get,
        result: create(ChatContextService.method.get.output, {
          outcome: {
            case: "success",
            value: {
              context: {
                contextWindowTokens: 100n,
                usedTokens: 4n,
                remainingTokens: 96n,
                automaticThresholdTokens: 80n,
                autoCompactionEnabled: true,
                compactionMode: ContextCompactionMode.LOCAL,
                manualCompactAvailable: true,
              },
            },
          },
        }),
      },
    ]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);

    expect(client.chat.getMainView(target)).resolves.toMatchObject({
      mainView: {
        sessionID,
        sessionName: null,
        executionTarget: { workspaceID: null, workspaceAvailability: "unlinked" },
      },
      goal: { goal: null, availability: null },
    });
    expect(client.chat.getContext(target)).resolves.toMatchObject({
      contextWindowTokens: 100,
      remainingTokens: 96,
    });
    for (const selection of [
      undefined,
      ...["worker", "default", undefined].map((agentRole) =>
        create(SessionRuntimeAgentSelectionSchema, {
          agentRole,
          baseline: {
            supervisor: "off",
            thinking: "high",
            fast: true,
            questions: false,
            autoCompaction: true,
          },
        }),
      ),
    ]) {
      const planned = runtimePlanResult(sessionID);
      if (planned.outcome.case !== "success" || planned.outcome.value.plan === undefined)
        throw new Error("Plan fixture missing");
      planned.outcome.value.plan.activationAgentSelection = selection;
      const activationTransport = new FakeRpcTransport([
        { descriptor: SessionLaunchService.method.plan, result: planned },
        {
          descriptor: SessionRuntimeService.method.activate,
          result: create(SessionRuntimeActivateResultSchema, {
            outcome: { case: "success", value: {} },
          }),
        },
      ]);
      const activationClient = new ApiClient(activationTransport, unexpectedProjectOverflow);
      await activationClient.chat.activateRuntime(target);
      if (selection === undefined) {
        expect(activationTransport.descriptorCalls[1]?.request).not.toHaveProperty("options.agentSelection");
      } else {
        expect(activationTransport.descriptorCalls[1]?.request).toMatchObject({
          options: { agentSelection: selection },
        });
      }
    }

    const mismatchedPlanClient = new ApiClient(
      new FakeRpcTransport([
        {
          descriptor: SessionLaunchService.method.plan,
          result: runtimePlanResult("223e4567-e89b-42d3-a456-426614174000"),
        },
      ]),
      unexpectedProjectOverflow,
    );
    const failure = await mismatchedPlanClient.chat.activateRuntime(target).catch((error: unknown) => error);
    expect(failure).toBeInstanceOf(ContractError);
  });

  it("converts Goal inspection, authoritative mutation, and ordered observation", async () => {
    const now = "2026-09-04T10:00:00.000Z";
    const createdAt = { seconds: BigInt(Date.parse(now) / 1000), nanos: 0 };
    const transport = new FakeRpcTransport([
      {
        descriptor: R.GoalService.method.show,
        result: create(R.GoalService.method.show.output, {
          outcome: {
            case: "success",
            value: {
              goal: {
                id: sessionID,
                objective: "ship",
                status: R.GoalStatus.RUNTIME_GOAL_STATUS_ACTIVE,
                createdAt,
                updatedAt: createdAt,
              },
              availability: R.GoalAvailability.AVAILABLE,
            },
          },
        }),
      },
      {
        descriptor: R.GoalService.method.pause,
        result: create(R.GoalService.method.pause.output, {
          outcome: {
            case: "success",
            value: {
              kind: R.GoalMutationResultKind.AUTHORITATIVE_GOAL,
              goal: {
                id: sessionID,
                objective: "ship",
                status: R.GoalStatus.RUNTIME_GOAL_STATUS_PAUSED,
                createdAt,
                updatedAt: createdAt,
              },
            },
          },
        }),
      },
      {
        subscriptionDescriptor: R.GoalService.method.observe,
        startResult: create(R.GoalService.method.observe.output, { outcome: { case: "success", value: {} } }),
      },
    ]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);

    expect(client.chat.getGoal(target)).resolves.toEqual({
      goal: { id: sessionID, objective: "ship", status: "active", createdAt: now, updatedAt: now },
      availability: "available",
    });
    expect(client.chat.pauseGoal(target)).resolves.toEqual({
      kind: "authoritative_goal",
      fact: {
        goal: {
          id: sessionID,
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
    const goalObservation = observeGoal(client.chat, target, {
      onEvent: (observation) => observations.push(observation),
      onComplete: () => undefined,
      onError: (error) => observationErrors.push(error),
    });
    await goalObservation.flush();
    transport.emitDescriptor(
      R.GoalService.method.observe,
      R.GoalService.method.observation,
      create(R.GoalObservationSchema, {
        sequence: 1n,
        kind: R.GoalObservationKind.HYDRATION,
        status: { availability: R.GoalAvailability.AVAILABLE },
      }),
    );
    await goalObservation.flush();
    expect(observations).toEqual([
      {
        sequence: 1,
        kind: "hydration",
        fact: { goal: null, availability: "available" },
      },
    ]);
    transport.failDescriptor(
      R.GoalService.method.observe,
      new RpcError({ code: "internal_failure", message: "Session unavailable", method: "goal.observe" }),
    );
    transport.failDescriptor(R.GoalService.method.observe, new TransportError("Subscription socket closed."));
    await goalObservation.flush();
    expect(observationErrors).toHaveLength(2);
    expect(observationErrors[0]).toBeInstanceOf(RpcError);
    expect(observationErrors[1]).toBeInstanceOf(TransportError);
    goalObservation.close();
  });

  it("reads bounded transcript pages in both cursor directions", async () => {
    const client = new ApiClient(
      new FakeRpcTransport([
        {
          descriptor: T.ReadService.method.getPage,
          resultFactory: (_params, callIndex) =>
            create(T.ReadService.method.getPage.output, {
              outcome: {
                case: "success",
                value: {
                  transcript: {
                    sessionId: sessionID,
                    conversationFreshness:
                      callIndex === 0 ? R.ConversationFreshness.FRESH : R.ConversationFreshness.ESTABLISHED,
                    olderCursor: 11n,
                    hasMoreAbove: true,
                    newerCursor: callIndex === 0 ? 29n : undefined,
                    hasMoreBelow: callIndex === 0,
                    latestRollbackCandidate: { userMessageSeq: 7n, candidatePageEndByte: 100n },
                    entries: [
                      {
                        visibility: T.EntryVisibility.ONGOING,
                        integrity: T.RowIntegrity.VALID,
                        locator: { eventSequence: 7n, rowOrdinal: 1 },
                        row: {
                          case: "assistant",
                          value: {
                            stepId: sessionID,
                            text: "done",
                            condensedText: "summary",
                            phase: T.AssistantPhase.FINAL,
                            committedAt: { seconds: 0n, nanos: 0 },
                          },
                        },
                      },
                      {
                        visibility: T.EntryVisibility.ONGOING,
                        integrity: T.RowIntegrity.VALID,
                        locator: { eventSequence: 8n, rowOrdinal: 1 },
                        row: {
                          case: "user",
                          value: {
                            text: "batched input",
                            queueItemIds:
                              callIndex === 0
                                ? [
                                    "223e4567-e89b-42d3-a456-426614174000",
                                    "323e4567-e89b-42d3-a456-426614174000",
                                  ]
                                : [],
                          },
                        },
                      },
                    ],
                  },
                },
              },
            }),
        },
      ]),
      unexpectedProjectOverflow,
    );

    const older = await client.chat.getTranscriptPage(target, { direction: "older", value: 41 });
    expect(older).toMatchObject({
      sessionID,
      sessionName: null,
      olderCursor: 11,
      newerCursor: 29,
      entries: [
        {
          Kind: "assistant",
          Locator: { event_sequence: 7, row_ordinal: 1 },
          Assistant: { Text: "done", CondensedText: "summary", committed_at_unix_ms: 0 },
        },
        {
          Kind: "user",
          User: {
            QueueItemIDs: ["223e4567-e89b-42d3-a456-426614174000", "323e4567-e89b-42d3-a456-426614174000"],
          },
        },
      ],
    });
    expect(older.entries[1]?.User?.QueueItemIDs).toEqual([
      "223e4567-e89b-42d3-a456-426614174000",
      "323e4567-e89b-42d3-a456-426614174000",
    ]);
    const newer = await client.chat.getTranscriptPage(target, { direction: "newer", value: 29 });
    expect(newer).toMatchObject({
      sessionID,
      conversationFreshness: 1,
      hasMoreBelow: false,
      entries: [{ Kind: "assistant" }, { Kind: "user", User: { QueueItemIDs: [] } }],
    });
  });

  it("delivers transcript hydration and live events with recoverable contract errors and typed completion", async () => {
    const events: unknown[] = [];
    const errors: Error[] = [];
    const transportLosses: unknown[] = [];
    const completions: unknown[] = [];
    const transport = new FakeRpcTransport([
      {
        subscriptionDescriptor: T.StreamService.method.subscribe,
        startResult: create(T.StreamService.method.subscribe.output, {
          outcome: { case: "success", value: {} },
        }),
      },
    ]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);
    const subscription = observeTranscript(client.chat, target, {
      onEvent: (event) => events.push(event),
      onComplete: (completion) => completions.push(completion),
      onError: (error) => errors.push(error),
      onTransportLoss: () => transportLosses.push({}),
    });
    transport.failDescriptor(T.StreamService.method.subscribe, new TransportError("isolated socket loss"));
    await subscription.flush();
    expect(errors).toEqual([]);
    expect(transportLosses).toHaveLength(1);

    transport.emitDescriptor(
      T.StreamService.method.subscribe,
      T.StreamService.method.event,
      create(T.MessageSchema, {
        sequence: 1n,
        event: { payload: { case: "hydration", value: transcriptHydrationPayload() } },
      }),
    );
    await subscription.flush();
    for (const [sequence, identity] of [
      [2n, "223e4567-e89b-42d3-a456-426614174000"],
      [3n, sessionID],
    ] as const) {
      transport.emitDescriptor(
        T.StreamService.method.subscribe,
        T.StreamService.method.event,
        create(T.MessageSchema, {
          sequence,
          event: {
            payload: {
              case: "sessionIdentity",
              value: {
                category: SessionCategory.MAIN,
                sessionId: identity,
                conversationFreshness: R.ConversationFreshness.FRESH,
              },
            },
          },
        }),
      );
    }
    transport.completeDescriptor(
      T.StreamService.method.subscribe,
      T.StreamService.method.complete,
      create(T.StreamService.method.complete.input, {
        code: StreamFailureCode.STREAM_GAP,
        message: "subscriber overflow",
        transcriptCloseReason: TranscriptCloseReason.SUBSCRIBER_OVERFLOW,
      }),
    );
    await subscription.flush();

    expect(events).toHaveLength(2);
    expect(events[0]).toMatchObject({ sequence: 1, kind: "hydration" });
    expect(events[1]).toMatchObject({ sequence: 3, kind: "session_identity" });
    expect(errors).toHaveLength(1);
    expect(errors[0]).toBeInstanceOf(ContractError);
    expect(completions).toEqual([
      { code: StreamFailureCode.STREAM_GAP, message: "subscriber overflow", reason: "subscriber_overflow" },
    ]);

    await subscription.flush();
    subscription.close();
  });

  it("rejects hydration whose older-history boundary has no cursor", () => {
    const payload = transcriptHydrationPayload();
    if (payload.tailSegment === undefined) throw new Error("Tail fixture missing.");
    payload.tailSegment.hasMoreAbove = true;
    expect(() =>
      encode(
        T.MessageSchema,
        create(T.MessageSchema, {
          sequence: 1n,
          event: { payload: { case: "hydration", value: payload } },
        }),
      ),
    ).toThrow();
  });

  it("rejects hydration whose tail segment is absent", () => {
    const payload = transcriptHydrationPayload();
    payload.tailSegment = undefined;
    expect(() =>
      encode(
        T.MessageSchema,
        create(T.MessageSchema, {
          sequence: 1n,
          event: { payload: { case: "hydration", value: payload } },
        }),
      ),
    ).toThrow();
  });
});

describe("Desktop Chat mutation adapter", () => {
  const queueItemID = "223e4567-e89b-42d3-a456-426614174000";
  const sessionTarget = {
    kind: "session",
    projectID: "project-1",
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
              outcome: {
                case: "accepted",
                value: {
                  queueItem: { id: queueItemID },
                  disposition: InputAdmissionDisposition.STARTED_TURN,
                },
              },
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
              outcome: {
                case: "accepted",
                value: {
                  queueItem: { id: queueItemID },
                  disposition: InputAdmissionDisposition.PENDING_QUEUE,
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
              outcome: { case: "notAccepted", value: { reason: { case: "tooSoon", value: {} } } },
            },
          },
        }),
      },
    ]);
    const chat = new ApiClient(transport, unexpectedProjectOverflow).chat;

    await chat.steer(
      sessionTarget,
      { kind: "text", text: "continue" },
      pendingWorkItemIDFromProto(queueItemID),
    );
    await chat.queue(
      newChatTarget,
      {
        kind: "command",
        catalogIdentity: "prompt:review",
        token: "/review",
        separatorWhitespace: "\t",
        arguments: "working tree",
      },
      pendingWorkItemIDFromProto(queueItemID),
    );
    const rejected = await chat.compact(newChatTarget, {
      token: "/compact",
      separatorWhitespace: " \t",
      rawGuidance: " keep   decisions ",
    });
    expect(rejected).toEqual({
      sessionID,
      outcome: { kind: "not_accepted", reason: { kind: "too_soon" } },
    });

    expect(transport.descriptorCalls.map(({ request }) => request)).toMatchObject([
      {
        target: {
          target: {
            case: "session",
            value: { sessionId: { value: { case: "sessionId", value: sessionID } } },
          },
        },
        activation: { input: { case: "text", value: "continue" } },
        queueItemId: queueItemID,
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
              catalogIdentity: "prompt:review",
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
        queueItemId: queueItemID,
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
              catalogIdentity: "prompt:review",
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
                  disposition: InputAdmissionDisposition.PENDING_QUEUE,
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
                value: {},
              },
            },
          },
        }),
      },
    ]);
    const chat = new ApiClient(transport, unexpectedProjectOverflow).chat;

    const queued = await chat.queue(
      sessionTarget,
      { kind: "text", text: "continue" },
      pendingWorkItemIDFromProto(queueItemID),
    );
    if (queued.outcome.kind !== "accepted") throw new Error("Expected accepted Queue fixture.");
    expect(queued.outcome.queueItemID.toJSONValue()).toBe(queueItemID);
    expect(queued.outcome.diagnostic).toMatchObject({ kind: "prompt_history_failure" });

    const compacted = await chat.compact(sessionTarget, {
      token: "/compact",
      separatorWhitespace: "",
      rawGuidance: "",
    });
    if (compacted.outcome.kind !== "accepted") throw new Error("Expected accepted compaction fixture.");

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
      unexpectedProjectOverflow,
    ).chat;
    const error = await failingChat
      .queue(sessionTarget, { kind: "text", text: "continue" }, pendingWorkItemIDFromProto(queueItemID))
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
        newChatTarget,
      ),
    ).toThrow(ContractError);

    for (const testCase of [
      {
        returnedSessionID: "423e4567-e89b-42d3-a456-426614174000",
        returnedQueueItemID: queueItemID,
      },
      { returnedSessionID: sessionID, returnedQueueItemID: "not-a-queue-item-id" },
      { returnedSessionID: sessionID, returnedQueueItemID: "33333333-3333-4333-8333-333333333333" },
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
                  value: {
                    queueItem: { id: testCase.returnedQueueItemID },
                    disposition: InputAdmissionDisposition.STARTED_TURN,
                  },
                },
              },
            },
          }),
        },
      ]);
      const rejected = await new ApiClient(transport, unexpectedProjectOverflow).chat
        .steer(
          sessionTarget,
          {
            kind: "text",
            text: "continue",
          },
          pendingWorkItemIDFromProto(queueItemID),
        )
        .then(
          () => {
            throw new Error("Expected the invalid admission response to be rejected.");
          },
          (error: unknown) => error,
        );
      expect(rejected).toBeInstanceOf(Error);
    }
  });
});
