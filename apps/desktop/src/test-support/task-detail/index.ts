import { unexpectedProjectOverflow } from "@/test-support/api";
import { create, decode, encode, type DescMessage } from "@app/server-api-contract";
import * as taskRead from "@app/server-api-contract/gen/kent/api/workflow_task/read_pb";
import * as taskLifecycle from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";
import * as taskAttention from "@app/server-api-contract/gen/kent/api/workflow_task/attention_pb";
import * as workflow from "@app/server-api-contract/gen/kent/api/workflow_definition/workflow_definition_pb";
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

import { type PromptAnswerBatchResponse, type QuestionAttentionItem, type TaskDetail } from "@/api";
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
import { attentionBase } from "./attention";
export { taskQuestionPage } from "./attention";

export {
  taskActionFixture,
  backlogTaskFixture,
  taskStartApplied,
  taskStartNeedsDependencies,
  taskStartRoute,
  taskResumeNeedsTarget,
  taskResumeApplied,
  taskResumeRoute,
  taskDeleted,
  taskAlreadyDeleted,
  taskDeleteRoute,
  taskDependencyAddedRoute,
  taskDependencyRemovedRoute,
  taskDependencyRemovalFailure,
} from "./lifecycle";
export {
  taskIdentityFixture,
  taskBlockedByFixture,
  taskCommentPage,
  taskCommentRoute,
  taskActivityPage,
  taskActivityRoute,
} from "./reads";

