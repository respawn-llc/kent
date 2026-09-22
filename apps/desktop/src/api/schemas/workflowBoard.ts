import { z } from "zod";
import type { ActivityPage, AttentionPage, CommentPage, OffsetPage, TaskAttention } from "../models";
import { attentionItemSchema, commentSchema, nonBlankString } from "./common";

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
    .transform((value) => ({ items: value.items, nextOffset: value.next_offset ?? null }));
}

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
  .object({ items: z.array(attentionItemSchema), generated_at_unix_ms: z.number() })
  .transform((value) => ({ items: value.items, generatedAt: value.generated_at_unix_ms }));

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
  .extend({ total_count: z.number().int().nonnegative() })
  .strict()
  .transform((value) => ({
    items: value.items,
    nextOffset: value.next_offset ?? null,
    totalCount: value.total_count,
  }));
