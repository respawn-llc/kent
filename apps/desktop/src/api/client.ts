/* eslint-disable max-lines -- RPC client methods are intentionally centralized by transport boundary. */
import { type z } from "zod";

import { ContractError } from "./errors";
import { compactJsonObject, emptyJsonObject, type JsonObject } from "./json";
import type {
  ActivityPage,
  AttentionPage,
  BindingPlan,
  BoardNodeCardsPage,
  PendingAsk,
  ProjectWorkflowLink,
  ProjectBinding,
  ProjectEdit,
  ProjectMutationResponse,
  ProjectPage,
  ServerReadiness,
  TaskComment,
  TaskDetail,
  WorkflowBoard,
  WorkflowDeleteImpact,
  WorkflowDeleteResponse,
  WorkflowDefinition,
  WorkflowGraphDraft,
  WorkflowGraphSaveConfirmation,
  WorkflowGraphMetadata,
  WorkflowGraphSavePreview,
  WorkflowGraphSaveResult,
  WorkflowGraphValidateDraftResult,
  WorkflowPage,
  WorkflowRecord,
  WorkflowValidation,
  WorkflowValidationMode,
  WorkspaceList,
  WorkspaceUnlinkResponse,
} from "./models";
import {
  bindingPlanSchema,
  projectCreateSchema,
  projectEditSchema,
  projectMutationResponseSchema,
  projectPageSchema,
  workspaceListSchema,
  workspaceUnlinkResponseSchema,
} from "./schemas/project";
import { readinessSchema } from "./schemas/status";
import {
  activityPageSchema,
  attentionPageSchema,
  boardNodeCardsPageSchema,
  commentAddResponseSchema,
  pendingAskListSchema,
  projectWorkflowLinksSchema,
  taskCreateResponseSchema,
  taskMoveResponseSchema,
  taskDetailSchema,
  taskUpdateResponseSchema,
  workflowCreateAndLinkSchema,
  workflowCreateSchema,
  workflowBoardSchema,
  workflowDeletePreviewSchema,
  workflowDeleteResponseSchema,
  workflowDefinitionSchema,
  workflowGraphSavePreviewSchema,
  workflowGraphSaveSchema,
  workflowGraphValidateDraftSchema,
  workflowLinkProjectSchema,
  workflowListSchema,
  workflowValidationSchema,
} from "./schemas/workflow";
import type { RpcEventHandler, RpcSubscription, RpcTransport } from "./transport";

export class BuilderApiClient {
  readonly transport: RpcTransport;

  constructor(transport: RpcTransport) {
    this.transport = transport;
  }

  async getReadiness(): Promise<ServerReadiness> {
    return parse(
      "server.readiness.get",
      readinessSchema,
      await this.transport.call("server.readiness.get", emptyJsonObject),
    );
  }

  async listProjects(pageToken: string): Promise<ProjectPage> {
    return parse(
      "project.home.list",
      projectPageSchema,
      await this.transport.call("project.home.list", { page_size: 40, page_token: pageToken }),
    );
  }

  async listWorkspaces(projectID: string, pageToken = ""): Promise<WorkspaceList> {
    return parse(
      "project.workspace.list",
      workspaceListSchema,
      await this.transport.call("project.workspace.list", {
        project_id: projectID,
        page_size: 100,
        page_token: pageToken,
      }),
    );
  }

  async getProjectEdit(projectID: string, pageToken = ""): Promise<ProjectEdit> {
    return parse(
      "project.edit.get",
      projectEditSchema,
      await this.transport.call("project.edit.get", {
        project_id: projectID,
        page_size: 100,
        page_token: pageToken,
      }),
    );
  }

  async planWorkspace(path: string): Promise<BindingPlan> {
    return parse(
      "project.planWorkspaceBinding",
      bindingPlanSchema,
      await this.transport.call("project.planWorkspaceBinding", { path, mode: "interactive" }),
    );
  }

  async createProject(
    displayName: string,
    projectKey: string,
    workspaceRoot: string,
  ): Promise<ProjectBinding> {
    return parse(
      "project.create",
      projectCreateSchema,
      await this.transport.call("project.create", {
        display_name: displayName,
        project_key: projectKey,
        workspace_root: workspaceRoot,
      }),
    );
  }

