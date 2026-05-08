package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"

	"builder/server/auth"
	"builder/server/core"
	"builder/shared/client"
	"builder/shared/clientui"
	"builder/shared/protocol"
	"builder/shared/rpccontract"
	"builder/shared/rpcwire"
	"builder/shared/serverapi"
)

type Gateway struct {
	core     *core.Core
	identity protocol.ServerIdentity
}

var gatewayAllowedPreAuthMethods = protocolAllowedPreAuthMethodSet()
var gatewaySubscriptionMethods = protocolSubscriptionMethodSet()

func protocolAllowedPreAuthMethodSet() map[string]struct{} {
	allowed := rpccontract.AllowedPreAuthMethods()
	set := make(map[string]struct{}, len(allowed))
	for _, method := range allowed {
		set[strings.TrimSpace(method)] = struct{}{}
	}
	return set
}

func protocolSubscriptionMethodSet() map[string]struct{} {
	methods := rpccontract.SubscriptionMethods()
	set := make(map[string]struct{}, len(methods))
	for _, method := range methods {
		set[strings.TrimSpace(method)] = struct{}{}
	}
	return set
}

type connectionState struct {
	handshakeDone         bool
	attachedProject       string
	attachedWorkspaceID   string
	attachedWorkspaceRoot string
	attachedSession       string
}

type gatewaySubscriptionHandler func(g *Gateway, conn rpcwire.Conn, ctx context.Context, state *connectionState, req protocol.Request)

var gatewaySubscriptionHandlers = map[string]gatewaySubscriptionHandler{
	protocol.MethodSessionSubscribeActivity: (*Gateway).serveSessionActivitySubscription,
	protocol.MethodProcessSubscribeOutput:   (*Gateway).serveProcessOutputSubscription,
	protocol.MethodPromptSubscribeActivity:  (*Gateway).servePromptActivitySubscription,
}

func NewGateway(appCore *core.Core, identity protocol.ServerIdentity) (*Gateway, error) {
	if appCore == nil {
		return nil, errors.New("server core is required")
	}
	if strings.TrimSpace(identity.ProtocolVersion) == "" {
		return nil, errors.New("server identity is required")
	}
	return &Gateway{core: appCore, identity: identity}, nil
}

func (g *Gateway) Handler() http.Handler {
	return rpcwire.NewWebSocketTransport().Handler(g.handleConn)
}

func (g *Gateway) handleConn(ctx context.Context, conn rpcwire.Conn) {
	defer func() { _ = conn.Close() }()
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-ctx.Done():
		case <-conn.Closed():
		}
		cancel()
	}()
	state := &connectionState{}
	for {
		req, err := receiveRequest(connCtx, conn)
		if err != nil {
			return
		}
		if req.Method == protocol.MethodRunPrompt {
			if !g.serveRunPrompt(conn, connCtx, state, req) {
				return
			}
			continue
		}
		if isSubscriptionMethod(req.Method) {
			g.serveSubscription(conn, connCtx, state, req)
			return
		}
		resp := g.dispatch(connCtx, state, req)
		if !sendResponse(connCtx, conn, resp) {
			return
		}
	}
}

func (g *Gateway) serveRunPrompt(conn rpcwire.Conn, ctx context.Context, state *connectionState, req protocol.Request) bool {
	if err := req.Validate(); err != nil {
		return sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidRequest, err.Error()))
	}
	if !state.handshakeDone {
		return sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidRequest, "handshake is required before other methods"))
	}
	ready, err := g.serverAuthReady(ctx)
	if err != nil {
		return sendResponse(ctx, conn, responseForError(req.ID, err))
	}
	if !ready {
		return sendResponse(ctx, conn, responseForError(req.ID, serverapi.ErrServerAuthRequired))
	}
	params, err := decodeParams[serverapi.RunPromptRequest](req.Params)
	if err != nil {
		return sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidParams, err.Error()))
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var progressBroken atomic.Bool
	progress := serverapi.RunPromptProgressFunc(func(update serverapi.RunPromptProgress) {
		if progressBroken.Load() {
			return
		}
		if err := sendNotification(runCtx, conn, protocol.MethodRunPromptProgress, update); err != nil {
			if progressBroken.CompareAndSwap(false, true) {
				cancel()
			}
		}
	})
	runClient, err := g.runPromptClientForState(runCtx, state)
	if err != nil {
		return sendResponse(ctx, conn, responseForError(req.ID, err))
	}
	resp, err := runClient.RunPrompt(runCtx, params, progress)
	if err != nil {
		return sendResponse(ctx, conn, responseForError(req.ID, err))
	}
	return sendResponse(ctx, conn, protocol.NewSuccessResponse(req.ID, resp))
}

