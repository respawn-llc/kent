package transport

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"core/server/metadata"
	"core/shared/apicontract"
	"core/shared/clientui"
	connectionpb "core/shared/protoapi/gen/kent/api/connection"
	projectpb "core/shared/protoapi/gen/kent/api/project"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

func (g *Gateway) resolveAttachedProjectWorkspace(ctx context.Context, request *connectionpb.AttachProjectRequest) (string, string, error) {
	switch workspace := request.GetWorkspace().(type) {
	case nil:
		store := g.deps.MetadataStore()
		if _, err := store.GetProjectEditMetadata(ctx, request.ProjectId); err != nil {
			return "", "", err
		}
		count, err := store.Queries().CountProjectWorkspaces(ctx, request.ProjectId)
		if err != nil {
			return "", "", err
		}
		if count == 0 {
			return "", "", fmt.Errorf("project %q has no attached workspaces", request.ProjectId)
		}
		if count > 1 {
			return "", "", fmt.Errorf("project %q requires explicit workspace selection", request.ProjectId)
		}
		selected, err := store.ResolveProjectSourceWorkspace(ctx, request.ProjectId)
		if err != nil {
			return "", "", err
		}
		return selected.ID, selected.CanonicalRootPath, nil
	case *connectionpb.AttachProjectRequest_WorkspaceId:
		binding, err := g.deps.MetadataStore().LookupWorkspaceBindingByID(ctx, workspace.WorkspaceId)
		if err != nil {
			return "", "", err
		}
		if strings.TrimSpace(binding.ProjectID) != request.ProjectId {
			return "", "", fmt.Errorf("workspace %q is not bound to project %q", binding.CanonicalRoot, request.ProjectId)
		}
		return binding.WorkspaceID, strings.TrimSpace(binding.CanonicalRoot), nil
	case *connectionpb.AttachProjectRequest_WorkspaceRoot:
		resolved, err := g.deps.ProjectViewClient().ResolveProjectPath(ctx, &projectpb.ResolvePathRequest{Path: workspace.WorkspaceRoot})
		if err != nil {
			return "", "", err
		}
		if resolved.Binding == nil {
			return "", "", errors.Join(serverapi.ErrWorkspaceNotRegistered, fmt.Errorf("workspace %q is not registered", resolved.CanonicalRoot))
		}
		if strings.TrimSpace(resolved.Binding.ProjectId) != request.ProjectId {
			return "", "", fmt.Errorf("workspace %q is not bound to project %q", resolved.Binding.CanonicalRoot, request.ProjectId)
		}
		return strings.TrimSpace(resolved.Binding.WorkspaceId), strings.TrimSpace(resolved.Binding.CanonicalRoot), nil
	default:
		return "", "", errors.New("AttachProject workspace selection is invalid")
	}
}

func (g *Gateway) resolveSessionAttachmentTarget(ctx context.Context, state *connectionState, sessionID string) (*worktreepb.SessionExecutionTarget, metadata.Binding, error) {
	return g.resolveSessionAttachmentTargetWithCapability(ctx, state, sessionID, nil)
}