  async attachWorkspace(projectID: string, workspaceRoot: string): Promise<ProjectBinding> {
    return parse(
      "project.attachWorkspace",
      projectCreateSchema,
      await this.transport.call("project.attachWorkspace", {
        project_id: projectID,
        workspace_root: workspaceRoot,
      }),
    );
  }

  async updateProject(projectID: string, displayName: string): Promise<ProjectMutationResponse> {
    return parse(
      "project.update",
      projectMutationResponseSchema,
      await this.transport.call("project.update", { project_id: projectID, display_name: displayName }),
    );
  }

  async setDefaultWorkspace(projectID: string, workspaceID: string): Promise<ProjectMutationResponse> {
    return parse(
      "project.defaultWorkspace.set",
      projectMutationResponseSchema,
      await this.transport.call("project.defaultWorkspace.set", {
        project_id: projectID,
        workspace_id: workspaceID,
      }),
    );
  }

  async unlinkWorkspace(projectID: string, workspaceID: string): Promise<WorkspaceUnlinkResponse> {
    return parse(
      "project.unlinkWorkspace",
      workspaceUnlinkResponseSchema,
      await this.transport.call("project.unlinkWorkspace", {
        project_id: projectID,
        workspace_id: workspaceID,
      }),
    );
  }

  async getBoard(projectID: string, workflowID: string): Promise<WorkflowBoard> {
    return parse(
      "workflow.board.get",
      workflowBoardSchema,
      await this.transport.call(
        "workflow.board.get",
        compactJsonObject({
          project_id: projectID,
          workflow_id: workflowID.length > 0 ? workflowID : undefined,
        }),
      ),
    );
  }

  async getWorkflow(workflowID: string): Promise<WorkflowDefinition> {
    return parse(
      "workflow.get",
      workflowDefinitionSchema,
      await this.transport.call("workflow.get", { workflow_id: workflowID }),
    );
  }

  async listWorkflows(input: WorkflowListInput = {}): Promise<WorkflowPage> {
    return parse(
      "workflow.list",
      workflowListSchema,
      await this.transport.call(
        "workflow.list",
        compactJsonObject({
          page_size: input.pageSize ?? 40,
          page_token: input.pageToken ?? "",
          query: input.query ?? "",
        }),
      ),
    );
  }

  async createWorkflow(input: WorkflowCreateInput): Promise<WorkflowRecord> {
    return parse(
      "workflow.create",
      workflowCreateSchema,
      await this.transport.call(
        "workflow.create",
        compactJsonObject({
          name: input.name,
          description: input.description,
        }),
      ),
    );
  }

  async createAndLinkWorkflowToProject(
    input: WorkflowCreateAndLinkInput,
  ): Promise<Readonly<{ workflow: WorkflowRecord; link: ProjectWorkflowLink }>> {
    return parse(
      "workflow.createAndLinkProject",
      workflowCreateAndLinkSchema,
      await this.transport.call(
        "workflow.createAndLinkProject",
        compactJsonObject({
          name: input.name,
          description: input.description,
          project_id: input.projectID,
          default_policy: "if_project_has_none",
        }),
      ),
    );
  }

  async linkWorkflowToProject(input: WorkflowProjectLinkInput): Promise<ProjectWorkflowLink> {
    return parse(
      "workflow.linkProject",
      workflowLinkProjectSchema,
      await this.transport.call(
        "workflow.linkProject",
        compactJsonObject({
          project_id: input.projectID,
          workflow_id: input.workflowID,
          default_policy: "if_project_has_none",
        }),
      ),
    );
  }

  async validateWorkflow(
    workflowID: string,
    mode: "draft" | "task_creation" | "execution",
  ): Promise<WorkflowValidation> {
    return parse(
      "workflow.validate",
      workflowValidationSchema,
      await this.transport.call("workflow.validate", { workflow_id: workflowID, mode }),
    );
  }

  async validateWorkflowGraphDraft(
    input: WorkflowGraphValidateDraftInput,
  ): Promise<WorkflowGraphValidateDraftResult> {
    return parse(
      "workflow.graph.validateDraft",
      workflowGraphValidateDraftSchema,
      await this.transport.call(
        "workflow.graph.validateDraft",
        compactJsonObject({
          workflow_id: input.workflowID,
          metadata: workflowGraphMetadataPayload(input.metadata),
          graph: workflowGraphDraftPayload(input.graph),
          modes: input.modes,
        }),
      ),
    );
  }