func (g *Gateway) dispatch(ctx context.Context, state *connectionState, req protocol.Request) protocol.Response {
	if err := req.Validate(); err != nil {
		return protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidRequest, err.Error())
	}
	if req.Method != protocol.MethodHandshake && !state.handshakeDone {
		return protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidRequest, "handshake is required before other methods")
	}
	if g.methodRequiresServerAuth(req.Method) {
		ready, err := g.serverAuthReady(ctx)
		if err != nil {
			return responseForError(req.ID, err)
		}
		if !ready {
			return responseForError(req.ID, serverapi.ErrServerAuthRequired)
		}
	}
	switch req.Method {
	case protocol.MethodHandshake:
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
	case protocol.MethodAuthGetBootstrapStatus:
		return decodeAndHandle(req, func(params serverapi.AuthGetBootstrapStatusRequest) (serverapi.AuthGetBootstrapStatusResponse, error) {
			client := g.core.AuthBootstrapClient()
			if client == nil {
				return serverapi.AuthGetBootstrapStatusResponse{}, serverapi.ErrServerAuthRequired
			}
			return client.GetAuthBootstrapStatus(ctx, params)
		})
	case protocol.MethodAuthCompleteBootstrap:
		return decodeAndHandle(req, func(params serverapi.AuthCompleteBootstrapRequest) (serverapi.AuthCompleteBootstrapResponse, error) {
			client := g.core.AuthBootstrapClient()
			if client == nil {
				return serverapi.AuthCompleteBootstrapResponse{}, serverapi.ErrServerAuthRequired
			}
			return client.CompleteAuthBootstrap(ctx, params)
		})
	case protocol.MethodAuthGetStatus:
		return decodeAndHandle(req, func(params serverapi.AuthStatusRequest) (serverapi.AuthStatusResponse, error) {
			client := g.core.AuthStatusClient()
			if client == nil {
				return serverapi.AuthStatusResponse{}, serverapi.ErrServerAuthRequired
			}
			return client.GetAuthStatus(ctx, params)
		})
	case protocol.MethodAttachProject:
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
	case protocol.MethodAttachSession:
		return decodeAndHandle(req, func(params protocol.AttachSessionRequest) (protocol.AttachResponse, error) {
			if err := params.Validate(); err != nil {
				return protocol.AttachResponse{}, err
			}
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return protocol.AttachResponse{}, err
			}
			state.attachedWorkspaceID = ""
			state.attachedWorkspaceRoot = ""
			state.attachedSession = params.SessionID
			return protocol.AttachResponse{Kind: "session", SessionID: params.SessionID}, nil
		})
	case protocol.MethodProjectList:
		return decodeAndHandle(req, func(params serverapi.ProjectListRequest) (serverapi.ProjectListResponse, error) {
			return g.core.ProjectViewClient().ListProjects(ctx, params)
		})
	case protocol.MethodProjectResolvePath:
		return decodeAndHandle(req, func(params serverapi.ProjectResolvePathRequest) (serverapi.ProjectResolvePathResponse, error) {
			return g.core.ProjectViewClient().ResolveProjectPath(ctx, params)
		})
	case protocol.MethodProjectPlanWorkspaceBinding:
		return decodeAndHandle(req, func(params serverapi.ProjectBindingPlanRequest) (serverapi.ProjectBindingPlanResponse, error) {
			return g.core.ProjectViewClient().PlanWorkspaceBinding(ctx, params)
		})
	case protocol.MethodProjectCreate:
		return decodeAndHandle(req, func(params serverapi.ProjectCreateRequest) (serverapi.ProjectCreateResponse, error) {
			return g.core.ProjectViewClient().CreateProject(ctx, params)
		})
	case protocol.MethodProjectAttachWorkspace:
		return decodeAndHandle(req, func(params serverapi.ProjectAttachWorkspaceRequest) (serverapi.ProjectAttachWorkspaceResponse, error) {
			return g.core.ProjectViewClient().AttachWorkspaceToProject(ctx, params)
		})
	case protocol.MethodProjectRebindWorkspace:
		return decodeAndHandle(req, func(params serverapi.ProjectRebindWorkspaceRequest) (serverapi.ProjectRebindWorkspaceResponse, error) {
			return g.core.ProjectViewClient().RebindWorkspace(ctx, params)
		})
	case protocol.MethodProjectGetOverview:
		return decodeAndHandle(req, func(params serverapi.ProjectGetOverviewRequest) (serverapi.ProjectGetOverviewResponse, error) {
			return g.core.ProjectViewClient().GetProjectOverview(ctx, params)
		})
	case protocol.MethodSessionListByProject:
		return decodeAndHandle(req, func(params serverapi.SessionListByProjectRequest) (serverapi.SessionListByProjectResponse, error) {
			return g.core.ProjectViewClient().ListSessionsByProject(ctx, params)
		})
	case protocol.MethodSessionPlan:
		return decodeAndHandle(req, func(params serverapi.SessionPlanRequest) (serverapi.SessionPlanResponse, error) {
			launchClient, err := g.sessionLaunchClientForState(ctx, state)
			if err != nil {
				return serverapi.SessionPlanResponse{}, err
			}
			return launchClient.PlanSession(ctx, params)
		})
	case protocol.MethodSessionGetMainView:
		return decodeAndHandle(req, func(params serverapi.SessionMainViewRequest) (serverapi.SessionMainViewResponse, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return serverapi.SessionMainViewResponse{}, err
			}
			return g.core.SessionViewClient().GetSessionMainView(ctx, params)
		})
	case protocol.MethodSessionGetTranscriptPage:
		return decodeAndHandle(req, func(params serverapi.SessionTranscriptPageRequest) (serverapi.SessionTranscriptPageResponse, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return serverapi.SessionTranscriptPageResponse{}, err
			}
			return g.core.SessionViewClient().GetSessionTranscriptPage(ctx, params)
		})
	case protocol.MethodSessionGetCommittedTranscriptSuffix:
		return decodeAndHandle(req, func(params serverapi.SessionCommittedTranscriptSuffixRequest) (serverapi.SessionCommittedTranscriptSuffixResponse, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return serverapi.SessionCommittedTranscriptSuffixResponse{}, err
			}
			suffixClient, ok := g.core.SessionViewClient().(client.SessionCommittedTranscriptSuffixClient)
			if !ok {
				return serverapi.SessionCommittedTranscriptSuffixResponse{}, errors.New("session committed transcript suffix client is required")
			}
			return suffixClient.GetSessionCommittedTranscriptSuffix(ctx, params)
		})
	case protocol.MethodSessionGetInitialInput:
		return decodeAndHandle(req, func(params serverapi.SessionInitialInputRequest) (serverapi.SessionInitialInputResponse, error) {
			if err := g.requireSessionInActiveProjectIfPresent(ctx, state, params.SessionID); err != nil {
				return serverapi.SessionInitialInputResponse{}, err
			}
			return g.core.SessionLifecycleClient().GetInitialInput(ctx, params)
		})
	case protocol.MethodSessionPersistInputDraft:
		return decodeAndHandle(req, func(params serverapi.SessionPersistInputDraftRequest) (serverapi.SessionPersistInputDraftResponse, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return serverapi.SessionPersistInputDraftResponse{}, err
			}
			return g.core.SessionLifecycleClient().PersistInputDraft(ctx, params)
		})
	case protocol.MethodSessionRetargetWorkspace:
		return decodeAndHandle(req, func(params serverapi.SessionRetargetWorkspaceRequest) (serverapi.SessionRetargetWorkspaceResponse, error) {
			// `builder rebind <session-id> <new-path>` opens an unscoped remote and only knows
			// the target workspace root. Enforce project isolation when the connection explicitly
			// attached a project, but do not inherit the daemon's default project as an implicit
			// authorization scope for this session-level maintenance RPC.
			if err := g.requireSessionInAttachedProject(ctx, state, params.SessionID); err != nil {
				return serverapi.SessionRetargetWorkspaceResponse{}, err
			}
			return g.core.SessionLifecycleClient().RetargetSessionWorkspace(ctx, params)
		})
	case protocol.MethodSessionResolveTransition:
		return decodeAndHandle(req, func(params serverapi.SessionResolveTransitionRequest) (serverapi.SessionResolveTransitionResponse, error) {
			if err := g.requireSessionInActiveProjectIfPresent(ctx, state, params.SessionID); err != nil {
				return serverapi.SessionResolveTransitionResponse{}, err
			}
			return g.core.SessionLifecycleClient().ResolveTransition(ctx, params)
		})
	case protocol.MethodWorktreeList:
		return decodeAndHandle(req, func(params serverapi.WorktreeListRequest) (serverapi.WorktreeListResponse, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return serverapi.WorktreeListResponse{}, err
			}
			return g.core.WorktreeClient().ListWorktrees(ctx, params)
		})
	case protocol.MethodWorktreeCreateTargetResolve:
		return decodeAndHandle(req, func(params serverapi.WorktreeCreateTargetResolveRequest) (serverapi.WorktreeCreateTargetResolveResponse, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return serverapi.WorktreeCreateTargetResolveResponse{}, err
			}
			return g.core.WorktreeClient().ResolveWorktreeCreateTarget(ctx, params)
		})
	case protocol.MethodWorktreeCreate:
		return decodeAndHandle(req, func(params serverapi.WorktreeCreateRequest) (serverapi.WorktreeCreateResponse, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return serverapi.WorktreeCreateResponse{}, err
			}
			return g.core.WorktreeClient().CreateWorktree(ctx, params)
		})
	case protocol.MethodWorktreeSwitch:
		return decodeAndHandle(req, func(params serverapi.WorktreeSwitchRequest) (serverapi.WorktreeSwitchResponse, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return serverapi.WorktreeSwitchResponse{}, err
			}
			return g.core.WorktreeClient().SwitchWorktree(ctx, params)
		})
	case protocol.MethodWorktreeDelete:
		return decodeAndHandle(req, func(params serverapi.WorktreeDeleteRequest) (serverapi.WorktreeDeleteResponse, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return serverapi.WorktreeDeleteResponse{}, err
			}
			return g.core.WorktreeClient().DeleteWorktree(ctx, params)
		})
	case protocol.MethodSessionRuntimeActivate:
		return decodeAndHandle(req, func(params serverapi.SessionRuntimeActivateRequest) (serverapi.SessionRuntimeActivateResponse, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return serverapi.SessionRuntimeActivateResponse{}, err
			}
			return g.core.SessionRuntimeClient().ActivateSessionRuntime(ctx, params)
		})
	case protocol.MethodSessionRuntimeRelease:
		return decodeAndHandle(req, func(params serverapi.SessionRuntimeReleaseRequest) (serverapi.SessionRuntimeReleaseResponse, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return serverapi.SessionRuntimeReleaseResponse{}, err
			}
			return g.core.SessionRuntimeClient().ReleaseSessionRuntime(ctx, params)
		})
	case protocol.MethodRunGet:
		return decodeAndHandle(req, func(params serverapi.RunGetRequest) (serverapi.RunGetResponse, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return serverapi.RunGetResponse{}, err
			}
			return g.core.SessionViewClient().GetRun(ctx, params)
		})
	case protocol.MethodRuntimeSetSessionName:
		return decodeAndHandle(req, func(params serverapi.RuntimeSetSessionNameRequest) (struct{}, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return struct{}{}, err
			}
			return struct{}{}, g.core.RuntimeControlClient().SetSessionName(ctx, params)
		})
	case protocol.MethodRuntimeSetThinkingLevel:
		return decodeAndHandle(req, func(params serverapi.RuntimeSetThinkingLevelRequest) (struct{}, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return struct{}{}, err
			}
			return struct{}{}, g.core.RuntimeControlClient().SetThinkingLevel(ctx, params)
		})
	case protocol.MethodRuntimeSetFastModeEnabled:
		return decodeAndHandle(req, func(params serverapi.RuntimeSetFastModeEnabledRequest) (serverapi.RuntimeSetFastModeEnabledResponse, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return serverapi.RuntimeSetFastModeEnabledResponse{}, err
			}
			return g.core.RuntimeControlClient().SetFastModeEnabled(ctx, params)
		})
	case protocol.MethodRuntimeSetReviewerEnabled:
		return decodeAndHandle(req, func(params serverapi.RuntimeSetReviewerEnabledRequest) (serverapi.RuntimeSetReviewerEnabledResponse, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return serverapi.RuntimeSetReviewerEnabledResponse{}, err
			}
			return g.core.RuntimeControlClient().SetReviewerEnabled(ctx, params)
		})
	case protocol.MethodRuntimeSetAutoCompactionEnabled:
		return decodeAndHandle(req, func(params serverapi.RuntimeSetAutoCompactionEnabledRequest) (serverapi.RuntimeSetAutoCompactionEnabledResponse, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return serverapi.RuntimeSetAutoCompactionEnabledResponse{}, err
			}
			return g.core.RuntimeControlClient().SetAutoCompactionEnabled(ctx, params)
		})
	case protocol.MethodRuntimeAppendLocalEntry:
		return decodeAndHandle(req, func(params serverapi.RuntimeAppendLocalEntryRequest) (struct{}, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return struct{}{}, err
			}
			return struct{}{}, g.core.RuntimeControlClient().AppendLocalEntry(ctx, params)
		})
	case protocol.MethodRuntimeShouldCompactBeforeUserMessage:
		return decodeAndHandle(req, func(params serverapi.RuntimeShouldCompactBeforeUserMessageRequest) (serverapi.RuntimeShouldCompactBeforeUserMessageResponse, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return serverapi.RuntimeShouldCompactBeforeUserMessageResponse{}, err
			}
			return g.core.RuntimeControlClient().ShouldCompactBeforeUserMessage(ctx, params)
		})
	case protocol.MethodRuntimeSubmitUserMessage:
		return decodeAndHandle(req, func(params serverapi.RuntimeSubmitUserMessageRequest) (serverapi.RuntimeSubmitUserMessageResponse, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return serverapi.RuntimeSubmitUserMessageResponse{}, err
			}
			return g.core.RuntimeControlClient().SubmitUserMessage(ctx, params)
		})
	case protocol.MethodRuntimeSubmitUserTurn:
		return decodeAndHandle(req, func(params serverapi.RuntimeSubmitUserTurnRequest) (serverapi.RuntimeSubmitUserTurnResponse, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return serverapi.RuntimeSubmitUserTurnResponse{}, err
			}
			return g.core.RuntimeControlClient().SubmitUserTurn(ctx, params)
		})
	case protocol.MethodRuntimeSubmitUserShellCommand:
		return decodeAndHandle(req, func(params serverapi.RuntimeSubmitUserShellCommandRequest) (struct{}, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return struct{}{}, err
			}
			return struct{}{}, g.core.RuntimeControlClient().SubmitUserShellCommand(ctx, params)
		})
	case protocol.MethodRuntimeCompactContext:
		return decodeAndHandle(req, func(params serverapi.RuntimeCompactContextRequest) (struct{}, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return struct{}{}, err
			}
			return struct{}{}, g.core.RuntimeControlClient().CompactContext(ctx, params)
		})
	case protocol.MethodRuntimeCompactContextForPreSubmit:
		return decodeAndHandle(req, func(params serverapi.RuntimeCompactContextForPreSubmitRequest) (struct{}, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return struct{}{}, err
			}
			return struct{}{}, g.core.RuntimeControlClient().CompactContextForPreSubmit(ctx, params)
		})
	case protocol.MethodRuntimeHasQueuedUserWork:
		return decodeAndHandle(req, func(params serverapi.RuntimeHasQueuedUserWorkRequest) (serverapi.RuntimeHasQueuedUserWorkResponse, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return serverapi.RuntimeHasQueuedUserWorkResponse{}, err
			}
			return g.core.RuntimeControlClient().HasQueuedUserWork(ctx, params)
		})
	case protocol.MethodRuntimeSubmitQueuedUserMessages:
		return decodeAndHandle(req, func(params serverapi.RuntimeSubmitQueuedUserMessagesRequest) (serverapi.RuntimeSubmitQueuedUserMessagesResponse, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return serverapi.RuntimeSubmitQueuedUserMessagesResponse{}, err
			}
			return g.core.RuntimeControlClient().SubmitQueuedUserMessages(ctx, params)
		})
	case protocol.MethodRuntimeInterrupt:
		return decodeAndHandle(req, func(params serverapi.RuntimeInterruptRequest) (struct{}, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return struct{}{}, err
			}
			return struct{}{}, g.core.RuntimeControlClient().Interrupt(ctx, params)
		})
	case protocol.MethodRuntimeQueueUserMessage:
		return decodeAndHandle(req, func(params serverapi.RuntimeQueueUserMessageRequest) (serverapi.RuntimeQueueUserMessageResponse, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return serverapi.RuntimeQueueUserMessageResponse{}, err
			}
			return g.core.RuntimeControlClient().QueueUserMessage(ctx, params)
		})
	case protocol.MethodRuntimeDiscardQueuedUserMessage:
		return decodeAndHandle(req, func(params serverapi.RuntimeDiscardQueuedUserMessageRequest) (serverapi.RuntimeDiscardQueuedUserMessageResponse, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return serverapi.RuntimeDiscardQueuedUserMessageResponse{}, err
			}
			return g.core.RuntimeControlClient().DiscardQueuedUserMessage(ctx, params)
		})
	case protocol.MethodRuntimeRecordPromptHistory:
		return decodeAndHandle(req, func(params serverapi.RuntimeRecordPromptHistoryRequest) (struct{}, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return struct{}{}, err
			}
			return struct{}{}, g.core.RuntimeControlClient().RecordPromptHistory(ctx, params)
		})
	case protocol.MethodRuntimeGoalShow:
		return decodeAndHandle(req, func(params serverapi.RuntimeGoalShowRequest) (serverapi.RuntimeGoalShowResponse, error) {
			if err := g.requireGoalSessionAccess(ctx, state, params.SessionID); err != nil {
				return serverapi.RuntimeGoalShowResponse{}, err
			}
			return g.core.RuntimeControlClient().ShowGoal(ctx, params)
		})
	case protocol.MethodRuntimeGoalSet:
		return decodeAndHandle(req, func(params serverapi.RuntimeGoalSetRequest) (serverapi.RuntimeGoalShowResponse, error) {
			if err := g.requireGoalSessionAccess(ctx, state, params.SessionID); err != nil {
				return serverapi.RuntimeGoalShowResponse{}, err
			}
			return g.core.RuntimeControlClient().SetGoal(ctx, params)
		})
	case protocol.MethodRuntimeGoalPause:
		return decodeAndHandle(req, func(params serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
			if err := g.requireGoalSessionAccess(ctx, state, params.SessionID); err != nil {
				return serverapi.RuntimeGoalShowResponse{}, err
			}
			return g.core.RuntimeControlClient().PauseGoal(ctx, params)
		})
	case protocol.MethodRuntimeGoalResume:
		return decodeAndHandle(req, func(params serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
			if err := g.requireGoalSessionAccess(ctx, state, params.SessionID); err != nil {
				return serverapi.RuntimeGoalShowResponse{}, err
			}
			return g.core.RuntimeControlClient().ResumeGoal(ctx, params)
		})
	case protocol.MethodRuntimeGoalComplete:
		return decodeAndHandle(req, func(params serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
			if err := g.requireGoalSessionAccess(ctx, state, params.SessionID); err != nil {
				return serverapi.RuntimeGoalShowResponse{}, err
			}
			return g.core.RuntimeControlClient().CompleteGoal(ctx, params)
		})
	case protocol.MethodRuntimeGoalClear:
		return decodeAndHandle(req, func(params serverapi.RuntimeGoalClearRequest) (serverapi.RuntimeGoalShowResponse, error) {
			if err := g.requireGoalSessionAccess(ctx, state, params.SessionID); err != nil {
				return serverapi.RuntimeGoalShowResponse{}, err
			}
			return g.core.RuntimeControlClient().ClearGoal(ctx, params)
		})
	case protocol.MethodProcessList:
		return decodeAndHandle(req, func(params serverapi.ProcessListRequest) (serverapi.ProcessListResponse, error) {
			if strings.TrimSpace(params.OwnerSessionID) != "" {
				if err := g.requireSessionInActiveProject(ctx, state, params.OwnerSessionID); err != nil {
					return serverapi.ProcessListResponse{}, err
				}
			}
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
	case protocol.MethodProcessGet:
		return decodeAndHandle(req, func(params serverapi.ProcessGetRequest) (serverapi.ProcessGetResponse, error) {
			return g.processInActiveProject(ctx, state, params.ProcessID)
		})
	case protocol.MethodProcessKill:
		return decodeAndHandle(req, func(params serverapi.ProcessKillRequest) (serverapi.ProcessKillResponse, error) {
			if _, err := g.processInActiveProject(ctx, state, params.ProcessID); err != nil {
				return serverapi.ProcessKillResponse{}, err
			}
			return g.core.ProcessControlClient().KillProcess(ctx, params)
		})
	case protocol.MethodProcessInlineOutput:
		return decodeAndHandle(req, func(params serverapi.ProcessInlineOutputRequest) (serverapi.ProcessInlineOutputResponse, error) {
			if _, err := g.processInActiveProject(ctx, state, params.ProcessID); err != nil {
				return serverapi.ProcessInlineOutputResponse{}, err
			}
			return g.core.ProcessControlClient().GetInlineOutput(ctx, params)
		})
	case protocol.MethodAskListPending:
		return decodeAndHandle(req, func(params serverapi.AskListPendingBySessionRequest) (serverapi.AskListPendingBySessionResponse, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return serverapi.AskListPendingBySessionResponse{}, err
			}
			return g.core.AskViewClient().ListPendingAsksBySession(ctx, params)
		})
	case protocol.MethodAskAnswer:
		return decodeAndHandle(req, func(params serverapi.AskAnswerRequest) (struct{}, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return struct{}{}, err
			}
			return struct{}{}, g.core.PromptControlClient().AnswerAsk(ctx, params)
		})
	case protocol.MethodApprovalListPending:
		return decodeAndHandle(req, func(params serverapi.ApprovalListPendingBySessionRequest) (serverapi.ApprovalListPendingBySessionResponse, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return serverapi.ApprovalListPendingBySessionResponse{}, err
			}
			return g.core.ApprovalViewClient().ListPendingApprovalsBySession(ctx, params)
		})
	case protocol.MethodApprovalAnswer:
		return decodeAndHandle(req, func(params serverapi.ApprovalAnswerRequest) (struct{}, error) {
			if err := g.requireSessionInActiveProject(ctx, state, params.SessionID); err != nil {
				return struct{}{}, err
			}
			return struct{}{}, g.core.PromptControlClient().AnswerApproval(ctx, params)
		})
	default:
		return protocol.NewErrorResponse(req.ID, protocol.ErrCodeMethodNotFound, fmt.Sprintf("method %q not found", req.Method))
	}
}

