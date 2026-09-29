import { classifyResultFailure, create } from "@app/server-api-contract";
import {
  TaskReadService,
  type SearchHit,
  type SearchSource,
} from "@app/server-api-contract/gen/kent/api/workflow_task/read_pb";
import { ContractError, TaskSearchError } from "./errors";
import { protobufRpcError, requireUnarySuccess } from "./protobufRpc";
import { taskStatus } from "./clientTaskProjection";
import { taskSearchMode, taskSearchSourceKind, taskStatusKind } from "./workflowProtoValues";
import type { TaskSearchHit, TaskSearchInput, TaskSearchResponse, TaskSearchSource } from "./taskSearch";
import type { DescriptorRpcTransport } from "./transport";

export async function searchTasks(
  transport: DescriptorRpcTransport,
  input: TaskSearchInput,
  signal?: AbortSignal,
): Promise<TaskSearchResponse> {
  const method = TaskReadService.method.search;
  const result = await transport.callDescriptor(
    method,
    create(method.input, {
      mode: taskSearchMode.encode(input.mode),
      query: input.query,
      context: input.context,
      caseSensitive: input.caseSensitive,
      includeComments: input.includeComments,
      projectIds: [...(input.projectIDs ?? [])],
      statusKinds: (input.statusKinds ?? []).map(taskStatusKind.encode),
      pageSize: input.pageSize,
      offset: input.offset,
    }),
    signal === undefined ? undefined : { signal },
  );
  if (
    result.outcome.case === "error" &&
    classifyResultFailure(method.output, result.outcome.value).kind !== "generic" &&
    result.outcome.value.detail.case === "normalizedTooShort"
  ) {
    throw new TaskSearchError(protobufRpcError(method, result.outcome.value), "normalized_too_short");
  }
  const response = requireUnarySuccess(method, result);
  const mode = taskSearchMode.decode(response.mode);
  if (mode !== input.mode) throw new ContractError("Task search response mode does not match the request.");
  return {
    mode,
    nextOffset: response.nextOffset ?? null,
    groups: response.groups.map((group) => ({
      projectID: group.projectId,
      projectKey: group.projectKey,
      taskID: group.taskId,
      shortID: group.shortId,
      workflowID: group.workflowId,
      title: group.title,
      status: taskStatus(group.status),
      totalHitCount: group.totalHitCount,
      hits: group.hits.map(taskSearchHit),
    })),
  };
}

function taskSearchSource(value: SearchSource | undefined): TaskSearchSource {
  if (value === undefined) throw new ContractError("Task search hit source is required.");
  const kind = taskSearchSourceKind.decode(value.kind);
  if (kind !== "comment") return { kind };
  if (value.commentId === undefined) throw new ContractError("Task search Comment identity is required.");
  return { kind, commentID: value.commentId };
}

function taskSearchHit(value: SearchHit): TaskSearchHit {
  const source = taskSearchSource(value.source);
  switch (value.match.case) {
    case "literal":
      return {
        ordinal: value.ordinal,
        source,
        literal: {
          before: value.match.value.before,
          match: value.match.value.match,
          after: value.match.value.after,
          leftTruncated: value.match.value.leftTruncated,
          rightTruncated: value.match.value.rightTruncated,
        },
      };
    case "fts5":
      return { ordinal: value.ordinal, source, fts5: { snippet: value.match.value.snippet } };
    case undefined:
      throw new ContractError("Task search hit match is required.");
  }
}
