import { create } from "@app/server-api-contract";
import { QuestionService } from "@app/server-api-contract/gen/kent/api/prompt/prompt_pb";
import { TaskReadService } from "@app/server-api-contract/gen/kent/api/workflow_task/read_pb";
import { pendingQuestion } from "./promptPresentation";
import { requireUnarySuccess } from "./protobufRpc";
import { taskDetail } from "./clientTaskProjection";
import { parseRpcResponse } from "./clientParse";
import { requireTaskBoundItems } from "./clientParse";
import type { ActivityPage, CommentPage, PendingAsk, TaskAttention, TaskComment, TaskDetail } from "./models";
import {
  activityPageSchema,
  commentAddResponseSchema,
  commentPageSchema,
  taskAttentionSchema,
} from "./schemas/workflowBoard";
import type { DescriptorRpcTransport, RpcTransport, SessionAttachmentTarget } from "./transport";

export async function listTaskAttention(transport: RpcTransport, taskID: string): Promise<TaskAttention> {
  const response = parseRpcResponse(
    "workflow.task.attention.list",
    taskAttentionSchema,
    await transport.call("workflow.task.attention.list", { task_id: taskID }),
  );
  requireTaskBoundItems(taskID, response.items);
  return response;
}

export async function getTask(transport: DescriptorRpcTransport, taskID: string): Promise<TaskDetail> {
  const method = TaskReadService.method.get;
  const result = await transport.callDescriptor(method, create(method.input, { taskId: taskID }));
  return taskDetail(requireUnarySuccess(method, result).task);
}

export async function listTaskActivity(
  transport: RpcTransport,
  taskID: string,
  offset: number,
): Promise<ActivityPage> {
  const response = parseRpcResponse(
    "workflow.task.activity.list",
    activityPageSchema,
    await transport.call("workflow.task.activity.list", {
      task_id: taskID,
      offset,
      limit: 50,
    }),
  );
  requireTaskBoundItems(taskID, response.items);
  return response;
}

export async function listTaskComments(
  transport: RpcTransport,
  taskID: string,
  offset: number,
): Promise<CommentPage> {
  return parseRpcResponse(
    "workflow.task.comment.list",
    commentPageSchema,
    await transport.call("workflow.task.comment.list", {
      task_id: taskID,
      offset,
      limit: 50,
    }),
  );
}

export async function addComment(
  transport: RpcTransport,
  taskID: string,
  body: string,
  author: string,
): Promise<TaskComment> {
  return parseRpcResponse(
    "workflow.task.comment.add",
    commentAddResponseSchema,
    await transport.call("workflow.task.comment.add", {
      task_id: taskID,
      body,
      author,
    }),
  ).comment;
}

export async function listPendingAsks(
  transport: DescriptorRpcTransport,
  target: SessionAttachmentTarget,
): Promise<readonly PendingAsk[]> {
  const method = QuestionService.method.listPending;
  const result = requireUnarySuccess(
    method,
    await transport.callDescriptorAttachedSession(
      target,
      method,
      create(method.input, { sessionId: target.sessionID }),
    ),
  );
  return result.questions.map(pendingQuestion);
}
