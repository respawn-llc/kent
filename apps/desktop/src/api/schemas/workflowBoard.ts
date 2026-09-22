import { z } from "zod";
import type { AttentionPage, TaskAttention } from "../models";
import { attentionItemSchema } from "./common";

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
