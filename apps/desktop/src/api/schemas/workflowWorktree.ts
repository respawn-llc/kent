import { z } from "zod";
import { decodeJson } from "@app/server-api-contract";
import {
  SetupRecoveryDisposition as ProtoSetupRecoveryDisposition,
  SetupRetainedDetailsSchema,
  RegisteredFactsSchema,
  type RegisteredFacts,
} from "@app/server-api-contract/gen/kent/api/worktree/worktree_pb";
import { LockedExecutionTargetDetailsSchema } from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";

import { ContractError, RpcError } from "../errors";
import { rpcErrorCodes } from "../rpcErrorCodes";
import { parseSetupOperationID, type SetupOperationID } from "../setupOperationID";
import { nonBlankString } from "./common";
import { workflowExecutionTargetSelectionSchema } from "./workflowExecutionTarget";

const nullableNonBlank = nonBlankString.nullable();
const registeredWorktreeWireSchema = z
  .object({
    variant: z.literal("registered"),
    registered: z.json(),
  })
  .strict();

function projectRegisteredWorktree(facts: RegisteredFacts | undefined) {
  if (facts?.kent === undefined) {
    throw new ContractError("Validated registered Worktree is missing Kent facts.");
  }
  return {
    kent: { canonicalRoot: facts.kent.canonicalRoot, worktreeID: facts.kent.worktreeId },
  };
}

const workflowRegisteredWorktreeSchema = registeredWorktreeWireSchema.transform((value, context) => {
  try {
    return projectRegisteredWorktree(decodeJson(RegisteredFactsSchema, value.registered));
  } catch {
    context.addIssue({ code: "custom", message: "Invalid registered Worktree." });
    return z.NEVER;
  }
});
export type WorkflowRegisteredWorktree = z.output<typeof workflowRegisteredWorktreeSchema>;
export const retainedPreviousWorktreeSchema = z
  .object({ worktree: workflowRegisteredWorktreeSchema })
  .strict();
export type RetainedPreviousWorktree = z.output<typeof retainedPreviousWorktreeSchema>;

const recoveryWorktreeSchema = z
  .object({ worktree_id: nonBlankString, root: nonBlankString })
  .strict()
  .transform((value) => ({ worktreeID: value.worktree_id, root: value.root }));
const setupOperationIDSchema = z.string().transform((value, context): SetupOperationID => {
  try {
    return parseSetupOperationID(value);
  } catch {
    context.addIssue({ code: "custom", message: "Setup operation id must be a UUID v4." });
    return z.NEVER;
  }
});
const taskSetupRecoverySchema = z
  .object({
    setup_operation_id: setupOperationIDSchema,
    cause: z.enum(["process_exit", "timeout", "target_preparation", "operational"]),
    diagnostic: nonBlankString,
    script_path: nullableNonBlank,
    setup_requirement: z.enum(["required", "already_completed"]),
    execution_target: workflowExecutionTargetSelectionSchema,
    retained_worktree: recoveryWorktreeSchema.nullable(),
    retained_previous_worktree: recoveryWorktreeSchema.nullable(),
  })
  .strict()
  .superRefine((value, context) => {
    const scriptFailure = value.cause !== "target_preparation";
    if (scriptFailure && (value.script_path === null || value.retained_worktree === null)) {
      context.addIssue({ code: "custom", message: "Setup failure requires script and Worktree facts." });
    }
    if (!scriptFailure && value.script_path !== null) {
      context.addIssue({ code: "custom", message: "Target preparation cannot include a setup script." });
    }
  })
  .transform((value) => ({
    setupOperationID: value.setup_operation_id,
    cause: value.cause,
    diagnostic: value.diagnostic,
    scriptPath: value.script_path,
    executionTarget: value.execution_target,
    retainedWorktree: value.retained_worktree,
    retainedPreviousWorktree: value.retained_previous_worktree,
  }));
export type TaskSetupRecovery = z.output<typeof taskSetupRecoverySchema> &
  Readonly<{ recoveryDisposition: SetupRecoveryDisposition }>;
const taskSetupRecoveryEnvelopeSchema = z
  .object({
    setup_recovery: taskSetupRecoverySchema.optional(),
    original_execution_target_unavailable: z.json().optional(),
  })
  .loose();