func (g *Gateway) resolveAttachedProjectWorkspace(ctx context.Context, projectID string, workspaceID string, workspaceRoot string) (string, string, error) {
	trimmedWorkspaceID := strings.TrimSpace(workspaceID)
	if trimmedWorkspaceID != "" {
		binding, err := g.core.MetadataStore().LookupWorkspaceBindingByID(ctx, trimmedWorkspaceID)
		if err != nil {
			return "", "", err
		}
		if strings.TrimSpace(binding.ProjectID) != strings.TrimSpace(projectID) {
			return "", "", fmt.Errorf("workspace %q is not bound to project %q", binding.CanonicalRoot, strings.TrimSpace(projectID))
		}
		return binding.WorkspaceID, strings.TrimSpace(binding.CanonicalRoot), nil
	}
	trimmedWorkspaceRoot := strings.TrimSpace(workspaceRoot)
	if trimmedWorkspaceRoot == "" {
		overview, err := g.core.ProjectViewClient().GetProjectOverview(ctx, serverapi.ProjectGetOverviewRequest{ProjectID: strings.TrimSpace(projectID)})
		if err != nil {
			return "", "", err
		}
		if len(overview.Overview.Workspaces) == 0 {
			return "", "", fmt.Errorf("project %q has no attached workspaces", strings.TrimSpace(projectID))
		}
		if len(overview.Overview.Workspaces) > 1 {
			return "", "", fmt.Errorf("project %q requires explicit workspace selection", strings.TrimSpace(projectID))
		}
		workspace := overview.Overview.Workspaces[0]
		return strings.TrimSpace(workspace.WorkspaceID), strings.TrimSpace(workspace.RootPath), nil
	}
	resolved, err := g.core.ProjectViewClient().ResolveProjectPath(ctx, serverapi.ProjectResolvePathRequest{Path: trimmedWorkspaceRoot})
	if err != nil {
		return "", "", err
	}
	if resolved.Binding == nil {
		return "", "", errors.Join(serverapi.ErrWorkspaceNotRegistered, fmt.Errorf("workspace %q is not registered", resolved.CanonicalRoot))
	}
	if strings.TrimSpace(resolved.Binding.ProjectID) != strings.TrimSpace(projectID) {
		return "", "", fmt.Errorf("workspace %q is not bound to project %q", resolved.Binding.CanonicalRoot, strings.TrimSpace(projectID))
	}
	return strings.TrimSpace(resolved.Binding.WorkspaceID), strings.TrimSpace(resolved.Binding.CanonicalRoot), nil
}

