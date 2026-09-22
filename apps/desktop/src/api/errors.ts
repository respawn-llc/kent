import { z } from "zod";
import type { Message } from "@app/server-api-contract";

import type { JsonValue } from "./json";
import { rpcErrorCodes } from "./rpcErrorCodes";
import type { ExecutionDetail } from "./taskExecutionFailure";
import { taskInitialBranchReason, taskExecutionResolutionCode } from "./workflowProtoValues";

export type RpcErrorInfo = Readonly<{
  code: number;
  message: string;
  method: string;
  data?: JsonValue | Message | undefined;
}>;

export class RpcError extends Error {
  readonly code: number;
  readonly method: string;
  readonly data: JsonValue | Message | undefined;

  constructor(info: RpcErrorInfo) {
    super(info.message);
    this.name = "RpcError";
    this.code = info.code;
    this.method = info.method;
    this.data = info.data;
  }
}

export class TaskExecutionError extends RpcError {
  constructor(rpcError: RpcError, readonly detail: ExecutionDetail) {
    super(rpcError);
    this.name = "TaskExecutionError";
  }
}

export type ExecutionTargetChoiceFailure =
  | Readonly<{ kind: "branch"; reason: ReturnType<typeof taskInitialBranchReason.decode>; value: string }>
  | Readonly<{ kind: "revision"; reason: ReturnType<typeof taskExecutionResolutionCode.decode>; value: string }>;

export function executionTargetChoiceFailure(error: unknown): ExecutionTargetChoiceFailure | null {
  if (!(error instanceof TaskExecutionError)) return null;
  switch (error.detail.case) {
    case "initialBranch":
      return { kind: "branch", reason: taskInitialBranchReason.decode(error.detail.value.reason), value: error.detail.value.branchName };
    case "executionTargetResolution":
      return { kind: "revision", reason: taskExecutionResolutionCode.decode(error.detail.value.code), value: error.detail.value.requestedRef };
    default:
      return null;
  }
}

export function isTaskMissingError(error: unknown): boolean {
  return error instanceof RpcError && error.code === rpcErrorCodes.workflowTaskNotFound;
}

export function isTaskContextSelectionRequiredError(error: unknown): boolean {
  return error instanceof TaskExecutionError && error.detail.case === "contextSelectionRequired";
}

export function isProjectMissingError(error: unknown): boolean {
  return (
    (error instanceof RpcError && error.code === rpcErrorCodes.projectNotFound) ||
    (error instanceof WorkflowLabelError && error.reason === "project_not_found")
  );
}

export const workflowTaskCreateSelectionErrorReasons = [
  "no_linked_workflows",
  "workflow_not_linked",
  "ambiguous_without_default",
] as const;
export type WorkflowTaskCreateSelectionErrorReason = (typeof workflowTaskCreateSelectionErrorReasons)[number];

export class WorkflowTaskCreateSelectionError extends RpcError {
  readonly reason: WorkflowTaskCreateSelectionErrorReason;
  readonly projectID: string;
  readonly workflowID: string | null;

  constructor(
    rpcError: RpcError,
    info: Readonly<{
      reason: WorkflowTaskCreateSelectionErrorReason;
      projectID: string;
      workflowID: string | null;
    }>,
  ) {
    super({
      code: rpcError.code,
      message: rpcError.message,
      method: rpcError.method,
      data: rpcError.data,
    });
    this.name = "WorkflowTaskCreateSelectionError";
    this.reason = info.reason;
    this.projectID = info.projectID;
    this.workflowID = info.workflowID;
  }
}

export type TaskSearchErrorReason = "normalized_too_short";

export class TaskSearchError extends RpcError {
  readonly reason: TaskSearchErrorReason;

  constructor(rpcError: RpcError, reason: TaskSearchErrorReason) {
    super({
      code: rpcError.code,
      message: rpcError.message,
      method: rpcError.method,
      data: rpcError.data,
    });
    this.name = "TaskSearchError";
    this.reason = reason;
  }
}

