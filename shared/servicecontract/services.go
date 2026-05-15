package servicecontract

import (
	"context"

	"builder/shared/serverapi"
)

// Interfaces in this package are in-process bindings for shared RPC routes.
// They intentionally describe method shapes only: no runtime handles,
// lifecycle orchestration, logging, timeout, or close policy belongs here.
type ApprovalViewService interface {
	ListPendingApprovalsBySession(ctx context.Context, req serverapi.ApprovalListPendingBySessionRequest) (serverapi.ApprovalListPendingBySessionResponse, error)
}

type AskViewService interface {
	ListPendingAsksBySession(ctx context.Context, req serverapi.AskListPendingBySessionRequest) (serverapi.AskListPendingBySessionResponse, error)
}

type AuthBootstrapService interface {
	GetBootstrapStatus(ctx context.Context, req serverapi.AuthGetBootstrapStatusRequest) (serverapi.AuthGetBootstrapStatusResponse, error)
	CompleteBootstrap(ctx context.Context, req serverapi.AuthCompleteBootstrapRequest) (serverapi.AuthCompleteBootstrapResponse, error)
}

type AuthStatusService interface {
	GetAuthStatus(ctx context.Context, req serverapi.AuthStatusRequest) (serverapi.AuthStatusResponse, error)
}

type ProcessControlService interface {
	KillProcess(ctx context.Context, req serverapi.ProcessKillRequest) (serverapi.ProcessKillResponse, error)
	GetInlineOutput(ctx context.Context, req serverapi.ProcessInlineOutputRequest) (serverapi.ProcessInlineOutputResponse, error)
}

type ProcessOutputService interface {
	SubscribeProcessOutput(ctx context.Context, req serverapi.ProcessOutputSubscribeRequest) (serverapi.ProcessOutputSubscription, error)
}

type ProcessViewService interface {
	ListProcesses(ctx context.Context, req serverapi.ProcessListRequest) (serverapi.ProcessListResponse, error)
	GetProcess(ctx context.Context, req serverapi.ProcessGetRequest) (serverapi.ProcessGetResponse, error)
}

type ProjectViewService interface {
	ListProjects(ctx context.Context, req serverapi.ProjectListRequest) (serverapi.ProjectListResponse, error)
	ResolveProjectPath(ctx context.Context, req serverapi.ProjectResolvePathRequest) (serverapi.ProjectResolvePathResponse, error)
	PlanWorkspaceBinding(ctx context.Context, req serverapi.ProjectBindingPlanRequest) (serverapi.ProjectBindingPlanResponse, error)
	CreateProject(ctx context.Context, req serverapi.ProjectCreateRequest) (serverapi.ProjectCreateResponse, error)
	AttachWorkspaceToProject(ctx context.Context, req serverapi.ProjectAttachWorkspaceRequest) (serverapi.ProjectAttachWorkspaceResponse, error)
	RebindWorkspace(ctx context.Context, req serverapi.ProjectRebindWorkspaceRequest) (serverapi.ProjectRebindWorkspaceResponse, error)
	GetProjectOverview(ctx context.Context, req serverapi.ProjectGetOverviewRequest) (serverapi.ProjectGetOverviewResponse, error)
	ListSessionsByProject(ctx context.Context, req serverapi.SessionListByProjectRequest) (serverapi.SessionListByProjectResponse, error)
}

type PromptActivityService interface {
	SubscribePromptActivity(ctx context.Context, req serverapi.PromptActivitySubscribeRequest) (serverapi.PromptActivitySubscription, error)
}

type PromptControlService interface {
	AnswerAsk(ctx context.Context, req serverapi.AskAnswerRequest) error
	AnswerApproval(ctx context.Context, req serverapi.ApprovalAnswerRequest) error
}

type RunPromptService interface {
	RunPrompt(ctx context.Context, req serverapi.RunPromptRequest, progress serverapi.RunPromptProgressSink) (serverapi.RunPromptResponse, error)
}

