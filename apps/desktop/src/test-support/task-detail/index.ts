import { unexpectedProjectOverflow } from "@/test-support/api";
import { z } from "zod";
import { create } from "@app/server-api-contract";
import * as taskRead from "@app/server-api-contract/gen/kent/api/workflow_task/read_pb";
import * as taskLifecycle from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";
import { AttentionCurrentNodeSchema } from "@app/server-api-contract/gen/kent/api/workflow_task/attention_pb";
import { ProjectAvailability } from "@app/server-api-contract/gen/kent/api/project/project_pb";
import { ExecutionTargetMode } from "@app/server-api-contract/gen/kent/api/workflow_definition/workflow_definition_pb";
import {
  AnswerService,
  AnswerBatchOutcome,
  QuestionService,
  type ListQuestionsResult,
} from "@app/server-api-contract/gen/kent/api/prompt/prompt_pb";
import { createElement } from "react";
import { render } from "@testing-library/react";
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterContextProvider,
} from "@tanstack/react-router";

import {
  guiTaskCommentAuthor,
  type JsonObject,
  type JsonValue,
  type PromptAnswerBatchResponse,
  type QuestionAttentionItem,
  type TaskDetail,
} from "@/api";
import { ApiClient } from "@/api/composition";
import {
  SidebarRootContext,
  type SidebarPageNavigator,
  type SidebarRootController,
  type SidebarMode,
  type TaskDetailInitialFocus,
} from "@/app-facade";
import {
  TaskDetailSurface,
  type TaskDetailDeleteDismissal,
  type TaskDetailSessionChatEntry,
} from "@/features/task-detail";
import { FakeRpcTransport, type FakeRoute } from "../api";
import { createTestServices, startupRoutes, TestAppProviders, type TestAppServices } from "../app-services";
import type { NativeBridge } from "../native-bridge";
import { createTestSidebarController } from "../sidebar";

const jsonObjectSchema = z.record(z.string(), z.unknown());

export const taskUpdateParamsSchema = jsonObjectSchema.and(
  z.object({
    body: z.string().optional(),
    title: z.string().optional(),
  }),
);

const taskActions = {
  canStart: false,
  canInterrupt: true,
  canResume: false,
  canDelete: false,
};

const attentionBase = {
  project_id: "project-1",
  workflow_id: "11111111-1111-4111-8111-111111111111",
  task_id: "task-1",
  task_short_id: "T-1",
  task_title: "Resolve blocker",
  occurred_at_unix_ms: 1,
};

export const taskDetailResponse = {
  task: create(taskRead.TaskDetailSchema, {
    summary: {
      id: "task-1",
      projectId: "project-1",
      workflowId: "11111111-1111-4111-8111-111111111111",
      shortId: "T-1",
      title: "Resolve blocker",
      createdAt: { seconds: 0n, nanos: 1_000_000 },
      updatedAt: { seconds: 0n, nanos: 2_000_000 },
      done: false,
    },
    project: { projectKey: "T", displayName: "Project", defaultWorkspaceId: "workspace-1", attachedWorkspaceCount: 1 },
    workflow: { workflowId: "11111111-1111-4111-8111-111111111111", displayName: "Delivery", version: 1n },
    body: "Need operator input",
    sourceWorkspace: {
      workspaceId: "workspace-1", displayName: "Main", rootPath: "/tmp/project",
      availability: ProjectAvailability.AVAILABLE, isPrimary: true,
      updatedAt: { seconds: 0n, nanos: 1_000_000 },
    },
    executionTarget: {
      mode: ExecutionTargetMode.WORKFLOW_EXECUTION_TARGET_MODE_HEAD,
      requestedRef: "HEAD",
      resolvedRef: "refs/heads/main",
      commitOid: "0123456789abcdef0123456789abcdef01234567",
      provenance: taskRead.ExecutionTargetProvenance.RESOLVED,
    },
    worktreePath: "/tmp/worktree",
    currentNodes: [
      {
        nodeId: "node-1",
        sessionId: "session-1",
      },
    ],
    liveSessions: [
      {
        sessionId: "session-1",
        sessionName: "Review chat",
        nodeDisplayName: "Code Review",
      },
      {
        sessionId: "session-2",
        nodeDisplayName: "Implementation",
      },
    ],
    retainedSessionCount: 1,
    status: {
      kind: taskRead.TaskStatusKind.RUNNING,
      nativeState: taskRead.TaskNativeState.RUNNING,
      nodeIds: ["node-1"],
      attentionTypes: [taskRead.TaskAttentionKind.QUESTION, taskRead.TaskAttentionKind.APPROVAL],
    },
    actions: taskActions,
    attentionCount: 2,
    dependencies: {
      directions: [
        {
          direction: taskRead.DependencyDirection.BLOCKED_BY,
          unsatisfiedCount: 0,
          addAvailability: { availability: { case: "available", value: { remainingCapacity: 5 } } },
        },
        {
          direction: taskRead.DependencyDirection.BLOCKS,
          addAvailability: { availability: { case: "available", value: { remainingCapacity: 4 } } },
        },
      ],
    },
  }),
};

