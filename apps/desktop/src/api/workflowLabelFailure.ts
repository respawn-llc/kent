import { classifyResultFailure, type DescMethod } from "@app/server-api-contract";
import type {
  ProjectLabelCatalogResult,
  ProjectLabelCreateResult,
  ProjectLabelDeleteResult,
  ProjectLabelRenameResult,
  ProjectLabelReorderResult,
} from "@app/server-api-contract/gen/kent/api/workflow_definition/workflow_definition_pb";
import type {
  LabelsGetResult,
  ListResult,
  BoardGetResult,
  BoardNodeCardsListResult,
} from "@app/server-api-contract/gen/kent/api/workflow_task/read_pb";
import {
  LabelErrorReason,
  type LabelErrorDetails,
  type LabelsUpdateResult,
  type CreateResult,
} from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";
import { WorkflowLabelError, type WorkflowLabelErrorReason } from "./errors";
import { protobufRpcError } from "./protobufRpc";

type LabelOutcome = (
  | ProjectLabelCatalogResult
  | ProjectLabelCreateResult
  | ProjectLabelDeleteResult
  | ProjectLabelRenameResult
  | ProjectLabelReorderResult
  | LabelsGetResult
  | ListResult
  | BoardGetResult
  | BoardNodeCardsListResult
  | LabelsUpdateResult
  | CreateResult
)["outcome"];
type LabelDetail = Extract<LabelOutcome, { case: "error" }>["value"]["detail"];
type LabelInfo = Readonly<{
  reason: WorkflowLabelErrorReason;
  projectID?: string | undefined;
  taskID?: string | undefined;
  labelID?: string | undefined;
  field?: string | undefined;
  limit?: number | undefined;
}>;

export function throwWorkflowLabelFailure(method: DescMethod, outcome: LabelOutcome): void {
  if (outcome.case !== "error") return;
  const rpcError = protobufRpcError(method, outcome.value);
  if (classifyResultFailure(method.output, outcome.value).kind === "generic") throw rpcError;
  const { detail } = outcome.value;
  const info: LabelInfo | undefined =
    detail.case === "label"
      ? taskLabelInfo(detail.value)
      : detail.case === "taskNotFound"
        ? { reason: "task_not_found" as const, taskID: detail.value.taskId }
        : projectLabelInfo(detail);
  if (info === undefined) return;
  throw new WorkflowLabelError(rpcError, {
    reason: info.reason,
    projectID: info.projectID ?? null,
    taskID: info.taskID ?? null,
    labelID: info.labelID ?? null,
    field: info.field ?? null,
    limit: info.limit ?? null,
  });
}

function projectLabelInfo(
  detail: Exclude<LabelDetail, { case: "label" | "taskNotFound" }>,
): LabelInfo | undefined {
  if (detail.case === "invalidName")
    return { reason: "invalid_name", projectID: detail.value.projectId, field: detail.value.field };
  if (detail.case === "nameConflict") return { reason: "name_conflict", projectID: detail.value.projectId };
  if (detail.case === "catalogLimit")
    return { reason: "catalog_limit", projectID: detail.value.projectId, limit: detail.value.limit };
  if (detail.case === "projectNotFound")
    return { reason: "project_not_found", projectID: detail.value.projectId };
  if (detail.case === "labelNotFound")
    return { reason: "label_not_found", projectID: detail.value.projectId, labelID: detail.value.labelId };
  if (detail.case === "invalidMutation")
    return { reason: "invalid_mutation", projectID: detail.value.projectId, field: detail.value.field };
  if (detail.case === "invalidFilter") return { reason: "invalid_filter", field: detail.value.field };
  if (detail.case === "wrongProject")
    return { reason: "wrong_project", projectID: detail.value.projectId, labelID: detail.value.labelId };
  return undefined;
}

function taskLabelInfo(value: LabelErrorDetails): LabelInfo | undefined {
  const reason = taskLabelReason(value.reason);
  if (reason === undefined) return undefined;
  return {
    reason,
    projectID: value.projectId,
    taskID: value.taskId,
    labelID: value.labelId,
    field: value.field,
    limit: value.limit,
  };
}

function taskLabelReason(reason: LabelErrorReason): WorkflowLabelErrorReason | undefined {
  switch (reason) {
    case LabelErrorReason.INVALID_NAME:
      return "invalid_name";
    case LabelErrorReason.NAME_CONFLICT:
      return "name_conflict";
    case LabelErrorReason.CATALOG_LIMIT:
      return "catalog_limit";
    case LabelErrorReason.PROJECT_NOT_FOUND:
      return "project_not_found";
    case LabelErrorReason.LABEL_NOT_FOUND:
      return "label_not_found";
    case LabelErrorReason.TASK_NOT_FOUND:
      return "task_not_found";
    case LabelErrorReason.WRONG_PROJECT:
      return "wrong_project";
    case LabelErrorReason.INVALID_FILTER:
      return "invalid_filter";
    case LabelErrorReason.INVALID_MUTATION:
      return "invalid_mutation";
    case LabelErrorReason.UNSPECIFIED:
      return undefined;
  }
}