export function parseTaskSetupRecoveryDetail(detailJSON: string | null): TaskSetupRecovery | null {
  if (detailJSON === null) return null;
  let detail: unknown;
  try {
    detail = JSON.parse(detailJSON);
  } catch {
    throw new ContractError("Task setup recovery detail was not valid JSON.");
  }
  const parsed = taskSetupRecoveryEnvelopeSchema.safeParse(detail);
  if (!parsed.success) {
    throw new ContractError(
      "Task setup recovery detail did not match GUI contract.",
      parsed.error.issues.map((issue) => ({ code: issue.code, path: issue.path.map(String) })),
    );
  }
  const recovery = parsed.data.setup_recovery;
  if (recovery === undefined) return null;
  const originalUnavailable = parsed.data.original_execution_target_unavailable;
  if (originalUnavailable !== undefined) {
    decodeJson(LockedExecutionTargetDetailsSchema, originalUnavailable);
  }
  return {
    ...recovery,
    recoveryDisposition: originalUnavailable === undefined ? "retry_existing" : "fresh_replacement",
  };
}

type SetupRecoveryDisposition = "retry_existing" | "fresh_replacement";

export class WorktreeSetupRetainedError extends RpcError {
  readonly recoveryDisposition: SetupRecoveryDisposition;
  readonly worktree: WorkflowRegisteredWorktree;
  readonly scriptPath: string;
  readonly diagnostic: string;
  readonly retainedPreviousWorktree: RetainedPreviousWorktree | null;

  constructor(
    rpcError: RpcError,
    facts: Readonly<{
      recoveryDisposition: SetupRecoveryDisposition;
      worktree: WorkflowRegisteredWorktree;
      scriptPath: string;
      diagnostic: string;
      retainedPreviousWorktree: RetainedPreviousWorktree | null;
    }>,
  ) {
    super(rpcError);
    this.name = "WorktreeSetupRetainedError";
    this.recoveryDisposition = facts.recoveryDisposition;
    this.worktree = facts.worktree;
    this.scriptPath = facts.scriptPath;
    this.diagnostic = facts.diagnostic;
    this.retainedPreviousWorktree = facts.retainedPreviousWorktree;
  }
}
const retainedErrorSchema = z
  .object({
    type: z.literal("worktree_setup_retained"),
    recovery_disposition: z.string(),
    worktree: registeredWorktreeWireSchema,
    script_path: z.string(),
    diagnostic: z.string(),
    retained_previous_worktree: z.object({ worktree: registeredWorktreeWireSchema }).strict().nullable(),
  })
  .strict();

const recoveryDispositions = [
  ["retry_existing", ProtoSetupRecoveryDisposition.RETRY_EXISTING],
  ["fresh_replacement", ProtoSetupRecoveryDisposition.FRESH_REPLACEMENT],
] as const;

export function decodeWorktreeSetupRetainedError(error: unknown): WorktreeSetupRetainedError | null {
  if (!(error instanceof RpcError) || error.code !== rpcErrorCodes.workflowWorktreeSetupRetained) {
    return null;
  }
  const parsed = retainedErrorSchema.safeParse(error.data);
  if (!parsed.success) {
    return null;
  }
  const raw = parsed.data;
  try {
    const details = decodeJson(SetupRetainedDetailsSchema, {
      worktree: raw.worktree.registered,
      script_path: parsed.data.script_path,
      diagnostic: parsed.data.diagnostic,
      recovery_disposition:
        recoveryDispositions.find(([name]) => name === raw.recovery_disposition)?.[1] ??
        ProtoSetupRecoveryDisposition.UNSPECIFIED,
      retained_previous_worktree:
        raw.retained_previous_worktree === null
          ? null
          : {
              worktree: raw.retained_previous_worktree.worktree.registered,
            },
    });
    const disposition = recoveryDispositions.find(([, value]) => value === details.recoveryDisposition)?.[0];
    if (disposition === undefined || details.worktree === undefined) {
      throw new ContractError("Validated setup error is missing required facts.");
    }
    return new WorktreeSetupRetainedError(error, {
      recoveryDisposition: disposition,
      worktree: projectRegisteredWorktree(details.worktree),
      scriptPath: details.scriptPath,
      diagnostic: details.diagnostic,
      retainedPreviousWorktree:
        details.retainedPreviousWorktree === undefined
          ? null
          : {
              worktree: projectRegisteredWorktree(details.retainedPreviousWorktree.worktree),
            },
    });
  } catch {
    return null;
  }
}