export const workflowLabelErrorReasons = [
  "invalid_name",
  "name_conflict",
  "catalog_limit",
  "project_not_found",
  "label_not_found",
  "task_not_found",
  "wrong_project",
  "invalid_filter",
  "invalid_mutation",
] as const;
export type WorkflowLabelErrorReason = (typeof workflowLabelErrorReasons)[number];

export class WorkflowLabelError extends RpcError {
  readonly reason: WorkflowLabelErrorReason;
  readonly projectID: string | null;
  readonly taskID: string | null;
  readonly labelID: string | null;
  readonly field: string | null;
  readonly limit: number | null;

  constructor(
    rpcError: RpcError,
    info: Readonly<{
      reason: WorkflowLabelErrorReason;
      projectID: string | null;
      taskID: string | null;
      labelID: string | null;
      field: string | null;
      limit: number | null;
    }>,
  ) {
    super({
      code: rpcError.code,
      message: rpcError.message,
      method: rpcError.method,
      data: rpcError.data,
    });
    this.name = "WorkflowLabelError";
    this.reason = info.reason;
    this.projectID = info.projectID;
    this.taskID = info.taskID;
    this.labelID = info.labelID;
    this.field = info.field;
    this.limit = info.limit;
  }
}

export const workflowTaskDependencyErrorReasons = [
  "missing_task",
  "self_dependency",
  "project_mismatch",
  "reciprocal_dependency",
  "blocker_limit",
  "blocked_limit",
] as const;
export type WorkflowTaskDependencyErrorReason = (typeof workflowTaskDependencyErrorReasons)[number];

export class WorkflowTaskDependencyError extends RpcError {
  readonly reason: WorkflowTaskDependencyErrorReason;
  readonly blockerTaskID: string;
  readonly blockedTaskID: string;
  readonly missingTaskID: string | null;
  readonly currentCount: number | null;
  readonly limit: number | null;

  constructor(
    rpcError: RpcError,
    info: Readonly<{
      reason: WorkflowTaskDependencyErrorReason;
      blockerTaskID: string;
      blockedTaskID: string;
      missingTaskID: string | null;
      currentCount: number | null;
      limit: number | null;
    }>,
  ) {
    super({
      code: rpcError.code,
      message: rpcError.message,
      method: rpcError.method,
      data: rpcError.data,
    });
    this.name = "WorkflowTaskDependencyError";
    this.reason = info.reason;
    this.blockerTaskID = info.blockerTaskID;
    this.blockedTaskID = info.blockedTaskID;
    this.missingTaskID = info.missingTaskID;
    this.currentCount = info.currentCount;
    this.limit = info.limit;
  }
}

export class TransportError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "TransportError";
  }
}

export type ContractIssueDiagnostic = Readonly<{
  code: string;
  path: readonly string[];
}>;

export class ContractError extends Error {
  readonly diagnostics: readonly ContractIssueDiagnostic[];
  readonly totalDiagnosticCount: number;

  constructor(
    message: string,
    diagnostics: readonly ContractIssueDiagnostic[] = [],
    totalDiagnosticCount = diagnostics.length,
  ) {
    const retainedDiagnostics = diagnostics.slice(0, 8);
    const completeDiagnosticCount = Math.max(totalDiagnosticCount, diagnostics.length);
    super(contractErrorMessage(message, retainedDiagnostics, completeDiagnosticCount));
    this.name = "ContractError";
    this.diagnostics = retainedDiagnostics;
    this.totalDiagnosticCount = completeDiagnosticCount;
  }
}

export type CatalogContractErrorReason =
  "malformed_response" | "project_mismatch" | "session_category_mismatch";