func (g *Gateway) sessionLaunchClientForState(ctx context.Context, state *connectionState) (service serverapi.SessionLaunchService, _ error) {
	projectID, err := g.activeProjectID(ctx, state)
	if err != nil {
		return nil, err
	}
	var launchClient any
	if strings.TrimSpace(state.attachedWorkspaceID) == "" {
		launchClient, err = g.core.SessionLaunchClientForProjectWorkspace(ctx, projectID, state.attachedWorkspaceRoot)
	} else {
		launchClient, err = g.core.SessionLaunchClientForProjectWorkspaceID(ctx, projectID, state.attachedWorkspaceID)
	}
	if err != nil {
		return nil, err
	}
	loopback, ok := launchClient.(interface {
		PlanSession(context.Context, serverapi.SessionPlanRequest) (serverapi.SessionPlanResponse, error)
	})
	if !ok {
		return nil, errors.New("session launch client does not implement service contract")
	}
	return loopback, nil
}

func (g *Gateway) runPromptClientForState(ctx context.Context, state *connectionState) (serverapi.RunPromptService, error) {
	projectID, err := g.activeProjectID(ctx, state)
	if err != nil {
		return nil, err
	}
	var runClient any
	if strings.TrimSpace(state.attachedWorkspaceID) == "" {
		runClient, err = g.core.RunPromptClientForProjectWorkspace(ctx, projectID, state.attachedWorkspaceRoot)
	} else {
		runClient, err = g.core.RunPromptClientForProjectWorkspaceID(ctx, projectID, state.attachedWorkspaceID)
	}
	if err != nil {
		return nil, err
	}
	service, ok := runClient.(interface {
		RunPrompt(context.Context, serverapi.RunPromptRequest, serverapi.RunPromptProgressSink) (serverapi.RunPromptResponse, error)
	})
	if !ok {
		return nil, errors.New("run prompt client does not implement service contract")
	}
	return service, nil
}

