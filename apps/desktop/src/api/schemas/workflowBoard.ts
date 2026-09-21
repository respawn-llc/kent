import { z } from "zod";
import { decodeJson } from "@app/server-api-contract";
import {
  LockedExecutionTargetCause,
  SelectionRequiredSchema,
  type SelectionRequired,
} from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";
import { ExecutionTargetUnavailableCause } from "@app/server-api-contract/gen/kent/api/workflow_task/attention_pb";
import { ExecutionTargetMode } from "@app/server-api-contract/gen/kent/api/workflow_definition/workflow_definition_pb";

import type {
  ActivityPage,
  AttentionPage,
  CommentPage,
  OffsetPage,
  TaskAttention,
  TaskApproveResponse,
  TaskCurrentNode,
  TaskMoveResponse,
  TaskMovePreviewResponse,
  TaskResumeResponse,
  TaskStartResponse,
  WorkflowExecutionTargetSelectionRequirement,
} from "../models";
import {
  attentionItemSchema,
  commentSchema,
  emptyString,
  nonBlankString,
  currentNodeSchema,
  workflowIDSchema,
} from "./common";
import { retainedPreviousWorktreeSchema, type RetainedPreviousWorktree } from "./workflowWorktree";
export {
  taskDependenciesSchema,
  taskDependencyAddResponseSchema,
  taskDependencyListResponseSchema,
  taskDependencyRemoveResponseSchema,
} from "./taskDependencies";
export { decodeWorktreeSetupRetainedError, WorktreeSetupRetainedError } from "./workflowWorktree";

function offsetPageObjectSchema<T>(itemSchema: z.ZodType<T>) {
  return z.object({
    items: z.array(itemSchema),
    next_offset: z.number().int().positive().nullable().optional(),
  });
}

function offsetPageSchema<T>(itemSchema: z.ZodType<T>): z.ZodType<OffsetPage<T>> {
  return offsetPageObjectSchema(itemSchema)
    .strict()
    .transform((value) => ({
      items: value.items,
      nextOffset: value.next_offset ?? null,
    }));
}

const unavailableCauses = [
  ["invalid_revision", ExecutionTargetUnavailableCause.INVALID_REVISION],
  ["non_commit", ExecutionTargetUnavailableCause.NON_COMMIT],
  ["default_branch_missing", ExecutionTargetUnavailableCause.DEFAULT_BRANCH_MISSING],
  ["default_branch_ambiguous", ExecutionTargetUnavailableCause.DEFAULT_BRANCH_AMBIGUOUS],
  ["git_failure", ExecutionTargetUnavailableCause.GIT_FAILURE],
] as const;
const originalTargetCauses = [
  ["detached_head", LockedExecutionTargetCause.DETACHED_HEAD],
  ["invalid_root", LockedExecutionTargetCause.INVALID_ROOT],
  ["root_inaccessible", LockedExecutionTargetCause.ROOT_INACCESSIBLE],
  ["missing_branch", LockedExecutionTargetCause.MISSING_BRANCH],
  ["conflict", LockedExecutionTargetCause.CONFLICT],
  ["git_failure", LockedExecutionTargetCause.GIT_FAILURE],
] as const;
const managedModes = [
  ["head", ExecutionTargetMode.WORKFLOW_EXECUTION_TARGET_MODE_HEAD],
  ["default_branch", ExecutionTargetMode.WORKFLOW_EXECUTION_TARGET_MODE_DEFAULT_BRANCH],
  ["custom_ref", ExecutionTargetMode.WORKFLOW_EXECUTION_TARGET_MODE_CUSTOM_REF],
] as const;

const selectionRequirementEnvelope = z.discriminatedUnion("reason", [
  z
    .object({
      reason: z.literal("original_target_unavailable"),
      original_target_cause: z.string(),
    })
    .strict(),
  z.object({ reason: z.literal("policy_requires_selection") }).strict(),
  z
    .object({
      reason: z.literal("configured_target_unavailable"),
      configured_target: z.object({ mode: z.string(), requested_ref: z.string().optional() }).strict(),
      unavailable_cause: z.string(),
    })
    .strict(),
]);

