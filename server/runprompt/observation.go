package runprompt

import (
	"context"
	"errors"
	"time"

	"core/server/runtime"
	"core/shared/protoapi"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	runpromptpb "core/shared/protoapi/gen/kent/api/run_prompt"
	"core/shared/runtimeids"
)

type runCompletion interface {
	Complete(time.Duration) (*runpromptpb.Success, error)
}

type observationScope struct {
	ctx          context.Context
	cancel       context.CancelCauseFunc
	timer        *time.Timer
	observerCtx  context.Context
	stopObserver context.CancelFunc
}

func newObservationScope(ctx context.Context) *observationScope {
	ctx, cancel := context.WithCancelCause(ctx)
	// The live observer cancels its waiters on return, but idle Run may still
	// need this completion deadline while scheduled model work finishes.
	observerCtx, stopObserver := context.WithCancel(ctx)
	return &observationScope{ctx: ctx, cancel: cancel, observerCtx: observerCtx, stopObserver: stopObserver}
}

func (s *observationScope) armTimeout(timeout time.Duration) {
	if timeout > 0 {
		s.timer = time.AfterFunc(timeout, func() { s.cancel(context.DeadlineExceeded) })
	}
}

func (s *observationScope) close() {
	if s.timer != nil {
		s.timer.Stop()
	}
	s.cancel(context.Canceled)
	s.stopObserver()
}

type runObservation struct {
	identity     runtimeids.SessionIdentity
	handle       *runtime.LiveRunWaitHandle
	scope        *observationScope
	stopProgress func()
	observer     LiveRunObserver
	response     *runpromptpb.Success
}

func (o *runObservation) Complete(timeout time.Duration) (*runpromptpb.Success, error) {
	defer o.scope.close()
	o.scope.armTimeout(timeout)
	return o.observe()
}

func (o *runObservation) observe() (*runpromptpb.Success, error) {
	if o.stopProgress != nil {
		defer o.stopProgress()
	}
	if o.observer == nil {
		o.scope.close()
		_, _ = o.handle.Wait()
		return o.response, errors.New("headless live run observer is required")
	}
	observed, err := o.observer.ObserveLiveRun(o.scope.observerCtx, o.scope.stopObserver, o.identity.ID, o.handle)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			if cause := context.Cause(o.scope.ctx); cause != nil {
				err = cause
			}
		}
		return o.response, err
	}
	if question := observed.GetOutcome().GetQuestion(); question != nil {
		o.response.Outcome = &runpromptpb.Success_Question{Question: question}
		return o.response, nil
	}
	switch outcome := observed.GetOutcome().GetOutcome().(type) {
	case *promptpb.LiveWatchOutcome_FinalAnswer:
		if outcome.FinalAnswer == nil || outcome.FinalAnswer.Result == nil {
			return o.response, errors.New("live run final answer content is required")
		}
		o.response.Outcome = &runpromptpb.Success_FinalAnswer{FinalAnswer: *outcome.FinalAnswer.Result}
		return o.response, nil
	case *promptpb.LiveWatchOutcome_NoFinalResult:
		return o.response, runtime.ErrLiveRunNoFinalAnswer
	case *promptpb.LiveWatchOutcome_ExecutionError:
		return o.response, liveWatchFailureError(outcome.ExecutionError)
	case *promptpb.LiveWatchOutcome_Interrupted:
		return o.response, liveWatchFailureError(outcome.Interrupted)
	default:
		return o.response, errors.New("live run observer returned an unsupported outcome")
	}
}

func runResponse(identity runtimeids.SessionIdentity) *runpromptpb.Success {
	return &runpromptpb.Success{SessionId: identity.ID.String(), ShortId: protoapi.SessionShortIDToProto(identity.ShortID)}
}
