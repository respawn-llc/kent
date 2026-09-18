package transport

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"core/shared/apicontract"
	remoteclient "core/shared/client"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/worktreecontract"
)

type partialDeleteWorktreeService struct {
	apicontract.WorktreeService
	failure error
}

func (s *partialDeleteWorktreeService) DeleteWorktree(context.Context, *worktreepb.DeleteRequest) (*worktreepb.DeleteSuccess, error) {
	return nil, s.failure
}

type partialDeleteDependencies struct {
	GatewayDependencies
	worktrees apicontract.WorktreeService
}

func (d *partialDeleteDependencies) WorktreeClient() apicontract.WorktreeService {
	return d.worktrees
}

func TestWorktreeDeletionPartialProgressSurvivesGateway(t *testing.T) {
	appCore, _ := newGatewayTestCore(t, true, true)
	t.Cleanup(func() { _ = appCore.Close() })
	sess := createGatewayAuthoritativeSession(t, appCore)
	for _, cause := range []error{
		errors.New("Git removal failed"),
		&worktreecontract.BlockedError{Details: &worktreepb.BlockedDetails{
			ActiveSessions: &worktreepb.ActiveSessionBlockers{Sessions: []*worktreepb.BlockingSession{{SessionId: "active-session"}}},
		}},
	} {
		gateway, err := NewGateway(&partialDeleteDependencies{
			GatewayDependencies: appCore,
			worktrees: &partialDeleteWorktreeService{
				WorktreeService: appCore.WorktreeClient(),
				failure:         &worktreecontract.DeletePartialError{RetargetedSessions: 50, Cause: cause},
			},
		}, gatewayTestIdentity())
		if err != nil {
			t.Fatal(err)
		}
		server := httptest.NewServer(gateway.Handler())
		t.Cleanup(server.Close)
		remote, err := remoteclient.DialRemoteURLForProject(t.Context(), "ws"+server.URL[len("http"):], appCore.ProjectID())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = remote.Close() })
		_, err = remote.DeleteWorktree(t.Context(), &worktreepb.DeleteRequest{
			Scope: worktreecontract.SessionManagementScope(sess.Meta().SessionID), Selector: "worktree",
			BranchCleanupPolicy: worktreepb.BranchCleanupMode_WORKTREE_BRANCH_CLEANUP_MODE_RETAIN,
		})
		var progress *worktreecontract.DeletePartialError
		if !errors.As(err, &progress) || progress.RetargetedSessions != 50 {
			t.Fatalf("partial progress lost at transport: %v", err)
		}
		if errors.Is(err, worktreecontract.ErrWorktreeBlocked) != errors.Is(cause, worktreecontract.ErrWorktreeBlocked) {
			t.Fatalf("blocker classification changed: %v", err)
		}
	}
}
