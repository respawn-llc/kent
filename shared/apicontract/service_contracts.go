package apicontract

import (
	"context"

	attentionpb "core/shared/protoapi/gen/kent/api/attention"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	capabilitypb "core/shared/protoapi/gen/kent/api/capability"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	chatcontextpb "core/shared/protoapi/gen/kent/api/chat_context"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	onboardingpb "core/shared/protoapi/gen/kent/api/onboarding"
	processpb "core/shared/protoapi/gen/kent/api/process"
	projectpb "core/shared/protoapi/gen/kent/api/project"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	promptcommandpb "core/shared/protoapi/gen/kent/api/prompt_command"
	runpromptpb "core/shared/protoapi/gen/kent/api/run_prompt"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	serverpb "core/shared/protoapi/gen/kent/api/server"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/serverapi"

	"google.golang.org/protobuf/types/known/emptypb"
)

// Interfaces in this package are in-process bindings for shared RPC routes.
// They intentionally describe method shapes only: no runtime handles,
// lifecycle orchestration, logging, timeout, or close policy belongs here.
type ApprovalViewService interface {
	ListPendingApprovalsBySession(ctx context.Context, req *promptpb.ListPendingRequest) (*promptpb.ListApprovalsSuccess, error)
}

type AskViewService interface {
	ListPendingAsksBySession(ctx context.Context, req *promptpb.ListPendingRequest) (*promptpb.ListQuestionsSuccess, error)
}

type AuthBootstrapService interface {
	GetBootstrapStatus(ctx context.Context, req *emptypb.Empty) (*authpb.BootstrapStatus, error)
	CompleteBootstrap(ctx context.Context, req *authpb.CompleteBootstrapRequest) (*authpb.BootstrapCompletion, error)
	AcknowledgeNoAuth(ctx context.Context, req *emptypb.Empty) (*authpb.NoAuthAcknowledgement, error)
}

type AuthStatusService interface {
	GetStatus(ctx context.Context, req *authpb.GetStatusRequest) (*authpb.Status, error)
}

type CapabilityFactsService interface {
	GetFacts(ctx context.Context, req *capabilitypb.GetFactsRequest) (*capabilitypb.Facts, error)
}

type ChatContextService interface {
	GetChatContext(ctx context.Context, req *chatcontextpb.GetRequest) (*chatcontextpb.GetSuccess, error)
}

type ChatMutationService interface {
	Steer(ctx context.Context, req *chatpb.SteerRequest) (*chatpb.InputMutationSuccess, error)
	Queue(ctx context.Context, req *chatpb.QueueRequest) (*chatpb.InputMutationSuccess, error)
	Compact(ctx context.Context, req *chatpb.CompactRequest) (*chatpb.CompactionMutationSuccess, error)
	SetGoal(ctx context.Context, req *runtimepb.GoalSetRequest) (*runtimepb.GoalSetSuccess, error)
}

type PromptCommandCatalogService interface {
	GetPromptCommandCatalog(ctx context.Context, req *promptcommandpb.GetCatalogRequest) (*promptcommandpb.Catalog, error)
}

type OnboardingFinalizeService interface {
	Finalize(ctx context.Context, req *onboardingpb.FinalizeRequest) (*onboardingpb.FinalizeSuccess, error)
}

type ProcessControlService interface {
	KillProcess(ctx context.Context, req *processpb.KillRequest) (*emptypb.Empty, error)
	GetInlineOutput(ctx context.Context, req *processpb.InlineOutputRequest) (*processpb.InlineOutputSuccess, error)
}

type ProcessViewService interface {
	ListProcesses(ctx context.Context, req *processpb.ListRequest) (*processpb.ListSuccess, error)
	GetProcess(ctx context.Context, req *processpb.GetRequest) (*processpb.GetSuccess, error)
}

