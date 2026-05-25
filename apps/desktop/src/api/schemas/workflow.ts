/* eslint-disable max-lines -- Workflow API schemas stay colocated with DTO-to-view-model mapping logic. */
import { z } from "zod";

import type {
  ActivityPage,
  AttentionPage,
  BoardColumn,
  BoardGroup,
  BoardNodeCardsPage,
  PendingAsk,
  TaskDetail,
  TeleportTarget,
  WorkflowBoard,
  WorkflowDeleteImpact,
  WorkflowDeleteResponse,
  WorkflowDefinition,
  WorkflowGraphSaveImpact,
  WorkflowGraphSavePreview,
  WorkflowGraphSaveResult,
  WorkflowGraphValidationResults,
  WorkflowPage,
  WorkflowRecord,
  ProjectWorkflowLink,
  WorkflowValidation,
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
  validationErrorSchema,
  workflowOutputFieldSchema,
  workspaceSummarySchema,
} from "./common";

const boardGroupsSchema = z
  .array(boardGroupSchema)
  .nullish()
  .transform((value) => value ?? []);
const boardColumnsSchema = z
  .array(boardColumnSchema)
  .nullish()
  .transform((value) => value ?? []);
const boardCardsSchema = z
  .array(boardCardSchema)
  .nullish()
  .transform((value) => value ?? []);
const workflowPickerSchema = z
  .array(workflowPickerItemSchema)
  .nullish()
  .transform((value) => value ?? []);

const workflowRecordSchema: z.ZodType<WorkflowRecord> = z
  .object({
    id: z.string(),
    name: z.string(),
    description: emptyString,
    version: z.number(),
  })
  .transform((value) => ({
    id: value.id,
    name: value.name,
    description: value.description,
    version: value.version,
  }));

export const workflowListSchema: z.ZodType<WorkflowPage> = z
  .object({
    workflows: z.array(workflowRecordSchema).nullish().transform(emptyArray),
    next_page_token: emptyString,
  })
  .transform((value) => ({
    workflows: value.workflows,
    nextPageToken: value.next_page_token,
  }));

export const workflowCreateSchema: z.ZodType<WorkflowRecord> = z
  .object({
    workflow: workflowRecordSchema,
  })
  .transform((value) => value.workflow);

const projectWorkflowLinkSchema: z.ZodType<ProjectWorkflowLink> = z
  .object({
    id: z.string(),
    project_id: z.string(),
    workflow_id: z.string(),
    default: z.boolean(),
  })
  .transform((value) => ({
    id: value.id,
    projectID: value.project_id,
    workflowID: value.workflow_id,
    isDefault: value.default,
  }));

export const workflowLinkProjectSchema: z.ZodType<ProjectWorkflowLink> = z
  .object({
    link: projectWorkflowLinkSchema,
  })
  .transform((value) => value.link);

export const workflowCreateAndLinkSchema: z.ZodType<
  Readonly<{ workflow: WorkflowRecord; link: ProjectWorkflowLink }>
> = z
  .object({
    workflow: workflowRecordSchema,
    link: projectWorkflowLinkSchema,
  })
  .transform((value) => ({
    workflow: value.workflow,
    link: value.link,
  }));

const workflowNodeGroupsSchema = z
  .array(
    z
      .object({
        group_id: z.string(),
        workflow_id: z.string(),
        group_key: z.string(),
        display_name: z.string(),
        sort_order: z.number(),
        node_ids: z.array(z.string()).nullish().transform(emptyArray),
      })
      .transform((value) => ({
        id: value.group_id,
        workflowID: value.workflow_id,
        key: value.group_key,
        name: value.display_name,
        sortOrder: value.sort_order,
        nodeIDs: value.node_ids,
      })),
  )
  .nullish()
  .transform(emptyArray);

const workflowNodesSchema = z
  .array(
    z
      .object({
        id: z.string(),
        workflow_id: z.string(),
        key: z.string(),
        kind: z.string(),
        display_name: z.string(),
        group_id: emptyString,
        group_key: emptyString,
        subagent_role: emptyString,
        prompt_template: emptyString,
        output_fields: z.array(workflowOutputFieldSchema).nullish().transform(emptyArray),
      })
      .transform((value) => ({
        id: value.id,
        workflowID: value.workflow_id,
        key: value.key,
        kind: value.kind,
        name: value.display_name,
        groupID: value.group_id,
        groupKey: value.group_key,
        subagentRole: value.subagent_role,
        promptTemplate: value.prompt_template,
        outputFields: value.output_fields,
      })),
  )
  .nullish()
  .transform(emptyArray);