func (g *Gateway) resolveSessionAttachmentTargetWithCapability(
	ctx context.Context,
	state *connectionState,
	sessionID string,
	reattachCapability *string,
) (*worktreepb.SessionExecutionTarget, metadata.Binding, error) {
	trimmedSessionID := strings.TrimSpace(sessionID)
	if trimmedSessionID == "" {
		return &worktreepb.SessionExecutionTarget{}, metadata.Binding{}, errors.New("session id is required")
	}
	metadataStore := g.deps.MetadataStore()
	if metadataStore == nil {
		return &worktreepb.SessionExecutionTarget{}, metadata.Binding{}, errors.New("metadata store is required")
	}
	target, err := metadataStore.ResolveSessionExecutionTarget(ctx, trimmedSessionID)
	if err != nil {
		return &worktreepb.SessionExecutionTarget{}, metadata.Binding{}, err
	}
	binding, err := metadataStore.LookupWorkspaceBindingByID(ctx, target.GetWorkspaceId())
	if err != nil {
		if errors.Is(err, serverapi.ErrWorkspaceNotRegistered) {
			return &worktreepb.SessionExecutionTarget{}, metadata.Binding{}, sessionWorkspaceNotRegisteredError{
				projectID:     connectionAttachmentProjectID(g, state),
				workspaceID:   target.GetWorkspaceId(),
				workspaceRoot: target.WorkspaceRoot,
				cause:         err,
			}
		}
		return &worktreepb.SessionExecutionTarget{}, metadata.Binding{}, err
	}
	activeProjectID := strings.TrimSpace(g.deps.ProjectID())
	if state != nil && strings.TrimSpace(state.attachedProject) != "" {
		activeProjectID = strings.TrimSpace(state.attachedProject)
	}
	if activeProjectID != "" && strings.TrimSpace(binding.ProjectID) != activeProjectID {
		authority, authorityErr := g.sessionReattachAuthority()
		if authorityErr != nil {
			return &worktreepb.SessionExecutionTarget{}, metadata.Binding{}, authorityErr
		}
		if !authority.authorizes(trimmedSessionID, reattachCapability) {
			return &worktreepb.SessionExecutionTarget{}, metadata.Binding{}, sessionOutsideActiveProjectError{sessionID: trimmedSessionID}
		}
	}
	return target, binding, nil
}

type sessionWorkspaceNotRegisteredError struct {
	projectID     string
	workspaceID   string
	workspaceRoot string
	cause         error
}

func (e sessionWorkspaceNotRegisteredError) Error() string {
	return e.cause.Error()
}

func (e sessionWorkspaceNotRegisteredError) Unwrap() error {
	return e.cause
}

func (g *Gateway) resolveSessionAttachment(ctx context.Context, state *connectionState, sessionID string) (metadata.Binding, error) {
	_, binding, err := g.resolveSessionAttachmentTarget(ctx, state, sessionID)
	return binding, err
}

func (g *Gateway) promptCommandWorkspaceRootForState(ctx context.Context, state *connectionState, sessionID *runtimeids.SessionID) (string, error) {
	if state == nil {
		return "", errors.New("connection state is required")
	}
	if sessionID == nil {
		sessionID = state.attachedSession
	}
	if sessionID == nil {
		return state.attachedWorkspaceRoot, nil
	}
	target, binding, err := g.resolveSessionAttachmentTarget(ctx, state, sessionID.String())
	if err != nil {
		return "", err
	}
	return clientui.SessionExecutionWorkspaceRoot(target, binding.CanonicalRoot)
}

func (g *Gateway) promptCommandWorkspaceRootForCatalog(ctx context.Context, state *connectionState, sessionID *runtimeids.SessionID) (string, error) {
	if sessionID != nil {
		return g.promptCommandWorkspaceRootForState(ctx, state, sessionID)
	}
	return g.promptCommandWorkspaceRootForState(ctx, state, nil)
}

func (g *Gateway) sessionLaunchClientForState(ctx context.Context, state *connectionState) (apicontract.SessionLaunchService, error) {
	projectID, err := g.activeProjectID(ctx, state)
	if err != nil {
		return nil, err
	}
	var launchClient apicontract.SessionLaunchService
	if strings.TrimSpace(state.attachedWorkspaceID) == "" {
		launchClient, err = g.deps.SessionLaunchClientForProjectWorkspace(ctx, projectID, state.attachedWorkspaceRoot)
	} else {
		launchClient, err = g.deps.SessionLaunchClientForProjectWorkspaceID(ctx, projectID, state.attachedWorkspaceID)
	}
	if err != nil {
		return nil, err
	}
	return launchClient, nil
}

func (g *Gateway) runPromptClientForState(ctx context.Context, state *connectionState) (apicontract.RunPromptService, error) {
	projectID, err := g.activeProjectID(ctx, state)
	if err != nil {
		return nil, err
	}
	var runClient apicontract.RunPromptService
	if strings.TrimSpace(state.attachedWorkspaceID) == "" {
		runClient, err = g.deps.RunPromptClientForProjectWorkspace(ctx, projectID, state.attachedWorkspaceRoot)
	} else {
		runClient, err = g.deps.RunPromptClientForProjectWorkspaceID(ctx, projectID, state.attachedWorkspaceID)
	}
	if err != nil {
		return nil, err
	}
	return runClient, nil
}