function adaptSelectionEnvelope(value: z.output<typeof selectionRequirementEnvelope>) {
  switch (value.reason) {
    case "policy_requires_selection":
      return { policy_requires_selection: {} };
    case "original_target_unavailable":
      return {
        original_target_unavailable: {
          cause:
            originalTargetCauses.find(([name]) => name === value.original_target_cause)?.[1] ??
            LockedExecutionTargetCause.UNSPECIFIED,
        },
      };
    case "configured_target_unavailable":
      return {
        configured_target_unavailable: {
          mode:
            value.configured_target.mode === "none"
              ? ExecutionTargetMode.WORKFLOW_EXECUTION_TARGET_MODE_NONE
              : (managedModes.find(([name]) => name === value.configured_target.mode)?.[1] ??
                ExecutionTargetMode.WORKFLOW_EXECUTION_TARGET_MODE_UNSPECIFIED),
          requested_ref: value.configured_target.requested_ref ?? null,
          cause:
            unavailableCauses.find(([name]) => name === value.unavailable_cause)?.[1] ??
            ExecutionTargetUnavailableCause.UNSPECIFIED,
        },
      };
  }
}

const selectionRequirementSchema: z.ZodType<WorkflowExecutionTargetSelectionRequirement> =
  selectionRequirementEnvelope.transform((value, context): WorkflowExecutionTargetSelectionRequirement => {
    try {
      return projectSelection(decodeJson(SelectionRequiredSchema, adaptSelectionEnvelope(value)));
    } catch {
      context.addIssue({ code: "custom", message: "Invalid execution target selection requirement." });
      return z.NEVER;
    }
  });

function projectSelection(decoded: SelectionRequired): WorkflowExecutionTargetSelectionRequirement {
  switch (decoded.reason.case) {
    case "policyRequiresSelection":
      return { reason: "policy_requires_selection" };
    case "originalTargetUnavailable": {
      const facts = decoded.reason.value;
      const cause = originalTargetCauses.find(([, cause]) => cause === facts.cause)?.[0];
      if (cause === undefined) throw new Error("Unknown original target cause");
      return { reason: "original_target_unavailable", originalTargetCause: cause };
    }
    case "configuredTargetUnavailable": {
      const facts = decoded.reason.value;
      const mode = managedModes.find(([, mode]) => mode === facts.mode)?.[0];
      const cause = unavailableCauses.find(([, cause]) => cause === facts.cause)?.[0];
      if (mode === undefined || cause === undefined) throw new Error("Unknown configured target");
      return {
        reason: "configured_target_unavailable",
        configuredTarget: {
          mode,
          requestedRef: facts.requestedRef ?? null,
        },
        unavailableCause: cause,
      };
    }
    case undefined:
      throw new Error("Missing selection reason");
  }
}

const selectionRequiredResponseSchema = z
  .object({
    outcome: z.literal("selection_required"),
    selection_required: selectionRequirementSchema,
  })
  .strict()
  .transform(
    (value) =>
      ({
        outcome: value.outcome,
        selectionRequired: value.selection_required,
      }) as const,
  );

const dependencyConfirmationRequiredResponseSchema = z
  .object({
    outcome: z.literal("dependency_confirmation_required"),
    unsatisfied_dependency_count: z.number().int().positive(),
  })
  .strict()
  .transform(
    (value) =>
      ({
        outcome: value.outcome,
        unsatisfiedDependencyCount: value.unsatisfied_dependency_count,
      }) as const,
  );

const appliedCurrentNodesResponseSchema = z
  .object({
    outcome: z.literal("applied"),
    applied: z
      .object({
        current_nodes: z.array(currentNodeSchema).min(1),
      })
      .strict(),
  })
  .strict()
  .transform(
    (value) =>
      ({
        outcome: value.outcome,
        applied: { currentNodes: value.applied.current_nodes },
      }) as const,
  );