type ProjectViewService interface {
	ListProjects(ctx context.Context, req *emptypb.Empty) (*projectpb.ProjectListSuccess, error)
	ListProjectHome(ctx context.Context, req *projectpb.ProjectHomeListRequest) (*projectpb.ProjectHomeListSuccess, error)
	ResolveProjectPath(ctx context.Context, req *projectpb.ResolvePathRequest) (*projectpb.ResolvePathSuccess, error)
	PlanWorkspaceBinding(ctx context.Context, req *projectpb.PlanWorkspaceBindingRequest) (*projectpb.PlanWorkspaceBindingSuccess, error)
	CreateProject(ctx context.Context, req *projectpb.CreateProjectRequest) (*projectpb.CreateProjectSuccess, error)
	GetProjectEdit(ctx context.Context, req *projectpb.ProjectEditGetRequest) (*projectpb.GetProjectEditSuccess, error)
	UpdateProject(ctx context.Context, req *projectpb.UpdateProjectRequest) (*projectpb.UpdateProjectSuccess, error)
	SetDefaultWorkspace(ctx context.Context, req *projectpb.SetDefaultWorkspaceRequest) (*projectpb.SetDefaultWorkspaceSuccess, error)
	ListProjectWorkspaces(ctx context.Context, req *projectpb.ProjectWorkspaceListRequest) (*projectpb.ListProjectWorkspacesSuccess, error)
	GetProjectWorkspace(ctx context.Context, req *projectpb.GetProjectWorkspaceRequest) (*projectpb.GetProjectWorkspaceSuccess, error)
	UnlinkWorkspaceFromProject(ctx context.Context, req *projectpb.UnlinkWorkspaceRequest) (*projectpb.UnlinkWorkspaceSuccess, error)
	DeleteProject(ctx context.Context, req *projectpb.DeleteProjectRequest) (*projectpb.DeleteProjectSuccess, error)
	AttachWorkspaceToProject(ctx context.Context, req *projectpb.AttachWorkspaceRequest) (*projectpb.AttachWorkspaceSuccess, error)
	RebindWorkspace(ctx context.Context, req *projectpb.RebindWorkspaceRequest) (*projectpb.RebindWorkspaceSuccess, error)
	GetProjectOverview(ctx context.Context, req *projectpb.GetOverviewRequest) (*projectpb.GetOverviewSuccess, error)
	ListSessionPage(ctx context.Context, req *projectpb.SessionPageRequest) (*projectpb.SessionPageSuccess, error)
}

type AttentionNotificationService interface {
	SubscribeAttentionNotifications(ctx context.Context, req serverapi.AttentionNotificationSubscribeRequest) (serverapi.AttentionNotificationSubscription, error)
	SubscribeSessionAttentionNotifications(ctx context.Context, req *attentionpb.SubscribeRequest) (serverapi.SessionAttentionNotificationSubscription, error)
}

type PromptControlService interface {
	AnswerPromptBatch(ctx context.Context, req *promptpb.AnswerBatchRequest) (*promptpb.AnswerBatchSuccess, error)
	SubscribeFollowUp(ctx context.Context, req *promptpb.FollowUpWatchRequest) (serverapi.PromptFollowUpSubscription, error)
}

type RunPromptService interface {
	RunPrompt(ctx context.Context, req serverapi.RunPromptRequest, progress serverapi.RunPromptProgressSink) (*runpromptpb.Success, error)
}

type ServerStatusService interface {
	GetReadiness(ctx context.Context, req *emptypb.Empty) (*serverpb.GetReadinessSuccess, error)
	GetUpdateStatus(ctx context.Context, req *emptypb.Empty) (*serverpb.GetUpdateStatusSuccess, error)
}

