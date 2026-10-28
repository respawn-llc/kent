package worktreeui

import (
	"context"
	"errors"
	"strings"
	"time"

	"core/shared/apicontract"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/runtimeinput"
	"core/shared/worktreecontract"
)

const defaultResolveTimeout = 3 * time.Second
const defaultMutationTimeout = 30 * time.Second

var (
	ErrClientUnavailable = errors.New("worktree client is unavailable")
)

type Service struct {
	Client        apicontract.WorktreeService
	SessionTarget *sharedpb.SessionSelector
	ProjectID     string
	WorkspaceID   string
	WorkspaceRoot string
}

func (s Service) managementScope() *worktreepb.ManagementScope {
	return worktreecontract.WorkspaceManagementScope(s.ProjectID, s.WorkspaceID, nil)
}

func (s Service) ListWorkspace() (*worktreepb.WorkspaceListSuccess, error) {
	ctx, cancel, err := s.resolveMutationContext(false)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return s.Client.ListWorkspaceWorktrees(ctx, &worktreepb.WorkspaceListRequest{
		ProjectId: s.ProjectID, WorkspaceId: s.WorkspaceID,
	})
}

func (s Service) ResolveCreateTarget(target string) (*worktreepb.CreateTargetResolveSuccess, error) {
	if s.Client == nil {
		return nil, ErrClientUnavailable
	}
	ctx, cancel := s.resolveContext()
	defer cancel()
	return s.Client.ResolveWorktreeCreateTarget(ctx, &worktreepb.CreateTargetResolveRequest{
		Scope:  s.managementScope(),
		Target: target,
	})
}

func (s Service) PreviewDelete(selector string) (*worktreepb.DeletePreviewSuccess, error) {
	if s.Client == nil {
		return nil, ErrClientUnavailable
	}
	ctx, cancel := s.resolveContext()
	defer cancel()
	return s.Client.PreviewWorktreeDelete(ctx, &worktreepb.DeletePreviewRequest{
		Scope: s.managementScope(), Selector: selector,
	})
}

func (s Service) Create(req *worktreepb.CreateRequest) (*worktreepb.CreateSuccess, error) {
	if _, err := worktreecontract.ParseSetupOperationID(req.SetupOperationId); err != nil {
		req.SetupOperationId = worktreecontract.NewSetupOperationID().String()
	}
	return runCreateMutation(s, func(ctx context.Context) (*worktreepb.CreateSuccess, error) {
		req.Scope = s.managementScope()
		return s.Client.CreateWorktree(ctx, req)
	})
}

func (s Service) Enter(selector string) (*worktreepb.ScheduledAcknowledgement, error) {
	operationID := s.operationID()
	return runMutation(s, func(ctx context.Context) (*worktreepb.ScheduledAcknowledgement, error) {
		return s.Client.EnterWorktree(ctx, &worktreepb.EnterRequest{
			OperationId: operationID.String(),
			SessionId:   s.SessionTarget,
			Selector:    runtimeinput.NormalizePendingWorkArgument(selector),
			TargetWorkspace: &worktreepb.TransitionWorkspace{
				WorkspaceId:   strings.TrimSpace(s.WorkspaceID),
				WorkspaceRoot: strings.TrimSpace(s.WorkspaceRoot),
			},
		})
	})
}

func (s Service) Leave() (*worktreepb.ScheduledAcknowledgement, error) {
	operationID := s.operationID()
	return runMutation(s, func(ctx context.Context) (*worktreepb.ScheduledAcknowledgement, error) {
		return s.Client.LeaveWorktree(ctx, &worktreepb.LeaveRequest{
			OperationId: operationID.String(),
			SessionId:   s.SessionTarget,
		})
	})
}

func (s Service) Delete(
	selector string,
	forceFolderRemoval bool,
	cleanupPolicy worktreepb.BranchCleanupMode,
) (*worktreepb.DeleteSuccess, error) {
	return runMutation(s, func(ctx context.Context) (*worktreepb.DeleteSuccess, error) {
		return s.Client.DeleteWorktree(ctx, &worktreepb.DeleteRequest{
			Scope:               s.managementScope(),
			Selector:            strings.TrimSpace(selector),
			ForceFolderRemoval:  forceFolderRemoval,
			BranchCleanupPolicy: cleanupPolicy,
		})
	})
}

func runMutation[T any](s Service, call func(context.Context) (T, error)) (T, error) {
	var zero T
	ctx, cancel, err := s.resolveMutationContext(true)
	if err != nil {
		return zero, err
	}
	defer cancel()
	return call(ctx)
}

func runCreateMutation[T any](s Service, call func(context.Context) (T, error)) (T, error) {
	var zero T
	if s.Client == nil {
		return zero, ErrClientUnavailable
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	return call(ctx)
}

func (s Service) resolveMutationContext(mutation bool) (context.Context, context.CancelFunc, error) {
	if s.Client == nil {
		return nil, nil, ErrClientUnavailable
	}
	timeout := defaultResolveTimeout
	if mutation {
		timeout = defaultMutationTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	return ctx, cancel, nil
}

func (s Service) resolveContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), defaultResolveTimeout)
}

func (s Service) operationID() worktreecontract.OperationID {
	return worktreecontract.NewOperationID()
}
