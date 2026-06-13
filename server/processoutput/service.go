package processoutput

import (
	"context"
	"errors"
	"fmt"

	shelltool "core/server/tools/shell"
	"core/shared/clientui"
	"core/shared/serverapi"
	"core/shared/servicecontract"
)

type Subscriber interface {
	SubscribeOutput(ctx context.Context, processID string, offsetBytes int64) (shelltool.OutputSubscription, error)
}

type ProcessSource interface {
	Snapshot(id string) (shelltool.Snapshot, error)
}

type Service struct {
	subscriber Subscriber
	processes  ProcessSource
}

func NewService(subscriber Subscriber, processes ProcessSource) *Service {
	return &Service{subscriber: subscriber, processes: processes}
}

func (s *Service) SubscribeProcessOutput(ctx context.Context, req serverapi.ProcessOutputSubscribeRequest) (serverapi.ProcessOutputSubscription, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if s == nil || s.subscriber == nil || s.processes == nil {
		return nil, errors.New("process output subscriber is required")
	}
	snapshot, err := s.processes.Snapshot(req.ProcessID)
	if err != nil {
		return nil, fmt.Errorf("process output stream for %q is unavailable: %w", req.ProcessID, serverapi.ErrProcessOutputUnavailable)
	}
	if !snapshot.OutputAvailable {
		return nil, fmt.Errorf("process output stream for %q is unavailable: %w", req.ProcessID, serverapi.ErrProcessOutputUnavailable)
	}
	if req.OffsetBytes < snapshot.OutputRetainedFromBytes || req.OffsetBytes > snapshot.OutputRetainedToBytes {
		return nil, fmt.Errorf(
			"process output offset %d is outside retained range [%d,%d] for %q: %w",
			req.OffsetBytes,
			snapshot.OutputRetainedFromBytes,
			snapshot.OutputRetainedToBytes,
			req.ProcessID,
			serverapi.ErrProcessOutputGap,
		)
	}
	sub, err := s.subscriber.SubscribeOutput(ctx, req.ProcessID, req.OffsetBytes)
	if err != nil {
		latest, snapErr := s.processes.Snapshot(req.ProcessID)
		switch {
		case snapErr != nil || !latest.OutputAvailable:
			return nil, fmt.Errorf("process output stream for %q is unavailable: %w", req.ProcessID, serverapi.ErrProcessOutputUnavailable)
		case req.OffsetBytes < latest.OutputRetainedFromBytes || req.OffsetBytes > latest.OutputRetainedToBytes:
			return nil, fmt.Errorf(
				"process output offset %d is outside retained range [%d,%d] for %q: %w",
				req.OffsetBytes,
				latest.OutputRetainedFromBytes,
				latest.OutputRetainedToBytes,
				req.ProcessID,
				serverapi.ErrProcessOutputGap,
			)
		default:
			return nil, fmt.Errorf("process output stream for %q failed: %w", req.ProcessID, serverapi.ErrStreamFailed)
		}
	}
	return &subscription{inner: sub}, nil
}

type subscription struct {
	inner shelltool.OutputSubscription
}

func (s *subscription) Next(ctx context.Context) (clientui.ProcessOutputChunk, error) {
	chunk, err := s.inner.Next(ctx)
	if err != nil {
		return clientui.ProcessOutputChunk{}, serverapi.NormalizeStreamError(err)
	}
	return clientui.ProcessOutputChunk{
		ProcessID:       chunk.ProcessID,
		OffsetBytes:     chunk.OffsetBytes,
		NextOffsetBytes: chunk.NextOffsetBytes,
		Text:            chunk.Text,
	}, nil
}

func (s *subscription) Close() error {
	if s == nil || s.inner == nil {
		return nil
	}
	return s.inner.Close()
}

var _ servicecontract.ProcessOutputService = (*Service)(nil)