type RuntimeControlService interface {
	SetSessionName(ctx context.Context, req serverapi.RuntimeSetSessionNameRequest) error
	SetThinkingLevel(ctx context.Context, req serverapi.RuntimeSetThinkingLevelRequest) error
	SetFastModeEnabled(ctx context.Context, req serverapi.RuntimeSetFastModeEnabledRequest) (serverapi.RuntimeSetFastModeEnabledResponse, error)
	SetReviewerEnabled(ctx context.Context, req serverapi.RuntimeSetReviewerEnabledRequest) (serverapi.RuntimeSetReviewerEnabledResponse, error)
	SetAutoCompactionEnabled(ctx context.Context, req serverapi.RuntimeSetAutoCompactionEnabledRequest) (serverapi.RuntimeSetAutoCompactionEnabledResponse, error)
	AppendLocalEntry(ctx context.Context, req serverapi.RuntimeAppendLocalEntryRequest) error
	ShouldCompactBeforeUserMessage(ctx context.Context, req serverapi.RuntimeShouldCompactBeforeUserMessageRequest) (serverapi.RuntimeShouldCompactBeforeUserMessageResponse, error)
	SubmitUserMessage(ctx context.Context, req serverapi.RuntimeSubmitUserMessageRequest) (serverapi.RuntimeSubmitUserMessageResponse, error)
	SubmitUserTurn(ctx context.Context, req serverapi.RuntimeSubmitUserTurnRequest) (serverapi.RuntimeSubmitUserTurnResponse, error)
	SubmitUserShellCommand(ctx context.Context, req serverapi.RuntimeSubmitUserShellCommandRequest) error
	CompactContext(ctx context.Context, req serverapi.RuntimeCompactContextRequest) error
	CompactContextForPreSubmit(ctx context.Context, req serverapi.RuntimeCompactContextForPreSubmitRequest) error
	HasQueuedUserWork(ctx context.Context, req serverapi.RuntimeHasQueuedUserWorkRequest) (serverapi.RuntimeHasQueuedUserWorkResponse, error)
	SubmitQueuedUserMessages(ctx context.Context, req serverapi.RuntimeSubmitQueuedUserMessagesRequest) (serverapi.RuntimeSubmitQueuedUserMessagesResponse, error)
	Interrupt(ctx context.Context, req serverapi.RuntimeInterruptRequest) error
	QueueUserMessage(ctx context.Context, req serverapi.RuntimeQueueUserMessageRequest) (serverapi.RuntimeQueueUserMessageResponse, error)
	DiscardQueuedUserMessage(ctx context.Context, req serverapi.RuntimeDiscardQueuedUserMessageRequest) (serverapi.RuntimeDiscardQueuedUserMessageResponse, error)
	RecordPromptHistory(ctx context.Context, req serverapi.RuntimeRecordPromptHistoryRequest) error
	ShowGoal(ctx context.Context, req serverapi.RuntimeGoalShowRequest) (serverapi.RuntimeGoalShowResponse, error)
	SetGoal(ctx context.Context, req serverapi.RuntimeGoalSetRequest) (serverapi.RuntimeGoalShowResponse, error)
	PauseGoal(ctx context.Context, req serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error)
	ResumeGoal(ctx context.Context, req serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error)
	CompleteGoal(ctx context.Context, req serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error)
	ClearGoal(ctx context.Context, req serverapi.RuntimeGoalClearRequest) (serverapi.RuntimeGoalShowResponse, error)
}

type SessionActivityService interface {
	SubscribeSessionActivity(ctx context.Context, req serverapi.SessionActivitySubscribeRequest) (serverapi.SessionActivitySubscription, error)
}

type SessionLaunchService interface {
	PlanSession(ctx context.Context, req serverapi.SessionPlanRequest) (serverapi.SessionPlanResponse, error)
}

type SessionLifecycleService interface {
	GetInitialInput(ctx context.Context, req serverapi.SessionInitialInputRequest) (serverapi.SessionInitialInputResponse, error)
	PersistInputDraft(ctx context.Context, req serverapi.SessionPersistInputDraftRequest) (serverapi.SessionPersistInputDraftResponse, error)
	RetargetSessionWorkspace(ctx context.Context, req serverapi.SessionRetargetWorkspaceRequest) (serverapi.SessionRetargetWorkspaceResponse, error)
	ResolveTransition(ctx context.Context, req serverapi.SessionResolveTransitionRequest) (serverapi.SessionResolveTransitionResponse, error)
}

type SessionRuntimeService interface {
	ActivateSessionRuntime(ctx context.Context, req serverapi.SessionRuntimeActivateRequest) (serverapi.SessionRuntimeActivateResponse, error)
	ReleaseSessionRuntime(ctx context.Context, req serverapi.SessionRuntimeReleaseRequest) (serverapi.SessionRuntimeReleaseResponse, error)
}

