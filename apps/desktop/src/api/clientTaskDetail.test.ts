import { unexpectedProjectOverflow } from "@/test-support/api";
import { ContractError } from "./errors";
import { ApiClient } from "./client";
import { FakeRpcTransport } from "@/test-support/api";
import { create } from "@app/server-api-contract";
import * as lifecycle from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";

describe("ApiClient Task Activity pagination", () => {
  it("uses offset pagination and rejects cursor-era responses", async () => {
    const transport = new FakeRpcTransport([
      {
        method: "workflow.task.activity.list",
        result: {
          items: [
            {
              activity_id: "activity-1",
              type: "session_started",
              task_id: "task-1",
              occurred_at_unix_ms: 2,
              updated_at_unix_ms: 2,
              session_started: { session_id: "session-1", name: "Implementation" },
            },
          ],
          next_offset: 50,
        },
      },
    ]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);

    await expect(client.listTaskActivity("task-1", 0)).resolves.toMatchObject({
      items: [{ id: "activity-1", sessionID: "session-1" }],
      nextOffset: 50,
    });
    expect(transport.calls).toContainEqual({
      method: "workflow.task.activity.list",
      params: { task_id: "task-1", offset: 0, limit: 50 },
    });

    const malformedClient = new ApiClient(
      new FakeRpcTransport([
        {
          method: "workflow.task.activity.list",
          result: { items: [], next_page_token: "legacy", generated_at_unix_ms: 1 },
        },
      ]),
      unexpectedProjectOverflow,
    );
    await expect(malformedClient.listTaskActivity("task-1", 0)).rejects.toBeInstanceOf(ContractError);

    const mismatchedClient = new ApiClient(
      new FakeRpcTransport([
        {
          method: "workflow.task.activity.list",
          result: {
            items: [
              {
                activity_id: "activity-1",
                type: "session_started",
                task_id: "task-other",
                occurred_at_unix_ms: 2,
                updated_at_unix_ms: 2,
                session_started: { session_id: "session-1", name: "Implementation" },
              },
            ],
          },
        },
      ]),
      unexpectedProjectOverflow,
    );
    await expect(mismatchedClient.listTaskActivity("task-1", 0)).rejects.toBeInstanceOf(ContractError);
  });
});

describe("ApiClient Task Comment pagination", () => {
  it("uses the paginated Task Comment RPC contract", async () => {
    const transport = new FakeRpcTransport([
      {
        descriptor: lifecycle.TaskCommentService.method.list,
        result: create(lifecycle.CommentListResultSchema, { outcome: { case: "success", value: {
          items: [
            {
              id: "comment-1",
              taskId: "task-1",
              body: "Existing comment",
              author: lifecycle.CommentAuthorKind.USER,
              authorId: "Nek-12",
              createdAt: { seconds: 0n, nanos: 1_000_000 },
              updatedAt: { seconds: 0n, nanos: 2_000_000 },
            },
          ],
          nextOffset: 40,
          totalCount: 41n,
        } } }),
      },
    ]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);

    await expect(client.listTaskComments("task-1", 0)).resolves.toMatchObject({
      items: [{ id: "comment-1", body: "Existing comment", authorKind: "user", authorID: "Nek-12" }],
      nextOffset: 40,
      totalCount: 41,
    });
    expect(transport.descriptorCalls).toContainEqual({
      descriptor: lifecycle.TaskCommentService.method.list,
      request: create(lifecycle.TaskOffsetPageRequestSchema, { taskId: "task-1", offset: 0, limit: 50 }),
    });
  });

  it("rejects zero continuation offsets before feature code receives a page", async () => {
    const client = new ApiClient(
      new FakeRpcTransport([{
        descriptor: lifecycle.TaskCommentService.method.list,
        result: create(lifecycle.CommentListResultSchema, { outcome: { case: "success", value: { nextOffset: 0 } } }),
      }]),
      unexpectedProjectOverflow,
    );

    await expect(client.listTaskComments("task-1", 0)).rejects.toThrow();
  });
});
