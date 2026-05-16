import { z } from "zod";

import type {
  ActivityPage,
  AttentionPage,
  PendingAsk,
  TaskDetail,
  TeleportTarget,
  WorkflowBoard,
} from "../models";
import {
  attentionItemSchema,
  boardCardSchema,
  boardColumnSchema,
  boardGroupSchema,
  commentSchema,
  emptyString,
  runSchema,
  taskActionsSchema,
  taskStatusSchema,
  transitionSchema,
  workflowPickerItemSchema,
  workspaceSummarySchema,
} from "./common";

export const workflowBoardSchema: z.ZodType<WorkflowBoard> = z
  .object({
    board: z.object({
      project_id: z.string(),
      project: z.object({
        project_key: z.string(),
        display_name: z.string(),
      }),
      selected_workflow: workflowPickerItemSchema,
      workflows: z.array(workflowPickerItemSchema),
      groups: z.array(boardGroupSchema).optional().default([]),
      columns: z.array(boardColumnSchema),
      cards: z.array(boardCardSchema),
      done_preview: z.array(boardCardSchema).optional().default([]),
      next_page_token: z.string().optional().default(""),
      generated_at_unix_ms: z.number(),
      latest_event_sequence: z.number(),
    }),
  })
  .transform((value) => ({
    projectID: value.board.project_id,
    projectKey: value.board.project.project_key,
    projectName: value.board.project.display_name,
    selectedWorkflow: value.board.selected_workflow,
    workflows: value.board.workflows,
    groups: value.board.groups,
    columns: value.board.columns,
    cards: value.board.cards,
    donePreview: value.board.done_preview,
    nextPageToken: value.board.next_page_token,
    generatedAt: value.board.generated_at_unix_ms,
    latestEventSequence: value.board.latest_event_sequence,
  }));

export const attentionPageSchema: z.ZodType<AttentionPage> = z
  .object({
    items: z.array(attentionItemSchema),
    next_page_token: z.string().optional().default(""),
    generated_at_unix_ms: z.number(),
    latest_event_sequence: z.number(),
  })
  .transform((value) => ({
    items: value.items,
    nextPageToken: value.next_page_token,
    generatedAt: value.generated_at_unix_ms,
    latestEventSequence: value.latest_event_sequence,
  }));

export const taskDetailSchema: z.ZodType<TaskDetail> = z
  .object({
    task: z.object({
      summary: z.object({
        id: z.string(),
        project_id: z.string(),
        workflow_id: z.string(),
        short_id: z.string(),
        title: z.string(),
        created_at_unix_ms: z.number(),
        updated_at_unix_ms: z.number(),
        done: z.boolean(),
        canceled_at_unix_ms: z.number().optional().default(0),
      }),
      project: z.object({
        display_name: z.string(),
      }),
      workflow: workflowPickerItemSchema,
      body: emptyString,
      source_workspace: workspaceSummarySchema,
      managed_worktree: z.object({ root_path: z.string().optional().default("") }).nullish(),
      status: taskStatusSchema,
      actions: taskActionsSchema,
      attention: z.array(attentionItemSchema).optional().default([]),
      runs: z.array(runSchema).optional().default([]),
      transitions: z.array(transitionSchema).optional().default([]),
      comments: z.array(commentSchema).optional().default([]),
    }),
  })
  .transform((value) => ({
    id: value.task.summary.id,
    shortID: value.task.summary.short_id,
    projectID: value.task.summary.project_id,
    projectName: value.task.project.display_name,
    workflowID: value.task.summary.workflow_id,
    workflowName: value.task.workflow.name,
    title: value.task.summary.title,
    body: value.task.body,
    sourceWorkspace: value.task.source_workspace,
    status: value.task.status,
    actions: value.task.actions,
    attention: value.task.attention,
    comments: value.task.comments.filter((comment) => comment.deletedAt === 0),
    runs: value.task.runs,
    transitions: value.task.transitions,
    worktreePath: value.task.managed_worktree?.root_path ?? "",
    createdAt: value.task.summary.created_at_unix_ms,
    updatedAt: value.task.summary.updated_at_unix_ms,
    done: value.task.summary.done,
    canceledAt: value.task.summary.canceled_at_unix_ms,
  }));

export const activityPageSchema: z.ZodType<ActivityPage> = z
  .object({
    items: z.array(
      z
        .object({
          activity_id: z.string(),
          type: z.string(),
          task_id: z.string(),
          occurred_at_unix_ms: z.number(),
          updated_at_unix_ms: z.number(),
          actor: emptyString,
          summary: z.string(),
          comment: commentSchema.nullish(),
          transition: transitionSchema.nullish(),
          run: runSchema.nullish(),
          attention: attentionItemSchema.nullish(),
        })
        .transform((value) => ({
          id: value.activity_id,
          type: value.type,
          taskID: value.task_id,
          occurredAt: value.occurred_at_unix_ms,
          updatedAt: value.updated_at_unix_ms,
          actor: value.actor,
          summary: value.summary,
          comment: value.comment ?? null,
          transition: value.transition ?? null,
          run: value.run ?? null,
          attention: value.attention ?? null,
        })),
    ),
    next_page_token: z.string().optional().default(""),
    generated_at_unix_ms: z.number(),
  })
  .transform((value) => ({
    items: value.items,
    nextPageToken: value.next_page_token,
    generatedAt: value.generated_at_unix_ms,
  }));

export const teleportTargetSchema: z.ZodType<TeleportTarget> = z
  .object({
    available: z.boolean(),
    task_id: emptyString,
    run_id: emptyString,
    session_id: emptyString,
    project_id: emptyString,
    workspace_id: emptyString,
    worktree_id: emptyString,
    cwd_relpath: emptyString,
    failure_reason: emptyString,
  })
  .transform((value) => ({
    available: value.available,
    taskID: value.task_id,
    runID: value.run_id,
    sessionID: value.session_id,
    projectID: value.project_id,
    workspaceID: value.workspace_id,
    worktreeID: value.worktree_id,
    cwdRelpath: value.cwd_relpath,
    failureReason: value.failure_reason,
  }));

export const pendingAskListSchema = z
  .object({
    Asks: z
      .array(
        z
          .object({
            AskID: z.string(),
            SessionID: z.string(),
            Question: z.string(),
            Suggestions: z.array(z.string()).optional().default([]),
            RecommendedOptionIndex: z.number().optional().default(0),
            CreatedAt: z.string().optional().default(""),
          })
          .transform(
            (value): PendingAsk => ({
              askID: value.AskID,
              sessionID: value.SessionID,
              question: value.Question,
              suggestions: value.Suggestions,
              recommendedOptionIndex: value.RecommendedOptionIndex,
              createdAt: value.CreatedAt,
            }),
          ),
      )
      .optional()
      .default([]),
  })
  .transform((value) => value.Asks);

const taskSummaryResponseSchema = z.object({ task: z.object({ id: z.string() }) });

export const taskCreateResponseSchema = taskSummaryResponseSchema;
export const taskUpdateResponseSchema = taskSummaryResponseSchema;
export const commentAddResponseSchema = z.object({ comment: commentSchema });
