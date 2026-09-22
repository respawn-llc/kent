import { z } from "zod";
import { decodeJson } from "@app/server-api-contract";
import { RegisteredFactsSchema } from "@app/server-api-contract/gen/kent/api/worktree/worktree_pb";
import { taskRegisteredWorktree } from "../taskWorktreeProjection";
export type { RetainedPreviousWorktree } from "../taskWorktreeProjection";

const workflowRegisteredWorktreeSchema = z
  .object({ variant: z.literal("registered"), registered: z.json() })
  .strict()
  .transform((value, context) => {
    try {
      return taskRegisteredWorktree(decodeJson(RegisteredFactsSchema, value.registered));
    } catch {
      context.addIssue({ code: "custom", message: "Invalid registered Worktree." });
      return z.NEVER;
    }
  });

export const retainedPreviousWorktreeSchema = z
  .object({ worktree: workflowRegisteredWorktreeSchema })
  .strict();