const workflowTransitionGroupsSchema = z
  .array(
    z
      .object({
        id: z.string(),
        workflow_id: z.string(),
        source_node_id: z.string(),
        transition_id: z.string(),
        display_name: z.string(),
      })
      .transform((value) => ({
        id: value.id,
        workflowID: value.workflow_id,
        sourceNodeID: value.source_node_id,
        transitionID: value.transition_id,
        name: value.display_name,
      })),
  )
  .nullish()
  .transform(emptyArray);

const workflowInputBindingsSchema = z
  .array(
    z
      .object({
        name: z.string(),
        source: z.string(),
        field: z.string(),
      })
      .transform((value) => ({
        name: value.name,
        source: value.source,
        field: value.field,
      })),
  )
  .nullish()
  .transform(emptyArray);

const workflowOutputRequirementsSchema = z
  .array(
    z
      .object({
        field_name: z.string(),
      })
      .transform((value) => ({
        fieldName: value.field_name,
      })),
  )
  .nullish()
  .transform(emptyArray);

const workflowContextSourceSchema = z
  .object({
    kind: z
      .string()
      .nullish()
      .transform((value) => value ?? "immediate_source"),
    node_key: emptyString,
  })
  .nullish()
  .transform((value) => ({
    kind: value?.kind ?? "immediate_source",
    nodeKey: value?.node_key ?? "",
  }));

const workflowEdgesSchema = z
  .array(
    z
      .object({
        id: z.string(),
        workflow_id: z.string(),
        transition_group_id: z.string(),
        key: z.string(),
        target_node_id: z.string(),
        requires_approval: z.boolean(),
        context_mode: z.string(),
        context_source: workflowContextSourceSchema,
        input_bindings: workflowInputBindingsSchema,
        output_requirements: workflowOutputRequirementsSchema,
      })
      .transform((value) => ({
        id: value.id,
        workflowID: value.workflow_id,
        transitionGroupID: value.transition_group_id,
        key: value.key,
        targetNodeID: value.target_node_id,
        requiresApproval: value.requires_approval,
        contextMode: value.context_mode,
        contextSource: value.context_source,
        inputBindings: value.input_bindings,
        outputRequirements: value.output_requirements,
      })),
  )
  .nullish()
  .transform(emptyArray);

const workflowDefinitionValueSchema: z.ZodType<WorkflowDefinition> = z
  .object({
    workflow: z.object({
      id: z.string(),
      name: z.string(),
      description: emptyString,
      version: z.number(),
    }),
    node_groups: workflowNodeGroupsSchema,
    nodes: workflowNodesSchema,
    transition_groups: workflowTransitionGroupsSchema,
    edges: workflowEdgesSchema,
  })
  .transform((value) => ({
    workflow: {
      id: value.workflow.id,
      name: value.workflow.name,
      description: value.workflow.description,
      version: value.workflow.version,
    },
    nodeGroups: value.node_groups,
    nodes: value.nodes,
    transitionGroups: value.transition_groups,
    edges: value.edges,
  }));

export const workflowDefinitionSchema: z.ZodType<WorkflowDefinition> = z
  .object({
    definition: workflowDefinitionValueSchema,
  })
  .transform((value) => value.definition);

export const workflowValidationSchema: z.ZodType<WorkflowValidation> = z
  .object({
    valid: z.boolean(),
    errors: z.array(validationErrorSchema).nullish().transform(emptyArray),
  })
  .transform((value) => ({
    valid: value.valid,
    errors: value.errors,
  }));

const workflowGraphValidationResultsSchema: z.ZodType<WorkflowGraphValidationResults> = z.record(
  z.string(),
  workflowValidationSchema,
);

export const workflowGraphValidateDraftSchema: z.ZodType<WorkflowGraphValidationResults> = z
  .object({
    results: workflowGraphValidationResultsSchema,
  })
  .transform((value) => value.results);

