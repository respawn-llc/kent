import { z } from "zod";
import { decodeJson } from "@app/server-api-contract";
import { SelectionRequiredSchema } from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";
import { taskTargetSelectionRequired } from "../clientTaskProjection";
import { taskOriginalTargetCause, taskUnavailableTargetCause, workflowExecutionTargetMode } from "../workflowProtoValues";

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
          cause: taskOriginalTargetCause.encode(value.original_target_cause),
        },
      };
    case "configured_target_unavailable":
      return {
        configured_target_unavailable: {
          mode: workflowExecutionTargetMode.encode(value.configured_target.mode),
          requested_ref: value.configured_target.requested_ref ?? null,
          cause: taskUnavailableTargetCause.encode(value.unavailable_cause),
        },
      };
  }
}

const selectionRequirementSchema: z.ZodType<WorkflowExecutionTargetSelectionRequirement> =
  selectionRequirementEnvelope.transform((value, context): WorkflowExecutionTargetSelectionRequirement => {
    try {
      return taskTargetSelectionRequired(decodeJson(SelectionRequiredSchema, adaptSelectionEnvelope(value)));
    } catch {
      context.addIssue({ code: "custom", message: "Invalid execution target selection requirement." });
      return z.NEVER;
    }
  });

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
