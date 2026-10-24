package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"core/shared/rpcwire"
)

type GenerationRetrySchedule struct {
	Failures     []time.Duration
	StreamStalls []time.Duration
}

func DefaultGenerationRetrySchedule() GenerationRetrySchedule {
	return GenerationRetrySchedule{
		Failures:     []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second},
		StreamStalls: []time.Duration{time.Second},
	}
}

type GenerationAttempt func(context.Context, StreamCallbacks, func()) (Response, error)

type GenerationRetryOptions struct {
	Schedule          GenerationRetrySchedule
	SingleAttempt     bool
	NonRetryable      func(error) bool
	Callbacks         StreamCallbacks
	OnAttemptReset    func()
	OnAttemptFinished func()
}

// GenerateWithRetry owns generation-attempt policy; callers retain observation
// and streaming presentation around each provider attempt.
func GenerateWithRetry(ctx context.Context, attempt GenerationAttempt, options GenerationRetryOptions) (Response, error) {
	var lastErr error
	for i := 0; ; i++ {
		var emitted, reasoningEmitted bool
		var done atomic.Bool
		callbacks := options.Callbacks
		if options.Callbacks.OnAssistantDelta != nil {
			callbacks.OnAssistantDelta = func(delta AssistantDelta) {
				if done.Load() || delta.Text == "" {
					return
				}
				emitted = true
				options.Callbacks.OnAssistantDelta(delta)
			}
		}
		if options.Callbacks.OnReasoningSummaryDelta != nil {
			callbacks.OnReasoningSummaryDelta = func(delta ReasoningSummaryDelta) {
				if done.Load() || strings.TrimSpace(delta.Text) == "" {
					return
				}
				reasoningEmitted = true
				options.Callbacks.OnReasoningSummaryDelta(delta)
			}
		}
		if options.Callbacks.OnOutputItem != nil {
			callbacks.OnOutputItem = func(item ResponseItem) {
				if done.Load() {
					return
				}
				emitted = true
				options.Callbacks.OnOutputItem(item)
			}
		}
		resp, err := attempt(ctx, callbacks, func() {
			done.Store(true)
			if options.OnAttemptFinished != nil {
				options.OnAttemptFinished()
			}
		})
		if err != nil && ctx.Err() != nil {
			return Response{}, ctx.Err()
		}
		if err == nil {
			return resp, nil
		}
		if options.NonRetryable != nil && options.NonRetryable(err) {
			return Response{}, err
		}
		reset := func() {
			if (emitted || reasoningEmitted) && options.OnAttemptReset != nil {
				options.OnAttemptReset()
			}
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			reset()
			return Response{}, err
		}
		if IsNonRetriableModelError(err) || IsContextLengthOverflowError(err) {
			if !HasHTTPStatus(err, 400) {
				reset()
			}
			return Response{}, err
		}
		reset()
		lastErr = err
		if options.SingleAttempt {
			return Response{}, err
		}
		delays := options.Schedule.Failures
		if errors.Is(err, ErrModelStreamStalled) {
			delays = options.Schedule.StreamStalls
		}
		if i >= len(delays) {
			break
		}
		if err := rpcwire.WaitForRetry(ctx, delays[i]); err != nil {
			return Response{}, err
		}
	}
	return Response{}, fmt.Errorf("model generation failed after retries: %w", lastErr)
}
