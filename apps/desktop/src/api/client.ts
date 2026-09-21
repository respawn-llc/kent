import type { AttentionNotificationEventHandler } from "./attentionNotifications";
import { attentionNotificationRpcHandler } from "./attentionNotificationSubscription";
import { create, operationName } from "@app/server-api-contract";
import {
  ReadinessSeverity,
  ServerService,
  type Readiness,
} from "@app/server-api-contract/gen/kent/api/server/server_pb";
import type { ApiService, ApiSubscription } from "./apiService";
import type { ChatApi } from "./chat";
import { createChatApi } from "./chat";
import { listSessionPage as listSessionCatalogPage } from "./clientCatalog";
import { parseRpcResponse as parse } from "./clientParse";
import * as taskLifecycle from "./clientTaskLifecycle";
import * as taskDependencies from "./clientTaskDependencies";
import * as taskDetail from "./clientTaskDetail";
import * as promptAnswers from "./clientPromptAnswers";
import { listPendingPrompts } from "./clientPendingPrompts";
import * as taskSearch from "./clientTaskSearch";
import * as worktree from "./clientWorktree";
import * as project from "./clientProject";
import * as workflow from "./clientWorkflow";
import * as processes from "./clientProcesses";
import type {
  BoardNodeCardsInput,
  PromptAnswerBatchInput,
  PromptAnswerBatchResponse,
  TaskEditInput,
  TaskMoveInput,
  TaskResumeInput,
  TaskStartInput,
  TaskMutationInput,
  ProjectTaskGroupCountsInput,
  TaskListInput,
  WorkflowCreateAndLinkInput,
  WorkflowCreateInput,
  WorkflowDeleteInput,
  WorkflowGraphDeriveWiringInput,
  WorkflowGraphSaveInput,
  WorkflowGraphSavePreviewInput,
  WorkflowGraphValidateDraftInput,
  WorkflowScriptPathValidateInput,
  WorkflowListInput,
  WorkflowProjectLinkInput,
} from "./clientInputs";
import { compactJsonObject, emptyJsonObject } from "./json";
import type { SetupOperationID } from "./setupOperationID";
import type * as worktreeModels from "./schemas/worktree";
import { subscribeWorktreeSetup, type WorktreeSetupEventHandler } from "./worktreeSetup";
import type {
  ActivityPage,
  AttentionPage,
  BoardNodeCardsPage,
  CommentPage,
  CreatedTaskSummary,
  PendingAsk,
  ProjectWorkflowLink,
  ProjectPage,
  ServerReadiness,
  SessionCatalogPage,
  SessionCategory,
  TaskAttention,
  TaskComment,
  TaskDetail,
  TaskDependencyDirection,
  TaskDependencyListResponse,
  TaskDependencyMutationResponse,
  TaskApproveResponse,
  TaskMoveResponse,
  TaskMovePreviewResponse,
  TaskResumeResponse,
  TaskStartResponse,
  WorkflowBoard,
  WorkflowDeleteImpact,
  WorkflowDeleteResponse,
  WorkflowDefinition,
  WorkflowDerivedWiring,
  WorkflowGraphSavePreview,
  WorkflowGraphSaveResult,
  WorkflowGraphValidateDraftResult,
  WorkflowPage,
  WorkflowRecord,
  WorkflowValidation,
} from "./models";
import type {
  ProjectLabel,
  ProjectLabelCatalog,
  ProjectTaskGroupCounts,
  TaskLabelAssignment,
  TaskListPage,
} from "./workflowLabels";
import type { BoardFilter } from "./workflowBoardFilters";
import { ContractError } from "./errors";
import { requireUnarySuccess } from "./protobufRpc";
import { workflowIDSchema } from "./schemas/workflowID";
import { attentionPageSchema, taskUpdateResponseSchema } from "./schemas/workflowBoard";
import type { DescriptorRpcTransport } from "./transport";
import type { WorkflowProjectEventHandler } from "./workflowProjectEvents";
import type { TaskSearchInput, TaskSearchResponse } from "./taskSearch";
import { workflowProjectEventRpcHandler } from "./workflowProjectEvents";
import { projectEvents, type ProjectOverflowReporter } from "./projectEvents";
import * as workflowBoard from "./clientWorkflowBoard";
import * as workflowLabels from "./clientWorkflowLabels";