const workflowGraphSaveImpactSchema: z.ZodType<WorkflowGraphSaveImpact> = z
  .object({
    removed_node_count: z.number(),
    removed_transition_group_count: z.number(),
    removed_edge_count: z.number(),
    node_task_reference_count: z.number(),
    edge_task_reference_count: z.number(),
    active_node_placement_count: z.number(),
    pending_approval_count: z.number(),
    active_run_count: z.number(),
    runnable_run_count: z.number(),
    start_node_change_count: z.number(),
    last_terminal_change_count: z.number(),
    task_referenced_node_kind_change_count: z.number(),
  })
  .transform((value) => ({
    removedNodeCount: value.removed_node_count,
    removedTransitionGroupCount: value.removed_transition_group_count,
    removedEdgeCount: value.removed_edge_count,
    nodeTaskReferenceCount: value.node_task_reference_count,
    edgeTaskReferenceCount: value.edge_task_reference_count,
    activeNodePlacementCount: value.active_node_placement_count,
    pendingApprovalCount: value.pending_approval_count,
    activeRunCount: value.active_run_count,
    runnableRunCount: value.runnable_run_count,
    startNodeChangeCount: value.start_node_change_count,
    lastTerminalChangeCount: value.last_terminal_change_count,
    taskReferencedNodeKindChangeCount: value.task_referenced_node_kind_change_count,
  }));

const workflowGraphSaveBlockersSchema = z
  .array(
    z.object({
      code: z.string(),
      message: z.string(),
      count: z.number(),
    }),
  )
  .nullish()
  .transform(emptyArray);

export const workflowGraphSavePreviewSchema: z.ZodType<WorkflowGraphSavePreview> = z
  .object({
    current_version: z.number(),
    validation_results: workflowGraphValidationResultsSchema,
    impact: workflowGraphSaveImpactSchema,
    blockers: workflowGraphSaveBlockersSchema,
    can_save: z.boolean(),
    confirmation_required: z.boolean(),
  })
  .transform((value) => ({
    currentVersion: value.current_version,
    validationResults: value.validation_results,
    impact: value.impact,
    blockers: value.blockers,
    canSave: value.can_save,
    confirmationRequired: value.confirmation_required,
  }));

export const workflowGraphSaveSchema: z.ZodType<WorkflowGraphSaveResult> = z
  .object({
    saved: z.boolean(),
    definition: workflowDefinitionValueSchema.nullish().transform((value) => value ?? null),
    current_version: z.number(),
    validation_results: workflowGraphValidationResultsSchema,
    impact: workflowGraphSaveImpactSchema,
    blockers: workflowGraphSaveBlockersSchema,
    can_save: z.boolean(),
    confirmation_required: z.boolean(),
  })
  .transform((value) => ({
    saved: value.saved,
    definition: value.definition,
    currentVersion: value.current_version,
    validationResults: value.validation_results,
    impact: value.impact,
    blockers: value.blockers,
    canSave: value.can_save,
    confirmationRequired: value.confirmation_required,
  }));

const workflowDeleteImpactSchema: z.ZodType<WorkflowDeleteImpact> = z
  .object({
    workflow_id: z.string(),
    version: z.number(),
    project_count: z.number(),
    link_count: z.number(),
    default_replacement_project_count: z.number(),
    task_count: z.number(),
    active_run_count: z.number(),
    runnable_run_count: z.number(),
    blocked_task_count: z.number(),
  })
  .transform((value) => ({
    workflowID: value.workflow_id,
    version: value.version,
    projectCount: value.project_count,
    linkCount: value.link_count,
    defaultReplacementProjectCount: value.default_replacement_project_count,
    taskCount: value.task_count,
    activeRunCount: value.active_run_count,
    runnableRunCount: value.runnable_run_count,
    blockedTaskCount: value.blocked_task_count,
  }));

export const workflowDeletePreviewSchema: z.ZodType<WorkflowDeleteImpact> = z
  .object({
    impact: workflowDeleteImpactSchema,
  })
  .transform((value) => value.impact);

export const workflowDeleteResponseSchema: z.ZodType<WorkflowDeleteResponse> = z
  .object({
    deleted: z.boolean(),
    impact: workflowDeleteImpactSchema,
    blockers: z
      .array(
        z.object({
          code: z.string(),
          message: z.string(),
          count: z.number(),
        }),
      )
      .nullish()
      .transform(emptyArray),
  })
  .transform((value) => ({
    deleted: value.deleted,
    impact: value.impact,
    blockers: value.blockers,
  }));

export const taskMoveResponseSchema = z
  .object({
    approval_error: emptyString,
  })
  .transform((value) => ({
    approvalError: value.approval_error,
  }));

