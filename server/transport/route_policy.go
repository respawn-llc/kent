package transport

import (
	"context"
	"errors"
	"fmt"
	"strings"

	chatpb "core/shared/protoapi/gen/kent/api/chat"
	processpb "core/shared/protoapi/gen/kent/api/process"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/serverapi"
)

type routePolicyExecutor struct {
	gateway *Gateway
}

var errSessionOutsideActiveProject = errors.New("session outside active project")
var errActiveProjectRequired = errors.New("active project required")

type activeProjectRequiredError struct{}

func (e activeProjectRequiredError) Error() string {
	return "project attachment is required"
}

func (e activeProjectRequiredError) Is(target error) bool {
	return target == errActiveProjectRequired
}

type sessionOutsideActiveProjectError struct {
	sessionID string
}

func (e sessionOutsideActiveProjectError) Error() string {
	return fmt.Sprintf("session %q not available", e.sessionID)
}

func (e sessionOutsideActiveProjectError) Is(target error) bool {
	return target == errSessionOutsideActiveProject
}

func newRoutePolicyExecutor(gateway *Gateway) routePolicyExecutor {
	return routePolicyExecutor{gateway: gateway}
}

func (e routePolicyExecutor) authorizeScopeFacts(
	ctx context.Context,
	state *connectionState,
	scope sharedpb.ScopePolicy,
	method string,
	scopeParams routeScopeParams,
) error {
	switch scope {
	case sharedpb.ScopePolicy_SCOPE_POLICY_NONE, sharedpb.ScopePolicy_SCOPE_POLICY_PROJECT_VIEW, sharedpb.ScopePolicy_SCOPE_POLICY_ATTACH_PROJECT, sharedpb.ScopePolicy_SCOPE_POLICY_NOTIFICATION:
		return nil
	case sharedpb.ScopePolicy_SCOPE_POLICY_PROJECT_WORKSPACE:
		_, err := e.gateway.activeProjectID(ctx, state)
		return err
	case sharedpb.ScopePolicy_SCOPE_POLICY_PROJECT_WORKSPACE_BINDING:
		return e.gateway.requireProjectWorkspaceBinding(
			ctx,
			state,
			scopeParams.projectID,
			scopeParams.workspaceID,
		)
	case sharedpb.ScopePolicy_SCOPE_POLICY_ATTACH_SESSION:
		_, _, err := e.gateway.resolveSessionAttachmentTargetWithCapability(
			ctx,
			state,
			scopeParams.sessionID,
			scopeParams.sessionReattachCapability,
		)
		return err
	case sharedpb.ScopePolicy_SCOPE_POLICY_SESSION_ACTIVE_PROJECT:
		return e.gateway.requireSessionInActiveProject(ctx, state, scopeParams.sessionID)
	case sharedpb.ScopePolicy_SCOPE_POLICY_SESSION_ACTIVE_PROJECT_IF_SET:
		if strings.TrimSpace(scopeParams.sessionID) == "" {
			return nil
		}
		return e.gateway.requireSessionInActiveProject(ctx, state, scopeParams.sessionID)
	case sharedpb.ScopePolicy_SCOPE_POLICY_SESSION_DRAFT_HANDOFF_PROJECT:
		return e.gateway.requireSessionInActiveProject(ctx, state, scopeParams.sessionID)
	case sharedpb.ScopePolicy_SCOPE_POLICY_SESSION_ATTACHED_PROJECT:
		return e.gateway.requireSessionInAttachedProject(ctx, state, scopeParams.sessionID)
	case sharedpb.ScopePolicy_SCOPE_POLICY_ATTACHED_SESSION:
		if state.attachedSession == nil || state.attachedSession.String() != scopeParams.sessionID {
			return errors.New("session attach is required before subscribing")
		}
		return nil
	case sharedpb.ScopePolicy_SCOPE_POLICY_GOAL_SESSION:
		return e.gateway.requireGoalSessionAccess(ctx, state, scopeParams.sessionID)
	case sharedpb.ScopePolicy_SCOPE_POLICY_RUNTIME_LIVE_SESSION_REQUIRED:
		return e.gateway.requireRuntimeLiveSession(ctx, scopeParams.sessionID)
	case sharedpb.ScopePolicy_SCOPE_POLICY_RUNTIME_LIVE_SESSION_OPTIONAL:
		return nil
	case sharedpb.ScopePolicy_SCOPE_POLICY_PROCESS_ACTIVE_PROJECT:
		_, err := e.gateway.processInActiveProject(ctx, state, scopeParams.processID)
		return err
	case sharedpb.ScopePolicy_SCOPE_POLICY_CHAT_TARGET:
		return e.gateway.requireChatTargetAccess(ctx, state, scopeParams.chatTarget)
	case sharedpb.ScopePolicy_SCOPE_POLICY_WORKTREE_MANAGEMENT:
		switch selected := scopeParams.worktreeManagement.GetScope().(type) {
		case *worktreepb.ManagementScope_SessionId:
			return e.gateway.requireSessionInActiveProject(ctx, state, selected.SessionId)
		case *worktreepb.ManagementScope_Workspace:
			target := selected.Workspace.GetTarget()
			return e.gateway.requireProjectWorkspaceBinding(ctx, state, target.GetProjectId(), target.GetWorkspaceId())
		default:
			return errors.New("worktree management scope is required")
		}
	default:
		return fmt.Errorf("unsupported route scope %q for method %q", scope, method)
	}
}

