package app

import (
	"context"
	"core/cli/tui/ongoing"
	"errors"
	"time"

	"core/cli/app/internal/runtimeattach"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/serverapi"
)

const transcriptSubscriptionRetryDelay = 250 * time.Millisecond

type ongoingTranscriptEventKind string

const (
	ongoingTranscriptEventMessage ongoingTranscriptEventKind = "message"
	ongoingTranscriptEventLoss    ongoingTranscriptEventKind = "loss"
	ongoingTranscriptEventFailure ongoingTranscriptEventKind = "failure"
	ongoingTranscriptEventCutover ongoingTranscriptEventKind = "cutover"
	ongoingTranscriptEventClosed  ongoingTranscriptEventKind = "closed"
)

type ongoingTranscriptEvent struct {
	Kind    ongoingTranscriptEventKind
	Message *transcriptpb.Message
	Err     error
	Reason  *ongoing.RehydrateReason
}

type ongoingTranscriptEventStream struct {
	Events             <-chan ongoingTranscriptEvent
	RequestRehydration func(ongoing.RehydrateReason)
	Stop               func()
}

type sessionTranscriptSubscriber func(context.Context, *transcriptpb.SubscribeRequest) (serverapi.TranscriptSubscription, error)

func startSessionTranscriptEvents(
	ctx context.Context,
	sessionID string,
	subscribe sessionTranscriptSubscriber,
	observers ...func(*transcriptpb.Message),
) ongoingTranscriptEventStream {
	out := make(chan ongoingTranscriptEvent, 64)
	requests := make(chan ongoing.RehydrateReason, 1)
	if subscribe == nil {
		close(out)
		return ongoingTranscriptEventStream{Events: out, RequestRehydration: func(ongoing.RehydrateReason) {}, Stop: func() {}}
	}
	pollCtx, cancel := context.WithCancel(ctx)
	go func() {
		defer close(out)
		connectionFailureReported := false
		for {
			sub, err := subscribeSessionTranscript(pollCtx, sessionID, subscribe)
			if err != nil {
				if (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) && pollCtx.Err() != nil {
					return
				}
				if runtimeattach.IsRuntimeConnectionError(err) {
					if !connectionFailureReported {
						emitSessionTranscriptFailure(pollCtx, out, err)
						connectionFailureReported = true
					}
					if waitForTranscriptSubscriptionRetry(pollCtx) {
						continue
					}
					return
				}
				emitSessionTranscriptFailure(pollCtx, out, err)
				return
			}
			connectionFailureReported = false
			reopen, stop, _ := pumpSessionTranscriptSubscription(pollCtx, sub, out, requests, observers...)
			if stop {
				return
			}
			if reopen == nil {
				return
			}
			select {
			case <-pollCtx.Done():
				return
			case out <- ongoingTranscriptEvent{Kind: ongoingTranscriptEventCutover, Reason: reopen}:
			}
		}
	}()
	requestRehydration := func(reason ongoing.RehydrateReason) {
		select {
		case requests <- reason:
		default:
		}
	}
	return ongoingTranscriptEventStream{Events: out, RequestRehydration: requestRehydration, Stop: cancel}
}

type transcriptNextResult struct {
	message *transcriptpb.Message
	err     error
}

func pumpSessionTranscriptSubscription(
	ctx context.Context,
	sub serverapi.TranscriptSubscription,
	out chan<- ongoingTranscriptEvent,
	requests <-chan ongoing.RehydrateReason,
	observers ...func(*transcriptpb.Message),
) (reopen *ongoing.RehydrateReason, stop bool, lossErr error) {
	subClosed := false
	closeSub := func() {
		if subClosed {
			return
		}
		_ = sub.Close()
		subClosed = true
	}
	defer closeSub()
	for {
		nextCtx, cancel := context.WithCancel(ctx)
		next := make(chan transcriptNextResult, 1)
		go func() {
			message, err := sub.Next(nextCtx)
			next <- transcriptNextResult{message: message, err: err}
		}()
		select {
		case <-ctx.Done():
			cancel()
			return nil, true, nil
		case reason := <-requests:
			cancel()
			return &reason, false, nil
		case result := <-next:
			cancel()
			if result.err != nil {
				if errors.Is(result.err, context.Canceled) && ctx.Err() != nil {
					return nil, true, nil
				}
				closeSub()
				emitSessionTranscriptLoss(ctx, out, result.err)
				reopen, stop := waitForTranscriptRehydrationRequest(ctx, requests)
				return reopen, stop, result.err
			}
			for _, observe := range observers {
				if observe != nil {
					observe(result.message)
				}
			}
			select {
			case <-ctx.Done():
				return nil, true, nil
			case out <- ongoingTranscriptEvent{Kind: ongoingTranscriptEventMessage, Message: result.message}:
			}
		}
	}
}

func waitForTranscriptRehydrationRequest(ctx context.Context, requests <-chan ongoing.RehydrateReason) (reopen *ongoing.RehydrateReason, stop bool) {
	select {
	case <-ctx.Done():
		return nil, true
	case reason := <-requests:
		return &reason, false
	}
}

func subscribeSessionTranscript(ctx context.Context, sessionID string, subscribe sessionTranscriptSubscriber) (serverapi.TranscriptSubscription, error) {
	return subscribe(ctx, &transcriptpb.SubscribeRequest{SessionId: &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: sessionID}}})
}

func waitForTranscriptSubscriptionRetry(ctx context.Context) bool {
	timer := time.NewTimer(transcriptSubscriptionRetryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func emitSessionTranscriptLoss(ctx context.Context, out chan<- ongoingTranscriptEvent, err error) {
	select {
	case <-ctx.Done():
	case out <- ongoingTranscriptEvent{Kind: ongoingTranscriptEventLoss, Err: err}:
	}
}

func emitSessionTranscriptFailure(ctx context.Context, out chan<- ongoingTranscriptEvent, err error) {
	select {
	case <-ctx.Done():
	case out <- ongoingTranscriptEvent{Kind: ongoingTranscriptEventFailure, Err: err}:
	}
}
