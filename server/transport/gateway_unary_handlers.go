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
			return protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidRequest, fmt.Sprintf("unsupported protocol version %q", params.ProtocolVersion))
		}
		state.handshakeDone = true
		return protocol.NewSuccessResponse(req.ID, protocol.HandshakeResponse{Identity: g.identity})
	},
	protocol.MethodAuthGetBootstrapStatus: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.AuthGetBootstrapStatusRequest) (serverapi.AuthGetBootstrapStatusResponse, error) {
			bootstrapClient := g.core.AuthBootstrapClient()
			if bootstrapClient == nil {
				return serverapi.AuthGetBootstrapStatusResponse{}, serverapi.ErrServerAuthRequired
			}
			return bootstrapClient.GetAuthBootstrapStatus(ctx, params)
		})
	},
	protocol.MethodAuthCompleteBootstrap: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.AuthCompleteBootstrapRequest) (serverapi.AuthCompleteBootstrapResponse, error) {
			bootstrapClient := g.core.AuthBootstrapClient()
			if bootstrapClient == nil {
				return serverapi.AuthCompleteBootstrapResponse{}, serverapi.ErrServerAuthRequired
			}
			return bootstrapClient.CompleteAuthBootstrap(ctx, params)
		})
	},
	protocol.MethodAuthGetStatus: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.AuthStatusRequest) (serverapi.AuthStatusResponse, error) {
			statusClient := g.core.AuthStatusClient()
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
			if err := g.core.ProjectExists(ctx, params.ProjectID); err != nil {
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
			return g.core.ProjectViewClient().ListProjects(ctx, params)
		})
	},
	protocol.MethodProjectResolvePath: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.ProjectResolvePathRequest) (serverapi.ProjectResolvePathResponse, error) {
			return g.core.ProjectViewClient().ResolveProjectPath(ctx, params)
		})
	},
	protocol.MethodProjectPlanWorkspaceBinding: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.ProjectBindingPlanRequest) (serverapi.ProjectBindingPlanResponse, error) {
			return g.core.ProjectViewClient().PlanWorkspaceBinding(ctx, params)
		})
	},
	protocol.MethodProjectCreate: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.ProjectCreateRequest) (serverapi.ProjectCreateResponse, error) {
			return g.core.ProjectViewClient().CreateProject(ctx, params)
		})
	},
	protocol.MethodProjectAttachWorkspace: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.ProjectAttachWorkspaceRequest) (serverapi.ProjectAttachWorkspaceResponse, error) {
			return g.core.ProjectViewClient().AttachWorkspaceToProject(ctx, params)
		})
	},
	protocol.MethodProjectRebindWorkspace: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.ProjectRebindWorkspaceRequest) (serverapi.ProjectRebindWorkspaceResponse, error) {
			return g.core.ProjectViewClient().RebindWorkspace(ctx, params)
		})
	},
	protocol.MethodProjectGetOverview: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.ProjectGetOverviewRequest) (serverapi.ProjectGetOverviewResponse, error) {
			return g.core.ProjectViewClient().GetProjectOverview(ctx, params)
		})
	},
	protocol.MethodSessionListByProject: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.SessionListByProjectRequest) (serverapi.SessionListByProjectResponse, error) {
			return g.core.ProjectViewClient().ListSessionsByProject(ctx, params)
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
			return g.core.SessionViewClient().GetSessionMainView(ctx, params)
		})
	},
	protocol.MethodSessionGetTranscriptPage: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.SessionTranscriptPageRequest) (serverapi.SessionTranscriptPageResponse, error) {
			return g.core.SessionViewClient().GetSessionTranscriptPage(ctx, params)
		})
	},
	protocol.MethodSessionGetCommittedTranscriptSuffix: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.SessionCommittedTranscriptSuffixRequest) (serverapi.SessionCommittedTranscriptSuffixResponse, error) {
			suffixClient, ok := g.core.SessionViewClient().(client.SessionCommittedTranscriptSuffixClient)
			if !ok {
				return serverapi.SessionCommittedTranscriptSuffixResponse{}, errors.New("session committed transcript suffix client is required")
			}
			return suffixClient.GetSessionCommittedTranscriptSuffix(ctx, params)
		})
	},
	protocol.MethodSessionGetInitialInput: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.SessionInitialInputRequest) (serverapi.SessionInitialInputResponse, error) {
			return g.core.SessionLifecycleClient().GetInitialInput(ctx, params)
		})
	},
	protocol.MethodSessionPersistInputDraft: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.SessionPersistInputDraftRequest) (serverapi.SessionPersistInputDraftResponse, error) {
			return g.core.SessionLifecycleClient().PersistInputDraft(ctx, params)
		})
	},
	protocol.MethodSessionRetargetWorkspace: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.SessionRetargetWorkspaceRequest) (serverapi.SessionRetargetWorkspaceResponse, error) {
			return g.core.SessionLifecycleClient().RetargetSessionWorkspace(ctx, params)
		})
	},
	protocol.MethodSessionResolveTransition: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.SessionResolveTransitionRequest) (serverapi.SessionResolveTransitionResponse, error) {
			return g.core.SessionLifecycleClient().ResolveTransition(ctx, params)
		})
	},
	protocol.MethodWorktreeList: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.WorktreeListRequest) (serverapi.WorktreeListResponse, error) {
			return g.core.WorktreeClient().ListWorktrees(ctx, params)
		})
	},
	protocol.MethodWorktreeCreateTargetResolve: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.WorktreeCreateTargetResolveRequest) (serverapi.WorktreeCreateTargetResolveResponse, error) {
			return g.core.WorktreeClient().ResolveWorktreeCreateTarget(ctx, params)
		})
	},
	protocol.MethodWorktreeCreate: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.WorktreeCreateRequest) (serverapi.WorktreeCreateResponse, error) {
			return g.core.WorktreeClient().CreateWorktree(ctx, params)
		})
	},
	protocol.MethodWorktreeSwitch: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.WorktreeSwitchRequest) (serverapi.WorktreeSwitchResponse, error) {
			return g.core.WorktreeClient().SwitchWorktree(ctx, params)
		})
	},
	protocol.MethodWorktreeDelete: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.WorktreeDeleteRequest) (serverapi.WorktreeDeleteResponse, error) {
			return g.core.WorktreeClient().DeleteWorktree(ctx, params)
		})
	},
	protocol.MethodSessionRuntimeActivate: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.SessionRuntimeActivateRequest) (serverapi.SessionRuntimeActivateResponse, error) {
			return g.core.SessionRuntimeClient().ActivateSessionRuntime(ctx, params)
		})
	},
	protocol.MethodSessionRuntimeRelease: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.SessionRuntimeReleaseRequest) (serverapi.SessionRuntimeReleaseResponse, error) {
			return g.core.SessionRuntimeClient().ReleaseSessionRuntime(ctx, params)
		})
	},
	protocol.MethodRunGet: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RunGetRequest) (serverapi.RunGetResponse, error) {
			return g.core.SessionViewClient().GetRun(ctx, params)
		})
	},
	protocol.MethodRuntimeSetSessionName: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeSetSessionNameRequest) (struct{}, error) {
			return struct{}{}, g.core.RuntimeControlClient().SetSessionName(ctx, params)
		})
	},
	protocol.MethodRuntimeSetThinkingLevel: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeSetThinkingLevelRequest) (struct{}, error) {
			return struct{}{}, g.core.RuntimeControlClient().SetThinkingLevel(ctx, params)
		})
	},
	protocol.MethodRuntimeSetFastModeEnabled: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeSetFastModeEnabledRequest) (serverapi.RuntimeSetFastModeEnabledResponse, error) {
			return g.core.RuntimeControlClient().SetFastModeEnabled(ctx, params)
		})
	},
	protocol.MethodRuntimeSetReviewerEnabled: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeSetReviewerEnabledRequest) (serverapi.RuntimeSetReviewerEnabledResponse, error) {
			return g.core.RuntimeControlClient().SetReviewerEnabled(ctx, params)
		})
	},
	protocol.MethodRuntimeSetAutoCompactionEnabled: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeSetAutoCompactionEnabledRequest) (serverapi.RuntimeSetAutoCompactionEnabledResponse, error) {
			return g.core.RuntimeControlClient().SetAutoCompactionEnabled(ctx, params)
		})
	},
	protocol.MethodRuntimeAppendLocalEntry: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeAppendLocalEntryRequest) (struct{}, error) {
			return struct{}{}, g.core.RuntimeControlClient().AppendLocalEntry(ctx, params)
		})
	},
	protocol.MethodRuntimeShouldCompactBeforeUserMessage: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeShouldCompactBeforeUserMessageRequest) (serverapi.RuntimeShouldCompactBeforeUserMessageResponse, error) {
			return g.core.RuntimeControlClient().ShouldCompactBeforeUserMessage(ctx, params)
		})
	},
	protocol.MethodRuntimeSubmitUserMessage: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeSubmitUserMessageRequest) (serverapi.RuntimeSubmitUserMessageResponse, error) {
			return g.core.RuntimeControlClient().SubmitUserMessage(ctx, params)
		})
	},
	protocol.MethodRuntimeSubmitUserTurn: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeSubmitUserTurnRequest) (serverapi.RuntimeSubmitUserTurnResponse, error) {
			return g.core.RuntimeControlClient().SubmitUserTurn(ctx, params)
		})
	},
	protocol.MethodRuntimeSubmitUserShellCommand: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeSubmitUserShellCommandRequest) (struct{}, error) {
			return struct{}{}, g.core.RuntimeControlClient().SubmitUserShellCommand(ctx, params)
		})
	},
	protocol.MethodRuntimeCompactContext: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeCompactContextRequest) (struct{}, error) {
			return struct{}{}, g.core.RuntimeControlClient().CompactContext(ctx, params)
		})
	},
	protocol.MethodRuntimeCompactContextForPreSubmit: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeCompactContextForPreSubmitRequest) (struct{}, error) {
			return struct{}{}, g.core.RuntimeControlClient().CompactContextForPreSubmit(ctx, params)
		})
	},
	protocol.MethodRuntimeHasQueuedUserWork: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeHasQueuedUserWorkRequest) (serverapi.RuntimeHasQueuedUserWorkResponse, error) {
			return g.core.RuntimeControlClient().HasQueuedUserWork(ctx, params)
		})
	},
	protocol.MethodRuntimeSubmitQueuedUserMessages: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeSubmitQueuedUserMessagesRequest) (serverapi.RuntimeSubmitQueuedUserMessagesResponse, error) {
			return g.core.RuntimeControlClient().SubmitQueuedUserMessages(ctx, params)
		})
	},
	protocol.MethodRuntimeInterrupt: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeInterruptRequest) (struct{}, error) {
			return struct{}{}, g.core.RuntimeControlClient().Interrupt(ctx, params)
		})
	},
	protocol.MethodRuntimeQueueUserMessage: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeQueueUserMessageRequest) (serverapi.RuntimeQueueUserMessageResponse, error) {
			return g.core.RuntimeControlClient().QueueUserMessage(ctx, params)
		})
	},
	protocol.MethodRuntimeDiscardQueuedUserMessage: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeDiscardQueuedUserMessageRequest) (serverapi.RuntimeDiscardQueuedUserMessageResponse, error) {
			return g.core.RuntimeControlClient().DiscardQueuedUserMessage(ctx, params)
		})
	},
	protocol.MethodRuntimeRecordPromptHistory: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeRecordPromptHistoryRequest) (struct{}, error) {
			return struct{}{}, g.core.RuntimeControlClient().RecordPromptHistory(ctx, params)
		})
	},
	protocol.MethodRuntimeGoalShow: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeGoalShowRequest) (serverapi.RuntimeGoalShowResponse, error) {
			return g.core.RuntimeControlClient().ShowGoal(ctx, params)
		})
	},
	protocol.MethodRuntimeGoalSet: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeGoalSetRequest) (serverapi.RuntimeGoalShowResponse, error) {
			return g.core.RuntimeControlClient().SetGoal(ctx, params)
		})
	},
	protocol.MethodRuntimeGoalPause: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
			return g.core.RuntimeControlClient().PauseGoal(ctx, params)
		})
	},
	protocol.MethodRuntimeGoalResume: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
			return g.core.RuntimeControlClient().ResumeGoal(ctx, params)
		})
	},
	protocol.MethodRuntimeGoalComplete: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
			return g.core.RuntimeControlClient().CompleteGoal(ctx, params)
		})
	},
	protocol.MethodRuntimeGoalClear: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.RuntimeGoalClearRequest) (serverapi.RuntimeGoalShowResponse, error) {
			return g.core.RuntimeControlClient().ClearGoal(ctx, params)
		})
	},
	protocol.MethodProcessList: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.ProcessListRequest) (serverapi.ProcessListResponse, error) {
			resp, err := g.core.ProcessViewClient().ListProcesses(ctx, params)
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
			return g.core.ProcessViewClient().GetProcess(ctx, params)
		})
	},
	protocol.MethodProcessKill: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.ProcessKillRequest) (serverapi.ProcessKillResponse, error) {
			return g.core.ProcessControlClient().KillProcess(ctx, params)
		})
	},
	protocol.MethodProcessInlineOutput: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.ProcessInlineOutputRequest) (serverapi.ProcessInlineOutputResponse, error) {
			return g.core.ProcessControlClient().GetInlineOutput(ctx, params)
		})
	},
	protocol.MethodAskListPending: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.AskListPendingBySessionRequest) (serverapi.AskListPendingBySessionResponse, error) {
			return g.core.AskViewClient().ListPendingAsksBySession(ctx, params)
		})
	},
	protocol.MethodAskAnswer: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.AskAnswerRequest) (struct{}, error) {
			return struct{}{}, g.core.PromptControlClient().AnswerAsk(ctx, params)
		})
	},
	protocol.MethodApprovalListPending: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.ApprovalListPendingBySessionRequest) (serverapi.ApprovalListPendingBySessionResponse, error) {
			return g.core.ApprovalViewClient().ListPendingApprovalsBySession(ctx, params)
		})
	},
	protocol.MethodApprovalAnswer: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.ApprovalAnswerRequest) (struct{}, error) {
			return struct{}{}, g.core.PromptControlClient().AnswerApproval(ctx, params)
		})
	},
}