func (g *Gateway) activeProjectID(ctx context.Context, state *connectionState) (string, error) {
	if trimmed := strings.TrimSpace(state.attachedProject); trimmed != "" {
		return trimmed, nil
	}
	if trimmed := strings.TrimSpace(g.core.ProjectID()); trimmed != "" {
		return trimmed, nil
	}
	return "", fmt.Errorf("project attachment is required")
}

func (g *Gateway) requireSessionInActiveProject(ctx context.Context, state *connectionState, sessionID string) error {
	projectID, err := g.activeProjectID(ctx, state)
	if err != nil {
		return err
	}
	return g.core.SessionBelongsToProject(ctx, sessionID, projectID)
}

func (g *Gateway) requireGoalSessionAccess(ctx context.Context, state *connectionState, sessionID string) error {
	if strings.TrimSpace(state.attachedProject) == "" && strings.TrimSpace(g.core.ProjectID()) == "" {
		return nil
	}
	return g.requireSessionInActiveProject(ctx, state, sessionID)
}

func (g *Gateway) requireSessionInAttachedProject(ctx context.Context, state *connectionState, sessionID string) error {
	projectID := strings.TrimSpace(state.attachedProject)
	if projectID == "" {
		return nil
	}
	return g.core.SessionBelongsToProject(ctx, sessionID, projectID)
}

