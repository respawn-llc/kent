import { z } from "zod";
import { decodeJson } from "@app/server-api-contract";
import {
  SetupRecoveryDisposition as ProtoSetupRecoveryDisposition,
  SetupRetainedDetailsSchema,
  RegisteredFactsSchema,
  type RegisteredFacts,
} from "@app/server-api-contract/gen/kent/api/worktree/worktree_pb";

import { ContractError, RpcError } from "../errors";
import { rpcErrorCodes } from "../rpcErrorCodes";

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
