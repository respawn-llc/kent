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

func gatewayClientCall[C any, Req any, Resp any](getClient func(*Gateway) C, call func(C, context.Context, Req) (Resp, error)) gatewayUnaryHandler {
	return func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params Req) (Resp, error) {
			return call(getClient(g), ctx, params)
		})
	}
}

func gatewayClientCallNoResponse[C any, Req any](getClient func(*Gateway) C, call func(C, context.Context, Req) error) gatewayUnaryHandler {
	return func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params Req) (struct{}, error) {
			return struct{}{}, call(getClient(g), ctx, params)
		})
	}
}

func gatewayProjectViewClient(g *Gateway) client.ProjectViewClient { return g.deps.ProjectViewClient() }
func gatewayWorkflowClient(g *Gateway) client.WorkflowClient       { return g.deps.WorkflowClient() }
func gatewaySessionViewClient(g *Gateway) client.SessionViewClient { return g.deps.SessionViewClient() }
func gatewaySessionLifecycleClient(g *Gateway) client.SessionLifecycleClient {
	return g.deps.SessionLifecycleClient()
}
func gatewaySessionRuntimeClient(g *Gateway) client.SessionRuntimeClient {
	return g.deps.SessionRuntimeClient()
}
func gatewayWorktreeClient(g *Gateway) client.WorktreeClient { return g.deps.WorktreeClient() }
func gatewayRuntimeControlClient(g *Gateway) client.RuntimeControlClient {
	return g.deps.RuntimeControlClient()
}
func gatewayProcessViewClient(g *Gateway) client.ProcessViewClient { return g.deps.ProcessViewClient() }
func gatewayProcessControlClient(g *Gateway) client.ProcessControlClient {
	return g.deps.ProcessControlClient()
}
func gatewayAskViewClient(g *Gateway) client.AskViewClient { return g.deps.AskViewClient() }
func gatewayApprovalViewClient(g *Gateway) client.ApprovalViewClient {
	return g.deps.ApprovalViewClient()
}
func gatewayPromptControlClient(g *Gateway) client.PromptControlClient {
	return g.deps.PromptControlClient()
}

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
	protocol.MethodServerReadinessGet: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.ServerReadinessRequest) (serverapi.ServerReadinessResponse, error) {
			statusClient := g.deps.ServerStatusClient()
			if statusClient == nil {
				return serverapi.ServerReadinessResponse{}, errors.New("server status client is required")
			}
			response, err := statusClient.GetServerReadiness(ctx, params)
			if err != nil {
				return serverapi.ServerReadinessResponse{}, err
			}
			response.ServerID = g.identity.ServerID
			response.ProtocolVersion = g.identity.ProtocolVersion
			return response, nil
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
	protocol.MethodProjectList:                   gatewayClientCall[client.ProjectViewClient, serverapi.ProjectListRequest, serverapi.ProjectListResponse](gatewayProjectViewClient, client.ProjectViewClient.ListProjects),
	protocol.MethodProjectHomeList:               gatewayClientCall[client.ProjectViewClient, serverapi.ProjectHomeListRequest, serverapi.ProjectHomeListResponse](gatewayProjectViewClient, client.ProjectViewClient.ListProjectHome),
	protocol.MethodProjectResolvePath:            gatewayClientCall[client.ProjectViewClient, serverapi.ProjectResolvePathRequest, serverapi.ProjectResolvePathResponse](gatewayProjectViewClient, client.ProjectViewClient.ResolveProjectPath),
	protocol.MethodProjectPlanWorkspaceBinding:   gatewayClientCall[client.ProjectViewClient, serverapi.ProjectBindingPlanRequest, serverapi.ProjectBindingPlanResponse](gatewayProjectViewClient, client.ProjectViewClient.PlanWorkspaceBinding),
	protocol.MethodProjectCreate:                 gatewayClientCall[client.ProjectViewClient, serverapi.ProjectCreateRequest, serverapi.ProjectCreateResponse](gatewayProjectViewClient, client.ProjectViewClient.CreateProject),
	protocol.MethodProjectEditGet:                gatewayClientCall[client.ProjectViewClient, serverapi.ProjectEditGetRequest, serverapi.ProjectEditGetResponse](gatewayProjectViewClient, client.ProjectViewClient.GetProjectEdit),
	protocol.MethodProjectUpdate:                 gatewayClientCall[client.ProjectViewClient, serverapi.ProjectUpdateRequest, serverapi.ProjectUpdateResponse](gatewayProjectViewClient, client.ProjectViewClient.UpdateProject),
	protocol.MethodProjectSetDefaultWorkspace:    gatewayClientCall[client.ProjectViewClient, serverapi.ProjectDefaultWorkspaceSetRequest, serverapi.ProjectDefaultWorkspaceSetResponse](gatewayProjectViewClient, client.ProjectViewClient.SetDefaultWorkspace),
	protocol.MethodProjectWorkspaceList:          gatewayClientCall[client.ProjectViewClient, serverapi.ProjectWorkspaceListRequest, serverapi.ProjectWorkspaceListResponse](gatewayProjectViewClient, client.ProjectViewClient.ListProjectWorkspaces),
	protocol.MethodProjectUnlinkWorkspace:        gatewayClientCall[client.ProjectViewClient, serverapi.ProjectWorkspaceUnlinkRequest, serverapi.ProjectWorkspaceUnlinkResponse](gatewayProjectViewClient, client.ProjectViewClient.UnlinkWorkspaceFromProject),
	protocol.MethodProjectDelete:                 gatewayClientCall[client.ProjectViewClient, serverapi.ProjectDeleteRequest, serverapi.ProjectDeleteResponse](gatewayProjectViewClient, client.ProjectViewClient.DeleteProject),
	protocol.MethodProjectAttachWorkspace:        gatewayClientCall[client.ProjectViewClient, serverapi.ProjectAttachWorkspaceRequest, serverapi.ProjectAttachWorkspaceResponse](gatewayProjectViewClient, client.ProjectViewClient.AttachWorkspaceToProject),
	protocol.MethodProjectRebindWorkspace:        gatewayClientCall[client.ProjectViewClient, serverapi.ProjectRebindWorkspaceRequest, serverapi.ProjectRebindWorkspaceResponse](gatewayProjectViewClient, client.ProjectViewClient.RebindWorkspace),
	protocol.MethodProjectGetOverview:            gatewayClientCall[client.ProjectViewClient, serverapi.ProjectGetOverviewRequest, serverapi.ProjectGetOverviewResponse](gatewayProjectViewClient, client.ProjectViewClient.GetProjectOverview),
	protocol.MethodSessionListByProject:          gatewayClientCall[client.ProjectViewClient, serverapi.SessionListByProjectRequest, serverapi.SessionListByProjectResponse](gatewayProjectViewClient, client.ProjectViewClient.ListSessionsByProject),
	protocol.MethodWorkflowCreate:                gatewayClientCall[client.WorkflowClient, serverapi.WorkflowCreateRequest, serverapi.WorkflowCreateResponse](gatewayWorkflowClient, client.WorkflowClient.CreateWorkflow),
	protocol.MethodWorkflowCreateAndLinkProject:  gatewayClientCall[client.WorkflowClient, serverapi.WorkflowCreateAndLinkProjectRequest, serverapi.WorkflowCreateAndLinkProjectResponse](gatewayWorkflowClient, client.WorkflowClient.CreateAndLinkWorkflowToProject),
	protocol.MethodWorkflowUpdate:                gatewayClientCall[client.WorkflowClient, serverapi.WorkflowUpdateRequest, serverapi.WorkflowGetResponse](gatewayWorkflowClient, client.WorkflowClient.UpdateWorkflow),
	protocol.MethodWorkflowList:                  gatewayClientCall[client.WorkflowClient, serverapi.WorkflowListRequest, serverapi.WorkflowListResponse](gatewayWorkflowClient, client.WorkflowClient.ListWorkflows),
	protocol.MethodWorkflowGet:                   gatewayClientCall[client.WorkflowClient, serverapi.WorkflowGetRequest, serverapi.WorkflowGetResponse](gatewayWorkflowClient, client.WorkflowClient.GetWorkflow),
	protocol.MethodWorkflowNodeGroupAdd:          gatewayClientCall[client.WorkflowClient, serverapi.WorkflowNodeGroupAddRequest, serverapi.WorkflowNodeGroupResponse](gatewayWorkflowClient, client.WorkflowClient.AddWorkflowNodeGroup),
	protocol.MethodWorkflowNodeGroupUpdate:       gatewayClientCall[client.WorkflowClient, serverapi.WorkflowNodeGroupUpdateRequest, serverapi.WorkflowNodeGroupResponse](gatewayWorkflowClient, client.WorkflowClient.UpdateWorkflowNodeGroup),
	protocol.MethodWorkflowNodeGroupDelete:       gatewayClientCallNoResponse[client.WorkflowClient, serverapi.WorkflowNodeGroupDeleteRequest](gatewayWorkflowClient, client.WorkflowClient.DeleteWorkflowNodeGroup),
	protocol.MethodWorkflowAddNode:               gatewayClientCall[client.WorkflowClient, serverapi.WorkflowNodeAddRequest, serverapi.WorkflowNodeAddResponse](gatewayWorkflowClient, client.WorkflowClient.AddWorkflowNode),
	protocol.MethodWorkflowUpdateNode:            gatewayClientCall[client.WorkflowClient, serverapi.WorkflowNodeUpdateRequest, serverapi.WorkflowNodeUpdateResponse](gatewayWorkflowClient, client.WorkflowClient.UpdateWorkflowNode),
	protocol.MethodWorkflowAddTransitionGroup:    gatewayClientCall[client.WorkflowClient, serverapi.WorkflowTransitionGroupAddRequest, serverapi.WorkflowTransitionGroupAddResponse](gatewayWorkflowClient, client.WorkflowClient.AddWorkflowTransitionGroup),
	protocol.MethodWorkflowUpdateTransitionGroup: gatewayClientCall[client.WorkflowClient, serverapi.WorkflowTransitionGroupUpdateRequest, serverapi.WorkflowTransitionGroupUpdateResponse](gatewayWorkflowClient, client.WorkflowClient.UpdateWorkflowTransitionGroup),
	protocol.MethodWorkflowAddEdge:               gatewayClientCall[client.WorkflowClient, serverapi.WorkflowEdgeAddRequest, serverapi.WorkflowEdgeAddResponse](gatewayWorkflowClient, client.WorkflowClient.AddWorkflowEdge),
	protocol.MethodWorkflowUpdateEdge:            gatewayClientCall[client.WorkflowClient, serverapi.WorkflowEdgeUpdateRequest, serverapi.WorkflowEdgeUpdateResponse](gatewayWorkflowClient, client.WorkflowClient.UpdateWorkflowEdge),
	protocol.MethodWorkflowLinkProject:           gatewayClientCall[client.WorkflowClient, serverapi.WorkflowLinkProjectRequest, serverapi.WorkflowLinkProjectResponse](gatewayWorkflowClient, client.WorkflowClient.LinkWorkflowToProject),
	protocol.MethodWorkflowListProjectLinks:      gatewayClientCall[client.WorkflowClient, serverapi.WorkflowListProjectLinksRequest, serverapi.WorkflowListProjectLinksResponse](gatewayWorkflowClient, client.WorkflowClient.ListProjectWorkflowLinks),
	protocol.MethodWorkflowSetDefaultProjectLink: gatewayClientCall[client.WorkflowClient, serverapi.WorkflowSetDefaultProjectLinkRequest, serverapi.WorkflowSetDefaultProjectLinkResponse](gatewayWorkflowClient, client.WorkflowClient.SetDefaultProjectWorkflowLink),
	protocol.MethodWorkflowUnlinkProject:         gatewayClientCall[client.WorkflowClient, serverapi.WorkflowUnlinkProjectRequest, serverapi.WorkflowUnlinkProjectResponse](gatewayWorkflowClient, client.WorkflowClient.UnlinkWorkflowFromProject),
	protocol.MethodWorkflowDeletePreview:         gatewayClientCall[client.WorkflowClient, serverapi.WorkflowDeletePreviewRequest, serverapi.WorkflowDeletePreviewResponse](gatewayWorkflowClient, client.WorkflowClient.PreviewWorkflowDelete),
	protocol.MethodWorkflowDelete:                gatewayClientCall[client.WorkflowClient, serverapi.WorkflowDeleteRequest, serverapi.WorkflowDeleteResponse](gatewayWorkflowClient, client.WorkflowClient.DeleteWorkflow),
	protocol.MethodWorkflowValidate:              gatewayClientCall[client.WorkflowClient, serverapi.WorkflowValidateRequest, serverapi.WorkflowValidateResponse](gatewayWorkflowClient, client.WorkflowClient.ValidateWorkflow),
	protocol.MethodWorkflowGraphValidateDraft:    gatewayClientCall[client.WorkflowClient, serverapi.WorkflowGraphValidateDraftRequest, serverapi.WorkflowGraphValidateDraftResponse](gatewayWorkflowClient, client.WorkflowClient.ValidateWorkflowGraphDraft),
	protocol.MethodWorkflowGraphSavePreview:      gatewayClientCall[client.WorkflowClient, serverapi.WorkflowGraphSavePreviewRequest, serverapi.WorkflowGraphSavePreviewResponse](gatewayWorkflowClient, client.WorkflowClient.PreviewWorkflowGraphSave),
	protocol.MethodWorkflowGraphSave:             gatewayClientCall[client.WorkflowClient, serverapi.WorkflowGraphSaveRequest, serverapi.WorkflowGraphSaveResponse](gatewayWorkflowClient, client.WorkflowClient.SaveWorkflowGraph),
	protocol.MethodWorkflowTaskCreate:            gatewayClientCall[client.WorkflowClient, serverapi.WorkflowTaskCreateRequest, serverapi.WorkflowTaskCreateResponse](gatewayWorkflowClient, client.WorkflowClient.CreateWorkflowTask),
	protocol.MethodWorkflowTaskUpdate:            gatewayClientCall[client.WorkflowClient, serverapi.WorkflowTaskUpdateRequest, serverapi.WorkflowTaskUpdateResponse](gatewayWorkflowClient, client.WorkflowClient.UpdateWorkflowTask),
	protocol.MethodWorkflowTaskStart:             gatewayClientCall[client.WorkflowClient, serverapi.WorkflowTaskStartRequest, serverapi.WorkflowTaskStartResponse](gatewayWorkflowClient, client.WorkflowClient.StartWorkflowTask),
	protocol.MethodWorkflowTaskInterrupt:         gatewayClientCall[client.WorkflowClient, serverapi.WorkflowTaskInterruptRequest, serverapi.WorkflowTaskInterruptResponse](gatewayWorkflowClient, client.WorkflowClient.InterruptWorkflowTask),
	protocol.MethodWorkflowTaskResume:            gatewayClientCall[client.WorkflowClient, serverapi.WorkflowTaskResumeRequest, serverapi.WorkflowTaskResumeResponse](gatewayWorkflowClient, client.WorkflowClient.ResumeWorkflowTask),
	protocol.MethodWorkflowTaskApprove:           gatewayClientCall[client.WorkflowClient, serverapi.WorkflowTaskApproveRequest, serverapi.WorkflowTaskApproveResponse](gatewayWorkflowClient, client.WorkflowClient.ApproveWorkflowTask),
	protocol.MethodWorkflowTaskMove:              gatewayClientCall[client.WorkflowClient, serverapi.WorkflowTaskMoveRequest, serverapi.WorkflowTaskMoveResponse](gatewayWorkflowClient, client.WorkflowClient.MoveWorkflowTask),
	protocol.MethodWorkflowTaskCancel:            gatewayClientCallNoResponse[client.WorkflowClient, serverapi.WorkflowTaskCancelRequest](gatewayWorkflowClient, client.WorkflowClient.CancelWorkflowTask),
	protocol.MethodWorkflowAttentionList:         gatewayClientCall[client.WorkflowClient, serverapi.WorkflowAttentionListRequest, serverapi.WorkflowAttentionListResponse](gatewayWorkflowClient, client.WorkflowClient.ListWorkflowAttention),
	protocol.MethodWorkflowTaskAttentionList:     gatewayClientCall[client.WorkflowClient, serverapi.WorkflowTaskAttentionListRequest, serverapi.WorkflowTaskAttentionListResponse](gatewayWorkflowClient, client.WorkflowClient.ListWorkflowTaskAttention),
	protocol.MethodWorkflowTaskQuestionAnswer:    gatewayClientCallNoResponse[client.WorkflowClient, serverapi.WorkflowTaskQuestionAnswerRequest](gatewayWorkflowClient, client.WorkflowClient.AnswerWorkflowTaskQuestion),
	protocol.MethodWorkflowTaskCommentAdd:        gatewayClientCall[client.WorkflowClient, serverapi.WorkflowTaskCommentAddRequest, serverapi.WorkflowTaskCommentAddResponse](gatewayWorkflowClient, client.WorkflowClient.AddWorkflowTaskComment),
	protocol.MethodWorkflowTaskCommentList:       gatewayClientCall[client.WorkflowClient, serverapi.WorkflowTaskCommentListRequest, serverapi.WorkflowTaskCommentListResponse](gatewayWorkflowClient, client.WorkflowClient.ListWorkflowTaskComments),
	protocol.MethodWorkflowTaskCommentReplace:    gatewayClientCallNoResponse[client.WorkflowClient, serverapi.WorkflowTaskCommentReplaceRequest](gatewayWorkflowClient, client.WorkflowClient.ReplaceWorkflowTaskComment),
	protocol.MethodWorkflowTaskCommentDelete:     gatewayClientCallNoResponse[client.WorkflowClient, serverapi.WorkflowTaskCommentDeleteRequest](gatewayWorkflowClient, client.WorkflowClient.DeleteWorkflowTaskComment),
	protocol.MethodWorkflowTaskActivityList:      gatewayClientCall[client.WorkflowClient, serverapi.WorkflowTaskActivityListRequest, serverapi.WorkflowTaskActivityListResponse](gatewayWorkflowClient, client.WorkflowClient.ListWorkflowTaskActivity),
	protocol.MethodWorkflowBoardGet:              gatewayClientCall[client.WorkflowClient, serverapi.WorkflowBoardRequest, serverapi.WorkflowBoardResponse](gatewayWorkflowClient, client.WorkflowClient.GetWorkflowBoard),
	protocol.MethodWorkflowBoardNodeCardsList:    gatewayClientCall[client.WorkflowClient, serverapi.WorkflowBoardNodeCardsListRequest, serverapi.WorkflowBoardNodeCardsListResponse](gatewayWorkflowClient, client.WorkflowClient.ListWorkflowBoardNodeCards),
	protocol.MethodWorkflowTaskGet:               gatewayClientCall[client.WorkflowClient, serverapi.WorkflowTaskGetRequest, serverapi.WorkflowTaskGetResponse](gatewayWorkflowClient, client.WorkflowClient.GetWorkflowTask),
	protocol.MethodSessionPlan: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.SessionPlanRequest) (serverapi.SessionPlanResponse, error) {
			launchClient, err := g.sessionLaunchClientForState(ctx, state)
			if err != nil {
				return serverapi.SessionPlanResponse{}, err
			}
			return launchClient.PlanSession(ctx, params)
		})
	},
	protocol.MethodSessionGetMainView:       gatewayClientCall[client.SessionViewClient, serverapi.SessionMainViewRequest, serverapi.SessionMainViewResponse](gatewaySessionViewClient, client.SessionViewClient.GetSessionMainView),
	protocol.MethodSessionGetTranscriptPage: gatewayClientCall[client.SessionViewClient, serverapi.SessionTranscriptPageRequest, serverapi.SessionTranscriptPageResponse](gatewaySessionViewClient, client.SessionViewClient.GetSessionTranscriptPage),
	protocol.MethodSessionGetCommittedTranscriptSuffix: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.SessionCommittedTranscriptSuffixRequest) (serverapi.SessionCommittedTranscriptSuffixResponse, error) {
			suffixClient, ok := g.deps.SessionViewClient().(client.SessionCommittedTranscriptSuffixClient)
			if !ok {
				return serverapi.SessionCommittedTranscriptSuffixResponse{}, errors.New("session committed transcript suffix client is required")
			}
			return suffixClient.GetSessionCommittedTranscriptSuffix(ctx, params)
		})
	},
	protocol.MethodSessionGetInitialInput:      gatewayClientCall[client.SessionLifecycleClient, serverapi.SessionInitialInputRequest, serverapi.SessionInitialInputResponse](gatewaySessionLifecycleClient, client.SessionLifecycleClient.GetInitialInput),
	protocol.MethodSessionPersistInputDraft:    gatewayClientCall[client.SessionLifecycleClient, serverapi.SessionPersistInputDraftRequest, serverapi.SessionPersistInputDraftResponse](gatewaySessionLifecycleClient, client.SessionLifecycleClient.PersistInputDraft),
	protocol.MethodSessionRetargetWorkspace:    gatewayClientCall[client.SessionLifecycleClient, serverapi.SessionRetargetWorkspaceRequest, serverapi.SessionRetargetWorkspaceResponse](gatewaySessionLifecycleClient, client.SessionLifecycleClient.RetargetSessionWorkspace),
	protocol.MethodSessionResolveTransition:    gatewayClientCall[client.SessionLifecycleClient, serverapi.SessionResolveTransitionRequest, serverapi.SessionResolveTransitionResponse](gatewaySessionLifecycleClient, client.SessionLifecycleClient.ResolveTransition),
	protocol.MethodWorktreeList:                gatewayClientCall[client.WorktreeClient, serverapi.WorktreeListRequest, serverapi.WorktreeListResponse](gatewayWorktreeClient, client.WorktreeClient.ListWorktrees),
	protocol.MethodWorktreeCreateTargetResolve: gatewayClientCall[client.WorktreeClient, serverapi.WorktreeCreateTargetResolveRequest, serverapi.WorktreeCreateTargetResolveResponse](gatewayWorktreeClient, client.WorktreeClient.ResolveWorktreeCreateTarget),
	protocol.MethodWorktreeCreate:              gatewayClientCall[client.WorktreeClient, serverapi.WorktreeCreateRequest, serverapi.WorktreeCreateResponse](gatewayWorktreeClient, client.WorktreeClient.CreateWorktree),
	protocol.MethodWorktreeSwitch:              gatewayClientCall[client.WorktreeClient, serverapi.WorktreeSwitchRequest, serverapi.WorktreeSwitchResponse](gatewayWorktreeClient, client.WorktreeClient.SwitchWorktree),
	protocol.MethodWorktreeDelete:              gatewayClientCall[client.WorktreeClient, serverapi.WorktreeDeleteRequest, serverapi.WorktreeDeleteResponse](gatewayWorktreeClient, client.WorktreeClient.DeleteWorktree),
	protocol.MethodSessionRuntimeActivate: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.SessionRuntimeActivateRequest) (serverapi.SessionRuntimeActivateResponse, error) {
			params.OwnerID = state.runtimeOwnerID
			resp, err := g.deps.SessionRuntimeClient().ActivateSessionRuntime(ctx, params)
			if err == nil && !resp.ReadOnly {
				state.recordOwnedRuntimeLease(params.SessionID, resp.LeaseID)
			}
			return resp, err
		})
	},
	protocol.MethodSessionRuntimeRelease: func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
		return decodeAndHandle(req, func(params serverapi.SessionRuntimeReleaseRequest) (serverapi.SessionRuntimeReleaseResponse, error) {
			params.OwnerID = state.runtimeOwnerID
			resp, err := g.deps.SessionRuntimeClient().ReleaseSessionRuntime(ctx, params)
			if err == nil && resp.Released {
				state.removeOwnedRuntimeLease(params.SessionID, params.LeaseID)
			}
			return resp, err
		})
	},
	protocol.MethodRunGet:                                gatewayClientCall[client.SessionViewClient, serverapi.RunGetRequest, serverapi.RunGetResponse](gatewaySessionViewClient, client.SessionViewClient.GetRun),
	protocol.MethodRuntimeSetSessionName:                 gatewayClientCallNoResponse[client.RuntimeControlClient, serverapi.RuntimeSetSessionNameRequest](gatewayRuntimeControlClient, client.RuntimeControlClient.SetSessionName),
	protocol.MethodRuntimeSetThinkingLevel:               gatewayClientCallNoResponse[client.RuntimeControlClient, serverapi.RuntimeSetThinkingLevelRequest](gatewayRuntimeControlClient, client.RuntimeControlClient.SetThinkingLevel),
	protocol.MethodRuntimeSetFastModeEnabled:             gatewayClientCall[client.RuntimeControlClient, serverapi.RuntimeSetFastModeEnabledRequest, serverapi.RuntimeSetFastModeEnabledResponse](gatewayRuntimeControlClient, client.RuntimeControlClient.SetFastModeEnabled),
	protocol.MethodRuntimeSetReviewerEnabled:             gatewayClientCall[client.RuntimeControlClient, serverapi.RuntimeSetReviewerEnabledRequest, serverapi.RuntimeSetReviewerEnabledResponse](gatewayRuntimeControlClient, client.RuntimeControlClient.SetReviewerEnabled),
	protocol.MethodRuntimeSetAutoCompactionEnabled:       gatewayClientCall[client.RuntimeControlClient, serverapi.RuntimeSetAutoCompactionEnabledRequest, serverapi.RuntimeSetAutoCompactionEnabledResponse](gatewayRuntimeControlClient, client.RuntimeControlClient.SetAutoCompactionEnabled),
	protocol.MethodRuntimeAppendLocalEntry:               gatewayClientCallNoResponse[client.RuntimeControlClient, serverapi.RuntimeAppendLocalEntryRequest](gatewayRuntimeControlClient, client.RuntimeControlClient.AppendLocalEntry),
	protocol.MethodRuntimeShouldCompactBeforeUserMessage: gatewayClientCall[client.RuntimeControlClient, serverapi.RuntimeShouldCompactBeforeUserMessageRequest, serverapi.RuntimeShouldCompactBeforeUserMessageResponse](gatewayRuntimeControlClient, client.RuntimeControlClient.ShouldCompactBeforeUserMessage),
	protocol.MethodRuntimeSubmitUserMessage:              gatewayClientCall[client.RuntimeControlClient, serverapi.RuntimeSubmitUserMessageRequest, serverapi.RuntimeSubmitUserMessageResponse](gatewayRuntimeControlClient, client.RuntimeControlClient.SubmitUserMessage),
	protocol.MethodRuntimeSubmitUserTurn:                 gatewayClientCall[client.RuntimeControlClient, serverapi.RuntimeSubmitUserTurnRequest, serverapi.RuntimeSubmitUserTurnResponse](gatewayRuntimeControlClient, client.RuntimeControlClient.SubmitUserTurn),
	protocol.MethodRuntimeSubmitUserShellCommand:         gatewayClientCallNoResponse[client.RuntimeControlClient, serverapi.RuntimeSubmitUserShellCommandRequest](gatewayRuntimeControlClient, client.RuntimeControlClient.SubmitUserShellCommand),
	protocol.MethodRuntimeCompactContext:                 gatewayClientCallNoResponse[client.RuntimeControlClient, serverapi.RuntimeCompactContextRequest](gatewayRuntimeControlClient, client.RuntimeControlClient.CompactContext),
	protocol.MethodRuntimeCompactContextForPreSubmit:     gatewayClientCallNoResponse[client.RuntimeControlClient, serverapi.RuntimeCompactContextForPreSubmitRequest](gatewayRuntimeControlClient, client.RuntimeControlClient.CompactContextForPreSubmit),
	protocol.MethodRuntimeHasQueuedUserWork:              gatewayClientCall[client.RuntimeControlClient, serverapi.RuntimeHasQueuedUserWorkRequest, serverapi.RuntimeHasQueuedUserWorkResponse](gatewayRuntimeControlClient, client.RuntimeControlClient.HasQueuedUserWork),
	protocol.MethodRuntimeSubmitQueuedUserMessages:       gatewayClientCall[client.RuntimeControlClient, serverapi.RuntimeSubmitQueuedUserMessagesRequest, serverapi.RuntimeSubmitQueuedUserMessagesResponse](gatewayRuntimeControlClient, client.RuntimeControlClient.SubmitQueuedUserMessages),
	protocol.MethodRuntimeInterrupt:                      gatewayClientCallNoResponse[client.RuntimeControlClient, serverapi.RuntimeInterruptRequest](gatewayRuntimeControlClient, client.RuntimeControlClient.Interrupt),
	protocol.MethodRuntimeQueueUserMessage:               gatewayClientCall[client.RuntimeControlClient, serverapi.RuntimeQueueUserMessageRequest, serverapi.RuntimeQueueUserMessageResponse](gatewayRuntimeControlClient, client.RuntimeControlClient.QueueUserMessage),
	protocol.MethodRuntimeDiscardQueuedUserMessage:       gatewayClientCall[client.RuntimeControlClient, serverapi.RuntimeDiscardQueuedUserMessageRequest, serverapi.RuntimeDiscardQueuedUserMessageResponse](gatewayRuntimeControlClient, client.RuntimeControlClient.DiscardQueuedUserMessage),
	protocol.MethodRuntimeRecordPromptHistory:            gatewayClientCallNoResponse[client.RuntimeControlClient, serverapi.RuntimeRecordPromptHistoryRequest](gatewayRuntimeControlClient, client.RuntimeControlClient.RecordPromptHistory),
	protocol.MethodRuntimeGoalShow:                       gatewayClientCall[client.RuntimeControlClient, serverapi.RuntimeGoalShowRequest, serverapi.RuntimeGoalShowResponse](gatewayRuntimeControlClient, client.RuntimeControlClient.ShowGoal),
	protocol.MethodRuntimeGoalSet:                        gatewayClientCall[client.RuntimeControlClient, serverapi.RuntimeGoalSetRequest, serverapi.RuntimeGoalShowResponse](gatewayRuntimeControlClient, client.RuntimeControlClient.SetGoal),
	protocol.MethodRuntimeGoalPause:                      gatewayClientCall[client.RuntimeControlClient, serverapi.RuntimeGoalStatusRequest, serverapi.RuntimeGoalShowResponse](gatewayRuntimeControlClient, client.RuntimeControlClient.PauseGoal),
	protocol.MethodRuntimeGoalResume:                     gatewayClientCall[client.RuntimeControlClient, serverapi.RuntimeGoalStatusRequest, serverapi.RuntimeGoalShowResponse](gatewayRuntimeControlClient, client.RuntimeControlClient.ResumeGoal),
	protocol.MethodRuntimeGoalComplete:                   gatewayClientCall[client.RuntimeControlClient, serverapi.RuntimeGoalStatusRequest, serverapi.RuntimeGoalShowResponse](gatewayRuntimeControlClient, client.RuntimeControlClient.CompleteGoal),
	protocol.MethodRuntimeGoalClear:                      gatewayClientCall[client.RuntimeControlClient, serverapi.RuntimeGoalClearRequest, serverapi.RuntimeGoalShowResponse](gatewayRuntimeControlClient, client.RuntimeControlClient.ClearGoal),
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
	protocol.MethodProcessGet:          gatewayClientCall[client.ProcessViewClient, serverapi.ProcessGetRequest, serverapi.ProcessGetResponse](gatewayProcessViewClient, client.ProcessViewClient.GetProcess),
	protocol.MethodProcessKill:         gatewayClientCall[client.ProcessControlClient, serverapi.ProcessKillRequest, serverapi.ProcessKillResponse](gatewayProcessControlClient, client.ProcessControlClient.KillProcess),
	protocol.MethodProcessInlineOutput: gatewayClientCall[client.ProcessControlClient, serverapi.ProcessInlineOutputRequest, serverapi.ProcessInlineOutputResponse](gatewayProcessControlClient, client.ProcessControlClient.GetInlineOutput),
	protocol.MethodAskListPending:      gatewayClientCall[client.AskViewClient, serverapi.AskListPendingBySessionRequest, serverapi.AskListPendingBySessionResponse](gatewayAskViewClient, client.AskViewClient.ListPendingAsksBySession),
	protocol.MethodAskAnswer:           gatewayClientCallNoResponse[client.PromptControlClient, serverapi.AskAnswerRequest](gatewayPromptControlClient, client.PromptControlClient.AnswerAsk),
	protocol.MethodApprovalListPending: gatewayClientCall[client.ApprovalViewClient, serverapi.ApprovalListPendingBySessionRequest, serverapi.ApprovalListPendingBySessionResponse](gatewayApprovalViewClient, client.ApprovalViewClient.ListPendingApprovalsBySession),
	protocol.MethodApprovalAnswer:      gatewayClientCallNoResponse[client.PromptControlClient, serverapi.ApprovalAnswerRequest](gatewayPromptControlClient, client.PromptControlClient.AnswerApproval),
}
