import { create } from "@app/server-api-contract";
import * as read from "@app/server-api-contract/gen/kent/api/workflow_task/read_pb";
import * as lifecycle from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";
import type { FakeRoute } from "../api";

type TaskFixture = Readonly<{ task: read.TaskDetail }>;
type ActionChanges = Partial<Pick<read.TaskActions, "canStart" | "canInterrupt" | "canResume" | "canDelete">>;

export function taskActionFixture(
  base: TaskFixture,
  actions: ActionChanges,
  liveSessions?: readonly Readonly<{ sessionID: string; sessionName?: string; nodeDisplayName: string }>[],
): TaskFixture {
  return {
    task: create(read.TaskDetailSchema, {
      ...base.task,
      attentionCount: 0,
      actions: create(read.TaskActionsSchema, { ...base.task.actions, ...actions }),
      liveSessions:
        liveSessions === undefined
          ? base.task.liveSessions
          : liveSessions.map((session) =>
              create(read.LiveSessionSchema, {
                sessionId: session.sessionID,
                sessionName: session.sessionName,
                nodeDisplayName: session.nodeDisplayName,
              }),
            ),
    }),
  };
}

export function backlogTaskFixture(base: TaskFixture): TaskFixture {
  const task = taskActionFixture(base, { canStart: true, canInterrupt: false }, []);
  return {
    task: create(read.TaskDetailSchema, {
      ...task.task,
      currentNodes: [],
      status: create(read.TaskStatusSchema, {
        kind: read.TaskStatusKind.BACKLOG,
        nativeState: read.TaskNativeState.ACTIVE,
      }),
    }),
  };
}

export function taskStartApplied(nodeIDs: readonly string[] = ["node-1"]) {
  return create(lifecycle.StartResultSchema, {
    outcome: {
      case: "success",
      value: {
        outcome: { case: "applied", value: { currentNodes: nodeIDs.map((nodeId) => ({ nodeId })) } },
      },
    },
  });
}

export const taskStartFailure = create(lifecycle.StartErrorSchema, {
  code: "internal_failure",
  detail: { case: "internalFailure", value: { cause: "preparation failed" } },
});

export function taskStartNeedsDependencies(count: number) {
  return create(lifecycle.StartResultSchema, {
    outcome: {
      case: "success",
      value: {
        outcome: { case: "dependencyConfirmationRequired", value: { unsatisfiedDependencyCount: count } },
      },
    },
  });
}

export function taskStartRoute(
  result: () => lifecycle.StartResult | Promise<lifecycle.StartResult>,
): FakeRoute {
  return { descriptor: lifecycle.TaskLifecycleService.method.start, resultFactory: result };
}

export function taskResumeNeedsTarget() {
  return create(lifecycle.ResumeResultSchema, {
    outcome: {
      case: "success",
      value: {
        outcome: {
          case: "selectionRequired",
          value: { reason: { case: "policyRequiresSelection", value: {} } },
        },
      },
    },
  });
}

export function taskResumeApplied(nodeIDs: readonly string[]) {
  return create(lifecycle.ResumeResultSchema, {
    outcome: {
      case: "success",
      value: {
        outcome: { case: "applied", value: { currentNodes: nodeIDs.map((nodeId) => ({ nodeId })) } },
      },
    },
  });
}

export function taskResumeRoute(
  result: () => lifecycle.ResumeResult | Promise<lifecycle.ResumeResult>,
): FakeRoute {
  return { descriptor: lifecycle.TaskLifecycleService.method.resume, resultFactory: result };
}

export function taskDeleted() {
  return create(lifecycle.DeleteResultSchema, { outcome: { case: "success", value: {} } });
}

export function taskAlreadyDeleted(taskID: string) {
  return create(lifecycle.DeleteResultSchema, {
    outcome: {
      case: "error",
      value: {
        code: "task_not_found",
        detail: { case: "taskNotFound", value: { taskId: taskID } },
      },
    },
  });
}

export function taskDeleteRoute(
  result: () => lifecycle.DeleteResult | Promise<lifecycle.DeleteResult> = taskDeleted,
): FakeRoute {
  return { descriptor: lifecycle.TaskLifecycleService.method.delete, resultFactory: result };
}

type DependencyPair = Readonly<{
  blockerTaskID: string;
  blockerShortID: string;
  blockedTaskID: string;
  blockedShortID: string;
}>;

export function taskDependencyAddedRoute(pair: DependencyPair): FakeRoute {
  return {
    descriptor: lifecycle.TaskDependencyService.method.add,
    result: create(lifecycle.DependencyAddResultSchema, {
      outcome: {
        case: "success",
        value: {
          outcome: lifecycle.DependencyMutationOutcome.ADDED,
          blockerTaskId: pair.blockerTaskID,
          blockerShortId: pair.blockerShortID,
          blockedTaskId: pair.blockedTaskID,
          blockedShortId: pair.blockedShortID,
        },
      },
    }),
  };
}

export function taskDependencyRemovedRoute(pair: DependencyPair): FakeRoute {
  return {
    descriptor: lifecycle.TaskDependencyService.method.remove,
    result: create(lifecycle.DependencyRemoveResultSchema, {
      outcome: {
        case: "success",
        value: {
          outcome: lifecycle.DependencyMutationOutcome.REMOVED,
          blockerTaskId: pair.blockerTaskID,
          blockerShortId: pair.blockerShortID,
          blockedTaskId: pair.blockedTaskID,
          blockedShortId: pair.blockedShortID,
        },
      },
    }),
  };
}

export function taskDependencyRemovalFailure(error: Error): FakeRoute {
  return { descriptor: lifecycle.TaskDependencyService.method.remove, error };
}
