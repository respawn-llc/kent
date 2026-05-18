package transport

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"builder/shared/client"
	"builder/shared/protocol"
	"builder/shared/serverapi"
)

var gatewayUnaryHandlerEntries = map[string]gatewayUnaryHandler{
	protocol.MethodHandshake: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		params, err := decodeParams[protocol.HandshakeRequest](req.Params)
		if err != nil {
			return protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidParams, err.Error())
		}
		if err := params.Validate(); err != nil {
			return protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidParams, err.Error())
		}
		if params.ProtocolVersion != protocol.Version {
			return protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidRequest, fmt.Sprintf("unsupported protocol version %q; server requires %q, upgrade the older Builder process", params.ProtocolVersion, protocol.Version))
		}
		state.handshakeDone = true
		return protocol.NewSuccessResponse(req.ID, protocol.HandshakeResponse{Identity: g.identity})
	},
	protocol.MethodAuthGetBootstrapStatus: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.AuthGetBootstrapStatusRequest) (serverapi.AuthGetBootstrapStatusResponse, error) {
			bootstrapClient := g.deps.AuthBootstrapClient()
			if bootstrapClient == nil {
				return serverapi.AuthGetBootstrapStatusResponse{}, serverapi.ErrServerAuthRequired
			}
			return bootstrapClient.GetAuthBootstrapStatus(ctx, params)
		})
	},
	protocol.MethodAuthCompleteBootstrap: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.AuthCompleteBootstrapRequest) (serverapi.AuthCompleteBootstrapResponse, error) {
			bootstrapClient := g.deps.AuthBootstrapClient()
			if bootstrapClient == nil {
				return serverapi.AuthCompleteBootstrapResponse{}, serverapi.ErrServerAuthRequired
			}
			return bootstrapClient.CompleteAuthBootstrap(ctx, params)
		})
	},
	protocol.MethodAuthGetStatus: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.AuthStatusRequest) (serverapi.AuthStatusResponse, error) {
			statusClient := g.deps.AuthStatusClient()
			if statusClient == nil {
				return serverapi.AuthStatusResponse{}, serverapi.ErrServerAuthRequired
			}
			return statusClient.GetAuthStatus(ctx, params)
		})
	},
	protocol.MethodAttachProject: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params protocol.AttachProjectRequest) (protocol.AttachResponse, error) {
			if err := params.Validate(); err != nil {
				return protocol.AttachResponse{}, err
			}
			if err := g.deps.ProjectExists(ctx, params.ProjectID); err != nil {
				return protocol.AttachResponse{}, err
			}
			attachedWorkspaceID, attachedRoot, err := g.resolveAttachedProjectWorkspace(ctx, params.ProjectID, params.WorkspaceID, params.WorkspaceRoot)
			if err != nil {
				return protocol.AttachResponse{}, err
			}
			state.attachedProject = params.ProjectID
			state.attachedWorkspaceID = attachedWorkspaceID
			state.attachedWorkspaceRoot = attachedRoot
			state.attachedSession = ""
			return protocol.AttachResponse{Kind: "project", ProjectID: params.ProjectID, WorkspaceID: attachedWorkspaceID, WorkspaceRoot: attachedRoot}, nil
		})
	},
	protocol.MethodAttachSession: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params protocol.AttachSessionRequest) (protocol.AttachResponse, error) {
			if err := params.Validate(); err != nil {
				return protocol.AttachResponse{}, err
			}
			state.attachedWorkspaceID = ""
			state.attachedWorkspaceRoot = ""
			state.attachedSession = params.SessionID
			return protocol.AttachResponse{Kind: "session", SessionID: params.SessionID}, nil
		})
	},
	protocol.MethodProjectList: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.ProjectListRequest) (serverapi.ProjectListResponse, error) {
			return g.deps.ProjectViewClient().ListProjects(ctx, params)
		})
	},
	protocol.MethodProjectResolvePath: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.ProjectResolvePathRequest) (serverapi.ProjectResolvePathResponse, error) {
			return g.deps.ProjectViewClient().ResolveProjectPath(ctx, params)
		})
	},
	protocol.MethodProjectPlanWorkspaceBinding: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.ProjectBindingPlanRequest) (serverapi.ProjectBindingPlanResponse, error) {
			return g.deps.ProjectViewClient().PlanWorkspaceBinding(ctx, params)
		})
	},
	protocol.MethodProjectCreate: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.ProjectCreateRequest) (serverapi.ProjectCreateResponse, error) {
			return g.deps.ProjectViewClient().CreateProject(ctx, params)
		})
	},
	protocol.MethodProjectAttachWorkspace: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.ProjectAttachWorkspaceRequest) (serverapi.ProjectAttachWorkspaceResponse, error) {
			return g.deps.ProjectViewClient().AttachWorkspaceToProject(ctx, params)
		})
	},
	protocol.MethodProjectRebindWorkspace: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.ProjectRebindWorkspaceRequest) (serverapi.ProjectRebindWorkspaceResponse, error) {
			return g.deps.ProjectViewClient().RebindWorkspace(ctx, params)
		})
	},
	protocol.MethodProjectGetOverview: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.ProjectGetOverviewRequest) (serverapi.ProjectGetOverviewResponse, error) {
			return g.deps.ProjectViewClient().GetProjectOverview(ctx, params)
		})
	},
	protocol.MethodSessionListByProject: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.SessionListByProjectRequest) (serverapi.SessionListByProjectResponse, error) {
			return g.deps.ProjectViewClient().ListSessionsByProject(ctx, params)
		})
	},
	protocol.MethodWorkflowCreate: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.WorkflowCreateRequest) (serverapi.WorkflowCreateResponse, error) {
			return g.deps.WorkflowClient().CreateWorkflow(ctx, params)
		})
	},
	protocol.MethodWorkflowUpdate: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.WorkflowUpdateRequest) (serverapi.WorkflowGetResponse, error) {
			return g.deps.WorkflowClient().UpdateWorkflow(ctx, params)
		})
	},
	protocol.MethodWorkflowList: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.WorkflowListRequest) (serverapi.WorkflowListResponse, error) {
			return g.deps.WorkflowClient().ListWorkflows(ctx, params)
		})
	},
	protocol.MethodWorkflowGet: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.WorkflowGetRequest) (serverapi.WorkflowGetResponse, error) {
			return g.deps.WorkflowClient().GetWorkflow(ctx, params)
		})
	},
	protocol.MethodWorkflowAddNode: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.WorkflowNodeAddRequest) (serverapi.WorkflowNodeAddResponse, error) {
			return g.deps.WorkflowClient().AddWorkflowNode(ctx, params)
		})
	},
	protocol.MethodWorkflowAddTransitionGroup: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.WorkflowTransitionGroupAddRequest) (serverapi.WorkflowTransitionGroupAddResponse, error) {
			return g.deps.WorkflowClient().AddWorkflowTransitionGroup(ctx, params)
		})
	},
	protocol.MethodWorkflowAddEdge: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.WorkflowEdgeAddRequest) (serverapi.WorkflowEdgeAddResponse, error) {
			return g.deps.WorkflowClient().AddWorkflowEdge(ctx, params)
		})
	},
	protocol.MethodWorkflowLinkProject: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.WorkflowLinkProjectRequest) (serverapi.WorkflowLinkProjectResponse, error) {
			return g.deps.WorkflowClient().LinkWorkflowToProject(ctx, params)
		})
	},
	protocol.MethodWorkflowListProjectLinks: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.WorkflowListProjectLinksRequest) (serverapi.WorkflowListProjectLinksResponse, error) {
			return g.deps.WorkflowClient().ListProjectWorkflowLinks(ctx, params)
		})
	},
	protocol.MethodWorkflowSetDefaultProjectLink: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.WorkflowSetDefaultProjectLinkRequest) (serverapi.WorkflowSetDefaultProjectLinkResponse, error) {
			return g.deps.WorkflowClient().SetDefaultProjectWorkflowLink(ctx, params)
		})
	},
	protocol.MethodWorkflowUnlinkProject: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.WorkflowUnlinkProjectRequest) (struct{}, error) {
			return struct{}{}, g.deps.WorkflowClient().UnlinkWorkflowFromProject(ctx, params)
		})
	},
	protocol.MethodWorkflowValidate: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.WorkflowValidateRequest) (serverapi.WorkflowValidateResponse, error) {
			return g.deps.WorkflowClient().ValidateWorkflow(ctx, params)
		})
	},
	protocol.MethodWorkflowTaskCreate: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.WorkflowTaskCreateRequest) (serverapi.WorkflowTaskCreateResponse, error) {
			return g.deps.WorkflowClient().CreateWorkflowTask(ctx, params)
		})
	},
	protocol.MethodWorkflowTaskStart: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.WorkflowTaskStartRequest) (serverapi.WorkflowTaskStartResponse, error) {
			return g.deps.WorkflowClient().StartWorkflowTask(ctx, params)
		})
	},
	protocol.MethodWorkflowTaskCancel: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.WorkflowTaskCancelRequest) (struct{}, error) {
			return struct{}{}, g.deps.WorkflowClient().CancelWorkflowTask(ctx, params)
		})
	},
	protocol.MethodWorkflowTaskCommentAdd: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.WorkflowTaskCommentAddRequest) (serverapi.WorkflowTaskCommentAddResponse, error) {
			return g.deps.WorkflowClient().AddWorkflowTaskComment(ctx, params)
		})
	},
	protocol.MethodWorkflowTaskCommentList: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.WorkflowTaskCommentListRequest) (serverapi.WorkflowTaskCommentListResponse, error) {
			return g.deps.WorkflowClient().ListWorkflowTaskComments(ctx, params)
		})
	},
	protocol.MethodWorkflowTaskCommentReplace: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.WorkflowTaskCommentReplaceRequest) (struct{}, error) {
			return struct{}{}, g.deps.WorkflowClient().ReplaceWorkflowTaskComment(ctx, params)
		})
	},
	protocol.MethodWorkflowTaskCommentDelete: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.WorkflowTaskCommentDeleteRequest) (struct{}, error) {
			return struct{}{}, g.deps.WorkflowClient().DeleteWorkflowTaskComment(ctx, params)
		})
	},
	protocol.MethodWorkflowBoardGet: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.WorkflowBoardRequest) (serverapi.WorkflowBoardResponse, error) {
			return g.deps.WorkflowClient().GetWorkflowBoard(ctx, params)
		})
	},
	protocol.MethodWorkflowTaskGet: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.WorkflowTaskGetRequest) (serverapi.WorkflowTaskGetResponse, error) {
			return g.deps.WorkflowClient().GetWorkflowTask(ctx, params)
		})
	},
	protocol.MethodSessionPlan: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.SessionPlanRequest) (serverapi.SessionPlanResponse, error) {
			launchClient, err := g.sessionLaunchClientForState(ctx, state)
			if err != nil {
				return serverapi.SessionPlanResponse{}, err
			}
			return launchClient.PlanSession(ctx, params)
		})
	},
	protocol.MethodSessionGetMainView: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.SessionMainViewRequest) (serverapi.SessionMainViewResponse, error) {
			return g.deps.SessionViewClient().GetSessionMainView(ctx, params)
		})
	},
	protocol.MethodSessionGetTranscriptPage: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.SessionTranscriptPageRequest) (serverapi.SessionTranscriptPageResponse, error) {
			return g.deps.SessionViewClient().GetSessionTranscriptPage(ctx, params)
		})
	},
	protocol.MethodSessionGetCommittedTranscriptSuffix: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.SessionCommittedTranscriptSuffixRequest) (serverapi.SessionCommittedTranscriptSuffixResponse, error) {
			suffixClient, ok := g.deps.SessionViewClient().(client.SessionCommittedTranscriptSuffixClient)
			if !ok {
				return serverapi.SessionCommittedTranscriptSuffixResponse{}, errors.New("session committed transcript suffix client is required")
			}
			return suffixClient.GetSessionCommittedTranscriptSuffix(ctx, params)
		})
	},
	protocol.MethodSessionGetInitialInput: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.SessionInitialInputRequest) (serverapi.SessionInitialInputResponse, error) {
			return g.deps.SessionLifecycleClient().GetInitialInput(ctx, params)
		})
	},
	protocol.MethodSessionPersistInputDraft: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.SessionPersistInputDraftRequest) (serverapi.SessionPersistInputDraftResponse, error) {
			return g.deps.SessionLifecycleClient().PersistInputDraft(ctx, params)
		})
	},
	protocol.MethodSessionRetargetWorkspace: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.SessionRetargetWorkspaceRequest) (serverapi.SessionRetargetWorkspaceResponse, error) {
			return g.deps.SessionLifecycleClient().RetargetSessionWorkspace(ctx, params)
		})
	},
	protocol.MethodSessionResolveTransition: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.SessionResolveTransitionRequest) (serverapi.SessionResolveTransitionResponse, error) {
			return g.deps.SessionLifecycleClient().ResolveTransition(ctx, params)
		})
	},
	protocol.MethodWorktreeList: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.WorktreeListRequest) (serverapi.WorktreeListResponse, error) {
			return g.deps.WorktreeClient().ListWorktrees(ctx, params)
		})
	},
	protocol.MethodWorktreeCreateTargetResolve: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.WorktreeCreateTargetResolveRequest) (serverapi.WorktreeCreateTargetResolveResponse, error) {
			return g.deps.WorktreeClient().ResolveWorktreeCreateTarget(ctx, params)
		})
	},
	protocol.MethodWorktreeCreate: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.WorktreeCreateRequest) (serverapi.WorktreeCreateResponse, error) {
			return g.deps.WorktreeClient().CreateWorktree(ctx, params)
		})
	},
	protocol.MethodWorktreeSwitch: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.WorktreeSwitchRequest) (serverapi.WorktreeSwitchResponse, error) {
			return g.deps.WorktreeClient().SwitchWorktree(ctx, params)
		})
	},
	protocol.MethodWorktreeDelete: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.WorktreeDeleteRequest) (serverapi.WorktreeDeleteResponse, error) {
			return g.deps.WorktreeClient().DeleteWorktree(ctx, params)
		})
	},
	protocol.MethodSessionRuntimeActivate: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.SessionRuntimeActivateRequest) (serverapi.SessionRuntimeActivateResponse, error) {
			return g.deps.SessionRuntimeClient().ActivateSessionRuntime(ctx, params)
		})
	},
	protocol.MethodSessionRuntimeRelease: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.SessionRuntimeReleaseRequest) (serverapi.SessionRuntimeReleaseResponse, error) {
			return g.deps.SessionRuntimeClient().ReleaseSessionRuntime(ctx, params)
		})
	},
	protocol.MethodRunGet: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RunGetRequest) (serverapi.RunGetResponse, error) {
			return g.deps.SessionViewClient().GetRun(ctx, params)
		})
	},
	protocol.MethodRuntimeSetSessionName: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeSetSessionNameRequest) (struct{}, error) {
			return struct{}{}, g.deps.RuntimeControlClient().SetSessionName(ctx, params)
		})
	},
	protocol.MethodRuntimeSetThinkingLevel: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeSetThinkingLevelRequest) (struct{}, error) {
			return struct{}{}, g.deps.RuntimeControlClient().SetThinkingLevel(ctx, params)
		})
	},
	protocol.MethodRuntimeSetFastModeEnabled: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeSetFastModeEnabledRequest) (serverapi.RuntimeSetFastModeEnabledResponse, error) {
			return g.deps.RuntimeControlClient().SetFastModeEnabled(ctx, params)
		})
	},
	protocol.MethodRuntimeSetReviewerEnabled: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeSetReviewerEnabledRequest) (serverapi.RuntimeSetReviewerEnabledResponse, error) {
			return g.deps.RuntimeControlClient().SetReviewerEnabled(ctx, params)
		})
	},
	protocol.MethodRuntimeSetAutoCompactionEnabled: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeSetAutoCompactionEnabledRequest) (serverapi.RuntimeSetAutoCompactionEnabledResponse, error) {
			return g.deps.RuntimeControlClient().SetAutoCompactionEnabled(ctx, params)
		})
	},
	protocol.MethodRuntimeAppendLocalEntry: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeAppendLocalEntryRequest) (struct{}, error) {
			return struct{}{}, g.deps.RuntimeControlClient().AppendLocalEntry(ctx, params)
		})
	},
	protocol.MethodRuntimeShouldCompactBeforeUserMessage: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeShouldCompactBeforeUserMessageRequest) (serverapi.RuntimeShouldCompactBeforeUserMessageResponse, error) {
			return g.deps.RuntimeControlClient().ShouldCompactBeforeUserMessage(ctx, params)
		})
	},
	protocol.MethodRuntimeSubmitUserMessage: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeSubmitUserMessageRequest) (serverapi.RuntimeSubmitUserMessageResponse, error) {
			return g.deps.RuntimeControlClient().SubmitUserMessage(ctx, params)
		})
	},
	protocol.MethodRuntimeSubmitUserTurn: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeSubmitUserTurnRequest) (serverapi.RuntimeSubmitUserTurnResponse, error) {
			return g.deps.RuntimeControlClient().SubmitUserTurn(ctx, params)
		})
	},
	protocol.MethodRuntimeSubmitUserShellCommand: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeSubmitUserShellCommandRequest) (struct{}, error) {
			return struct{}{}, g.deps.RuntimeControlClient().SubmitUserShellCommand(ctx, params)
		})
	},
	protocol.MethodRuntimeCompactContext: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeCompactContextRequest) (struct{}, error) {
			return struct{}{}, g.deps.RuntimeControlClient().CompactContext(ctx, params)
		})
	},
	protocol.MethodRuntimeCompactContextForPreSubmit: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeCompactContextForPreSubmitRequest) (struct{}, error) {
			return struct{}{}, g.deps.RuntimeControlClient().CompactContextForPreSubmit(ctx, params)
		})
	},
	protocol.MethodRuntimeHasQueuedUserWork: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeHasQueuedUserWorkRequest) (serverapi.RuntimeHasQueuedUserWorkResponse, error) {
			return g.deps.RuntimeControlClient().HasQueuedUserWork(ctx, params)
		})
	},
	protocol.MethodRuntimeSubmitQueuedUserMessages: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeSubmitQueuedUserMessagesRequest) (serverapi.RuntimeSubmitQueuedUserMessagesResponse, error) {
			return g.deps.RuntimeControlClient().SubmitQueuedUserMessages(ctx, params)
		})
	},
	protocol.MethodRuntimeInterrupt: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeInterruptRequest) (struct{}, error) {
			return struct{}{}, g.deps.RuntimeControlClient().Interrupt(ctx, params)
		})
	},
	protocol.MethodRuntimeQueueUserMessage: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeQueueUserMessageRequest) (serverapi.RuntimeQueueUserMessageResponse, error) {
			return g.deps.RuntimeControlClient().QueueUserMessage(ctx, params)
		})
	},
	protocol.MethodRuntimeDiscardQueuedUserMessage: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeDiscardQueuedUserMessageRequest) (serverapi.RuntimeDiscardQueuedUserMessageResponse, error) {
			return g.deps.RuntimeControlClient().DiscardQueuedUserMessage(ctx, params)
		})
	},
	protocol.MethodRuntimeRecordPromptHistory: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeRecordPromptHistoryRequest) (struct{}, error) {
			return struct{}{}, g.deps.RuntimeControlClient().RecordPromptHistory(ctx, params)
		})
	},
	protocol.MethodRuntimeGoalShow: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeGoalShowRequest) (serverapi.RuntimeGoalShowResponse, error) {
			return g.deps.RuntimeControlClient().ShowGoal(ctx, params)
		})
	},
	protocol.MethodRuntimeGoalSet: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeGoalSetRequest) (serverapi.RuntimeGoalShowResponse, error) {
			return g.deps.RuntimeControlClient().SetGoal(ctx, params)
		})
	},
	protocol.MethodRuntimeGoalPause: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
			return g.deps.RuntimeControlClient().PauseGoal(ctx, params)
		})
	},
	protocol.MethodRuntimeGoalResume: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
			return g.deps.RuntimeControlClient().ResumeGoal(ctx, params)
		})
	},
	protocol.MethodRuntimeGoalComplete: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
			return g.deps.RuntimeControlClient().CompleteGoal(ctx, params)
		})
	},
	protocol.MethodRuntimeGoalClear: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeGoalClearRequest) (serverapi.RuntimeGoalShowResponse, error) {
			return g.deps.RuntimeControlClient().ClearGoal(ctx, params)
		})
	},
	protocol.MethodProcessList: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.ProcessListRequest) (serverapi.ProcessListResponse, error) {
			resp, err := g.deps.ProcessViewClient().ListProcesses(ctx, params)
			if err != nil {
				return serverapi.ProcessListResponse{}, err
			}
			if strings.TrimSpace(params.OwnerSessionID) != "" {
				return resp, nil
			}
			filtered, err := g.filterProcessesForActiveProject(ctx, state, resp.Processes)
			if err != nil {
				return serverapi.ProcessListResponse{}, err
			}
			resp.Processes = filtered
			return resp, nil
		})
	},
	protocol.MethodProcessGet: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.ProcessGetRequest) (serverapi.ProcessGetResponse, error) {
			return g.deps.ProcessViewClient().GetProcess(ctx, params)
		})
	},
	protocol.MethodProcessKill: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.ProcessKillRequest) (serverapi.ProcessKillResponse, error) {
			return g.deps.ProcessControlClient().KillProcess(ctx, params)
		})
	},
	protocol.MethodProcessInlineOutput: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.ProcessInlineOutputRequest) (serverapi.ProcessInlineOutputResponse, error) {
			return g.deps.ProcessControlClient().GetInlineOutput(ctx, params)
		})
	},
	protocol.MethodAskListPending: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.AskListPendingBySessionRequest) (serverapi.AskListPendingBySessionResponse, error) {
			return g.deps.AskViewClient().ListPendingAsksBySession(ctx, params)
		})
	},
	protocol.MethodAskAnswer: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.AskAnswerRequest) (struct{}, error) {
			return struct{}{}, g.deps.PromptControlClient().AnswerAsk(ctx, params)
		})
	},
	protocol.MethodApprovalListPending: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.ApprovalListPendingBySessionRequest) (serverapi.ApprovalListPendingBySessionResponse, error) {
			return g.deps.ApprovalViewClient().ListPendingApprovalsBySession(ctx, params)
		})
	},
	protocol.MethodApprovalAnswer: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.ApprovalAnswerRequest) (struct{}, error) {
			return struct{}{}, g.deps.PromptControlClient().AnswerApproval(ctx, params)
		})
	},
}