  async previewWorkflowGraphSave(input: WorkflowGraphSavePreviewInput): Promise<WorkflowGraphSavePreview> {
    return parse(
      "workflow.graph.savePreview",
      workflowGraphSavePreviewSchema,
      await this.transport.call(
        "workflow.graph.savePreview",
        compactJsonObject({
          workflow_id: input.workflowID,
          expected_version: input.expectedVersion,
          metadata: workflowGraphMetadataPayload(input.metadata),
          graph: workflowGraphDraftPayload(input.graph),
        }),
      ),
    );
  }

  async saveWorkflowGraph(input: WorkflowGraphSaveInput): Promise<WorkflowGraphSaveResult> {
    return parse(
      "workflow.graph.save",
      workflowGraphSaveSchema,
      await this.transport.call(
        "workflow.graph.save",
        compactJsonObject({
          workflow_id: input.workflowID,
          expected_version: input.expectedVersion,
          metadata: workflowGraphMetadataPayload(input.metadata),
          graph: workflowGraphDraftPayload(input.graph),
          confirmation: workflowGraphSaveConfirmationPayload(input.confirmation),
        }),
      ),
    );
  }

  async previewWorkflowDelete(workflowID: string): Promise<WorkflowDeleteImpact> {
    return parse(
      "workflow.deletePreview",
      workflowDeletePreviewSchema,
      await this.transport.call("workflow.deletePreview", { workflow_id: workflowID }),
    );
  }

  async deleteWorkflow(input: WorkflowDeleteInput): Promise<WorkflowDeleteResponse> {
    return parse(
      "workflow.delete",
      workflowDeleteResponseSchema,
      await this.transport.call(
        "workflow.delete",
        compactJsonObject({
          workflow_id: input.workflowID,
          confirmed: input.confirmed,
          expected_version: input.expectedVersion,
          expected_project_count: input.expectedProjectCount,
          expected_link_count: input.expectedLinkCount,
          expected_task_count: input.expectedTaskCount,
          cleanup_artifacts: input.cleanupArtifacts,
        }),
      ),
    );
  }

  async listProjectWorkflowLinks(projectID: string): Promise<readonly ProjectWorkflowLink[]> {
    return parse(
      "workflow.listProjectLinks",
      projectWorkflowLinksSchema,
      await this.transport.call("workflow.listProjectLinks", { project_id: projectID }),
    );
  }

  async listBoardNodeCards(
    projectID: string,
    workflowID: string,
    nodeID: string,
    pageToken = "",
  ): Promise<BoardNodeCardsPage> {
    return parse(
      "workflow.board.nodeCards.list",
      boardNodeCardsPageSchema,
      await this.transport.call(
        "workflow.board.nodeCards.list",
        compactJsonObject({
          project_id: projectID,
          workflow_id: workflowID,
          node_id: nodeID,
          page_size: 100,
          page_token: pageToken,
        }),
      ),
    );
  }

  async listAttention(projectID: string, pageToken: string): Promise<AttentionPage> {
    return parse(
      "workflow.attention.list",
      attentionPageSchema,
      await this.transport.call(
        "workflow.attention.list",
        compactJsonObject({
          project_id: projectID.length > 0 ? projectID : undefined,
          page_size: 40,
          page_token: pageToken,
        }),
      ),
    );
  }

  async createTask(input: TaskMutationInput): Promise<string> {
    const response = parse(
      "workflow.task.create",
      taskCreateResponseSchema,
      await this.transport.call(
        "workflow.task.create",
        compactJsonObject({
          project_id: input.projectID,
          workflow_id: input.workflowID,
          title: input.title,
          body: input.body,
          source_workspace_id: input.sourceWorkspaceID,
        }),
      ),
    );
    return response.task.id;
  }

  async updateTask(input: TaskEditInput): Promise<string> {
    const response = parse(
      "workflow.task.update",
      taskUpdateResponseSchema,
      await this.transport.call(
        "workflow.task.update",
        compactJsonObject({
          task_id: input.taskID,
          title: input.title,
          body: input.body,
          source_workspace_id: input.sourceWorkspaceID,
        }),
      ),
    );
    return response.task.id;
  }