export const taskAttentionResponse = {
  items: [
    {
      ...attentionBase,
      id: "attention-question",
      kind: "question",
      current_node: {
        node_id: "node-1",
        transition_branch_key: null,
        session_id: null,
      },
      session_name: "Session one",
      question: {
        session_id: "session-1",
        step_id: "22222222-2222-4222-8222-222222222222",
        tool_call_id: "ask-1",
        kind: "ordinary",
        suggestions: [],
        recommended_option_index: null,
      },
      message: "Approve protected path?",
    },
    {
      ...attentionBase,
      id: "attention-approval",
      kind: "approval",
      session_name: null,
      approval_id: "approval-1",
      approval_snapshot: {
        source_node_display_name: "Implement",
        targets: [{ display_name: "Ship" }],
        commentary: "Looks good",
        output_values: { result: "ok" },
        workflow_revision_seen: 7,
      },
    },
  ],
  generated_at_unix_ms: 3,
};

export const emptyTaskAttentionResponse = {
  items: [],
  generated_at_unix_ms: 3,
};

export async function createTaskDetailFixture(): Promise<TaskDetail> {
  const client = new ApiClient(
    new FakeRpcTransport([{
      descriptor: taskRead.TaskReadService.method.get,
      result: create(taskRead.GetResultSchema, { outcome: { case: "success", value: taskDetailResponse } }),
    }]),
    unexpectedProjectOverflow,
  );
  return client.getTask("task-1");
}

export const taskDetailResponseWithAdditionalLiveSession = {
  task: create(taskRead.TaskDetailSchema, {
    ...taskDetailResponse.task,
    liveSessions: [
      ...taskDetailResponse.task.liveSessions,
      create(taskRead.LiveSessionSchema, {
        sessionId: "session-3",
        sessionName: "QA chat",
        nodeDisplayName: "QA",
      }),
    ],
  }),
};

export const taskDetailNoInboxResponse = {
  task: create(taskRead.TaskDetailSchema, {
    ...taskDetailResponse.task,
    attentionCount: 0,
  }),
};

export const taskDetailResponseWithCurrentScript = {
  task: create(taskRead.TaskDetailSchema, {
    ...taskDetailNoInboxResponse.task,
    currentNodes: [create(AttentionCurrentNodeSchema, { nodeId: "node-script" })],
    liveSessions: [],
    currentScripts: [
      create(taskRead.CurrentScriptSchema, {
        currentNode: { nodeId: "node-script" },
        path: "scripts/run",
      }),
    ],
  }),
};

export const taskDetailResponseWithInterruptedCurrentScript = {
  task: create(taskRead.TaskDetailSchema, {
    ...taskDetailResponseWithCurrentScript.task,
    actions: create(taskRead.TaskActionsSchema, { ...taskActions, canInterrupt: false, canResume: true }),
    attentionCount: 1,
    currentScripts: [],
  }),
};

export const interruptedTaskAttentionResponse = {
  items: [
    {
      ...attentionBase,
      id: "attention-interrupted",
      kind: "interrupted_current_node",
      session_name: null,
      current_node: { node_id: "node-script", transition_branch_key: null, session_id: null },
      session_id: null,
      detail_json: '{"kind":"script_failure","stderr":"permission denied"}',
    },
  ],
  generated_at_unix_ms: 3,
};

export const questionAttention = {
  ...attentionBase,
  id: "attention-question",
  kind: "question",
  current_node: {
    node_id: "node-1",
    transition_branch_key: null,
    session_id: null,
  },
  session_name: "Session one",
  message: "Choose snack",
  question: {
    session_id: "session-1",
    step_id: "22222222-2222-4222-8222-222222222222",
    tool_call_id: "ask-1",
    kind: "ordinary",
    recommended_option_index: 1,
    suggestions: ["Trail mix", "Dark chocolate"],
  },
};