export class CatalogContractError extends ContractError {
  readonly reason: CatalogContractErrorReason;
  readonly method: string | null;
  readonly expectedProjectID: string | null;
  readonly actualProjectID: string | null;
  readonly expectedCategory: "main" | "subagent" | null;
  readonly actualCategory: "main" | "subagent" | null;

  private constructor(
    message: string,
    reason: CatalogContractErrorReason,
    facts: Readonly<{
      method?: string;
      expectedProjectID?: string;
      actualProjectID?: string;
      expectedCategory?: "main" | "subagent";
      actualCategory?: "main" | "subagent";
      diagnostics?: readonly ContractIssueDiagnostic[];
      totalDiagnosticCount?: number;
    }>,
  ) {
    super(message, facts.diagnostics, facts.totalDiagnosticCount);
    this.name = "CatalogContractError";
    this.reason = reason;
    this.method = facts.method ?? null;
    this.expectedProjectID = facts.expectedProjectID ?? null;
    this.actualProjectID = facts.actualProjectID ?? null;
    this.expectedCategory = facts.expectedCategory ?? null;
    this.actualCategory = facts.actualCategory ?? null;
  }

  static malformedResponse(method: string, error: ContractError): CatalogContractError {
    return new CatalogContractError(
      `${method} response did not match the catalog contract.`,
      "malformed_response",
      {
        method,
        diagnostics: error.diagnostics,
        totalDiagnosticCount: error.totalDiagnosticCount,
      },
    );
  }

  static projectMismatch(
    method: string,
    expectedProjectID: string,
    actualProjectID: string,
  ): CatalogContractError {
    return new CatalogContractError(
      `${method} response Project identity did not match the request.`,
      "project_mismatch",
      {
        method,
        expectedProjectID,
        actualProjectID,
      },
    );
  }

  static sessionCategoryMismatch(
    method: string,
    expectedCategory: "main" | "subagent",
    actualCategory: "main" | "subagent",
  ): CatalogContractError {
    return new CatalogContractError(
      `${method} response category did not match the request.`,
      "session_category_mismatch",
      {
        method,
        expectedCategory,
        actualCategory,
      },
    );
  }
}

export class ProtocolMismatchError extends Error {
  constructor(
    readonly requiredProtocolVersion: string,
    readonly clientProtocolVersion: string,
  ) {
    super(
      `Kent server requires protocol version ${requiredProtocolVersion}, but this client uses ${clientProtocolVersion}. Update the Kent client and server to the same build.`,
    );
    this.name = "ProtocolMismatchError";
  }
}

export class StartupConfigurationError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "StartupConfigurationError";
  }
}

export class ServerRootMismatchError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ServerRootMismatchError";
  }
}

export function errorMessage(error: unknown): string {
  const stringError = z.string().safeParse(error);
  if (stringError.success) {
    return normalizeMessage(stringError.data);
  }
  if (error instanceof Error) {
    return normalizeMessage(error.message);
  }
  const messageObject = z.object({ message: z.string() }).safeParse(error);
  if (messageObject.success) {
    return normalizeMessage(messageObject.data.message);
  }
  if (error !== null && Object(error) === error) {
    try {
      return normalizeMessage(JSON.stringify(error));
    } catch {
      return "Unknown error";
    }
  }
  return "Unknown error";
}

function normalizeMessage(message: string): string {
  const trimmed = message.trim();
  return trimmed.length > 0 ? trimmed : "Unknown error";
}

function contractErrorMessage(
  message: string,
  diagnostics: readonly ContractIssueDiagnostic[],
  totalDiagnosticCount: number,
): string {
  if (diagnostics.length === 0) {
    return message;
  }
  const retained = diagnostics
    .map((diagnostic) => `${diagnostic.path.join(".") || "<root>"} (${diagnostic.code})`)
    .join(", ");
  const omittedCount = totalDiagnosticCount - diagnostics.length;
  const omitted = omittedCount > 0 ? `, +${omittedCount.toString()} more` : "";
  return `${message} ${retained}${omitted}`;
}
