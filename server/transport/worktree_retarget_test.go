package transport

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"core/shared/apicontract"
	remoteclient "core/shared/client"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/serverapi"

	"github.com/google/uuid"
)

type rejectedEnterService struct {
	apicontract.WorktreeService
	failure error
}

func (s rejectedEnterService) EnterWorktree(context.Context, *worktreepb.EnterRequest) (*worktreepb.ScheduledAcknowledgement, error) {
	return nil, s.failure
}

func TestWorktreeEnterRetainsWorkflowRejection(t *testing.T) {
	appCore, _ := newGatewayTestCore(t, true, true)
	t.Cleanup(func() { _ = appCore.Close() })
	sess := createGatewayAuthoritativeSession(t, appCore)
	failure := &serverapi.SessionRetargetError{
		Reason: serverapi.SessionRetargetWorkflowOwned, SessionID: sess.Meta().SessionID,
		SourceProject: serverapi.ProjectReference{ID: appCore.ProjectID(), Name: "Source"},
		TargetRoot:    t.TempDir(), WorkflowTaskIDs: []string{uuid.NewString()},
	}
	gateway, err := NewGateway(&partialDeleteDependencies{
		GatewayDependencies: appCore,
		worktrees:           rejectedEnterService{WorktreeService: appCore.WorktreeClient(), failure: failure},
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
	_, err = remote.EnterWorktree(t.Context(), &worktreepb.EnterRequest{
		OperationId: uuid.NewString(), SessionId: failure.SessionID, Selector: "branch",
		TargetWorkspace: &worktreepb.TransitionWorkspace{WorkspaceId: uuid.NewString(), WorkspaceRoot: failure.TargetRoot},
	})
	var rejected *serverapi.SessionRetargetError
	if !errors.As(err, &rejected) || rejected.Reason != failure.Reason {
		t.Fatalf("Workflow rejection lost: %v", err)
	}
	if rejected.SessionID != failure.SessionID || rejected.SourceProject != failure.SourceProject ||
		rejected.TargetRoot != failure.TargetRoot || len(rejected.WorkflowTaskIDs) != 1 || rejected.WorkflowTaskIDs[0] != failure.WorkflowTaskIDs[0] {
		t.Fatalf("rejection facts lost: %+v", rejected)
	}
}
