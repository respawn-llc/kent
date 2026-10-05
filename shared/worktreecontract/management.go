package worktreecontract

import (
	projectpb "core/shared/protoapi/gen/kent/api/project"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
)

func SessionManagementScope(sessionID string) *worktreepb.ManagementScope {
	return &worktreepb.ManagementScope{Scope: &worktreepb.ManagementScope_SessionId{SessionId: sessionID}}
}

func WorkspaceManagementScope(projectID, workspaceID string, callerSessionID *string) *worktreepb.ManagementScope {
	return &worktreepb.ManagementScope{Scope: &worktreepb.ManagementScope_Workspace{
		Workspace: &worktreepb.WorkspaceManagementTarget{
			Target:          &projectpb.SelectedProjectWorkspace{ProjectId: projectID, WorkspaceId: workspaceID},
			CallerSessionId: callerSessionID,
		},
	}}
}
