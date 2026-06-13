package runtimeattach

import (
	"context"
	"errors"
	"strings"
	"time"

	"core/shared/config"
	"core/shared/serverapi"
	"core/shared/servicecontract"
	"core/shared/toolspec"

	"github.com/google/uuid"
)

const ReleaseTimeout = 3 * time.Second

var ErrEmptyControllerLease = errors.New("session runtime activation returned empty controller lease id")
var ErrReadOnlyControllerLease = errors.New("session runtime activation switched to read-only mode")

type Request struct {
	SessionID          string
	ActiveSettings     config.Settings
	EnabledTools       []toolspec.ID
	Source             config.SourceReport
	NewClientRequestID func() string
}

type Lease struct {
	ID       string
	ReadOnly bool
	Recover  func(context.Context) (string, error)
}

func Activate(ctx context.Context, service servicecontract.SessionRuntimeService, req Request) (Lease, error) {
	if service == nil {
		return Lease{}, errors.New("session runtime service is required")
	}
	resp, err := service.ActivateSessionRuntime(ctx, activateRequest(req))
	if err != nil {
		return Lease{}, err
	}
	if resp.ReadOnly {
		return Lease{ReadOnly: true}, nil
	}
	leaseID, err := normalizeLeaseID(resp.LeaseID)
	if err != nil {
		return Lease{}, err
	}
	return Lease{
		ID: leaseID,
		Recover: func(ctx context.Context) (string, error) {
			resp, err := service.ActivateSessionRuntime(ctx, activateRequest(req))
			if err != nil {
				return "", err
			}
			if resp.ReadOnly {
				return "", ErrReadOnlyControllerLease
			}
			return normalizeLeaseID(resp.LeaseID)
		},
	}, nil
}

func Release(service servicecontract.SessionRuntimeService, sessionID string, leaseID string) {
	if service == nil {
		return
	}
	trimmedLeaseID := strings.TrimSpace(leaseID)
	if trimmedLeaseID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), ReleaseTimeout)
	defer cancel()
	_, _ = service.ReleaseSessionRuntime(ctx, serverapi.SessionRuntimeReleaseRequest{
		ClientRequestID: newClientRequestID(nil),
		SessionID:       sessionID,
		LeaseID:         trimmedLeaseID,
		OnlyIfIdle:      true,
		DropOwner:       true,
	})
}

func activateRequest(req Request) serverapi.SessionRuntimeActivateRequest {
	return serverapi.SessionRuntimeActivateRequest{
		ClientRequestID: newClientRequestID(req.NewClientRequestID),
		SessionID:       req.SessionID,
		ActiveSettings:  req.ActiveSettings,
		EnabledToolIDs:  toolIDs(req.EnabledTools),
		Source:          req.Source,
	}
}

func toolIDs(enabledTools []toolspec.ID) []string {
	ids := make([]string, 0, len(enabledTools))
	for _, id := range enabledTools {
		ids = append(ids, string(id))
	}
	return ids
}

func normalizeLeaseID(leaseID string) (string, error) {
	trimmedLeaseID := strings.TrimSpace(leaseID)
	if trimmedLeaseID == "" {
		return "", ErrEmptyControllerLease
	}
	return trimmedLeaseID, nil
}

func newClientRequestID(newID func() string) string {
	if newID == nil {
		return uuid.NewString()
	}
	return newID()
}