export const taskStartResponseSchema: z.ZodType<TaskStartResponse> = z.discriminatedUnion("outcome", [
  appliedCurrentNodesResponseSchema,
  selectionRequiredResponseSchema,
  dependencyConfirmationRequiredResponseSchema,
]);

export const taskResumeResponseSchema: z.ZodType<TaskResumeResponse> = z.discriminatedUnion("outcome", [
  appliedCurrentNodesResponseSchema,
  z
    .object({
      outcome: z.literal("no_op"),
      no_op: z
        .object({
          current_nodes: z.array(currentNodeSchema).min(1),
        })
        .strict(),
    })
    .strict()
    .transform((value) => ({
      outcome: value.outcome,
      noOp: { currentNodes: value.no_op.current_nodes },
    })),
  selectionRequiredResponseSchema,
]);

type TaskMoveMutationResult = Readonly<{
  currentNodes: readonly TaskCurrentNode[];
  retainedPreviousWorktree: RetainedPreviousWorktree | null;
}>;
const taskMoveResultSchema: z.ZodType<TaskMoveMutationResult> = z
  .object({
    current_nodes: z.array(currentNodeSchema).min(1),
    retained_previous_worktree: retainedPreviousWorktreeSchema.nullable(),
  })
  .strict()
  .transform((value) => ({
    currentNodes: value.current_nodes,
    retainedPreviousWorktree: value.retained_previous_worktree,
  }));

const taskMoveNoOpResponseSchema = z
  .object({ outcome: z.literal("no_op"), no_op: taskMoveResultSchema })
  .strict()
  .transform((value) => ({ outcome: value.outcome, noOp: value.no_op }));

const taskMovePreviewNoOpResponseSchema = z
  .object({
    outcome: z.literal("no_op"),
    no_op: z.object({ current_nodes: z.array(currentNodeSchema).min(1) }).strict(),
  })
  .strict()
  .transform((value) => ({ outcome: value.outcome, noOp: { currentNodes: value.no_op.current_nodes } }));

const taskMoveAppliedResponseSchema = z
  .object({ outcome: z.literal("applied"), applied: taskMoveResultSchema })
  .strict()
  .transform((value) => ({ outcome: value.outcome, applied: value.applied }));

export const taskMoveResponseSchema: z.ZodType<TaskMoveResponse> = z.discriminatedUnion("outcome", [
  taskMoveAppliedResponseSchema,
  selectionRequiredResponseSchema,
  taskMoveNoOpResponseSchema,
  dependencyConfirmationRequiredResponseSchema,
]);

const manualMoveBlockerSchema = z.enum([
  "invalid_workflow",
  "no_source_position",
  "unsupported_destination",
  "lifecycle_conflict",
  "context_session_unavailable",
  "no_usable_transition",
  "parallel_branch_requires_fan_out",
]);

const nonBlankPreservingString = z.string().refine((value) => value.trim().length > 0);

const manualMoveRequiredValueSchema = z
  .object({
    node_key: z.string().trim().min(1),
    output_name: z.string().trim().min(1),
    description: nonBlankPreservingString.nullable(),
    resolved_value: nonBlankPreservingString.nullable().optional(),
  })
  .strict()
  .transform((value) => ({
    nodeKey: value.node_key,
    outputName: value.output_name,
    description: value.description,
    resolvedValue: value.resolved_value ?? null,
  }));

export const taskMovePreviewResponseSchema: z.ZodType<TaskMovePreviewResponse> = z.discriminatedUnion(
  "outcome",
  [
    taskMovePreviewNoOpResponseSchema,
    z
      .object({ outcome: z.literal("direct"), direct: z.object({}).strict() })
      .strict()
      .transform((value) => ({ outcome: value.outcome, direct: {} })),
    z
      .object({
        outcome: z.literal("transition"),
        transition: z
          .object({
            choices: z
              .array(
                z
                  .object({
                    transition_key: z.string().trim().min(1),
                    label: z.string().trim().min(1),
                    source_node_display_name: z.string().trim().min(1),
                    required_values: z.array(manualMoveRequiredValueSchema),
                  })
                  .strict()
                  .transform((value) => ({
                    transitionKey: value.transition_key,
                    label: value.label,
                    sourceNodeDisplayName: value.source_node_display_name,
                    requiredValues: value.required_values,
                  })),
              )
              .min(1),
          })
          .strict(),
      })
      .strict()
      .transform((value) => ({ outcome: value.outcome, transition: value.transition })),
    z
      .object({
        outcome: z.literal("blocked"),
        blocked: z.object({ reason: manualMoveBlockerSchema }).strict(),
      })
      .strict()
      .transform((value) => ({ outcome: value.outcome, blocked: value.blocked })),
  ],
);

