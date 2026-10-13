package worktreecontract

import (
	projectpb "core/shared/protoapi/gen/kent/api/project"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
)

func SessionManagementScope(sessionID *sharedpb.SessionSelector) *worktreepb.ManagementScope {
	return &worktreepb.ManagementScope{Scope: &worktreepb.ManagementScope_SessionId{SessionId: sessionID}}
}

func WorkspaceManagementScope(projectID, workspaceID string, sessionContext *sharedpb.SessionSelector) *worktreepb.ManagementScope {
	return &worktreepb.ManagementScope{Scope: &worktreepb.ManagementScope_Workspace{
		Workspace: &worktreepb.WorkspaceManagementTarget{
			Target:         &projectpb.SelectedProjectWorkspace{ProjectId: projectID, WorkspaceId: workspaceID},
			SessionContext: sessionContext,
		},
	}}
}