func (g *Gateway) requireSessionInActiveProjectIfPresent(ctx context.Context, state *connectionState, sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	return g.requireSessionInActiveProject(ctx, state, sessionID)
}

func (g *Gateway) processInActiveProject(ctx context.Context, state *connectionState, processID string) (serverapi.ProcessGetResponse, error) {
	resp, err := g.core.ProcessViewClient().GetProcess(ctx, serverapi.ProcessGetRequest{ProcessID: processID})
	if err != nil {
		return serverapi.ProcessGetResponse{}, err
	}
	if resp.Process == nil {
		return serverapi.ProcessGetResponse{}, fmt.Errorf("process %q not available", strings.TrimSpace(processID))
	}
	ownerSessionID := strings.TrimSpace(resp.Process.OwnerSessionID)
	if ownerSessionID == "" {
		return serverapi.ProcessGetResponse{}, fmt.Errorf("process %q not available", strings.TrimSpace(processID))
	}
	if err := g.requireSessionInActiveProject(ctx, state, ownerSessionID); err != nil {
		return serverapi.ProcessGetResponse{}, err
	}
	return resp, nil
}

func (g *Gateway) filterProcessesForActiveProject(ctx context.Context, state *connectionState, processes []clientui.BackgroundProcess) ([]clientui.BackgroundProcess, error) {
	filtered := make([]clientui.BackgroundProcess, 0, len(processes))
	for _, process := range processes {
		ownerSessionID := strings.TrimSpace(process.OwnerSessionID)
		if ownerSessionID == "" {
			continue
		}
		if err := g.requireSessionInActiveProject(ctx, state, ownerSessionID); err != nil {
			continue
		}
		filtered = append(filtered, process)
	}
	return filtered, nil
}