type RuntimeControlService interface {
	SetSessionName(ctx context.Context, req *runtimepb.SetSessionNameRequest) error
	AppendCommittedEntry(ctx context.Context, req *transcriptpb.AppendCommittedEntryRequest) error
	ShouldCompactBeforeUserMessage(ctx context.Context, req *runtimepb.ShouldCompactRequest) (*runtimepb.ShouldCompactSuccess, error)
	SubmitUserTurn(ctx context.Context, req *runtimepb.SubmitUserTurnRequest) (*runtimepb.SubmitUserTurnSuccess, error)
	SubmitUserShellCommand(ctx context.Context, req *runtimepb.ShellCommandRequest) error
	CompactContext(ctx context.Context, req *runtimepb.CompactContextRequest) error
	Interrupt(ctx context.Context, req *runtimepb.InterruptRequest) (*runtimepb.ReadModelUpdate, error)
	RecordPromptHistory(ctx context.Context, req *promptpb.RecordHistoryRequest) error
	ShowGoal(ctx context.Context, req *runtimepb.GoalShowRequest) (*runtimepb.GoalShowSuccess, error)
	PauseGoal(ctx context.Context, req *runtimepb.GoalMutationRequest) (*runtimepb.GoalMutationSuccess, error)
	ResumeGoal(ctx context.Context, req *runtimepb.GoalMutationRequest) (*runtimepb.GoalMutationSuccess, error)
	CompleteGoal(ctx context.Context, req *runtimepb.GoalMutationRequest) (*runtimepb.GoalMutationSuccess, error)
	ClearGoal(ctx context.Context, req *runtimepb.GoalClearRequest) (*runtimepb.GoalMutationSuccess, error)
}

type RuntimePendingWorkService interface {
	ListPendingWork(ctx context.Context, req *runtimepb.ListPendingWorkRequest) (*runtimepb.ListPendingWorkSuccess, error)
	RemovePendingWork(ctx context.Context, req *runtimepb.RemovePendingWorkRequest) (*runtimepb.RemovePendingWorkSuccess, error)
}

type RuntimeLiveControlService interface {
	LiveSteer(ctx context.Context, req *runtimepb.LiveSteerRequest) (*runtimepb.LiveSteerSuccess, error)
	LiveStop(ctx context.Context, req *runtimepb.LiveStopRequest) (*runtimepb.LiveStopSuccess, error)
	LiveWait(ctx context.Context, req *runtimepb.LiveWaitRequest) (*runtimepb.LiveWaitSuccess, error)
	LiveWatch(ctx context.Context, req *promptpb.LiveWatchRequest) (*promptpb.LiveWatchSuccess, error)
}

type SessionTranscriptService interface {
	SubscribeSessionTranscript(ctx context.Context, req *transcriptpb.SubscribeRequest) (serverapi.TranscriptSubscription, error)
}

type GoalObservationService interface {
	SubscribeGoalObservation(ctx context.Context, req *runtimepb.GoalObserveRequest) (serverapi.GoalObservationSubscription, error)
}

type SessionLaunchService interface {
	PlanSession(ctx context.Context, req *sessionlaunchpb.SessionPlanRequest) (*sessionlaunchpb.SessionPlanSuccess, error)
}

type ChatSettingsService interface {
	ReadChatSettings(ctx context.Context, req *chatsettingspb.ReadRequest) (*chatsettingspb.ReadSuccess, error)
	MutateChatSettings(ctx context.Context, req *chatsettingspb.MutationRequest) (*chatsettingspb.MutationSuccess, error)
}

type SessionLifecycleService interface {
	GetInitialInput(ctx context.Context, req *sessionlaunchpb.SessionInitialInputRequest) (*sessionlaunchpb.SessionInitialInputSuccess, error)
	PersistInputDraft(ctx context.Context, req *sessionlaunchpb.SessionPersistInputDraftRequest) (*emptypb.Empty, error)
	RetargetSessionWorkspace(ctx context.Context, req *sessionlaunchpb.SessionRetargetWorkspaceRequest) (*sessionlaunchpb.SessionRetargetWorkspaceSuccess, error)
	ResolveTransition(ctx context.Context, req *sessionlaunchpb.SessionResolveTransitionRequest) (*sessionlaunchpb.SessionDirective, error)
	ArchiveSession(ctx context.Context, req *sessionlaunchpb.SessionArchiveRequest) (*sessionlaunchpb.SessionArchiveSuccess, error)
	DeleteSession(ctx context.Context, req *sessionlaunchpb.SessionDeleteRequest) (*sessionlaunchpb.SessionDeleteSuccess, error)
}

