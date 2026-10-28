package worktreeui

import (
	"context"
	"errors"
	"io"
	"testing"

	"core/shared/apicontract"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/serverapi"
	"core/shared/worktreecontract"

	"github.com/google/uuid"
)

type testWorktreeClient struct {
	workspaceRequests []*worktreepb.WorkspaceListRequest
	listResp          *worktreepb.ListSuccess
	listErr           error
	listCtx           context.Context
	listRequests      []*worktreepb.ListRequest
	selectorCtx       context.Context
	selectorResp      *worktreepb.SelectorResolveSuccess
	selectorRequests  []*worktreepb.SelectorResolveRequest
	resolveCtx        context.Context
	resolveResp       *worktreepb.CreateTargetResolveSuccess
	resolveRequests   []*worktreepb.CreateTargetResolveRequest
	createCtx         context.Context
	createResp        *worktreepb.CreateSuccess
	createRequests    []*worktreepb.CreateRequest
	enterCtx          context.Context
	enterResp         *worktreepb.ScheduledAcknowledgement
	enterRequests     []*worktreepb.EnterRequest
	deleteCtx         context.Context
	deleteResp        *worktreepb.DeleteSuccess
	deleteRequests    []*worktreepb.DeleteRequest
	errs              []error
}

func (c *testWorktreeClient) GetWorktreeStatus(context.Context, *worktreepb.StatusRequest) (*worktreepb.StatusSuccess, error) {
	return nil, c.nextErr()
}

func (c *testWorktreeClient) ListWorktrees(ctx context.Context, req *worktreepb.ListRequest) (*worktreepb.ListSuccess, error) {
	c.listCtx = ctx
	c.listRequests = append(c.listRequests, req)
	return c.listResp, c.listErr
}

func (c *testWorktreeClient) ListWorkspaceWorktrees(_ context.Context, req *worktreepb.WorkspaceListRequest) (*worktreepb.WorkspaceListSuccess, error) {
	c.workspaceRequests = append(c.workspaceRequests, req)
	return &worktreepb.WorkspaceListSuccess{WorkspaceId: req.WorkspaceId}, c.nextErr()
}

func TestWorkspaceBrowseAndDeleteUseWorkspaceWithoutSession(t *testing.T) {
	client := &testWorktreeClient{}
	service := Service{Client: client, ProjectID: "project", WorkspaceID: "workspace"}
	_, err := service.ListWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Delete("target", false, worktreepb.BranchCleanupMode_WORKTREE_BRANCH_CLEANUP_MODE_RETAIN)
	if err != nil {
		t.Fatal(err)
	}
	if len(client.listRequests) != 0 || len(client.workspaceRequests) != 1 {
		t.Fatal("workspace browsing required a Session")
	}
	scope := client.deleteRequests[0].Scope.GetWorkspace()
	if scope == nil || scope.SessionContext != nil || scope.Target.ProjectId != "project" || scope.Target.WorkspaceId != "workspace" {
		t.Fatal("deletion did not retain workspace management authority")
	}
}

func (c *testWorktreeClient) ResolveWorktreeSelector(ctx context.Context, req *worktreepb.SelectorResolveRequest) (*worktreepb.SelectorResolveSuccess, error) {
	c.selectorCtx = ctx
	c.selectorRequests = append(c.selectorRequests, req)
	return c.selectorResp, c.nextErr()
}

func (c *testWorktreeClient) PreviewWorktreeDelete(context.Context, *worktreepb.DeletePreviewRequest) (*worktreepb.DeletePreviewSuccess, error) {
	return nil, c.nextErr()
}

func (c *testWorktreeClient) ResolveWorktreeCreateTarget(ctx context.Context, req *worktreepb.CreateTargetResolveRequest) (*worktreepb.CreateTargetResolveSuccess, error) {
	c.resolveCtx = ctx
	c.resolveRequests = append(c.resolveRequests, req)
	return c.resolveResp, c.nextErr()
}

func (c *testWorktreeClient) CreateWorktree(ctx context.Context, req *worktreepb.CreateRequest) (*worktreepb.CreateSuccess, error) {
	c.createCtx = ctx
	c.createRequests = append(c.createRequests, req)
	return c.createResp, c.nextErr()
}

func (c *testWorktreeClient) EnterWorktree(ctx context.Context, req *worktreepb.EnterRequest) (*worktreepb.ScheduledAcknowledgement, error) {
	c.enterCtx = ctx
	c.enterRequests = append(c.enterRequests, req)
	return c.enterResp, c.nextErr()
}

func (c *testWorktreeClient) LeaveWorktree(context.Context, *worktreepb.LeaveRequest) (*worktreepb.ScheduledAcknowledgement, error) {
	return nil, c.nextErr()
}

func (c *testWorktreeClient) DeleteWorktree(ctx context.Context, req *worktreepb.DeleteRequest) (*worktreepb.DeleteSuccess, error) {
	c.deleteCtx = ctx
	c.deleteRequests = append(c.deleteRequests, req)
	return c.deleteResp, c.nextErr()
}