export function parsedQuestionAttention(): QuestionAttentionItem &
  Readonly<{
    question: Extract<QuestionAttentionItem["question"], Readonly<{ kind: "ordinary" }>>;
  }> {
  return {
    id: questionAttention.id,
    projectID: questionAttention.project_id,
    workflowID: questionAttention.workflow_id,
    taskID: questionAttention.task_id,
    taskShortID: questionAttention.task_short_id,
    taskTitle: questionAttention.task_title,
    occurredAt: questionAttention.occurred_at_unix_ms,
    kind: "question",
    currentNode: {
      nodeID: questionAttention.current_node.node_id,
      transitionBranchKey: questionAttention.current_node.transition_branch_key,
      sessionID: questionAttention.current_node.session_id,
      effectiveAssignee: null,
      effectiveThinking: null,
    },
    sessionName: questionAttention.session_name,
    message: questionAttention.message,
    question: {
      sessionID: questionAttention.question.session_id,
      stepID: questionAttention.question.step_id,
      toolCallID: questionAttention.question.tool_call_id,
      kind: "ordinary",
      recommendedOptionIndex: questionAttention.question.recommended_option_index,
      suggestions: questionAttention.question.suggestions,
    },
  };
}

export const taskQuestionWaitingEvent = {
  event: {
    resource: "task",
    action: "question_waiting",
    occurred_at_unix_ms: 1,
    primary_entity_id: "task-1",
    project_id: "project-1",
    related_ids: ["session-1", "ask-1"],
    workflow_id: "11111111-1111-4111-8111-111111111111",
  },
};

export const taskUpdatedEvent = {
  event: {
    resource: "task",
    action: "updated",
    occurred_at_unix_ms: 1,
    primary_entity_id: "task-1",
    project_id: "project-1",
    workflow_id: "11111111-1111-4111-8111-111111111111",
  },
};

export const activityResponse = {
  items: [
    {
      activity_id: "activity-1",
      type: "comment",
      task_id: "task-1",
      occurred_at_unix_ms: 2,
      updated_at_unix_ms: 2,
      comment: {
        id: "comment-activity-1",
        task_id: "task-1",
        body: "Comment added",
        author: "user",
        created_at_unix_ms: 2,
        updated_at_unix_ms: 2,
      },
    },
  ],
  next_offset: null,
};

export const pendingAskResponse = create(QuestionService.method.listPending.output, {
  outcome: {
    case: "success",
    value: {
      questions: [
        {
          toolCallId: "ask-1",
          sessionId: "session-1",
          stepId: "11111111-1111-4111-8111-111111111111",
          question: "Choose path",
          suggestions: ["Use option A", "Use option B"],
          recommendedOptionIndex: 1,
          createdAt: { seconds: 1778889600n },
        },
      ],
    },
  },
});

export function promptAnswerBatchRoute(
  handler: () => PromptAnswerBatchResponse | Promise<PromptAnswerBatchResponse>,
): FakeRoute {
  return {
    descriptor: AnswerService.method.answerBatch,
    resultFactory: async () => {
      const response = await handler();
      return create(AnswerService.method.answerBatch.output, {
        outcome: {
          case: "success",
          value: {
            results: response.results.map((result) => ({
              toolCallId: result.toolCallID,
              outcome: { resolved: AnswerBatchOutcome.RESOLVED, skipped: AnswerBatchOutcome.SKIPPED }[
                result.outcome
              ],
            })),
          },
        },
      });
    },
  };
}

export const commentAddResponse = {
  comment: {
    id: "comment-2",
    task_id: "task-1",
    body: "Fresh comment",
    author: guiTaskCommentAuthor,
    created_at_unix_ms: 4,
    updated_at_unix_ms: 4,
  },
};

export const commentListResponse = {
  items: [
    {
      id: "comment-1",
      task_id: "task-1",
      body: "Existing comment",
      author: guiTaskCommentAuthor,
      created_at_unix_ms: 1,
      updated_at_unix_ms: 1,
    },
  ],
  next_offset: null,
  total_count: 1,
};

export const firstCommentListResponse = {
  items: [
    {
      id: "comment-page-1",
      task_id: "task-1",
      body: "First paged comment",
      author: guiTaskCommentAuthor,
      created_at_unix_ms: 5,
      updated_at_unix_ms: 5,
    },
  ],
  next_offset: 50,
  total_count: 2,
};

export const secondCommentListResponse = {
  items: [
    {
      id: "comment-page-2",
      task_id: "task-1",
      body: "Second paged comment",
      author: guiTaskCommentAuthor,
      created_at_unix_ms: 6,
      updated_at_unix_ms: 6,
    },
  ],
  next_offset: null,
  total_count: 2,
};

export const taskUpdateResponse = create(taskLifecycle.UpdateResultSchema, {
  outcome: { case: "success", value: { task: taskDetailResponse.task.summary } },
});

