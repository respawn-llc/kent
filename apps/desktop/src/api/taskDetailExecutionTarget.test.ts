import { describe, expect, it } from "bun:test";
import { create, ValidationError } from "@app/server-api-contract";
import * as pb from "@app/server-api-contract/gen/kent/api/workflow_task/read_pb";
import { AttentionCurrentNodeSchema } from "@app/server-api-contract/gen/kent/api/workflow_task/attention_pb";
import { ExecutionTargetMode } from "@app/server-api-contract/gen/kent/api/workflow_definition/workflow_definition_pb";
import { taskDetailResponse } from "@/test-support/task-detail";
import { FakeRpcTransport, unexpectedProjectOverflow } from "@/test-support/api";
import { requireValue } from "@/test-support/chat-runtime";
import { ApiClient } from "./client";

async function getTask(task: pb.TaskDetail) {
  return new ApiClient(
    new FakeRpcTransport([
      {
        descriptor: pb.TaskReadService.method.get,
        result: create(pb.GetResultSchema, { outcome: { case: "success", value: { task } } }),
      },
    ]),
    unexpectedProjectOverflow,
  ).getTask("task-1");
}

describe("task detail execution target contract", () => {
  it.each([0n, -1n])("rejects invalid Session numeric ID %s at the wire boundary", (shortId) => {
    const task = create(pb.TaskDetailSchema, {
      ...taskDetailResponse.task,
      liveSessions: taskDetailResponse.task.liveSessions.map((session) =>
        create(pb.LiveSessionSchema, { ...session, shortId }),
      ),
    });
    expect(getTask(task)).rejects.toThrow(ValidationError);
  });

  it("maps durable target facts, recorded worktree path, and current executions", async () => {
    const detail = await getTask(taskDetailResponse.task);
    expect(detail.executionTarget).toEqual({
      mode: "head",
      requestedRef: "HEAD",
      resolvedRef: "refs/heads/main",
      commitOID: "0123456789abcdef0123456789abcdef01234567",
      provenance: "resolved",
    });
    expect(detail.worktreePath).toBe("/tmp/worktree");
    expect(detail.currentNodes).toEqual([
      {
        effectiveAssignee: null,
        effectiveThinking: null,
        nodeID: "node-1",
        displayName: "Code Review",
        transitionBranchKey: null,
        sessionID: "33333333-3333-4333-8333-333333333333",
      },
    ]);
    expect(detail.liveSessions).toEqual([
      {
        sessionID: "33333333-3333-4333-8333-333333333333",
        displayName: "Review chat",
      },
      {
        sessionID: "44444444-4444-4444-8444-444444444444",
        displayName: "302",
      },
    ]);
    expect(detail.currentScripts).toEqual([]);
  });

  it("uses UUIDs for historical Sessions without another display identity", async () => {
    const task = create(pb.TaskDetailSchema, {
      ...taskDetailResponse.task,
      liveSessions: taskDetailResponse.task.liveSessions.map((session) =>
        create(pb.LiveSessionSchema, { ...session, shortId: undefined }),
      ),
    });
    const detail = await getTask(task);
    expect(detail.liveSessions).toEqual([
      {
        sessionID: "33333333-3333-4333-8333-333333333333",
        displayName: "Review chat",
      },
      {
        sessionID: "44444444-4444-4444-8444-444444444444",
        displayName: "44444444-4444-4444-8444-444444444444",
      },
    ]);
  });

  it.each([
    { sessionName: "Named review", role: "reviewer", expected: "Named review" },
    { sessionName: undefined, role: "reviewer", expected: "reviewer" },
  ])("uses $expected without requiring an unused numeric ID", async ({ sessionName, role, expected }) => {
    const sessionID = "33333333-3333-4333-8333-333333333333";
    const detail = await getTask(
      create(pb.TaskDetailSchema, {
        ...taskDetailResponse.task,
        currentNodes: [
          create(AttentionCurrentNodeSchema, {
            nodeId: "node-1",
            nodeDisplayName: "Review",
            sessionId: sessionID,
            effectiveAssignee: role,
          }),
        ],
        liveSessions: [
          create(pb.LiveSessionSchema, { sessionId: sessionID, sessionName, nodeDisplayName: "Review" }),
        ],
      }),
    );
    expect(detail.liveSessions).toEqual([{ sessionID, displayName: expected }]);
  });

  it("distinguishes unlocked and source-workspace targets", async () => {
    const unlocked = await getTask(
      create(pb.TaskDetailSchema, { ...taskDetailResponse.task, executionTarget: undefined }),
    );
    const sourceTarget = await getTask(
      create(pb.TaskDetailSchema, {
        ...taskDetailResponse.task,
        executionTarget: create(pb.ExecutionTargetSchema, {
          mode: ExecutionTargetMode.WORKFLOW_EXECUTION_TARGET_MODE_NONE,
          provenance: pb.ExecutionTargetProvenance.RESOLVED,
        }),
      }),
    );
    expect(unlocked.executionTarget).toBeNull();
    expect(sourceTarget.executionTarget).toEqual({
      mode: "none",
      requestedRef: null,
      resolvedRef: null,
      commitOID: null,
      provenance: "resolved",
    });
  });

  it("accepts session-backed Current Nodes and sessionless Current Scripts", async () => {
    const executionIDs = Array.from({ length: 201 }, (_, index) => index.toString());
    const currentNodes = executionIDs.map((id) =>
      create(AttentionCurrentNodeSchema, {
        nodeId: `node-${id}`,
        nodeDisplayName: `Review ${id}`,
        sessionId: `00000000-0000-4000-8000-${id.padStart(12, "0")}`,
      }),
    );
    const currentScripts = executionIDs.map((id) =>
      create(pb.CurrentScriptSchema, {
        currentNode: { nodeId: `script-node-${id}` },
        path: "script",
      }),
    );
    const detail = await getTask(
      create(pb.TaskDetailSchema, { ...taskDetailResponse.task, currentNodes, currentScripts }),
    );
    expect(detail.currentNodes).toEqual(
      currentNodes.map((node) => ({
        effectiveAssignee: null,
        effectiveThinking: null,
        nodeID: node.nodeId,
        displayName: requireValue(node.nodeDisplayName),
        transitionBranchKey: null,
        sessionID: node.sessionId ?? null,
      })),
    );
    expect(detail.currentScripts).toEqual(
      currentScripts.map((script) => ({
        currentNode: {
          nodeID: requireValue(script.currentNode?.nodeId),
          transitionBranchKey: null,
          sessionID: null,
        },
        path: script.path,
      })),
    );
  });

  it("rejects a Current Script with a Session", async () => {
    expect(
      getTask(
        create(pb.TaskDetailSchema, {
          ...taskDetailResponse.task,
          currentScripts: [
            create(pb.CurrentScriptSchema, {
              currentNode: {
                nodeId: "node-script",
                sessionId: "11111111-1111-4111-8111-111111111111",
              },
              path: "scripts/run",
            }),
          ],
        }),
      ),
    ).rejects.toThrow();
  });
});
