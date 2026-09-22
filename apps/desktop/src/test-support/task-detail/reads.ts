import { create, decode, encode, type DescMessage } from "@app/server-api-contract";
import * as read from "@app/server-api-contract/gen/kent/api/workflow_task/read_pb";
import * as lifecycle from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";
import type { FakeRoute } from "../api";

type TaskFixture = Readonly<{ task: read.TaskDetail }>;

export function taskIdentityFixture(
  base: TaskFixture,
  taskID: string,
  title: string,
  shortID: string,
): TaskFixture {
  const summary = base.task.summary;
  if (summary === undefined) throw new Error("Task Summary fixture is required.");
  return {
    task: create(read.TaskDetailSchema, {
      ...base.task,
      summary: create(read.TaskSummarySchema, { ...summary, id: taskID, title, shortId: shortID }),
    }),
  };
}

export function taskBlockedByFixture(base: TaskFixture): TaskFixture {
  return {
    task: create(read.TaskDetailSchema, {
      ...base.task,
      dependencies: create(read.TaskDependenciesSchema, {
        blockerCount: 1,
        unsatisfiedBlockerCount: 1,
        directions: [
          {
            direction: read.DependencyDirection.BLOCKED_BY,
            totalCount: 1,
            unsatisfiedCount: 1,
            items: [
              {
                taskId: "task-2",
                shortId: "T-2",
                title: "Prepare",
                workflowId: "22222222-2222-4222-8222-222222222222",
                status: { kind: read.TaskStatusKind.BACKLOG, nativeState: read.TaskNativeState.ACTIVE },
                satisfaction: read.DependencySatisfaction.UNSATISFIED,
              },
            ],
            addAvailability: { availability: { case: "available", value: { remainingCapacity: 3 } } },
          },
          {
            direction: read.DependencyDirection.BLOCKS,
            addAvailability: { availability: { case: "available", value: { remainingCapacity: 2 } } },
          },
        ],
      }),
    }),
  };
}

export function taskCommentPage(
  comments: readonly Readonly<{ id: string; body: string; milliseconds: number }>[],
  totalCount = comments.length,
  nextOffset?: number,
) {
  return create(lifecycle.CommentListSuccessSchema, {
    items: comments.map((comment) => ({
      id: comment.id,
      taskId: "task-1",
      body: comment.body,
      author: lifecycle.CommentAuthorKind.USER,
      createdAt: timestamp(comment.milliseconds),
      updatedAt: timestamp(comment.milliseconds),
    })),
    totalCount: BigInt(totalCount),
    nextOffset,
  });
}

export function taskCommentRoute(
  load: (
    taskID: string,
    callIndex: number,
    offset: number,
  ) => lifecycle.CommentListSuccess | Promise<lifecycle.CommentListSuccess>,
): FakeRoute {
  return {
    descriptor: lifecycle.TaskCommentService.method.list,
    resultFactory: async (request, callIndex) => {
      const params = decode(
        lifecycle.TaskOffsetPageRequestSchema,
        encode<DescMessage>(lifecycle.TaskOffsetPageRequestSchema, request),
      );
      return create(lifecycle.CommentListResultSchema, {
        outcome: { case: "success", value: await load(params.taskId, callIndex, params.offset ?? 0) },
      });
    },
  };
}

export function taskActivityPage(taskID: string, count: number) {
  return create(lifecycle.ActivityListSuccessSchema, {
    items: Array.from({ length: count }, (_value, index) => create(lifecycle.ActivityItemSchema, {
      activityId: `activity-${taskID}-${index.toString()}`,
      taskId: taskID,
      occurredAt: timestamp(1000 - index),
      updatedAt: timestamp(1000 - index),
      activity: {
        case: "comment",
        value: {
          id: `comment-activity-${taskID}-${index.toString()}`,
          taskId: taskID,
          body: `Activity item ${index.toString()}`,
          author: lifecycle.CommentAuthorKind.USER,
          createdAt: timestamp(1000 - index),
          updatedAt: timestamp(1000 - index),
        },
      },
    })),
  });
}

export function taskActivityRoute(load: (taskID: string) => lifecycle.ActivityListSuccess): FakeRoute {
  return {
    descriptor: lifecycle.TaskActivityService.method.list,
    resultFactory: (request) => {
      const params = decode(
        lifecycle.TaskOffsetPageRequestSchema,
        encode<DescMessage>(lifecycle.TaskOffsetPageRequestSchema, request),
      );
      return create(lifecycle.ActivityListResultSchema, {
        outcome: { case: "success", value: load(params.taskId) },
      });
    },
  };
}

function timestamp(milliseconds: number) {
  return { seconds: BigInt(Math.floor(milliseconds / 1000)), nanos: (milliseconds % 1000) * 1_000_000 };
}