type routeScopeParams struct {
	sessionID                 string
	sessionReattachCapability *string
	processID                 string
	projectID                 string
	workspaceID               string
	chatTarget                *chatpb.ChatTarget
	worktreeManagement        *worktreepb.ManagementScope
}

func (g *Gateway) activeProjectID(ctx context.Context, state *connectionState) (string, error) {
	if trimmed := strings.TrimSpace(state.attachedProject); trimmed != "" {
		return trimmed, nil
	}
	if trimmed := strings.TrimSpace(g.deps.ProjectID()); trimmed != "" {
		return trimmed, nil
	}
	return "", activeProjectRequiredError{}
}

func (g *Gateway) requireSessionInActiveProject(ctx context.Context, state *connectionState, sessionID string) error {
	trimmedSessionID := strings.TrimSpace(sessionID)
	if state != nil &&
		state.attachedSession != nil &&
		state.attachedSession.String() == trimmedSessionID {
		return nil
	}
	projectID, err := g.activeProjectID(ctx, state)
	if err != nil {
		return err
	}
	if trimmedSessionID == "" {
		return fmt.Errorf("session id is required")
	}
	metadataStore := g.deps.MetadataStore()
	if metadataStore == nil {
		return errors.New("metadata store is required")
	}
	belongs, err := metadataStore.SessionBelongsToProject(ctx, trimmedSessionID, projectID)
	if err != nil {
		return err
	}
	if !belongs {
		return sessionOutsideActiveProjectError{sessionID: trimmedSessionID}
	}
	return nil
}

func (g *Gateway) requireGoalSessionAccess(ctx context.Context, state *connectionState, sessionID string) error {
	if strings.TrimSpace(state.attachedProject) == "" && strings.TrimSpace(g.deps.ProjectID()) == "" {
		return nil
	}
	return g.requireSessionInActiveProject(ctx, state, sessionID)
}

func (g *Gateway) requireChatTargetAccess(ctx context.Context, state *connectionState, target *chatpb.ChatTarget) error {
	switch selected := target.GetTarget().(type) {
	case *chatpb.ChatTarget_Session:
		if strings.TrimSpace(state.attachedProject) != "" {
			return g.requireSessionInActiveProject(ctx, state, selected.Session.SessionId)
		}
		metadataStore := g.deps.MetadataStore()
		if metadataStore == nil {
			return errors.New("metadata store is required")
		}
		_, err := metadataStore.ResolvePersistedSession(ctx, selected.Session.SessionId)
		return err
	case *chatpb.ChatTarget_NewChat:
		return g.requireProjectWorkspaceBinding(
			ctx,
			state,
			selected.NewChat.ProjectId,
			selected.NewChat.WorkspaceId,
		)
	default:
		return errors.New("Chat target selection is required")
	}
}

func (g *Gateway) requireProjectWorkspaceBinding(
	ctx context.Context,
	state *connectionState,
	projectID string,
	workspaceID string,
) error {
	activeProjectID, err := g.activeProjectID(ctx, state)
	if err != nil {
		return err
	}
	if strings.TrimSpace(projectID) != strings.TrimSpace(activeProjectID) {
		return serverapi.ErrWorkspaceNotRegistered
	}
	if strings.TrimSpace(state.attachedWorkspaceID) != strings.TrimSpace(workspaceID) {
		return serverapi.ErrWorkspaceNotRegistered
	}
	metadataStore := g.deps.MetadataStore()
	if metadataStore == nil {
		return errors.New("metadata store is required")
	}
	binding, err := metadataStore.LookupWorkspaceBindingByID(ctx, workspaceID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(binding.ProjectID) != strings.TrimSpace(activeProjectID) {
		return serverapi.ErrWorkspaceNotRegistered
	}
	return nil
}

func (g *Gateway) requireRuntimeLiveSession(ctx context.Context, sessionID string) error {
	trimmedSessionID := strings.TrimSpace(sessionID)
	if trimmedSessionID == "" {
		return serverapi.ErrRuntimeUnavailable
	}
	metadataStore := g.deps.MetadataStore()
	if metadataStore == nil {
		return serverapi.ErrRuntimeUnavailable
	}
	if _, err := metadataStore.ResolvePersistedSession(ctx, trimmedSessionID); err != nil {
		return fmt.Errorf("%w: %w", serverapi.ErrRuntimeUnavailable, err)
	}
	return nil
}

func (g *Gateway) requireSessionInAttachedProject(ctx context.Context, state *connectionState, sessionID string) error {
	projectID := strings.TrimSpace(state.attachedProject)
	if projectID == "" {
		return nil
	}
	return g.deps.SessionBelongsToProject(ctx, sessionID, projectID)
}

func (g *Gateway) processInActiveProject(ctx context.Context, state *connectionState, processID string) (*processpb.GetSuccess, error) {
	resp, err := g.deps.ProcessViewClient().GetProcess(ctx, &processpb.GetRequest{ProcessId: processID})
	if err != nil {
		return nil, err
	}
	if resp.Process == nil {
		return nil, fmt.Errorf("process %q not available", strings.TrimSpace(processID))
	}
	ownerSessionID := strings.TrimSpace(resp.Process.OwnerSessionId)
	if ownerSessionID == "" {
		return nil, fmt.Errorf("process %q not available", strings.TrimSpace(processID))
	}
	if err := g.requireSessionInActiveProject(ctx, state, ownerSessionID); err != nil {
		return nil, err
	}
	return resp, nil
}
