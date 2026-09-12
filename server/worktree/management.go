package worktree

import (
	"context"
	"errors"

	"core/server/metadata"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/serverapi"
	"core/shared/worktreecontract"
)

type managementContext struct {
	binding metadata.Binding
	caller  *sessionWorkspaceContext
}

func (s *Service) resolveWorkspaceBinding(ctx context.Context, projectID, workspaceID string) (metadata.Binding, error) {
	selector, err := serverapi.NewProjectWorkspaceSelectorForID(workspaceID)
	if err != nil {
		return metadata.Binding{}, err
	}
	return s.metadata.ResolveProjectWorkspaceSelector(ctx, projectID, selector)
}

func (s *Service) resolveManagementContext(ctx context.Context, scope *worktreepb.ManagementScope) (managementContext, error) {
	switch selected := scope.GetScope().(type) {
	case *worktreepb.ManagementScope_SessionId:
		caller, err := s.resolveSessionWorkspaceContext(ctx, selected.SessionId)
		if err != nil {
			return managementContext{}, err
		}
		return managementContext{binding: metadata.Binding{
			ProjectID: caller.projectID, WorkspaceID: caller.workspaceID, CanonicalRoot: caller.workspaceRoot,
		}, caller: &caller}, nil
	case *worktreepb.ManagementScope_Workspace:
		target := selected.Workspace.GetTarget()
		binding, err := s.resolveWorkspaceBinding(ctx, target.GetProjectId(), target.GetWorkspaceId())
		if err != nil {
			return managementContext{}, err
		}
		result := managementContext{binding: binding}
		if selected.Workspace.CallerSessionId != nil {
			caller, err := s.resolveSessionWorkspaceContext(ctx, *selected.Workspace.CallerSessionId)
			if err != nil {
				return managementContext{}, err
			}
			result.caller = &caller
		}
		return result, nil
	default:
		return managementContext{}, errors.New("worktree management scope is required")
	}
}

func (m managementContext) sessionID() *string {
	if m.caller == nil {
		return nil
	}
	return &m.caller.sessionID
}

func (m managementContext) target() *worktreepb.SessionExecutionTarget {
	if m.caller == nil {
		return nil
	}
	return m.caller.target
}

func (s *Service) beginManagementMutation(ctx context.Context, scope *worktreepb.ManagementScope) (func(), managementContext, error) {
	for {
		selected, err := s.resolveManagementContext(ctx, scope)
		if err != nil {
			return nil, managementContext{}, err
		}
		lease, err := s.acquireWorkspaceMutationLease(ctx, selected.binding.WorkspaceID)
		if err != nil {
			return nil, managementContext{}, err
		}
		locked, err := s.resolveManagementContext(ctx, scope)
		if err != nil {
			lease.Release()
			return nil, managementContext{}, err
		}
		if locked.binding.WorkspaceID == selected.binding.WorkspaceID {
			return lease.Release, locked, nil
		}
		lease.Release()
	}
}

func (s *Service) beginWorkspaceMutation(ctx context.Context, sessionID string) (func(), sessionWorkspaceContext, error) {
	release, selected, err := s.beginManagementMutation(ctx, worktreecontract.SessionManagementScope(sessionID))
	if err != nil {
		return nil, sessionWorkspaceContext{}, err
	}
	return release, *selected.caller, nil
}
