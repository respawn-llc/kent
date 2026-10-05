import { create } from "@app/server-api-contract";
import * as pb from "@app/server-api-contract/gen/kent/api/workflow_task/read_pb";
import { AttentionCurrentNodeSchema } from "@app/server-api-contract/gen/kent/api/workflow_task/attention_pb";
import { ExecutionTargetMode } from "@app/server-api-contract/gen/kent/api/workflow_definition/workflow_definition_pb";
import { taskDetailResponse } from "@/test-support/task-detail";
import { FakeRpcTransport, unexpectedProjectOverflow } from "@/test-support/api";
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
        transitionBranchKey: null,
        sessionID: "33333333-3333-4333-8333-333333333333",
      },
    ]);
    expect(detail.liveSessions).toEqual([
      {
        sessionID: "33333333-3333-4333-8333-333333333333",
        sessionName: "Review chat",
        nodeDisplayName: "Code Review",
      },
      {
        sessionID: "44444444-4444-4444-8444-444444444444",
        sessionName: null,
        nodeDisplayName: "Implementation",
      },
    ]);
    expect(detail.currentScripts).toEqual([]);
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
      create(AttentionCurrentNodeSchema, { nodeId: `node-${id}`, sessionId: `session-${id}` }),
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
        transitionBranchKey: null,
        sessionID: node.sessionId,
      })),
    );
    expect(detail.currentScripts).toEqual(
      currentScripts.map((script) => ({
        currentNode: { nodeID: script.currentNode?.nodeId, transitionBranchKey: null, sessionID: null },
        path: script.path,
      })),
    );
  });

  it("rejects a Current Script with a Session", async () => {
    await expect(
      getTask(
        create(pb.TaskDetailSchema, {
          ...taskDetailResponse.task,
          currentScripts: [
            create(pb.CurrentScriptSchema, {
              currentNode: { nodeId: "node-script", sessionId: "session-1" },
              path: "scripts/run",
            }),
          ],
        }),
      ),
    ).rejects.toThrow();
  });
});