const taskActions = {
  canStart: false,
  canInterrupt: true,
  canResume: false,
  canDelete: false,
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
    project: {
      projectKey: "T",
      displayName: "Project",
      defaultWorkspaceId: "workspace-1",
      attachedWorkspaceCount: 1,
    },
    workflow: { workflowId: "11111111-1111-4111-8111-111111111111", displayName: "Delivery", version: 1n },
    body: "Need operator input",
    sourceWorkspace: {
      workspaceId: "workspace-1",
      displayName: "Main",
      rootPath: "/tmp/project",
      availability: ProjectAvailability.AVAILABLE,
      isPrimary: true,
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
        sessionId: "33333333-3333-4333-8333-333333333333",
      },
    ],
    liveSessions: [
      {
        sessionId: "33333333-3333-4333-8333-333333333333",
        sessionName: "Review chat",
        nodeDisplayName: "Code Review",
      },
      {
        sessionId: "44444444-4444-4444-8444-444444444444",
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

export const taskAttentionResponse = create(taskAttention.TaskAttentionListSuccessSchema, {
  items: [
    {
      ...attentionBase,
      id: "attention-question",
      kind: taskAttention.AttentionItemKind.QUESTION,
      detail: {
        case: "question",
        value: {
          currentNode: { nodeId: "node-1" },
          sessionName: "Session one",
          question: {
            sessionId: "33333333-3333-4333-8333-333333333333",
            stepId: "22222222-2222-4222-8222-222222222222",
            toolCallId: "ask-1",
            kind: taskAttention.AttentionQuestionKind.ORDINARY,
            prompt: { case: "ordinary", value: { suggestions: [] } },
          },
          message: "Approve protected path?",
        },
      },
    },
    {
      ...attentionBase,
      id: "attention-approval",
      kind: taskAttention.AttentionItemKind.APPROVAL,
      detail: {
        case: "approval",
        value: {
          approvalId: "approval-1",
          approvalSnapshot: {
            sourceNodeDisplayName: "Implement",
            targets: [{ displayName: "Ship" }],
            commentary: "Looks good",
            outputValues: [{ name: "result", value: "ok" }],
            workflowRevisionSeen: 7n,
          },
        },
      },
    },
  ],
  generatedAt: { seconds: 0n, nanos: 3_000_000 },
});

export const emptyTaskAttentionResponse = create(taskAttention.TaskAttentionListSuccessSchema, {
  generatedAt: { seconds: 0n, nanos: 3_000_000 },
});

export async function createTaskDetailFixture(): Promise<TaskDetail> {
  const client = new ApiClient(
    new FakeRpcTransport([
      {
        descriptor: taskRead.TaskReadService.method.get,
        result: create(taskRead.GetResultSchema, { outcome: { case: "success", value: taskDetailResponse } }),
      },
    ]),
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
        sessionId: "55555555-5555-4555-8555-555555555555",
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

export const interruptedTaskAttentionResponse = create(taskAttention.TaskAttentionListSuccessSchema, {
  items: [
    {
      ...attentionBase,
      id: "attention-interrupted",
      kind: taskAttention.AttentionItemKind.INTERRUPTED_CURRENT_NODE,
      detail: {
        case: "interruptedCurrentNode",
        value: {
          currentNode: { nodeId: "node-script" },
          details: {
            code: "script_failure",
            fields: [{ name: "stderr", value: "permission denied" }],
            detail: { case: "generic", value: {} },
          },
        },
      },
    },
  ],
  generatedAt: { seconds: 0n, nanos: 3_000_000 },
});

export const questionAttention = create(taskAttention.AttentionItemSchema, {
  ...attentionBase,
  id: "attention-question",
  kind: taskAttention.AttentionItemKind.QUESTION,
  detail: {
    case: "question",
    value: {
      currentNode: { nodeId: "node-1" },
      sessionName: "Session one",
      message: "Choose snack",
      question: {
        sessionId: "33333333-3333-4333-8333-333333333333",
        stepId: "22222222-2222-4222-8222-222222222222",
        toolCallId: "ask-1",
        kind: taskAttention.AttentionQuestionKind.ORDINARY,
        prompt: {
          case: "ordinary",
          value: { recommendedOptionIndex: 1, suggestions: ["Trail mix", "Dark chocolate"] },
        },
      },
    },
  },
});

export const questionAttentionResponse = create(taskAttention.TaskAttentionListSuccessSchema, {
  generatedAt: { seconds: 0n, nanos: 3_000_000 },
  items: [questionAttention],
});

export function parsedQuestionAttention(): QuestionAttentionItem &
  Readonly<{
    question: Extract<QuestionAttentionItem["question"], Readonly<{ kind: "ordinary" }>>;
  }> {
  const detail = questionAttention.detail;
  if (detail.case !== "question" || detail.value.question?.prompt.case !== "ordinary") {
    throw new Error("Ordinary Question fixture is required.");
  }
  const question = detail.value.question;
  const prompt = question.prompt;
  if (prompt.case !== "ordinary") throw new Error("Ordinary Question fixture is required.");
  return {
    id: questionAttention.id,
    projectID: questionAttention.projectId,
    workflowID: questionAttention.workflowId,
    taskID: questionAttention.taskId,
    taskShortID: questionAttention.taskShortId,
    taskTitle: questionAttention.taskTitle,
    occurredAt: 1,
    kind: "question",
    currentNode: {
      nodeID: "node-1",
      transitionBranchKey: null,
      sessionID: null,
      effectiveAssignee: null,
      effectiveThinking: null,
    },
    sessionName: detail.value.sessionName ?? null,
    message: detail.value.message ?? null,
    question: {
      sessionID: question.sessionId,
      stepID: question.stepId,
      toolCallID: question.toolCallId,
      kind: "ordinary",
      recommendedOptionIndex: prompt.value.recommendedOptionIndex ?? null,
      suggestions: prompt.value.suggestions,
    },
  };
}

export const taskQuestionWaitingEvent = create(workflow.ProjectEventSchema, {
  resource: workflow.ProjectEventResource.WORKFLOW_PROJECT_EVENT_RESOURCE_TASK,
  action: workflow.ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_QUESTION_WAITING,
  occurredAt: { seconds: 0n, nanos: 1_000_000 },
  primaryEntityId: "task-1",
  projectId: "project-1",
  relatedIds: ["33333333-3333-4333-8333-333333333333", "ask-1"],
  workflowId: "11111111-1111-4111-8111-111111111111",
});

export const taskUpdatedEvent = create(workflow.ProjectEventSchema, {
  ...taskQuestionWaitingEvent,
  action: workflow.ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_UPDATED,
  relatedIds: [],
});

export const activityResponse = create(taskLifecycle.ActivityListSuccessSchema, {
  items: [
    {
      activityId: "activity-1",
      taskId: "task-1",
      occurredAt: { seconds: 0n, nanos: 2_000_000 },
      updatedAt: { seconds: 0n, nanos: 2_000_000 },
      activity: {
        case: "comment",
        value: {
          id: "comment-activity-1",
          taskId: "task-1",
          body: "Comment added",
          author: taskLifecycle.CommentAuthorKind.USER,
          createdAt: { seconds: 0n, nanos: 2_000_000 },
          updatedAt: { seconds: 0n, nanos: 2_000_000 },
        },
      },
    },
  ],
});

export const pendingAskResponse = create(QuestionService.method.listPending.output, {
  outcome: {
    case: "success",
    value: {
      questions: [
        {
          toolCallId: "ask-1",
          sessionId: "33333333-3333-4333-8333-333333333333",
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

export const commentAddResponse = create(taskLifecycle.CommentAddSuccessSchema, {
  comment: {
    id: "comment-2",
    taskId: "task-1",
    body: "Fresh comment",
    author: taskLifecycle.CommentAuthorKind.USER,
    createdAt: { seconds: 0n, nanos: 4_000_000 },
    updatedAt: { seconds: 0n, nanos: 4_000_000 },
  },
});

export const commentListResponse = create(taskLifecycle.CommentListSuccessSchema, {
  items: [
    {
      id: "comment-1",
      taskId: "task-1",
      body: "Existing comment",
      author: taskLifecycle.CommentAuthorKind.USER,
      createdAt: { seconds: 0n, nanos: 1_000_000 },
      updatedAt: { seconds: 0n, nanos: 1_000_000 },
    },
  ],
  totalCount: 1n,
});

export const firstCommentListResponse = create(taskLifecycle.CommentListSuccessSchema, {
  items: [
    {
      id: "comment-page-1",
      taskId: "task-1",
      body: "First paged comment",
      author: taskLifecycle.CommentAuthorKind.USER,
      createdAt: { seconds: 0n, nanos: 5_000_000 },
      updatedAt: { seconds: 0n, nanos: 5_000_000 },
    },
  ],
  nextOffset: 50,
  totalCount: 2n,
});

export const secondCommentListResponse = create(taskLifecycle.CommentListSuccessSchema, {
  items: [
    {
      id: "comment-page-2",
      taskId: "task-1",
      body: "Second paged comment",
      author: taskLifecycle.CommentAuthorKind.USER,
      createdAt: { seconds: 0n, nanos: 6_000_000 },
      updatedAt: { seconds: 0n, nanos: 6_000_000 },
    },
  ],
  totalCount: 2n,
});

export const taskUpdateResponse = create(taskLifecycle.UpdateResultSchema, {
  outcome: { case: "success", value: { task: taskDetailResponse.task.summary } },
});

export type TaskDetailFixtureOptions = Readonly<{
  attention?: taskAttention.TaskAttentionListSuccess;
  asks?: ListQuestionsResult;
  comments?: taskLifecycle.CommentListSuccess;
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
    comments = create(taskLifecycle.CommentListSuccessSchema),
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
      {
        descriptor: taskAttention.AttentionReadService.method.listTask,
        result: create(taskAttention.TaskAttentionListResultSchema, {
          outcome: { case: "success", value: attention },
        }),
      },
      {
        descriptor: taskLifecycle.TaskCommentService.method.list,
        result: create(taskLifecycle.CommentListResultSchema, {
          outcome: { case: "success", value: comments },
        }),
      },
      {
        descriptor: taskLifecycle.TaskActivityService.method.list,
        result: create(taskLifecycle.ActivityListResultSchema, {
          outcome: { case: "success", value: activityResponse },
        }),
      },
      ...(asks === undefined ? [] : [{ descriptor: QuestionService.method.listPending, result: asks }]),
      ...routes,
    ],
    nativeBridge,
  );
}

export function taskGetRoute(
  load: (taskID: string, callIndex: number) => typeof taskDetailResponse | Promise<typeof taskDetailResponse>,
): FakeRoute {
  return {
    descriptor: taskRead.TaskReadService.method.get,
    resultFactory: async (request, callIndex) => {
      const params = decode(
        taskRead.GetRequestSchema,
        encode<DescMessage>(taskRead.GetRequestSchema, request),
      );
      if (params.taskId === undefined) throw new Error("Task fixture read requires Task identity.");
      return create(taskRead.GetResultSchema, {
        outcome: { case: "success", value: await load(params.taskId, callIndex) },
      });
    },
  };
}

export function taskUpdateRoute(
  result: () => typeof taskUpdateResponse | Promise<typeof taskUpdateResponse> = () => taskUpdateResponse,
): FakeRoute {
  return { descriptor: taskLifecycle.TaskLifecycleService.method.update, resultFactory: result };
}

export function taskAttentionRoute(
  load: () => taskAttention.TaskAttentionListSuccess | Promise<taskAttention.TaskAttentionListSuccess>,
): FakeRoute {
  return {
    descriptor: taskAttention.AttentionReadService.method.listTask,
    resultFactory: async () =>
      create(taskAttention.TaskAttentionListResultSchema, {
        outcome: { case: "success", value: await load() },
      }),
  };
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

function taskAttentionFixture(task: typeof taskDetailResponse): taskAttention.TaskAttentionListSuccess {
  if (task === taskDetailResponseWithInterruptedCurrentScript) {
    return interruptedTaskAttentionResponse;
  }
  if (task.task.attentionCount === 0) {
    return emptyTaskAttentionResponse;
  }
  return taskAttentionResponse;
}