func (g *Gateway) serveSubscription(conn rpcwire.Conn, ctx context.Context, state *connectionState, req protocol.Request) {
	if err := req.Validate(); err != nil {
		_ = sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidRequest, err.Error()))
		return
	}
	if !state.handshakeDone {
		_ = sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidRequest, "handshake is required before other methods"))
		return
	}
	if g.methodRequiresServerAuth(req.Method) {
		ready, err := g.serverAuthReady(ctx)
		if err != nil {
			_ = sendResponse(ctx, conn, responseForError(req.ID, err))
			return
		}
		if !ready {
			_ = sendResponse(ctx, conn, responseForError(req.ID, serverapi.ErrServerAuthRequired))
			return
		}
	}
	handler, ok := gatewaySubscriptionHandlers[req.Method]
	if !ok {
		_ = sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeMethodNotFound, fmt.Sprintf("method %q not found", req.Method)))
		return
	}
	handler(g, conn, ctx, state, req)
}

func (g *Gateway) serveSessionActivitySubscription(conn rpcwire.Conn, ctx context.Context, state *connectionState, req protocol.Request) {
	params, err := decodeParams[serverapi.SessionActivitySubscribeRequest](req.Params)
	if err != nil {
		_ = sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidParams, err.Error()))
		return
	}
	if err := params.Validate(); err != nil {
		_ = sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidParams, err.Error()))
		return
	}
	if state.attachedSession != params.SessionID {
		_ = sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidRequest, "session attach is required before subscribing"))
		return
	}
	sub, err := g.core.SessionActivityClient().SubscribeSessionActivity(ctx, params)
	if err != nil {
		_ = sendResponse(ctx, conn, responseForError(req.ID, err))
		return
	}
	defer func() { _ = sub.Close() }()
	if !sendResponse(ctx, conn, protocol.NewSuccessResponse(req.ID, protocol.SubscribeResponse{Stream: protocol.MethodSessionActivityEvent})) {
		return
	}
	for {
		evt, err := sub.Next(ctx)
		if err != nil {
			_ = sendNotification(ctx, conn, protocol.MethodSessionActivityComplete, streamCompleteParams(err))
			return
		}
		if err := sendNotification(ctx, conn, protocol.MethodSessionActivityEvent, protocol.SessionActivityEventParams{Event: evt}); err != nil {
			return
		}
	}
}

func (g *Gateway) serveProcessOutputSubscription(conn rpcwire.Conn, ctx context.Context, state *connectionState, req protocol.Request) {
	params, err := decodeParams[serverapi.ProcessOutputSubscribeRequest](req.Params)
	if err != nil {
		_ = sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidParams, err.Error()))
		return
	}
	if err := params.Validate(); err != nil {
		_ = sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidParams, err.Error()))
		return
	}
	if _, err := g.processInActiveProject(ctx, state, params.ProcessID); err != nil {
		_ = sendResponse(ctx, conn, responseForError(req.ID, err))
		return
	}
	sub, err := g.core.ProcessOutputClient().SubscribeProcessOutput(ctx, params)
	if err != nil {
		_ = sendResponse(ctx, conn, responseForError(req.ID, err))
		return
	}
	defer func() { _ = sub.Close() }()
	if !sendResponse(ctx, conn, protocol.NewSuccessResponse(req.ID, protocol.SubscribeResponse{Stream: protocol.MethodProcessOutputEvent})) {
		return
	}
	for {
		chunk, err := sub.Next(ctx)
		if err != nil {
			_ = sendNotification(ctx, conn, protocol.MethodProcessOutputComplete, streamCompleteParams(err))
			return
		}
		if err := sendNotification(ctx, conn, protocol.MethodProcessOutputEvent, protocol.ProcessOutputEventParams{Chunk: chunk}); err != nil {
			return
		}
	}
}