export type TaskDetailFixtureOptions = Readonly<{
  attention?: JsonValue;
  asks?: ListQuestionsResult;
  comments?: unknown;
  initialFocus?: TaskDetailInitialFocus | undefined;
  nativeBridge?: NativeBridge | undefined;
  navigator?: SidebarPageNavigator | undefined;
  onDeleteDismiss?: TaskDetailDeleteDismissal | undefined;
  openSessionChat?: TaskDetailSessionChatEntry | undefined;
  onMutated?: (() => void) | undefined;
  openSidebar?: SidebarRootController["open"] | undefined;
  path?: string | undefined;
  retainedState?: unknown;
  sidebarMode?: SidebarMode | undefined;
  routes?: readonly FakeRoute[] | undefined;
}>;

export function createTaskDetailTestServices(
  task: typeof taskDetailResponse,
  {
    asks,
    attention = taskAttentionFixture(task),
    comments,
    nativeBridge,
    path = "/tasks/task-1",
    routes = [],
  }: TaskDetailFixtureOptions = {},
): TestAppServices {
  window.history.pushState(null, "", path);
  return createTestServices(
    [
      ...startupRoutes,
      {
        descriptor: taskRead.TaskReadService.method.get,
        result: create(taskRead.GetResultSchema, { outcome: { case: "success", value: task } }),
      },
      { method: "workflow.task.attention.list", result: attention },
      ...(comments === undefined ? [] : [{ method: "workflow.task.comment.list", result: comments }]),
      { method: "workflow.task.activity.list", result: activityResponse },
      ...(asks === undefined ? [] : [{ descriptor: QuestionService.method.listPending, result: asks }]),
      ...routes,
    ],
    nativeBridge,
  );
}

export type MountedTaskDetailServices = TestAppServices &
  Readonly<{
    rerenderTaskDetail(taskID: string, retainedState?: unknown): void;
    unmountTaskDetail(): void;
  }>;

export function mountTaskDetailSurface(
  task: typeof taskDetailResponse,
  options: TaskDetailFixtureOptions = {},
): MountedTaskDetailServices {
  const services = createTaskDetailTestServices(task, options);
  const router = createRouter({
    history: createMemoryHistory({ initialEntries: [options.path ?? "/tasks/task-1"] }),
    routeTree: createRootRoute(),
  });
  const renderSurface = (taskID: string, retainedState: unknown) =>
    createElement(RouterContextProvider, {
      router,
      children: createElement(SidebarRootContext.Provider, {
        value: createTestSidebarController(),
        children: createElement(TestAppProviders, {
          children: createElement(
            TaskDetailSurface,
            options.navigator === undefined
              ? {
                  enabled: true,
                  initialFocus: options.initialFocus,
                  onDeleteDismiss: options.onDeleteDismiss ?? (async () => ({ kind: "accepted" })),
                  openSessionChat: options.openSessionChat,
                  onMutated: options.onMutated,
                  openSidebar: options.openSidebar,
                  retainedState,
                  sidebarMode: options.sidebarMode,
                  taskId: taskID,
                }
              : {
                  enabled: true,
                  initialFocus: options.initialFocus,
                  navigator: options.navigator,
                  openSessionChat: options.openSessionChat,
                  onMutated: options.onMutated,
                  openSidebar: options.openSidebar,
                  retainedState,
                  sidebarDestination: {
                    kind: "taskDetail",
                    taskID,
                    ...(options.sidebarMode === undefined ? {} : { mode: options.sidebarMode }),
                    ...(options.onMutated === undefined ? {} : { onMutated: options.onMutated }),
                  },
                  sidebarMode: options.sidebarMode,
                  taskId: taskID,
                },
          ),
          services,
        }),
      }),
    });
  const mounted = render(renderSurface("task-1", options.retainedState));
  return {
    ...services,
    rerenderTaskDetail: (taskID, retainedState) => {
      mounted.rerender(renderSurface(taskID, retainedState));
    },
    unmountTaskDetail: mounted.unmount,
  };
}

function taskAttentionFixture(task: typeof taskDetailResponse): JsonValue {
  if (task === taskDetailResponseWithInterruptedCurrentScript) {
    return interruptedTaskAttentionResponse;
  }
  if (task.task.attentionCount === 0) {
    return emptyTaskAttentionResponse;
  }
  return taskAttentionResponse;
}

export function callParams(
  calls: readonly Readonly<{ method: string; params: JsonValue }>[],
  method: string,
): JsonObject {
  const params = calls.find((call) => call.method === method)?.params;
  if (!isJsonObject(params)) {
    throw new Error(`Missing object params for ${method}.`);
  }
  return params;
}

export function getCallCount(
  calls: readonly Readonly<{ method: string; params: JsonValue }>[],
  method: string,
): number {
  return calls.filter((call) => call.method === method).length;
}

export function isJsonObject(value: JsonValue | undefined): value is JsonObject {
  return jsonObjectSchema.safeParse(value).success && !Array.isArray(value);
}
