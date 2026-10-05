import { create } from "@app/server-api-contract";
import * as read from "@app/server-api-contract/gen/kent/api/workflow_task/read_pb";
import * as lifecycle from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";
import { taskDetailResponse } from "@/test-support/task-detail";
import { ApiClient } from "./client";
import { FakeRpcTransport, unexpectedProjectOverflow } from "@/test-support/api";

const item = create(read.DependencyItemSchema, {
  taskId: "task-2",
  shortId: "KENT-2",
  title: "Prepare release",
  workflowId: "11111111-1111-4111-8111-111111111111",
  status: { kind: read.TaskStatusKind.BACKLOG, nativeState: read.TaskNativeState.ACTIVE },
  satisfaction: read.DependencySatisfaction.UNSATISFIED,
});

function detailClient(dependencies: read.TaskDependencies) {
  return new ApiClient(
    new FakeRpcTransport([
      {
        descriptor: read.TaskReadService.method.get,
        result: create(read.GetResultSchema, {
          outcome: {
            case: "success",
            value: {
              task: create(read.TaskDetailSchema, { ...taskDetailResponse.task, dependencies }),
            },
          },
        }),
      },
    ]),
    unexpectedProjectOverflow,
  );
}

describe("task dependency client contract", () => {
  it("rejects outcomes belonging to the opposite dependency mutation", async () => {
    for (const add of [true, false]) {
      const value = create(lifecycle.DependencyMutationSuccessSchema, {
        outcome: add
          ? lifecycle.DependencyMutationOutcome.REMOVED
          : lifecycle.DependencyMutationOutcome.ADDED,
        blockerTaskId: "task-1",
        blockerShortId: "KENT-1",
        blockedTaskId: "task-2",
        blockedShortId: "KENT-2",
      });
      const transport = new FakeRpcTransport(
        add
          ? [
              {
                descriptor: lifecycle.TaskDependencyService.method.add,
                result: create(lifecycle.DependencyAddResultSchema, { outcome: { case: "success", value } }),
              },
            ]
          : [
              {
                descriptor: lifecycle.TaskDependencyService.method.remove,
                result: create(lifecycle.DependencyRemoveResultSchema, {
                  outcome: { case: "success", value },
                }),
              },
            ],
      );
      const client = new ApiClient(transport, unexpectedProjectOverflow);
      await expect(
        add ? client.addTaskDependency("task-1", "task-2") : client.removeTaskDependency("task-1", "task-2"),
      ).rejects.toThrow();
    }
  });

  it("projects detail availability and server-owned satisfaction", async () => {
    const client = detailClient(
      create(read.TaskDependenciesSchema, {
        blockerCount: 1,
        unsatisfiedBlockerCount: 1,
        directions: [
          {
            direction: read.DependencyDirection.BLOCKED_BY,
            totalCount: 1,
            unsatisfiedCount: 1,
            items: [item],
            addAvailability: { availability: { case: "available", value: { remainingCapacity: 49 } } },
          },
          {
            direction: read.DependencyDirection.BLOCKS,
            addAvailability: { availability: { case: "limitReached", value: {} } },
          },
        ],
      }),
    );
    await expect(client.getTask("task-1")).resolves.toMatchObject({
      dependencies: {
        blockerCount: 1,
        unsatisfiedBlockerCount: 1,
        directlyBlockedTaskCount: 0,
        directions: [
          {
            direction: "blocked-by",
            items: [{ satisfaction: "unsatisfied" }],
            addAvailability: { kind: "available", remainingCapacity: 49 },
          },
          { direction: "blocks", addAvailability: { kind: "limit_reached" } },
        ],
      },
    });
  });

  it("rejects inconsistent counts and missing availability", async () => {
    for (const directions of [
      [
        create(read.DependencyDirectionProjectionSchema, {
          direction: read.DependencyDirection.BLOCKED_BY,
          unsatisfiedCount: 1,
          addAvailability: { availability: { case: "available", value: { remainingCapacity: 1 } } },
        }),
        create(read.DependencyDirectionProjectionSchema, {
          direction: read.DependencyDirection.BLOCKS,
          addAvailability: { availability: { case: "available", value: { remainingCapacity: 1 } } },
        }),
      ],
      [
        create(read.DependencyDirectionProjectionSchema, {
          direction: read.DependencyDirection.BLOCKED_BY,
          unsatisfiedCount: 0,
        }),
        create(read.DependencyDirectionProjectionSchema, { direction: read.DependencyDirection.BLOCKS }),
      ],
    ]) {
      await expect(
        detailClient(create(read.TaskDependenciesSchema, { directions })).getTask("task-1"),
      ).rejects.toThrow();
    }
  });

  it("sends relationship identities and keeps list output free of mutation availability", async () => {
    const mutation = create(lifecycle.DependencyMutationSuccessSchema, {
      outcome: lifecycle.DependencyMutationOutcome.ADDED,
      blockerTaskId: "task-1",
      blockerShortId: "KENT-1",
      blockedTaskId: "task-2",
      blockedShortId: "KENT-2",
    });
    const transport = new FakeRpcTransport([
      {
        descriptor: lifecycle.TaskDependencyService.method.add,
        result: create(lifecycle.DependencyAddResultSchema, {
          outcome: { case: "success", value: mutation },
        }),
      },
      {
        descriptor: lifecycle.TaskDependencyService.method.remove,
        result: create(lifecycle.DependencyRemoveResultSchema, {
          outcome: {
            case: "success",
            value: {
              ...mutation,
              outcome: lifecycle.DependencyMutationOutcome.REMOVED,
            },
          },
        }),
      },
      {
        descriptor: lifecycle.TaskDependencyService.method.list,
        result: create(lifecycle.DependencyListResultSchema, {
          outcome: {
            case: "success",
            value: {
              taskId: "task-2",
              shortId: "KENT-2",
              directions: [
                {
                  direction: read.DependencyDirection.BLOCKED_BY,
                  totalCount: 1,
                  unsatisfiedCount: 1,
                  items: [item],
                },
              ],
            },
          },
        }),
      },
    ]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);
    await expect(client.addTaskDependency("task-1", "task-2")).resolves.toMatchObject({
      outcome: "added",
      blockerTaskID: "task-1",
      blockedTaskID: "task-2",
    });
    await expect(client.removeTaskDependency("task-1", "task-2")).resolves.toMatchObject({
      outcome: "removed",
    });
    await expect(client.listTaskDependencies("task-2", "blocked-by")).resolves.toMatchObject({
      taskID: "task-2",
      shortID: "KENT-2",
      directions: [
        {
          direction: "blocked-by",
          totalCount: 1,
          unsatisfiedCount: 1,
          items: [{ satisfaction: "unsatisfied" }],
        },
      ],
    });
    expect(transport.descriptorCalls.map(({ request }) => request)).toEqual([
      create(lifecycle.DependencyAddRequestSchema, { blockerTaskId: "task-1", blockedTaskId: "task-2" }),
      create(lifecycle.DependencyRemoveRequestSchema, { blockerTaskId: "task-1", blockedTaskId: "task-2" }),
      create(lifecycle.DependencyListRequestSchema, {
        taskId: "task-2",
        direction: read.DependencyDirection.BLOCKED_BY,
      }),
    ]);
  });
});