export const guiTaskCommentAuthor = "user";

export class ApiClient implements ApiService {
  readonly #transport: DescriptorRpcTransport;

  constructor(
    transport: DescriptorRpcTransport,
    private readonly reportProjectOverflow: ProjectOverflowReporter,
  ) {
    this.#transport = transport;
    this.chat = createChatApi(transport);
  }

  readonly chat: ChatApi;

  listProcesses = async (projectID: string) => processes.listProcesses(this.#transport, projectID);
  killProcess = async (processID: string) => processes.killProcess(this.#transport, processID);

  async getReadiness(): Promise<ServerReadiness> {
    const method = ServerService.method.getReadiness;
    const success = requireUnarySuccess(
      method,
      await this.#transport.callDescriptor(method, create(method.input)),
    );
    if (success.readiness === undefined) {
      throw new ContractError(`${operationName(method)} response did not match GUI contract.`);
    }
    return projectReadiness(success.readiness);
  }

  async listProjects(pageToken: string | null): Promise<ProjectPage> {
    return project.listProjectHome(this.#transport, pageToken);
  }

  async listSessionPage(
    projectID: string,
    category: SessionCategory,
    offset: number,
  ): Promise<SessionCatalogPage> {
    return listSessionCatalogPage(this.#transport, projectID, category, offset);
  }

  listWorkspaces = async (projectID: string, offset: number) =>
    project.listWorkspaces(this.#transport, projectID, offset);
  getProjectWorkspace = async (
    projectID: string,
    selector: Readonly<{ workspaceID: string } | { workspaceRoot: string }>,
  ) => project.getProjectWorkspace(this.#transport, projectID, selector);
  getProjectEdit = async (projectID: string) => project.getProjectEdit(this.#transport, projectID);
  planWorkspace = async (path: string) => project.planWorkspace(this.#transport, path);
  createProject = async (displayName: string, projectKey: string, workspaceRoot: string) =>
    project.createProject(this.#transport, displayName, projectKey, workspaceRoot);
  attachWorkspace = async (projectID: string, workspaceRoot: string) =>
    project.attachWorkspace(this.#transport, projectID, workspaceRoot);
  updateProject = async (projectID: string, displayName: string, projectKey = "") =>
    project.updateProject(this.#transport, projectID, displayName, projectKey);
  setDefaultWorkspace = async (projectID: string, workspaceID: string) =>
    project.setDefaultWorkspace(this.#transport, projectID, workspaceID);
  unlinkWorkspace = async (projectID: string, workspaceID: string) =>
    project.unlinkWorkspace(this.#transport, projectID, workspaceID);
  deleteProject = async (projectID: string) => project.deleteProject(this.#transport, projectID);

  async listProjectLabels(projectID: string): Promise<ProjectLabelCatalog> {
    return workflowLabels.listProjectLabels(this.#transport, projectID);
  }

  async createProjectLabel(projectID: string, name: string): Promise<ProjectLabel> {
    return workflowLabels.createProjectLabel(this.#transport, projectID, name);
  }

  async reorderProjectLabels(projectID: string, labelIDs: readonly string[]): Promise<ProjectLabelCatalog> {
    return workflowLabels.reorderProjectLabels(this.#transport, projectID, labelIDs);
  }

  async renameProjectLabel(projectID: string, labelID: string, name: string): Promise<ProjectLabel> {
    return workflowLabels.renameProjectLabel(this.#transport, projectID, labelID, name);
  }

  async deleteProjectLabel(projectID: string, labelID: string): Promise<string> {
    return workflowLabels.deleteProjectLabel(this.#transport, projectID, labelID);
  }

  async getTaskLabels(taskID: string): Promise<TaskLabelAssignment> {
    return workflowLabels.getTaskLabels(this.#transport, taskID);
  }

  async updateTaskLabels(
    taskID: string,
    addLabelIDs: readonly string[],
    removeLabelIDs: readonly string[],
  ): Promise<TaskLabelAssignment> {
    return workflowLabels.updateTaskLabels(this.#transport, taskID, addLabelIDs, removeLabelIDs);
  }

  async getBoard(
    projectID: string,
    workflowID: string | undefined,
    filter: BoardFilter,
  ): Promise<WorkflowBoard> {
    return workflowBoard.getBoard(this.#transport, projectID, workflowID, filter);
  }

  async getWorkflow(workflowID: string): Promise<WorkflowDefinition> {
    return workflow.getWorkflow(this.#transport, workflowID);
  }

  async listWorkflows(input: WorkflowListInput = {}): Promise<WorkflowPage> {
    return workflow.listWorkflows(this.#transport, input);
  }

  async createWorkflow(input: WorkflowCreateInput): Promise<WorkflowRecord> {
    return workflow.createWorkflow(this.#transport, input);
  }

  async createAndLinkWorkflowToProject(
    input: WorkflowCreateAndLinkInput,
  ): Promise<Readonly<{ workflow: WorkflowRecord; link: ProjectWorkflowLink }>> {
    return workflow.createAndLinkWorkflowToProject(this.#transport, input);
  }

  async linkWorkflowToProject(input: WorkflowProjectLinkInput): Promise<ProjectWorkflowLink> {
    return workflow.linkWorkflowToProject(this.#transport, input);
  }

  async validateWorkflow(
    workflowID: string,
    mode: "draft" | "task_creation" | "execution",
  ): Promise<WorkflowValidation> {
    return workflow.validateWorkflow(this.#transport, workflowID, mode);
  }

  async validateWorkflowScriptPath(input: WorkflowScriptPathValidateInput): Promise<WorkflowValidation> {
    return workflow.validateWorkflowScriptPath(this.#transport, input);
  }

  async validateWorkflowGraphDraft(
    input: WorkflowGraphValidateDraftInput,
  ): Promise<WorkflowGraphValidateDraftResult> {
    return workflow.validateWorkflowGraphDraft(this.#transport, input);
  }

  async deriveWorkflowGraphWiring(input: WorkflowGraphDeriveWiringInput): Promise<WorkflowDerivedWiring> {
    return workflow.deriveWorkflowGraphWiring(this.#transport, input);
  }

  async previewWorkflowGraphSave(input: WorkflowGraphSavePreviewInput): Promise<WorkflowGraphSavePreview> {
    return workflow.previewWorkflowGraphSave(this.#transport, input);
  }

  async saveWorkflowGraph(input: WorkflowGraphSaveInput): Promise<WorkflowGraphSaveResult> {
    return workflow.saveWorkflowGraph(this.#transport, input);
  }

  async previewWorkflowDelete(workflowID: string): Promise<WorkflowDeleteImpact> {
    return workflow.previewWorkflowDelete(this.#transport, workflowID);
  }

  async deleteWorkflow(input: WorkflowDeleteInput): Promise<WorkflowDeleteResponse> {
    return workflow.deleteWorkflow(this.#transport, input);
  }

  async listProjectWorkflowLinks(projectID: string): Promise<readonly ProjectWorkflowLink[]> {
    return workflow.listProjectWorkflowLinks(this.#transport, projectID);
  }

  async listBoardNodeCards(input: BoardNodeCardsInput): Promise<BoardNodeCardsPage> {
    return workflowBoard.listBoardNodeCards(this.#transport, input);
  }

  async listAttention(pageToken: string): Promise<AttentionPage> {
    return parse(
      "workflow.attention.list",
      attentionPageSchema,
      await this.#transport.call(
        "workflow.attention.list",
        compactJsonObject({
          page_size: 40,
          page_token: pageToken,
        }),
      ),
    );
  }

  async listTaskAttention(taskID: string): Promise<TaskAttention> {
    return taskDetail.listTaskAttention(this.#transport, taskID);
  }

  async createTask(input: TaskMutationInput): Promise<CreatedTaskSummary> {
    return workflowLabels.createTask(this.#transport, input);
  }

  async addTaskDependency(
    blockerTaskID: string,
    blockedTaskID: string,
  ): Promise<TaskDependencyMutationResponse> {
    return taskDependencies.addTaskDependency(this.#transport, blockerTaskID, blockedTaskID);
  }

  async removeTaskDependency(
    blockerTaskID: string,
    blockedTaskID: string,
  ): Promise<TaskDependencyMutationResponse> {
    return taskDependencies.removeTaskDependency(this.#transport, blockerTaskID, blockedTaskID);
  }

  async listTaskDependencies(
    taskID: string,
    direction?: TaskDependencyDirection,
  ): Promise<TaskDependencyListResponse> {
    return taskDependencies.listTaskDependencies(this.#transport, taskID, direction);
  }

  async listTasks(input: TaskListInput): Promise<TaskListPage> {
    return workflowLabels.listTasks(this.#transport, input);
  }

  async getProjectTaskGroupCounts(input: ProjectTaskGroupCountsInput): Promise<ProjectTaskGroupCounts> {
    return workflowLabels.getProjectTaskGroupCounts(this.#transport, input);
  }

  async searchTasks(input: TaskSearchInput, signal?: AbortSignal): Promise<TaskSearchResponse> {
    return taskSearch.searchTasks(this.#transport, input, signal);
  }

  async updateTask(input: TaskEditInput): Promise<string> {
    const response = parse(
      "workflow.task.update",
      taskUpdateResponseSchema,
      await this.#transport.call(
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

  async startTask(input: TaskStartInput): Promise<TaskStartResponse> {
    return taskLifecycle.startTask(this.#transport, input);
  }

  async moveTask(input: TaskMoveInput): Promise<TaskMoveResponse> {
    return taskLifecycle.moveTask(this.#transport, input);
  }

  async previewMoveTask(taskID: string, targetNodeID: string): Promise<TaskMovePreviewResponse> {
    return taskLifecycle.previewMoveTask(this.#transport, taskID, targetNodeID);
  }

  async interruptTask(taskID: string, sessionID?: string): Promise<void> {
    await this.#transport.call(
      "workflow.task.interrupt",
      compactJsonObject({ task_id: taskID, session_id: sessionID }),
    );
  }

  async resumeTask(input: TaskResumeInput): Promise<TaskResumeResponse> {
    return taskLifecycle.resumeTask(this.#transport, input);
  }

  async approveApproval(approvalID: string): Promise<TaskApproveResponse> {
    return taskLifecycle.approveApproval(this.#transport, approvalID);
  }

  async deleteTask(taskID: string): Promise<void> {
    await this.#transport.call("workflow.task.delete", { task_id: taskID });
  }

  async getTask(taskID: string): Promise<TaskDetail> {
    return taskDetail.getTask(this.#transport, taskID);
  }

  async listTaskActivity(taskID: string, offset: number): Promise<ActivityPage> {
    return taskDetail.listTaskActivity(this.#transport, taskID, offset);
  }

  async listTaskComments(taskID: string, offset: number): Promise<CommentPage> {
    return taskDetail.listTaskComments(this.#transport, taskID, offset);
  }

  async addComment(taskID: string, body: string): Promise<TaskComment> {
    return taskDetail.addComment(this.#transport, taskID, body, guiTaskCommentAuthor);
  }

  async replaceComment(commentID: string, body: string): Promise<void> {
    await this.#transport.call("workflow.task.comment.replace", { comment_id: commentID, body });
  }

  async deleteComment(commentID: string): Promise<void> {
    await this.#transport.call("workflow.task.comment.delete", { comment_id: commentID });
  }

  async answerPromptBatch(input: PromptAnswerBatchInput): Promise<PromptAnswerBatchResponse> {
    return promptAnswers.answerPromptBatch(this.#transport, input);
  }

  async listPendingAsks(sessionID: string): Promise<readonly PendingAsk[]> {
    return taskDetail.listPendingAsks(this.#transport, { sessionID });
  }

  async listPendingPrompts(sessionID: string) {
    return listPendingPrompts(this.#transport, { sessionID });
  }

  subscribeProject(projectID: string) {
    return projectEvents(this.#transport, projectID, this.reportProjectOverflow);
  }

  subscribeWorkflow(workflowID: string, handler: WorkflowProjectEventHandler): ApiSubscription {
    return this.#transport.subscribe(
      "workflow.subscribe",
      { workflow_id: workflowIDSchema.parse(workflowID) },
      workflowProjectEventRpcHandler("workflow.event", handler),
    );
  }

  subscribeAttentionNotifications(handler: AttentionNotificationEventHandler): ApiSubscription {
    return this.#transport.subscribe(
      "attention.notification.subscribe",
      emptyJsonObject,
      attentionNotificationRpcHandler(handler),
    );
  }

  getWorktreeStatus = async (sessionID: string) => worktree.getWorktreeStatus(this.#transport, sessionID);
  listWorktrees = async (sessionID: string) => worktree.listWorktrees(this.#transport, sessionID);
  resolveWorktreeSelector = async (sessionID: string, selector: string) =>
    worktree.resolveWorktreeSelector(this.#transport, sessionID, selector);
  resolveWorktreeCreateTarget = async (sessionID: string, target: string) =>
    worktree.resolveWorktreeCreateTarget(this.#transport, sessionID, target);
  previewWorktreeDelete = async (sessionID: string, selector: string) =>
    worktree.previewWorktreeDelete(this.#transport, sessionID, selector);
  createWorktree = async (input: worktreeModels.WorktreeCreateInput) =>
    worktree.createWorktree(this.#transport, input);
  switchWorktree = async (sessionID: string, operation: worktreeModels.WorktreeTransition) =>
    worktree.switchWorktree(this.#transport, sessionID, operation);
  deleteWorktree = async (
    sessionID: string,
    preview: worktreeModels.WorktreeDeletePreview,
    confirmation: worktreeModels.WorktreeDeleteConfirmationChoice,
  ) => worktree.deleteWorktree(this.#transport, sessionID, preview, confirmation);
  subscribeWorktreeSetup = (setupOperationID: SetupOperationID, handler: WorktreeSetupEventHandler) =>
    subscribeWorktreeSetup(this.#transport, setupOperationID, handler);
}

function projectReadiness(readiness: Readiness): ServerReadiness {
  return {
    ready: readiness.ready,
    serverID: readiness.serverId,
    serverVersion: readiness.serverVersion,
    protocolVersion: readiness.protocolVersion,
    authReady: readiness.authReady,
    authRequired: readiness.authRequired,
    endpoint: readiness.endpoint,
    subagentRoles: readiness.subagentRoles.map((role) => ({ name: role.name })),
    causes: readiness.causes.map((cause) => ({
      code: cause.code,
      severity: projectReadinessSeverity(cause.severity),
      ...(cause.summary === undefined ? {} : { summary: cause.summary }),
      ...(cause.nextAction === undefined ? {} : { nextAction: cause.nextAction }),
      ...(cause.diagnosticId === undefined ? {} : { diagnosticID: cause.diagnosticId }),
    })),
  };
}

function projectReadinessSeverity(severity: ReadinessSeverity): string {
  if (severity === ReadinessSeverity.ERROR) {
    return "error";
  }
  throw new ContractError("server readiness response contained an unsupported cause severity.");
}
