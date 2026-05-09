package worktreemutation

import (
	"context"
	"errors"
	"strings"
	"time"

	"builder/cli/app/internal/worktreeview"
	"builder/shared/client"
	"builder/shared/serverapi"
	"github.com/google/uuid"
)

const defaultResolveTimeout = 3 * time.Second

var (
	ErrClientUnavailable          = errors.New("worktree client is unavailable")
	ErrControllerLeaseUnavailable = errors.New("controller lease is unavailable")
)

type RuntimeControl struct {
	Context        func() (context.Context, context.CancelFunc)
	CurrentLeaseID func() string
	RecoverLease   func(context.Context, error) error
}

type Service struct {
	Client             client.WorktreeClient
	SessionID          string
	Runtime            RuntimeControl
	ResolveContext     func() (context.Context, context.CancelFunc)
	NewClientRequestID func() string
}

func (s Service) List(includeDirtyCount bool) (serverapi.WorktreeListResponse, error) {
	ctx, cancel, leaseID, err := s.controlContextWithLease()
	if err != nil {
		return serverapi.WorktreeListResponse{}, err
	}
	defer cancel()
	return s.client().ListWorktrees(ctx, serverapi.WorktreeListRequest{
		SessionID:         s.SessionID,
		ControllerLeaseID: leaseID,
		IncludeDirtyCount: includeDirtyCount,
	})
}

func (s Service) ResolveToken(token string) (serverapi.WorktreeView, error) {
	resp, err := s.List(false)
	if err != nil {
		return serverapi.WorktreeView{}, err
	}
	return worktreeview.ResolveToken(resp.Worktrees, token)
}

func (s Service) ResolveCreateTarget(target string) (serverapi.WorktreeCreateTargetResolveResponse, error) {
	if s.client() == nil {
		return serverapi.WorktreeCreateTargetResolveResponse{}, ErrClientUnavailable
	}
	ctx, cancel := s.resolveContext()
	defer cancel()
	return s.client().ResolveWorktreeCreateTarget(ctx, serverapi.WorktreeCreateTargetResolveRequest{
		SessionID: strings.TrimSpace(s.SessionID),
		Target:    target,
	})
}

func (s Service) Create(req serverapi.WorktreeCreateRequest) (serverapi.WorktreeCreateResponse, error) {
	return runMutation(s, func(ctx context.Context, leaseID string) (serverapi.WorktreeCreateResponse, error) {
		req.ClientRequestID = s.clientRequestID()
		req.SessionID = s.SessionID
		req.ControllerLeaseID = leaseID
		return s.client().CreateWorktree(ctx, req)
	})
}

func (s Service) Switch(worktreeID string) (serverapi.WorktreeSwitchResponse, error) {
	return runMutation(s, func(ctx context.Context, leaseID string) (serverapi.WorktreeSwitchResponse, error) {
		return s.client().SwitchWorktree(ctx, serverapi.WorktreeSwitchRequest{
			ClientRequestID:   s.clientRequestID(),
			SessionID:         s.SessionID,
			ControllerLeaseID: leaseID,
			WorktreeID:        strings.TrimSpace(worktreeID),
		})
	})
}

func (s Service) Delete(worktreeID string, deleteBranch bool) (serverapi.WorktreeDeleteResponse, error) {
	return runMutation(s, func(ctx context.Context, leaseID string) (serverapi.WorktreeDeleteResponse, error) {
		return s.client().DeleteWorktree(ctx, serverapi.WorktreeDeleteRequest{
			ClientRequestID:   s.clientRequestID(),
			SessionID:         s.SessionID,
			ControllerLeaseID: leaseID,
			WorktreeID:        strings.TrimSpace(worktreeID),
			DeleteBranch:      deleteBranch,
		})
	})
}

func runMutation[T any](s Service, call func(context.Context, string) (T, error)) (T, error) {
	var zero T
	ctx, cancel, _, err := s.controlContextWithLease()
	if err != nil {
		return zero, err
	}
	defer cancel()
	return retryControlCall(ctx, s.Runtime.CurrentLeaseID, s.Runtime.RecoverLease, func(controllerLeaseID string) (T, error) {
		return call(ctx, controllerLeaseID)
	})
}

func (s Service) controlContextWithLease() (context.Context, context.CancelFunc, string, error) {
	if s.client() == nil {
		return nil, nil, "", ErrClientUnavailable
	}
	if s.Runtime.Context == nil || s.Runtime.CurrentLeaseID == nil || s.Runtime.RecoverLease == nil {
		return nil, nil, "", ErrControllerLeaseUnavailable
	}
	ctx, cancel := s.Runtime.Context()
	if ctx == nil || cancel == nil {
		return nil, nil, "", ErrControllerLeaseUnavailable
	}
	return ctx, cancel, s.Runtime.CurrentLeaseID(), nil
}

func (s Service) resolveContext() (context.Context, context.CancelFunc) {
	if s.Runtime.Context != nil {
		if ctx, cancel := s.Runtime.Context(); ctx != nil && cancel != nil {
			return ctx, cancel
		}
	}
	if s.ResolveContext != nil {
		if ctx, cancel := s.ResolveContext(); ctx != nil && cancel != nil {
			return ctx, cancel
		}
	}
	return context.WithTimeout(context.Background(), defaultResolveTimeout)
}

func (s Service) client() client.WorktreeClient {
	return s.Client
}

func (s Service) clientRequestID() string {
	if s.NewClientRequestID != nil {
		if id := strings.TrimSpace(s.NewClientRequestID()); id != "" {
			return id
		}
	}
	return uuid.NewString()
}

func retryControlCall[T any](ctx context.Context, currentLeaseID func() string, recoverLease func(context.Context, error) error, call func(string) (T, error)) (T, error) {
	value, err := call(currentLeaseID())
	if !isRecoverableControlError(err) {
		return value, err
	}
	var zero T
	if recoverErr := recoverLease(ctx, err); recoverErr != nil {
		return zero, recoverErr
	}
	return call(currentLeaseID())
}

func isRecoverableControlError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, serverapi.ErrInvalidControllerLease) || errors.Is(err, serverapi.ErrRuntimeUnavailable)
}
