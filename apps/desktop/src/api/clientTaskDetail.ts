import { create } from "@app/server-api-contract";
import { QuestionService } from "@app/server-api-contract/gen/kent/api/prompt/prompt_pb";
import { TaskReadService } from "@app/server-api-contract/gen/kent/api/workflow_task/read_pb";
import { TaskCommentService } from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";
import { pendingQuestion } from "./promptPresentation";
import { requireUnarySuccess } from "./protobufRpc";
import { taskDetail, taskComment } from "./clientTaskProjection";
import { taskCommentAuthor } from "./workflowProtoValues";
import { parseRpcResponse } from "./clientParse";
import { requireTaskBoundItems } from "./clientParse";
import type { ActivityPage, CommentPage, PendingAsk, TaskAttention, TaskComment, TaskDetail } from "./models";
import {
  activityPageSchema,
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
  transport: DescriptorRpcTransport,
  taskID: string,
  offset: number,
): Promise<CommentPage> {
  const method = TaskCommentService.method.list;
  const result = await transport.callDescriptor(method, create(method.input, { taskId: taskID, offset, limit: 50 }));
  const response = requireUnarySuccess(method, result);
  return { items: response.items.map(taskComment), nextOffset: response.nextOffset ?? null, totalCount: Number(response.totalCount) };
}

export async function addComment(
  transport: DescriptorRpcTransport,
  taskID: string,
  body: string,
  author: string,
): Promise<TaskComment> {
  const method = TaskCommentService.method.add;
  const result = await transport.callDescriptor(method, create(method.input, {
    taskId: taskID, body, author: taskCommentAuthor.encode(author),
  }));
  return taskComment(requireUnarySuccess(method, result).comment);
}

export async function replaceComment(transport: DescriptorRpcTransport, commentID: string, body: string): Promise<void> {
  const method = TaskCommentService.method.replace;
  const result = await transport.callDescriptor(method, create(method.input, { commentId: commentID, body }));
  requireUnarySuccess(method, result);
}

export async function deleteComment(transport: DescriptorRpcTransport, commentID: string): Promise<void> {
  const method = TaskCommentService.method.delete;
  const result = await transport.callDescriptor(method, create(method.input, { commentId: commentID }));
  requireUnarySuccess(method, result);
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