func (g *Gateway) servePromptActivitySubscription(conn rpcwire.Conn, ctx context.Context, state *connectionState, req protocol.Request) {
	params, err := decodeParams[serverapi.PromptActivitySubscribeRequest](req.Params)
	if err != nil {
		_ = sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidParams, err.Error()))
		return
	}
	if err := params.Validate(); err != nil {
		_ = sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidParams, err.Error()))
		return
	}
	if state.attachedSession != params.SessionID {
		_ = sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidRequest, "session attach is required before subscribing"))
		return
	}
	sub, err := g.core.PromptActivityClient().SubscribePromptActivity(ctx, params)
	if err != nil {
		_ = sendResponse(ctx, conn, responseForError(req.ID, err))
		return
	}
	defer func() { _ = sub.Close() }()
	if !sendResponse(ctx, conn, protocol.NewSuccessResponse(req.ID, protocol.SubscribeResponse{Stream: protocol.MethodPromptActivityEvent})) {
		return
	}
	for {
		evt, err := sub.Next(ctx)
		if err != nil {
			_ = sendNotification(ctx, conn, protocol.MethodPromptActivityComplete, streamCompleteParams(err))
			return
		}
		if err := sendNotification(ctx, conn, protocol.MethodPromptActivityEvent, protocol.PromptActivityEventParams{Event: evt}); err != nil {
			return
		}
	}
}

func decodeAndHandle[TReq any, TResp any](req protocol.Request, handler func(TReq) (TResp, error)) protocol.Response {
	params, err := decodeParams[TReq](req.Params)
	if err != nil {
		return protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidParams, err.Error())
	}
	resp, err := handler(params)
	if err != nil {
		return responseForError(req.ID, err)
	}
	return protocol.NewSuccessResponse(req.ID, resp)
}

func isSubscriptionMethod(method string) bool {
	_, ok := gatewaySubscriptionMethods[strings.TrimSpace(method)]
	return ok
}

func receiveRequest(ctx context.Context, conn rpcwire.Conn) (protocol.Request, error) {
	for {
		select {
		case <-ctx.Done():
			return protocol.Request{}, ctx.Err()
		case event, ok := <-conn.Events():
			if !ok {
				return protocol.Request{}, io.EOF
			}
			if event.Err != nil {
				return protocol.Request{}, event.Err
			}
			return event.Frame.Request(), nil
		}
	}
}

func sendNotification(ctx context.Context, conn rpcwire.Conn, method string, params any) error {
	data, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return conn.Send(ctx, rpcwire.FrameFromRequest(protocol.Request{JSONRPC: protocol.JSONRPCVersion, Method: method, Params: data}))
}

func sendResponse(ctx context.Context, conn rpcwire.Conn, resp protocol.Response) bool {
	return conn.Send(ctx, rpcwire.FrameFromResponse(resp)) == nil
}

func responseForError(id string, err error) protocol.Response {
	code, message := protocolError(err)
	return protocol.NewErrorResponse(id, code, message)
}

func protocolError(err error) (int, string) {
	if err == nil {
		return protocol.ErrCodeInternalError, "internal error"
	}
	message := strings.TrimSpace(err.Error())
	if errors.Is(err, context.Canceled) {
		if message == "" || message == context.Canceled.Error() {
			message = "request canceled by client"
		}
		return protocol.ErrCodeRequestCanceled, message
	}
	if errors.Is(err, serverapi.ErrStreamGap) {
		return protocol.ErrCodeStreamGap, message
	}
	if errors.Is(err, serverapi.ErrWorkspaceNotRegistered) {
		return protocol.ErrCodeWorkspaceNotRegistered, message
	}
	if errors.Is(err, serverapi.ErrProjectNotFound) {
		return protocol.ErrCodeProjectNotFound, message
	}
	if errors.Is(err, serverapi.ErrProjectUnavailable) {
		return protocol.ErrCodeProjectUnavailable, message
	}
	if errors.Is(err, serverapi.ErrSessionAlreadyControlled) {
		return protocol.ErrCodeSessionAlreadyControlled, message
	}
	if errors.Is(err, serverapi.ErrInvalidControllerLease) {
		return protocol.ErrCodeInvalidControllerLease, message
	}
	if errors.Is(err, serverapi.ErrRuntimeUnavailable) {
		return protocol.ErrCodeRuntimeUnavailable, message
	}
	if errors.Is(err, serverapi.ErrStreamUnavailable) {
		return protocol.ErrCodeStreamUnavailable, message
	}
	if errors.Is(err, serverapi.ErrStreamFailed) {
		return protocol.ErrCodeStreamFailed, message
	}
	if errors.Is(err, serverapi.ErrPromptNotFound) {
		return protocol.ErrCodePromptNotFound, message
	}
	if errors.Is(err, serverapi.ErrPromptAlreadyResolved) {
		return protocol.ErrCodePromptResolved, message
	}
	if errors.Is(err, serverapi.ErrPromptUnsupported) {
		return protocol.ErrCodePromptUnsupported, message
	}
	if errors.Is(err, serverapi.ErrServerAuthRequired) || errors.Is(err, auth.ErrAuthNotConfigured) {
		return protocol.ErrCodeAuthRequired, message
	}
	return protocol.ErrCodeInternalError, message
}

func (g *Gateway) serverAuthReady(ctx context.Context) (bool, error) {
	if g == nil || g.core == nil || g.core.AuthManager() == nil {
		return false, nil
	}
	state, err := g.core.AuthManager().Load(ctx)
	if err != nil {
		return false, err
	}
	return auth.EvaluateStartupGate(state).Ready, nil
}

func (g *Gateway) methodRequiresServerAuth(method string) bool {
	trimmed := strings.TrimSpace(method)
	if trimmed == "" || trimmed == protocol.MethodHandshake {
		return false
	}
	if _, ok := gatewayAllowedPreAuthMethods[trimmed]; ok {
		return false
	}
	return true
}

func streamCompleteParams(err error) protocol.StreamCompleteParams {
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return protocol.StreamCompleteParams{}
	}
	code, message := protocolError(err)
	return protocol.StreamCompleteParams{Code: code, Message: message}
}

func decodeParams[T any](raw json.RawMessage) (T, error) {
	var zero T
	if len(raw) == 0 {
		return zero, nil
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		return zero, fmt.Errorf("decode params: %w", err)
	}
	return out, nil
}
