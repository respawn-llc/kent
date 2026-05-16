import { z } from "zod";

import type { BindingPlan, ProjectPage, WorkspaceList } from "../models";
import { projectBindingSchema, workspaceSummarySchema } from "./common";

const projectSummarySchema = z
  .object({
    project_id: z.string(),
    project_key: z.string(),
    display_name: z.string(),
    primary_workspace: workspaceSummarySchema,
    default_workflow_id: z.string().optional().default(""),
    default_workflow_name: z.string().optional().default(""),
    default_workflow_valid: z.boolean(),
    updated_at_unix_ms: z.number(),
    task_count: z.number(),
    attention_count: z.number(),
    workflow_count: z.number(),
  })
  .transform((value) => ({
    id: value.project_id,
    key: value.project_key,
    name: value.display_name,
    primaryWorkspace: value.primary_workspace,
    defaultWorkflowID: value.default_workflow_id,
    defaultWorkflowName: value.default_workflow_name,
    defaultWorkflowValid: value.default_workflow_valid,
    updatedAt: value.updated_at_unix_ms,
    taskCount: value.task_count,
    attentionCount: value.attention_count,
    workflowCount: value.workflow_count,
  }));

export const projectPageSchema: z.ZodType<ProjectPage> = z
  .object({
    projects: z.array(projectSummarySchema),
    next_page_token: z.string().optional().default(""),
    generated_at_unix_ms: z.number(),
    latest_event_sequence: z.number(),
  })
  .transform((value) => ({
    projects: value.projects,
    nextPageToken: value.next_page_token,
    generatedAt: value.generated_at_unix_ms,
    latestEventSequence: value.latest_event_sequence,
  }));

export const workspaceListSchema: z.ZodType<WorkspaceList> = z
  .object({
    project_id: z.string(),
    workspaces: z.array(workspaceSummarySchema),
    default_workspace_id: z.string(),
  })
  .transform((value) => ({
    projectID: value.project_id,
    workspaces: value.workspaces,
    defaultWorkspaceID: value.default_workspace_id,
  }));

export const bindingPlanSchema: z.ZodType<BindingPlan> = z
  .object({
    kind: z.string(),
    canonical_root: z.string().optional().default(""),
    binding: projectBindingSchema.nullish(),
  })
  .transform((value) => ({
    kind: value.kind,
    canonicalRoot: value.canonical_root,
    binding: value.binding ?? null,
  }));

export const projectCreateSchema = z
  .object({
    binding: projectBindingSchema,
  })
  .transform((value) => value.binding);
