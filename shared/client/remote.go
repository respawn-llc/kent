package client

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"

	"builder/shared/config"
	"builder/shared/protocol"
	"builder/shared/rpcwire"
	"builder/shared/serverapi"
)

type Remote struct {
	plan          remoteDialPlan
	transport     rpcwire.ClientTransport
	mu            sync.Mutex
	control       *remoteControlConn
	identity      protocol.ServerIdentity
	projectID     string
	workspaceID   string
	workspaceRoot string
	closed        atomic.Bool
}

func DialRemote(ctx context.Context, record protocol.DiscoveryRecord) (*Remote, error) {
	return DialRemoteURL(ctx, record.RPCURL)
}

func DialRemoteURL(ctx context.Context, rpcURL string) (*Remote, error) {
	return dialRemoteURL(ctx, rpcURL, "", "", "")
}

func DialRemoteURLForProject(ctx context.Context, rpcURL string, projectID string) (*Remote, error) {
	return dialRemoteURL(ctx, rpcURL, projectID, "", "")
}

func DialRemoteURLForProjectWorkspace(ctx context.Context, rpcURL string, projectID string, workspaceRoot string) (*Remote, error) {
	return dialRemoteURL(ctx, rpcURL, projectID, "", workspaceRoot)
}

func DialRemoteURLForProjectWorkspaceID(ctx context.Context, rpcURL string, projectID string, workspaceID string) (*Remote, error) {
	return dialRemoteURL(ctx, rpcURL, projectID, workspaceID, "")
}

func DialConfiguredRemote(ctx context.Context, cfg config.App) (*Remote, error) {
	return dialConfiguredRemote(ctx, cfg, "", "", "")
}

func DialConfiguredRemoteForProject(ctx context.Context, cfg config.App, projectID string) (*Remote, error) {
	return dialConfiguredRemote(ctx, cfg, projectID, "", "")
}

func DialConfiguredRemoteForProjectWorkspace(ctx context.Context, cfg config.App, projectID string, workspaceRoot string) (*Remote, error) {
	return dialConfiguredRemote(ctx, cfg, projectID, "", workspaceRoot)
}

func DialConfiguredRemoteForProjectWorkspaceID(ctx context.Context, cfg config.App, projectID string, workspaceID string) (*Remote, error) {
	return dialConfiguredRemote(ctx, cfg, projectID, workspaceID, "")
}

func (c *Remote) Close() error {
	if c == nil {
		return nil
	}
	c.closed.Store(true)
	c.mu.Lock()
	control := c.control
	c.control = nil
	c.mu.Unlock()
	if control == nil {
		return nil
	}
	return control.Close()
}