type SessionViewService interface {
	GetSessionMainView(ctx context.Context, req serverapi.SessionMainViewRequest) (serverapi.SessionMainViewResponse, error)
	GetSessionTranscriptPage(ctx context.Context, req serverapi.SessionTranscriptPageRequest) (serverapi.SessionTranscriptPageResponse, error)
	GetSessionCommittedTranscriptSuffix(ctx context.Context, req serverapi.SessionCommittedTranscriptSuffixRequest) (serverapi.SessionCommittedTranscriptSuffixResponse, error)
	GetRun(ctx context.Context, req serverapi.RunGetRequest) (serverapi.RunGetResponse, error)
}

type WorktreeService interface {
	ListWorktrees(ctx context.Context, req serverapi.WorktreeListRequest) (serverapi.WorktreeListResponse, error)
	ResolveWorktreeCreateTarget(ctx context.Context, req serverapi.WorktreeCreateTargetResolveRequest) (serverapi.WorktreeCreateTargetResolveResponse, error)
	CreateWorktree(ctx context.Context, req serverapi.WorktreeCreateRequest) (serverapi.WorktreeCreateResponse, error)
	SwitchWorktree(ctx context.Context, req serverapi.WorktreeSwitchRequest) (serverapi.WorktreeSwitchResponse, error)
	DeleteWorktree(ctx context.Context, req serverapi.WorktreeDeleteRequest) (serverapi.WorktreeDeleteResponse, error)
}

type WorkflowService interface {
	CreateWorkflow(ctx context.Context, req serverapi.WorkflowCreateRequest) (serverapi.WorkflowCreateResponse, error)
	UpdateWorkflow(ctx context.Context, req serverapi.WorkflowUpdateRequest) (serverapi.WorkflowGetResponse, error)
	ListWorkflows(ctx context.Context, req serverapi.WorkflowListRequest) (serverapi.WorkflowListResponse, error)
	GetWorkflow(ctx context.Context, req serverapi.WorkflowGetRequest) (serverapi.WorkflowGetResponse, error)
	AddWorkflowNode(ctx context.Context, req serverapi.WorkflowNodeAddRequest) (serverapi.WorkflowNodeAddResponse, error)
	AddWorkflowTransitionGroup(ctx context.Context, req serverapi.WorkflowTransitionGroupAddRequest) (serverapi.WorkflowTransitionGroupAddResponse, error)
	AddWorkflowEdge(ctx context.Context, req serverapi.WorkflowEdgeAddRequest) (serverapi.WorkflowEdgeAddResponse, error)
	LinkWorkflowToProject(ctx context.Context, req serverapi.WorkflowLinkProjectRequest) (serverapi.WorkflowLinkProjectResponse, error)
	ListProjectWorkflowLinks(ctx context.Context, req serverapi.WorkflowListProjectLinksRequest) (serverapi.WorkflowListProjectLinksResponse, error)
	SetDefaultProjectWorkflowLink(ctx context.Context, req serverapi.WorkflowSetDefaultProjectLinkRequest) (serverapi.WorkflowSetDefaultProjectLinkResponse, error)
	UnlinkWorkflowFromProject(ctx context.Context, req serverapi.WorkflowUnlinkProjectRequest) error
	ValidateWorkflow(ctx context.Context, req serverapi.WorkflowValidateRequest) (serverapi.WorkflowValidateResponse, error)
	CreateWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskCreateRequest) (serverapi.WorkflowTaskCreateResponse, error)
	StartWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskStartRequest) (serverapi.WorkflowTaskStartResponse, error)
	ApproveWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskApproveRequest) (serverapi.WorkflowTaskApproveResponse, error)
	CancelWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskCancelRequest) error
	AddWorkflowTaskComment(ctx context.Context, req serverapi.WorkflowTaskCommentAddRequest) (serverapi.WorkflowTaskCommentAddResponse, error)
	ListWorkflowTaskComments(ctx context.Context, req serverapi.WorkflowTaskCommentListRequest) (serverapi.WorkflowTaskCommentListResponse, error)
	ReplaceWorkflowTaskComment(ctx context.Context, req serverapi.WorkflowTaskCommentReplaceRequest) error
	DeleteWorkflowTaskComment(ctx context.Context, req serverapi.WorkflowTaskCommentDeleteRequest) error
	GetWorkflowBoard(ctx context.Context, req serverapi.WorkflowBoardRequest) (serverapi.WorkflowBoardResponse, error)
	GetWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskGetRequest) (serverapi.WorkflowTaskGetResponse, error)
}