func (c *testWorktreeClient) SubscribeWorktreeSetup(ctx context.Context, req *worktreepb.SetupSubscribeRequest) (apicontract.WorktreeSetupSubscription, error) {
	return testNoopWorktreeSetupSubscription{}, nil
}

type testNoopWorktreeSetupSubscription struct{}

func (testNoopWorktreeSetupSubscription) Next(ctx context.Context) (*worktreepb.SetupEvent, error) {
	return nil, io.EOF
}

func (testNoopWorktreeSetupSubscription) Close() error { return nil }

func (c *testWorktreeClient) nextErr() error {
	if len(c.errs) == 0 {
		return nil
	}
	err := c.errs[0]
	c.errs = c.errs[1:]
	return err
}

func TestMutationDoesNotReplayUnavailableRuntime(t *testing.T) {
	client := &testWorktreeClient{
		errs: []error{serverapi.ErrRuntimeUnavailable, nil},
	}
	service := newTestService(client)

	_, err := service.Enter("feature")
	if !errors.Is(err, serverapi.ErrRuntimeUnavailable) {
		t.Fatalf("Enter error = %v, want unavailable", err)
	}
	if len(client.enterRequests) != 1 {
		t.Fatalf("enter requests = %+v, want one attempt", client.enterRequests)
	}
}

func TestCreateEnterDeletePopulateRequests(t *testing.T) {
	client := &testWorktreeClient{}
	service := newTestService(client)

	baseRef := "HEAD"
	branchName := "feature/a"
	if _, err := service.Create(&worktreepb.CreateRequest{Spec: &worktreepb.CreateSpec{BaseRef: &baseRef, CreateBranch: true, BranchName: &branchName}}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := service.Enter(" feature/a "); err != nil {
		t.Fatalf("Enter: %v", err)
	}
	if _, err := service.Delete(" wt-3 ", true, worktreepb.BranchCleanupMode_WORKTREE_BRANCH_CLEANUP_MODE_DELETE_SAFE); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	gotCreate := client.createRequests[0]
	_, setupIDErr := worktreecontract.ParseSetupOperationID(gotCreate.SetupOperationId)
	if setupIDErr != nil || gotCreate.Scope.GetWorkspace().GetTarget().GetWorkspaceId() != "workspace-1" || gotCreate.Spec.GetBranchName() != "feature/a" {
		t.Fatalf("create request = %+v", gotCreate)
	}
	if got := client.enterRequests[0]; got.OperationId == "" ||
		got.SessionId.GetSessionId() != "session-1" ||
		got.Selector != "feature/a" ||
		got.TargetWorkspace.GetWorkspaceId() != "workspace-1" ||
		got.TargetWorkspace.GetWorkspaceRoot() != "/repo" {
		t.Fatalf("enter request = %+v", got)
	}
	if got := client.deleteRequests[0]; got.Scope.GetWorkspace().GetTarget().GetWorkspaceId() != "workspace-1" || got.Selector != "wt-3" || !got.ForceFolderRemoval || got.BranchCleanupPolicy != worktreepb.BranchCleanupMode_WORKTREE_BRANCH_CLEANUP_MODE_DELETE_SAFE {
		t.Fatalf("delete request = %+v", got)
	}
}

func TestCreateDoesNotInstallFixedMutationDeadline(t *testing.T) {
	client := &testWorktreeClient{}
	service := newTestService(client)

	baseRef := "HEAD"
	branchName := "feature/a"
	if _, err := service.Create(&worktreepb.CreateRequest{Spec: &worktreepb.CreateSpec{BaseRef: &baseRef, CreateBranch: true, BranchName: &branchName}}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if client.createCtx == nil {
		t.Fatal("expected create context recorded")
	}
	if _, ok := client.createCtx.Deadline(); ok {
		t.Fatal("create context has a deadline")
	}
}

func TestResolveCreateTargetUsesBoundedContext(t *testing.T) {
	client := &testWorktreeClient{resolveResp: &worktreepb.CreateTargetResolveSuccess{Resolution: &worktreepb.CreateTargetResolution{Input: "main"}}}
	service := newTestService(client)

	if _, err := service.ResolveCreateTarget("main"); err != nil {
		t.Fatalf("ResolveCreateTarget: %v", err)
	}
	if client.resolveCtx == nil {
		t.Fatal("expected resolve context recorded")
	}
	if _, ok := client.resolveCtx.Deadline(); !ok {
		t.Fatal("expected bounded resolve context")
	}
	if got := client.resolveRequests[0]; got.Scope.GetWorkspace().GetTarget().GetWorkspaceId() != "workspace-1" || got.Target != "main" {
		t.Fatalf("resolve request = %+v", got)
	}
}

func newTestService(client *testWorktreeClient) Service {
	return Service{
		Client:        client,
		ProjectID:     "project-1",
		SessionTarget: &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: "session-1"}},
		WorkspaceID:   "workspace-1",
		WorkspaceRoot: "/repo",
	}
}

func testWorktreeOperationID(t *testing.T) worktreecontract.OperationID {
	id := worktreecontract.OperationID(uuid.MustParse("11111111-1111-4111-8111-111111111111"))
	if err := id.Validate(); err != nil && t != nil {
		t.Fatalf("OperationID.Validate: %v", err)
	}
	return id
}