export const projectWorkflowLinksSchema: z.ZodType<readonly ProjectWorkflowLink[]> = z
  .object({
    links: z
      .array(
        z
          .object({
            id: z.string(),
            project_id: z.string(),
            workflow_id: z.string(),
            default: z.boolean(),
          })
          .transform((value) => ({
            id: value.id,
            projectID: value.project_id,
            workflowID: value.workflow_id,
            isDefault: value.default,
          })),
      )
      .nullish()
      .transform(emptyArray),
  })
  .transform((value) => value.links);

export const workflowBoardSchema: z.ZodType<WorkflowBoard> = z
  .object({
    board: z.object({
      project_id: z.string(),
      project: z.object({
        project_key: z.string(),
        display_name: z.string(),
      }),
      selected_workflow: workflowPickerItemSchema,
      workflows: workflowPickerSchema,
      groups: boardGroupsSchema,
      columns: boardColumnsSchema,
      generated_at_unix_ms: z.number(),
    }),
  })
  .transform((value) => {
    const columns = visibleBoardColumns(value.board.columns);
    return {
      projectID: value.board.project_id,
      projectKey: value.board.project.project_key,
      projectName: value.board.project.display_name,
      selectedWorkflow: value.board.selected_workflow,
      workflows: value.board.workflows,
      groups: visibleBoardGroups(value.board.groups, columns),
      columns,
      generatedAt: value.board.generated_at_unix_ms,
    };
  });

function visibleBoardColumns(columns: readonly BoardColumn[]): readonly BoardColumn[] {
  return columns.filter((column) => column.kind !== "join");
}

function visibleBoardGroups(
  groups: readonly BoardGroup[],
  columns: readonly BoardColumn[],
): readonly BoardGroup[] {
  const visibleNodeIDs = new Set(columns.map((column) => column.id));
  return groups
    .map((group) => ({
      ...group,
      nodeIDs: group.nodeIDs.filter((nodeID) => visibleNodeIDs.has(nodeID)),
    }))
    .filter((group) => group.nodeIDs.length > 0);
}

export const boardNodeCardsPageSchema: z.ZodType<BoardNodeCardsPage> = z
  .object({
    project_id: z.string(),
    workflow_id: z.string(),
    node_id: z.string(),
    cards: boardCardsSchema,
    next_page_token: z.string().optional().default(""),
    generated_at_unix_ms: z.number(),
  })
  .transform((value) => ({
    projectID: value.project_id,
    workflowID: value.workflow_id,
    nodeID: value.node_id,
    cards: value.cards,
    nextPageToken: value.next_page_token,
    generatedAt: value.generated_at_unix_ms,
  }));

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
      managed_worktree: z
        .object({
          canonical_root: z.string().optional().default(""),
          root_path: z.string().optional().default(""),
        })
        .nullish(),
      status: taskStatusSchema,
      actions: taskActionsSchema,
      attention: z.array(attentionItemSchema).nullish().transform(emptyArray),
      runs: z.array(runSchema).nullish().transform(emptyArray),
      transitions: z.array(transitionSchema).nullish().transform(emptyArray),
      comments: z.array(commentSchema).nullish().transform(emptyArray),
    }),
  })
  .transform((value) => ({
    id: value.task.summary.id,
    shortID: value.task.summary.short_id,
    projectID: value.task.summary.project_id,
    projectName: value.task.project.display_name,
    workflowID: value.task.summary.workflow_id,
    workflowName: value.task.workflow.name,
    workflowVersion: value.task.workflow.version,
    title: value.task.summary.title,
    body: value.task.body,
    sourceWorkspace: value.task.source_workspace,
    status: value.task.status,
    actions: value.task.actions,
    attention: value.task.attention,
    comments: value.task.comments,
    runs: value.task.runs,
    transitions: value.task.transitions,
    worktreePath: firstNonEmpty(
      value.task.managed_worktree?.canonical_root,
      value.task.managed_worktree?.root_path,
      "",
    ),
    createdAt: value.task.summary.created_at_unix_ms,
    updatedAt: value.task.summary.updated_at_unix_ms,
    done: value.task.summary.done,
    canceledAt: value.task.summary.canceled_at_unix_ms,
  }));

function emptyArray<T>(value: T[] | null | undefined): T[] {
  return value ?? [];
}

function firstNonEmpty(...values: readonly (string | undefined)[]): string {
  return values.find((value) => value !== undefined && value.length > 0) ?? "";
}

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