type SessionRuntimeService interface {
	ActivateSessionRuntime(ctx context.Context, req serverapi.SessionRuntimeActivateRequest) (serverapi.SessionRuntimeAttachment, error)
	ReleaseSessionRuntime(ctx context.Context, req serverapi.SessionRuntimeReleaseRequest) (*sessionlaunchpb.SessionRuntimeReleaseSuccess, error)
}

type SessionViewService interface {
	GetSessionMainView(ctx context.Context, req *sessionpb.MainViewRequest) (*sessionpb.MainViewSuccess, error)
	GetSessionTranscriptPage(ctx context.Context, req *transcriptpb.PageRequest) (*transcriptpb.PageSuccess, error)
	GetLatestCommittedAssistantFinalAnswer(ctx context.Context, req *transcriptpb.LatestFinalAnswerRequest) (*transcriptpb.LatestFinalAnswerSuccess, error)
	SubscribeQuestionHistory(ctx context.Context, req *sessionpb.QuestionHistorySubscribeRequest) (serverapi.QuestionHistorySubscription, error)
}

type WorktreeService interface {
	GetWorktreeStatus(ctx context.Context, req *worktreepb.StatusRequest) (*worktreepb.StatusSuccess, error)
	ListWorktrees(ctx context.Context, req *worktreepb.ListRequest) (*worktreepb.ListSuccess, error)
	ListWorkspaceWorktrees(ctx context.Context, req *worktreepb.WorkspaceListRequest) (*worktreepb.WorkspaceListSuccess, error)
	ResolveWorktreeSelector(ctx context.Context, req *worktreepb.SelectorResolveRequest) (*worktreepb.SelectorResolveSuccess, error)
	PreviewWorktreeDelete(ctx context.Context, req *worktreepb.DeletePreviewRequest) (*worktreepb.DeletePreviewSuccess, error)
	ResolveWorktreeCreateTarget(ctx context.Context, req *worktreepb.CreateTargetResolveRequest) (*worktreepb.CreateTargetResolveSuccess, error)
	CreateWorktree(ctx context.Context, req *worktreepb.CreateRequest) (*worktreepb.CreateSuccess, error)
	EnterWorktree(ctx context.Context, req *worktreepb.EnterRequest) (*worktreepb.ScheduledAcknowledgement, error)
	LeaveWorktree(ctx context.Context, req *worktreepb.LeaveRequest) (*worktreepb.ScheduledAcknowledgement, error)
	DeleteWorktree(ctx context.Context, req *worktreepb.DeleteRequest) (*worktreepb.DeleteSuccess, error)
	SubscribeWorktreeSetup(ctx context.Context, req *worktreepb.SetupSubscribeRequest) (WorktreeSetupSubscription, error)
}

type WorktreeSetupSubscription interface {
	Next(context.Context) (*worktreepb.SetupEvent, error)
	Close() error
}