export const taskApproveResponseSchema: z.ZodType<TaskApproveResponse> = z.discriminatedUnion("outcome", [
  z
    .object({
      outcome: z.literal("applied"),
      applied: z
        .object({
          task_id: z.string().trim().min(1),
          current_nodes: z.array(currentNodeSchema).min(1),
        })
        .strict(),
    })
    .strict()
    .transform(
      (value) =>
        ({
          outcome: value.outcome,
          applied: { taskID: value.applied.task_id, currentNodes: value.applied.current_nodes },
        }) as const,
    ),
  selectionRequiredResponseSchema,
]);

export const attentionPageSchema: z.ZodType<AttentionPage> = z
  .object({
    items: z.array(attentionItemSchema),
    next_page_token: z.string().optional().default(""),
    generated_at_unix_ms: z.number(),
  })
  .transform((value) => ({
    items: value.items,
    nextPageToken: value.next_page_token,
    generatedAt: value.generated_at_unix_ms,
  }));

export const taskAttentionSchema: z.ZodType<TaskAttention> = z
  .object({
    items: z.array(attentionItemSchema),
    generated_at_unix_ms: z.number(),
  })
  .transform((value) => ({
    items: value.items,
    generatedAt: value.generated_at_unix_ms,
  }));

const activityItemSchema = z.discriminatedUnion("type", [
  z
    .object({
      activity_id: nonBlankString,
      type: z.literal("comment"),
      task_id: nonBlankString,
      occurred_at_unix_ms: z.number(),
      updated_at_unix_ms: z.number(),
      comment: commentSchema,
    })
    .strict()
    .transform((value) => ({
      id: value.activity_id,
      type: value.type,
      taskID: value.task_id,
      occurredAt: value.occurred_at_unix_ms,
      updatedAt: value.updated_at_unix_ms,
      comment: value.comment,
    })),
  z
    .object({
      activity_id: nonBlankString,
      type: z.literal("session_started"),
      task_id: nonBlankString,
      occurred_at_unix_ms: z.number(),
      updated_at_unix_ms: z.number(),
      session_started: z.object({ session_id: nonBlankString, name: nonBlankString }).strict(),
    })
    .strict()
    .transform((value) => ({
      id: value.activity_id,
      type: value.type,
      taskID: value.task_id,
      occurredAt: value.occurred_at_unix_ms,
      updatedAt: value.updated_at_unix_ms,
      sessionID: value.session_started.session_id,
      sessionName: value.session_started.name,
    })),
]);

export const activityPageSchema: z.ZodType<ActivityPage> = offsetPageSchema(activityItemSchema);

export const taskCreateResponseSchema = z
  .object({
    task: z.object({
      id: z.string().min(1),
      short_id: z.string().min(1),
      title: z.string(),
      workflow_id: workflowIDSchema,
    }),
  })
  .transform((value) => ({
    id: value.task.id,
    shortID: value.task.short_id,
    title: value.task.title,
    workflowID: value.task.workflow_id,
  }));
export const taskUpdateResponseSchema = z.object({ task: z.object({ id: z.string() }) });
export const commentAddResponseSchema = z.object({ comment: commentSchema });

export const commentPageSchema: z.ZodType<CommentPage> = offsetPageObjectSchema(commentSchema)
  .extend({
    total_count: z.number().int().nonnegative(),
  })
  .strict()
  .transform((value) => ({
    items: value.items,
    nextOffset: value.next_offset ?? null,
    totalCount: value.total_count,
  }));
