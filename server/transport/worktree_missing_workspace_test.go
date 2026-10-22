package transport

import (
	"errors"
	"testing"

	"core/server/metadata"
	remoteclient "core/shared/client"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/serverapi"
)

func TestWorktreeRetainedSessionMissingWorkspace(t *testing.T) {
	appCore, server := newGatewayTestServer(t)
	defer func() { _ = appCore.Close() }()
	defer server.Close()
	store := createGatewayAuthoritativeSession(t, appCore)
	sessionID := store.Meta().SessionID
	metadataStore := appCore.MetadataStore()
	workspace, err := metadataStore.AttachWorkspaceToProject(t.Context(), appCore.ProjectID(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := metadataStore.UpdateSessionExecutionTarget(t.Context(), metadata.SessionExecutionTargetUpdate{
		SessionID: sessionID,
		Workspace: &metadata.SessionExecutionTargetUpdateWorkspace{ID: workspace.WorkspaceID},
	}); err != nil {
		t.Fatal(err)
	}
	blockers, err := metadataStore.UnlinkProjectWorkspaceWithRuntimeBlockers(t.Context(), appCore.ProjectID(), workspace.WorkspaceID, nil, nil)
	if err != nil || len(blockers) != 0 {
		t.Fatalf("unlink Workspace: blockers=%v, err=%v", blockers, err)
	}
	belongs, err := metadataStore.SessionBelongsToProject(t.Context(), sessionID, appCore.ProjectID())
	if err != nil || !belongs {
		t.Fatalf("retained Session membership: belongs=%v, err=%v", belongs, err)
	}
	remote, err := remoteclient.DialRemoteURLForProject(
		t.Context(), "ws"+server.URL[len("http"):], appCore.ProjectID(),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = remote.Close() }()
	_, err = remote.ListWorktrees(t.Context(), &worktreepb.ListRequest{SessionId: &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: sessionID}}})
	if !errors.Is(err, serverapi.ErrWorkspaceNotRegistered) {
		t.Fatalf("Worktree list error = %v, want ErrWorkspaceNotRegistered", err)
	}
	_, err = remote.GetWorktreeStatus(t.Context(), &worktreepb.StatusRequest{SessionId: &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: sessionID}}})
	if !errors.Is(err, serverapi.ErrWorkspaceNotRegistered) {
		t.Fatalf("Worktree status error = %v, want ErrWorkspaceNotRegistered", err)
	}
}