  async startTask(taskID: string): Promise<void> {
    await this.transport.call("workflow.task.start", { task_id: taskID });
  }

  async moveTask(input: TaskMoveInput): Promise<void> {
    const response = parse(
      "workflow.task.move",
      taskMoveResponseSchema,
      await this.transport.call(
        "workflow.task.move",
        compactJsonObject({
          task_id: input.taskID,
          target_node_id: input.targetNodeID,
          output_values: input.outputValues ?? {},
          allow_missing_edge: input.allowMissingEdge,
          auto_approve: input.autoApprove,
        }),
      ),
    );
    if (response.approvalError.length > 0) {
      throw new Error(response.approvalError);
    }
  }

  async interruptTask(taskID: string, runID: string): Promise<void> {
    await this.transport.call(
      "workflow.task.interrupt",
      compactJsonObject({ task_id: taskID, run_id: runID }),
    );
  }

  async resumeTask(taskID: string, runID: string): Promise<void> {
    await this.transport.call("workflow.task.resume", compactJsonObject({ task_id: taskID, run_id: runID }));
  }

  async approveTransition(taskTransitionID: string): Promise<void> {
    await this.transport.call("workflow.task.approve", { task_transition_id: taskTransitionID });
  }

  async cancelTask(taskID: string): Promise<void> {
    await this.transport.call("workflow.task.cancel", { task_id: taskID });
  }

  async getTask(taskID: string): Promise<TaskDetail> {
    return parse(
      "workflow.task.get",
      taskDetailSchema,
      await this.transport.call("workflow.task.get", { task_id: taskID }),
    );
  }

  async listTaskActivity(taskID: string, pageToken: string): Promise<ActivityPage> {
    return parse(
      "workflow.task.activity.list",
      activityPageSchema,
      await this.transport.call("workflow.task.activity.list", {
        task_id: taskID,
        page_size: 40,
        page_token: pageToken,
      }),
    );
  }

  async addComment(taskID: string, body: string): Promise<TaskComment> {
    return parse(
      "workflow.task.comment.add",
      commentAddResponseSchema,
      await this.transport.call("workflow.task.comment.add", { task_id: taskID, body, author: "GUI" }),
    ).comment;
  }

  async replaceComment(commentID: string, body: string): Promise<void> {
    await this.transport.call("workflow.task.comment.replace", { comment_id: commentID, body });
  }

  async deleteComment(commentID: string): Promise<void> {
    await this.transport.call("workflow.task.comment.delete", { comment_id: commentID });
  }

  async answerQuestion(input: QuestionAnswerInput): Promise<void> {
    await this.transport.call(
      "workflow.task.question.answer",
      compactJsonObject({
        client_request_id: input.clientRequestID,
        task_id: input.taskID,
        run_id: input.runID,
        ask_id: input.askID,
        selected_option_number: input.selectedOptionNumber > 0 ? input.selectedOptionNumber : undefined,
        freeform_answer: input.freeformAnswer,
      }),
    );
  }

  async listPendingAsks(sessionID: string): Promise<readonly PendingAsk[]> {
    return parse(
      "ask.listPendingBySession",
      pendingAskListSchema,
      await this.transport.call("ask.listPendingBySession", { SessionID: sessionID }),
    );
  }

  subscribeProject(projectID: string, handler: RpcEventHandler): RpcSubscription {
    return this.transport.subscribe("workflow.subscribeProject", { project_id: projectID }, handler);
  }

  subscribeWorkflow(workflowID: string, handler: RpcEventHandler): RpcSubscription {
    return this.transport.subscribe("workflow.subscribe", { workflow_id: workflowID }, handler);
  }
}

export type TaskMutationInput = Readonly<{
  projectID: string;
  workflowID: string;
  title: string;
  body: string;
  sourceWorkspaceID: string;
}>;

export type WorkflowListInput = Readonly<{
  pageSize?: number | undefined;
  pageToken?: string | undefined;
  query?: string | undefined;
}>;

export type WorkflowCreateInput = Readonly<{
  name: string;
  description: string;
}>;

export type WorkflowCreateAndLinkInput = WorkflowCreateInput &
  Readonly<{
    projectID: string;
  }>;

export type WorkflowProjectLinkInput = Readonly<{
  projectID: string;
  workflowID: string;
}>;