type WorkflowService interface {
	CreateWorkflow(ctx context.Context, req serverapi.WorkflowCreateRequest) (serverapi.WorkflowCreateResponse, error)
	CreateAndLinkWorkflowToProject(ctx context.Context, req serverapi.WorkflowCreateAndLinkProjectRequest) (serverapi.WorkflowCreateAndLinkProjectResponse, error)
	UpdateWorkflow(ctx context.Context, req serverapi.WorkflowUpdateRequest) (serverapi.WorkflowGetResponse, error)
	ListWorkflows(ctx context.Context, req serverapi.WorkflowListRequest) (serverapi.WorkflowListResponse, error)
	GetWorkflow(ctx context.Context, req serverapi.WorkflowGetRequest) (serverapi.WorkflowGetResponse, error)
	LinkWorkflowToProject(ctx context.Context, req serverapi.WorkflowLinkProjectRequest) (serverapi.WorkflowLinkProjectResponse, error)
	ListProjectWorkflowLinks(ctx context.Context, req serverapi.WorkflowListProjectLinksRequest) (serverapi.WorkflowListProjectLinksResponse, error)
	SetDefaultProjectWorkflowLink(ctx context.Context, req serverapi.WorkflowSetDefaultProjectLinkRequest) (serverapi.WorkflowSetDefaultProjectLinkResponse, error)
	UnlinkWorkflowFromProject(ctx context.Context, req serverapi.WorkflowUnlinkProjectRequest) (serverapi.WorkflowUnlinkProjectResponse, error)
	PreviewWorkflowDelete(ctx context.Context, req serverapi.WorkflowDeletePreviewRequest) (serverapi.WorkflowDeletePreviewResponse, error)
	DeleteWorkflow(ctx context.Context, req serverapi.WorkflowDeleteRequest) (serverapi.WorkflowDeleteResponse, error)
	ValidateWorkflow(ctx context.Context, req serverapi.WorkflowValidateRequest) (serverapi.WorkflowValidateResponse, error)
	ValidateWorkflowScriptPath(ctx context.Context, req serverapi.WorkflowScriptPathValidateRequest) (serverapi.WorkflowValidateResponse, error)
	ValidateWorkflowGraphDraft(ctx context.Context, req serverapi.WorkflowGraphValidateDraftRequest) (serverapi.WorkflowGraphValidateDraftResponse, error)
	DeriveWorkflowGraphWiring(ctx context.Context, req serverapi.WorkflowGraphDeriveWiringRequest) (serverapi.WorkflowGraphDeriveWiringResponse, error)
	PreviewWorkflowGraphSave(ctx context.Context, req serverapi.WorkflowGraphSavePreviewRequest) (serverapi.WorkflowGraphSavePreviewResponse, error)
	SaveWorkflowGraph(ctx context.Context, req serverapi.WorkflowGraphSaveRequest) (serverapi.WorkflowGraphSaveResponse, error)
	CreateWorkflowProjectLabel(ctx context.Context, req serverapi.WorkflowProjectLabelCreateRequest) (serverapi.WorkflowProjectLabelCreateResponse, error)
	ListWorkflowProjectLabels(ctx context.Context, req serverapi.WorkflowProjectLabelCatalogRequest) (serverapi.WorkflowProjectLabelCatalogResponse, error)
	RenameWorkflowProjectLabel(ctx context.Context, req serverapi.WorkflowProjectLabelRenameRequest) (serverapi.WorkflowProjectLabelRenameResponse, error)
	DeleteWorkflowProjectLabel(ctx context.Context, req serverapi.WorkflowProjectLabelDeleteRequest) (serverapi.WorkflowProjectLabelDeleteResponse, error)
	ReorderWorkflowProjectLabels(ctx context.Context, req serverapi.WorkflowProjectLabelReorderRequest) (serverapi.WorkflowProjectLabelReorderResponse, error)
	GetWorkflowTaskLabels(ctx context.Context, req serverapi.WorkflowTaskLabelsGetRequest) (serverapi.WorkflowTaskLabelsGetResponse, error)
	UpdateWorkflowTaskLabels(ctx context.Context, req serverapi.WorkflowTaskLabelsUpdateRequest) (serverapi.WorkflowTaskLabelsUpdateResponse, error)
	CreateWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskCreateRequest) (serverapi.WorkflowTaskCreateResponse, error)
	AddWorkflowTaskDependency(ctx context.Context, req serverapi.WorkflowTaskDependencyAddRequest) (serverapi.WorkflowTaskDependencyAddResponse, error)
	RemoveWorkflowTaskDependency(ctx context.Context, req serverapi.WorkflowTaskDependencyRemoveRequest) (serverapi.WorkflowTaskDependencyRemoveResponse, error)
	ListWorkflowTaskDependencies(ctx context.Context, req serverapi.WorkflowTaskDependencyListRequest) (serverapi.WorkflowTaskDependencyListResponse, error)
	UpdateWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskUpdateRequest) (serverapi.WorkflowTaskUpdateResponse, error)
	StartWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskStartRequest) (serverapi.WorkflowTaskStartResponse, error)
	InterruptWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskInterruptRequest) (serverapi.WorkflowTaskInterruptResponse, error)
	ResumeWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskResumeRequest) (serverapi.WorkflowTaskResumeResponse, error)
	ApproveWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskApproveRequest) (serverapi.WorkflowTaskApproveResponse, error)
	PreviewWorkflowTaskMove(ctx context.Context, req serverapi.WorkflowTaskMovePreviewRequest) (serverapi.WorkflowTaskMovePreviewResponse, error)
	MoveWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskMoveRequest) (serverapi.WorkflowTaskMoveResponse, error)
	CompleteWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskCompleteRequest) (serverapi.WorkflowTaskCompleteResponse, error)
	DeleteWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskDeleteRequest) error
	ListWorkflowAttention(ctx context.Context, req serverapi.WorkflowAttentionListRequest) (serverapi.WorkflowAttentionListResponse, error)
	ListWorkflowTaskAttention(ctx context.Context, req serverapi.WorkflowTaskAttentionListRequest) (serverapi.WorkflowTaskAttentionListResponse, error)
	AddWorkflowTaskComment(ctx context.Context, req serverapi.WorkflowTaskCommentAddRequest) (serverapi.WorkflowTaskCommentAddResponse, error)
	ListWorkflowTaskComments(ctx context.Context, req serverapi.WorkflowTaskOffsetPageRequest) (serverapi.WorkflowTaskCommentListResponse, error)
	ReplaceWorkflowTaskComment(ctx context.Context, req serverapi.WorkflowTaskCommentReplaceRequest) error
	DeleteWorkflowTaskComment(ctx context.Context, req serverapi.WorkflowTaskCommentDeleteRequest) error
	ListWorkflowTaskActivity(ctx context.Context, req serverapi.WorkflowTaskOffsetPageRequest) (serverapi.WorkflowTaskActivityListResponse, error)
	ListWorkflowTaskSessions(ctx context.Context, req serverapi.WorkflowTaskOffsetPageRequest) (serverapi.WorkflowTaskSessionListResponse, error)
	ListWorkflowTasks(ctx context.Context, req serverapi.WorkflowTaskListRequest) (serverapi.WorkflowTaskListResponse, error)
	GetWorkflowProjectTaskGroupCounts(ctx context.Context, req serverapi.WorkflowProjectTaskGroupCountsRequest) (serverapi.WorkflowProjectTaskGroupCountsResponse, error)
	SearchWorkflowTasks(ctx context.Context, req serverapi.TaskSearchRequest) (serverapi.TaskSearchResponse, error)
	SubscribeWorkflow(ctx context.Context, req serverapi.WorkflowSubscribeRequest) (serverapi.WorkflowSubscription, error)
	SubscribeWorkflowProject(ctx context.Context, req serverapi.WorkflowProjectSubscribeRequest) (serverapi.WorkflowProjectSubscription, error)
	GetWorkflowBoard(ctx context.Context, req serverapi.WorkflowBoardRequest) (serverapi.WorkflowBoardResponse, error)
	ListWorkflowBoardNodeCards(ctx context.Context, req serverapi.WorkflowBoardNodeCardsListRequest) (serverapi.WorkflowBoardNodeCardsListResponse, error)
	GetWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskGetRequest) (serverapi.WorkflowTaskGetResponse, error)
	ObserveWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskObservationRequest) (serverapi.WorkflowTaskObservationResponse, error)
}