func (c *Remote) Identity() protocol.ServerIdentity {
	if c == nil {
		return protocol.ServerIdentity{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.identity
}

func (c *Remote) ProjectID() string {
	if c == nil {
		return ""
	}
	return c.projectID
}

func (c *Remote) WorkspaceRoot() string {
	if c == nil {
		return ""
	}
	return c.workspaceRoot
}

func (c *Remote) WorkspaceID() string {
	if c == nil {
		return ""
	}
	return c.workspaceID
}

func (c *Remote) GetAuthBootstrapStatus(ctx context.Context, req serverapi.AuthGetBootstrapStatusRequest) (serverapi.AuthGetBootstrapStatusResponse, error) {
	var resp serverapi.AuthGetBootstrapStatusResponse
	return resp, c.callUnscoped(ctx, protocol.MethodAuthGetBootstrapStatus, req, &resp)
}

func (c *Remote) CompleteAuthBootstrap(ctx context.Context, req serverapi.AuthCompleteBootstrapRequest) (serverapi.AuthCompleteBootstrapResponse, error) {
	var resp serverapi.AuthCompleteBootstrapResponse
	return resp, c.callUnscoped(ctx, protocol.MethodAuthCompleteBootstrap, req, &resp)
}

func (c *Remote) GetAuthStatus(ctx context.Context, req serverapi.AuthStatusRequest) (serverapi.AuthStatusResponse, error) {
	var resp serverapi.AuthStatusResponse
	return resp, c.callUnscoped(ctx, protocol.MethodAuthGetStatus, req, &resp)
}

func (c *Remote) ListProjects(ctx context.Context, req serverapi.ProjectListRequest) (serverapi.ProjectListResponse, error) {
	var resp serverapi.ProjectListResponse
	return resp, c.callUnscoped(ctx, protocol.MethodProjectList, req, &resp)
}

func (c *Remote) ResolveProjectPath(ctx context.Context, req serverapi.ProjectResolvePathRequest) (serverapi.ProjectResolvePathResponse, error) {
	var resp serverapi.ProjectResolvePathResponse
	return resp, c.callUnscoped(ctx, protocol.MethodProjectResolvePath, req, &resp)
}

func (c *Remote) CreateProject(ctx context.Context, req serverapi.ProjectCreateRequest) (serverapi.ProjectCreateResponse, error) {
	var resp serverapi.ProjectCreateResponse
	return resp, c.callUnscoped(ctx, protocol.MethodProjectCreate, req, &resp)
}

func (c *Remote) AttachWorkspaceToProject(ctx context.Context, req serverapi.ProjectAttachWorkspaceRequest) (serverapi.ProjectAttachWorkspaceResponse, error) {
	var resp serverapi.ProjectAttachWorkspaceResponse
	return resp, c.callUnscoped(ctx, protocol.MethodProjectAttachWorkspace, req, &resp)
}

func (c *Remote) RebindWorkspace(ctx context.Context, req serverapi.ProjectRebindWorkspaceRequest) (serverapi.ProjectRebindWorkspaceResponse, error) {
	var resp serverapi.ProjectRebindWorkspaceResponse
	return resp, c.callUnscoped(ctx, protocol.MethodProjectRebindWorkspace, req, &resp)
}

func (c *Remote) GetProjectOverview(ctx context.Context, req serverapi.ProjectGetOverviewRequest) (serverapi.ProjectGetOverviewResponse, error) {
	var resp serverapi.ProjectGetOverviewResponse
	return resp, c.callUnscoped(ctx, protocol.MethodProjectGetOverview, req, &resp)
}

func (c *Remote) ListSessionsByProject(ctx context.Context, req serverapi.SessionListByProjectRequest) (serverapi.SessionListByProjectResponse, error) {
	var resp serverapi.SessionListByProjectResponse
	return resp, c.callUnscoped(ctx, protocol.MethodSessionListByProject, req, &resp)
}

func (c *Remote) PlanSession(ctx context.Context, req serverapi.SessionPlanRequest) (serverapi.SessionPlanResponse, error) {
	var resp serverapi.SessionPlanResponse
	return resp, c.call(ctx, protocol.MethodSessionPlan, req, &resp)
}

func (c *Remote) GetSessionMainView(ctx context.Context, req serverapi.SessionMainViewRequest) (serverapi.SessionMainViewResponse, error) {
	var resp serverapi.SessionMainViewResponse
	return resp, c.call(ctx, protocol.MethodSessionGetMainView, req, &resp)
}

func (c *Remote) GetSessionTranscriptPage(ctx context.Context, req serverapi.SessionTranscriptPageRequest) (serverapi.SessionTranscriptPageResponse, error) {
	var resp serverapi.SessionTranscriptPageResponse
	return resp, c.call(ctx, protocol.MethodSessionGetTranscriptPage, req, &resp)
}

func (c *Remote) GetSessionCommittedTranscriptSuffix(ctx context.Context, req serverapi.SessionCommittedTranscriptSuffixRequest) (serverapi.SessionCommittedTranscriptSuffixResponse, error) {
	var resp serverapi.SessionCommittedTranscriptSuffixResponse
	return resp, c.call(ctx, protocol.MethodSessionGetCommittedTranscriptSuffix, req, &resp)
}

func (c *Remote) GetInitialInput(ctx context.Context, req serverapi.SessionInitialInputRequest) (serverapi.SessionInitialInputResponse, error) {
	var resp serverapi.SessionInitialInputResponse
	return resp, c.call(ctx, protocol.MethodSessionGetInitialInput, req, &resp)
}

func (c *Remote) PersistInputDraft(ctx context.Context, req serverapi.SessionPersistInputDraftRequest) (serverapi.SessionPersistInputDraftResponse, error) {
	var resp serverapi.SessionPersistInputDraftResponse
	return resp, c.call(ctx, protocol.MethodSessionPersistInputDraft, req, &resp)
}

func (c *Remote) RetargetSessionWorkspace(ctx context.Context, req serverapi.SessionRetargetWorkspaceRequest) (serverapi.SessionRetargetWorkspaceResponse, error) {
	var resp serverapi.SessionRetargetWorkspaceResponse
	return resp, c.callUnscoped(ctx, protocol.MethodSessionRetargetWorkspace, req, &resp)
}

func (c *Remote) ResolveTransition(ctx context.Context, req serverapi.SessionResolveTransitionRequest) (serverapi.SessionResolveTransitionResponse, error) {
	var resp serverapi.SessionResolveTransitionResponse
	return resp, c.call(ctx, protocol.MethodSessionResolveTransition, req, &resp)
}

func (c *Remote) ListWorktrees(ctx context.Context, req serverapi.WorktreeListRequest) (serverapi.WorktreeListResponse, error) {
	var resp serverapi.WorktreeListResponse
	return resp, c.call(ctx, protocol.MethodWorktreeList, req, &resp)
}

func (c *Remote) ResolveWorktreeCreateTarget(ctx context.Context, req serverapi.WorktreeCreateTargetResolveRequest) (serverapi.WorktreeCreateTargetResolveResponse, error) {
	var resp serverapi.WorktreeCreateTargetResolveResponse
	return resp, c.call(ctx, protocol.MethodWorktreeCreateTargetResolve, req, &resp)
}

func (c *Remote) CreateWorktree(ctx context.Context, req serverapi.WorktreeCreateRequest) (serverapi.WorktreeCreateResponse, error) {
	var resp serverapi.WorktreeCreateResponse
	return resp, c.call(ctx, protocol.MethodWorktreeCreate, req, &resp)
}

func (c *Remote) SwitchWorktree(ctx context.Context, req serverapi.WorktreeSwitchRequest) (serverapi.WorktreeSwitchResponse, error) {
	var resp serverapi.WorktreeSwitchResponse
	return resp, c.call(ctx, protocol.MethodWorktreeSwitch, req, &resp)
}

func (c *Remote) DeleteWorktree(ctx context.Context, req serverapi.WorktreeDeleteRequest) (serverapi.WorktreeDeleteResponse, error) {
	var resp serverapi.WorktreeDeleteResponse
	return resp, c.call(ctx, protocol.MethodWorktreeDelete, req, &resp)
}

func (c *Remote) GetRun(ctx context.Context, req serverapi.RunGetRequest) (serverapi.RunGetResponse, error) {
	var resp serverapi.RunGetResponse
	return resp, c.call(ctx, protocol.MethodRunGet, req, &resp)
}

func (c *Remote) ActivateSessionRuntime(ctx context.Context, req serverapi.SessionRuntimeActivateRequest) (serverapi.SessionRuntimeActivateResponse, error) {
	var resp serverapi.SessionRuntimeActivateResponse
	return resp, c.call(ctx, protocol.MethodSessionRuntimeActivate, req, &resp)
}

func (c *Remote) ReleaseSessionRuntime(ctx context.Context, req serverapi.SessionRuntimeReleaseRequest) (serverapi.SessionRuntimeReleaseResponse, error) {
	var resp serverapi.SessionRuntimeReleaseResponse
	return resp, c.call(ctx, protocol.MethodSessionRuntimeRelease, req, &resp)
}

func (c *Remote) SetSessionName(ctx context.Context, req serverapi.RuntimeSetSessionNameRequest) error {
	return c.call(ctx, protocol.MethodRuntimeSetSessionName, req, nil)
}

func (c *Remote) SetThinkingLevel(ctx context.Context, req serverapi.RuntimeSetThinkingLevelRequest) error {
	return c.call(ctx, protocol.MethodRuntimeSetThinkingLevel, req, nil)
}

func (c *Remote) SetFastModeEnabled(ctx context.Context, req serverapi.RuntimeSetFastModeEnabledRequest) (serverapi.RuntimeSetFastModeEnabledResponse, error) {
	var resp serverapi.RuntimeSetFastModeEnabledResponse
	return resp, c.call(ctx, protocol.MethodRuntimeSetFastModeEnabled, req, &resp)
}

func (c *Remote) SetReviewerEnabled(ctx context.Context, req serverapi.RuntimeSetReviewerEnabledRequest) (serverapi.RuntimeSetReviewerEnabledResponse, error) {
	var resp serverapi.RuntimeSetReviewerEnabledResponse
	return resp, c.call(ctx, protocol.MethodRuntimeSetReviewerEnabled, req, &resp)
}

func (c *Remote) SetAutoCompactionEnabled(ctx context.Context, req serverapi.RuntimeSetAutoCompactionEnabledRequest) (serverapi.RuntimeSetAutoCompactionEnabledResponse, error) {
	var resp serverapi.RuntimeSetAutoCompactionEnabledResponse
	return resp, c.call(ctx, protocol.MethodRuntimeSetAutoCompactionEnabled, req, &resp)
}

func (c *Remote) AppendLocalEntry(ctx context.Context, req serverapi.RuntimeAppendLocalEntryRequest) error {
	return c.call(ctx, protocol.MethodRuntimeAppendLocalEntry, req, nil)
}

func (c *Remote) ShouldCompactBeforeUserMessage(ctx context.Context, req serverapi.RuntimeShouldCompactBeforeUserMessageRequest) (serverapi.RuntimeShouldCompactBeforeUserMessageResponse, error) {
	var resp serverapi.RuntimeShouldCompactBeforeUserMessageResponse
	return resp, c.call(ctx, protocol.MethodRuntimeShouldCompactBeforeUserMessage, req, &resp)
}

func (c *Remote) SubmitUserMessage(ctx context.Context, req serverapi.RuntimeSubmitUserMessageRequest) (serverapi.RuntimeSubmitUserMessageResponse, error) {
	var resp serverapi.RuntimeSubmitUserMessageResponse
	return resp, c.callDedicated(ctx, "runtime-submit-user-message", protocol.MethodRuntimeSubmitUserMessage, req, &resp)
}

func (c *Remote) SubmitUserTurn(ctx context.Context, req serverapi.RuntimeSubmitUserTurnRequest) (serverapi.RuntimeSubmitUserTurnResponse, error) {
	var resp serverapi.RuntimeSubmitUserTurnResponse
	return resp, c.callDedicated(ctx, "runtime-submit-user-turn", protocol.MethodRuntimeSubmitUserTurn, req, &resp)
}

func (c *Remote) SubmitUserShellCommand(ctx context.Context, req serverapi.RuntimeSubmitUserShellCommandRequest) error {
	return c.callDedicated(ctx, "runtime-submit-user-shell-command", protocol.MethodRuntimeSubmitUserShellCommand, req, nil)
}

func (c *Remote) CompactContext(ctx context.Context, req serverapi.RuntimeCompactContextRequest) error {
	return c.callDedicated(ctx, "runtime-compact-context", protocol.MethodRuntimeCompactContext, req, nil)
}

func (c *Remote) CompactContextForPreSubmit(ctx context.Context, req serverapi.RuntimeCompactContextForPreSubmitRequest) error {
	return c.callDedicated(ctx, "runtime-compact-context-pre-submit", protocol.MethodRuntimeCompactContextForPreSubmit, req, nil)
}

func (c *Remote) HasQueuedUserWork(ctx context.Context, req serverapi.RuntimeHasQueuedUserWorkRequest) (serverapi.RuntimeHasQueuedUserWorkResponse, error) {
	var resp serverapi.RuntimeHasQueuedUserWorkResponse
	return resp, c.call(ctx, protocol.MethodRuntimeHasQueuedUserWork, req, &resp)
}

func (c *Remote) SubmitQueuedUserMessages(ctx context.Context, req serverapi.RuntimeSubmitQueuedUserMessagesRequest) (serverapi.RuntimeSubmitQueuedUserMessagesResponse, error) {
	var resp serverapi.RuntimeSubmitQueuedUserMessagesResponse
	return resp, c.callDedicated(ctx, "runtime-submit-queued-user-messages", protocol.MethodRuntimeSubmitQueuedUserMessages, req, &resp)
}

func (c *Remote) Interrupt(ctx context.Context, req serverapi.RuntimeInterruptRequest) error {
	return c.callDedicated(ctx, "runtime-interrupt", protocol.MethodRuntimeInterrupt, req, nil)
}

func (c *Remote) QueueUserMessage(ctx context.Context, req serverapi.RuntimeQueueUserMessageRequest) error {
	return c.call(ctx, protocol.MethodRuntimeQueueUserMessage, req, nil)
}

func (c *Remote) DiscardQueuedUserMessagesMatching(ctx context.Context, req serverapi.RuntimeDiscardQueuedUserMessagesMatchingRequest) (serverapi.RuntimeDiscardQueuedUserMessagesMatchingResponse, error) {
	var resp serverapi.RuntimeDiscardQueuedUserMessagesMatchingResponse
	return resp, c.call(ctx, protocol.MethodRuntimeDiscardQueuedUserMessagesMatching, req, &resp)
}

func (c *Remote) RecordPromptHistory(ctx context.Context, req serverapi.RuntimeRecordPromptHistoryRequest) error {
	return c.call(ctx, protocol.MethodRuntimeRecordPromptHistory, req, nil)
}

func (c *Remote) ShowGoal(ctx context.Context, req serverapi.RuntimeGoalShowRequest) (serverapi.RuntimeGoalShowResponse, error) {
	var resp serverapi.RuntimeGoalShowResponse
	return resp, c.call(ctx, protocol.MethodRuntimeGoalShow, req, &resp)
}

func (c *Remote) SetGoal(ctx context.Context, req serverapi.RuntimeGoalSetRequest) (serverapi.RuntimeGoalShowResponse, error) {
	var resp serverapi.RuntimeGoalShowResponse
	return resp, c.call(ctx, protocol.MethodRuntimeGoalSet, req, &resp)
}

func (c *Remote) PauseGoal(ctx context.Context, req serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
	var resp serverapi.RuntimeGoalShowResponse
	return resp, c.call(ctx, protocol.MethodRuntimeGoalPause, req, &resp)
}

func (c *Remote) ResumeGoal(ctx context.Context, req serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
	var resp serverapi.RuntimeGoalShowResponse
	return resp, c.call(ctx, protocol.MethodRuntimeGoalResume, req, &resp)
}

func (c *Remote) CompleteGoal(ctx context.Context, req serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
	var resp serverapi.RuntimeGoalShowResponse
	return resp, c.call(ctx, protocol.MethodRuntimeGoalComplete, req, &resp)
}

func (c *Remote) ClearGoal(ctx context.Context, req serverapi.RuntimeGoalClearRequest) (serverapi.RuntimeGoalShowResponse, error) {
	var resp serverapi.RuntimeGoalShowResponse
	return resp, c.call(ctx, protocol.MethodRuntimeGoalClear, req, &resp)
}

func (c *Remote) ListProcesses(ctx context.Context, req serverapi.ProcessListRequest) (serverapi.ProcessListResponse, error) {
	var resp serverapi.ProcessListResponse
	return resp, c.call(ctx, protocol.MethodProcessList, req, &resp)
}

func (c *Remote) GetProcess(ctx context.Context, req serverapi.ProcessGetRequest) (serverapi.ProcessGetResponse, error) {
	var resp serverapi.ProcessGetResponse
	return resp, c.call(ctx, protocol.MethodProcessGet, req, &resp)
}

func (c *Remote) KillProcess(ctx context.Context, req serverapi.ProcessKillRequest) (serverapi.ProcessKillResponse, error) {
	var resp serverapi.ProcessKillResponse
	return resp, c.call(ctx, protocol.MethodProcessKill, req, &resp)
}

func (c *Remote) GetInlineOutput(ctx context.Context, req serverapi.ProcessInlineOutputRequest) (serverapi.ProcessInlineOutputResponse, error) {
	var resp serverapi.ProcessInlineOutputResponse
	return resp, c.call(ctx, protocol.MethodProcessInlineOutput, req, &resp)
}

func (c *Remote) ListPendingAsksBySession(ctx context.Context, req serverapi.AskListPendingBySessionRequest) (serverapi.AskListPendingBySessionResponse, error) {
	var resp serverapi.AskListPendingBySessionResponse
	return resp, c.call(ctx, protocol.MethodAskListPending, req, &resp)
}

func (c *Remote) AnswerAsk(ctx context.Context, req serverapi.AskAnswerRequest) error {
	return c.call(ctx, protocol.MethodAskAnswer, req, nil)
}

func (c *Remote) ListPendingApprovalsBySession(ctx context.Context, req serverapi.ApprovalListPendingBySessionRequest) (serverapi.ApprovalListPendingBySessionResponse, error) {
	var resp serverapi.ApprovalListPendingBySessionResponse
	return resp, c.call(ctx, protocol.MethodApprovalListPending, req, &resp)
}

func (c *Remote) AnswerApproval(ctx context.Context, req serverapi.ApprovalAnswerRequest) error {
	return c.call(ctx, protocol.MethodApprovalAnswer, req, nil)
}

func (c *Remote) ensureOpen() error {
	if c == nil {
		return errors.New("remote client is required")
	}
	if c.closed.Load() {
		return errors.New("remote client is closed")
	}
	return nil
}

func (c *Remote) call(ctx context.Context, method string, params any, out any) error {
	return c.callUnscoped(ctx, method, params, out)
}

func (c *Remote) callUnscoped(ctx context.Context, method string, params any, out any) error {
	control, err := c.ensureControl(ctx)
	if err != nil {
		return err
	}
	return control.call(ctx, method, params, out)
}

func (c *Remote) ensureControl(ctx context.Context) (*remoteControlConn, error) {
	if err := c.ensureOpen(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed.Load() {
		return nil, errors.New("remote client is closed")
	}
	if c.control != nil && !c.control.IsDone() {
		return c.control, nil
	}
	if c.control != nil {
		_ = c.control.Close()
		c.control = nil
	}
	conn, identity, err := c.openControlRPCConn(ctx)
	if err != nil {
		return nil, err
	}
	if c.closed.Load() {
		_ = conn.Close()
		return nil, errors.New("remote client is closed")
	}
	control := newRemoteControlConn(conn)
	c.control = control
	c.identity = identity
	return control, nil
}

func (c *Remote) openControlRPCConn(ctx context.Context) (rpcwire.Conn, protocol.ServerIdentity, error) {
	conn, err := c.plan.dial(ctx, c.transport)
	if err != nil {
		return nil, protocol.ServerIdentity{}, err
	}
	cleanup := func() { _ = conn.Close() }
	identity, err := handshakeRPC(ctx, conn)
	if err != nil {
		cleanup()
		return nil, protocol.ServerIdentity{}, err
	}
	if err := attachProjectRPC(ctx, conn, c.projectID, c.workspaceID, c.workspaceRoot); err != nil {
		cleanup()
		return nil, protocol.ServerIdentity{}, err
	}
	return conn, identity, nil
}

func dialRemoteURL(ctx context.Context, rpcURL string, projectID string, workspaceID string, workspaceRoot string) (*Remote, error) {
	endpoint, err := rpcwire.ParseWebSocketEndpoint(strings.TrimSpace(rpcURL))
	if err != nil {
		return nil, err
	}
	return dialRemoteWithPlan(ctx, remoteDialPlan{endpoints: []rpcwire.Endpoint{endpoint}}, projectID, workspaceID, workspaceRoot)
}

func dialConfiguredRemote(ctx context.Context, cfg config.App, projectID string, workspaceID string, workspaceRoot string) (*Remote, error) {
	plan, err := configuredRemoteDialPlan(cfg)
	if err != nil {
		return nil, err
	}
	return dialRemoteWithPlan(ctx, plan, projectID, workspaceID, workspaceRoot)
}

var _ ProjectViewClient = (*Remote)(nil)
var _ AuthStatusClient = (*Remote)(nil)
var _ SessionLaunchClient = (*Remote)(nil)
var _ SessionViewClient = (*Remote)(nil)
var _ SessionLifecycleClient = (*Remote)(nil)
var _ SessionRuntimeClient = (*Remote)(nil)
var _ RuntimeControlClient = (*Remote)(nil)
var _ ProcessViewClient = (*Remote)(nil)
var _ ProcessControlClient = (*Remote)(nil)
var _ ProcessOutputClient = (*Remote)(nil)
var _ SessionActivityClient = (*Remote)(nil)
var _ RunPromptClient = (*Remote)(nil)
var _ AskViewClient = (*Remote)(nil)
var _ PromptControlClient = (*Remote)(nil)
var _ ApprovalViewClient = (*Remote)(nil)