export type WorkflowDeleteInput = Readonly<{
  workflowID: string;
  confirmed: boolean;
  expectedVersion: number;
  expectedProjectCount: number;
  expectedLinkCount: number;
  expectedTaskCount: number;
  cleanupArtifacts?: boolean;
}>;

export type WorkflowGraphValidateDraftInput = Readonly<{
  workflowID: string;
  metadata?: WorkflowGraphMetadata | undefined;
  graph: WorkflowGraphDraft;
  modes: readonly WorkflowValidationMode[];
}>;

export type WorkflowGraphSavePreviewInput = Readonly<{
  workflowID: string;
  expectedVersion: number;
  metadata?: WorkflowGraphMetadata | undefined;
  graph: WorkflowGraphDraft;
}>;

export type WorkflowGraphSaveInput = WorkflowGraphSavePreviewInput &
  Readonly<{
    confirmation?: WorkflowGraphSaveConfirmation | undefined;
  }>;

export type TaskEditInput = Readonly<{
  taskID: string;
  title: string;
  body: string;
  sourceWorkspaceID?: string | undefined;
}>;

export type TaskMoveInput = Readonly<{
  taskID: string;
  targetNodeID: string;
  outputValues?: Readonly<Record<string, string>>;
  allowMissingEdge?: boolean;
  autoApprove?: boolean;
}>;

export type QuestionAnswerInput = Readonly<{
  clientRequestID: string;
  taskID: string;
  runID: string;
  askID: string;
  selectedOptionNumber: number;
  freeformAnswer: string;
}>;

function parse<T>(method: string, schema: z.ZodType<T>, value: unknown): T {
  const result = schema.safeParse(value);
  if (!result.success) {
    throw new ContractError(`${method} response did not match GUI contract.`);
  }
  return result.data;
}

function workflowGraphDraftPayload(graph: WorkflowGraphDraft): JsonObject {
  return {
    node_groups: graph.nodeGroups.map((group) => ({
      id: group.id,
      key: group.key,
      display_name: group.name,
    })),
    nodes: graph.nodes.map((node) =>
      compactJsonObject({
        id: node.id,
        key: node.key,
        kind: node.kind,
        display_name: node.name,
        group_id: node.groupID.length > 0 ? node.groupID : undefined,
        group_key: node.groupKey.length > 0 ? node.groupKey : undefined,
        subagent_role: node.subagentRole.length > 0 ? node.subagentRole : undefined,
        prompt_template: node.promptTemplate.length > 0 ? node.promptTemplate : undefined,
        input_fields: node.inputFields.map((field) => ({
          name: field.name,
          description: field.description,
        })),
        join_input_providers: node.joinInputProviders.map((provider) => ({
          input_name: provider.inputName,
          provider_edge_id: provider.providerEdgeID,
        })),
      }),
    ),
    transition_groups: graph.transitionGroups.map((group) => ({
      id: group.id,
      source_node_id: group.sourceNodeID,
      transition_id: group.transitionID,
      display_name: group.name,
    })),
    edges: graph.edges.map((edge) => ({
      id: edge.id,
      transition_group_id: edge.transitionGroupID,
      key: edge.key,
      target_node_id: edge.targetNodeID,
      requires_approval: edge.requiresApproval,
      context_mode: edge.contextMode,
      context_source: {
        kind: edge.contextSource.kind,
        node_key: edge.contextSource.nodeKey,
      },
    })),
  };
}

function workflowGraphMetadataPayload(metadata: WorkflowGraphMetadata | undefined): JsonObject | undefined {
  if (!metadata) {
    return undefined;
  }
  return {
    name: metadata.name,
    description: metadata.description,
  };
}

function workflowGraphSaveConfirmationPayload(
  confirmation: WorkflowGraphSaveConfirmation | undefined,
): JsonObject | undefined {
  if (!confirmation) {
    return undefined;
  }
  return {
    expected_removed_node_count: confirmation.expectedRemovedNodeCount,
    expected_removed_transition_group_count: confirmation.expectedRemovedTransitionGroupCount,
    expected_removed_edge_count: confirmation.expectedRemovedEdgeCount,
    expected_node_task_reference_count: confirmation.expectedNodeTaskReferenceCount,
    expected_edge_task_reference_count: confirmation.expectedEdgeTaskReferenceCount,
  };
}
