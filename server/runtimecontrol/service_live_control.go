package runtimecontrol

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"core/server/registry"
	"core/server/runtime"
	"core/server/session"
	servicecontract "core/shared/apicontract"
	"core/shared/clientui"
	"core/shared/protoapi"
	attentionpb "core/shared/protoapi/gen/kent/api/attention"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/textutil"
	"google.golang.org/protobuf/types/known/durationpb"
)

var _ servicecontract.RuntimeLiveControlService = (*Service)(nil)

func retainLiveTargetFailure(identity runtimeids.SessionIdentity, err *error) {
	if *err != nil {
		*err = serverapi.WithSessionTarget(serverapi.ProjectedSessionTarget{Identity: identity}, *err)
	}
}

func (s *Service) liveTarget(ctx context.Context, target *sharedpb.SessionSelector, caller *string) (identity runtimeids.SessionIdentity, err error) {
	var captured serverapi.SessionTarget = serverapi.UnresolvedSessionTarget{}
	defer func() { err = serverapi.WithSessionTarget(captured, err) }()
	id, err := s.metadata.ResolveSessionTarget(ctx, target)
	if err != nil {
		if errors.Is(err, session.ErrSessionNotFound) {
			err = errors.Join(serverapi.ErrRuntimeUnavailable, err)
		}
		return identity, err
	}
	captured = serverapi.CanonicalSessionTarget{ID: id}
	identity, err = s.metadata.SessionIdentity(ctx, id)
	if err != nil {
		return identity, err
	}
	captured = serverapi.ProjectedSessionTarget{Identity: identity}
	if caller != nil {
		source, err := runtimeids.ParseSessionID(*caller)
		if err != nil {
			return identity, err
		}
		if source == id {
			return identity, &serverapi.SessionSelfTargetError{SessionID: id}
		}
	}
	return identity, nil
}

func (s *Service) withLiveExecutionRuntime(ctx context.Context, id runtimeids.SessionID, fn func(context.Context, *runtime.Engine) error) error {
	if s == nil || s.authority == nil {
		return errors.New("session runtime authority is required")
	}
	return s.authority.WithLiveExecutionRuntime(ctx, id, fn)
}

func (s *Service) LiveSteer(ctx context.Context, req *runtimepb.LiveSteerRequest) (resp *runtimepb.LiveSteerSuccess, err error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	identity, err := s.liveTarget(ctx, req.SessionId, req.CallerSessionId)
	if err != nil {
		return nil, err
	}
	defer retainLiveTargetFailure(identity, &err)
	sessionID := identity.ID
	callerSessionID := req.CallerSessionId
	text := strings.TrimSpace(req.Text)
	queueText := text
	var agentSteer runtime.AgentSteer
	if callerSessionID != nil {
		callerID, parseErr := runtimeids.ParseSessionID(*callerSessionID)
		if parseErr != nil {
			return nil, parseErr
		}
		source, err := s.metadata.SessionIdentity(ctx, callerID)
		if err != nil {
			return nil, err
		}
		agentSteer, err = runtime.NewAgentSteer(source, text)
		if err != nil {
			return nil, err
		}
		queueText = *agentSteer.Message().Content
	}
	err = s.withLiveExecutionRuntime(ctx, sessionID, func(callbackCtx context.Context, engine *runtime.Engine) error {
		var item runtime.QueuedUserMessage
		var accepted bool
		if callerSessionID != nil {
			item, accepted, err = engine.QueueAgentSteerForActiveRun(callbackCtx, agentSteer, nil)
		} else {
			item, accepted, err = engine.QueueUserMessageForActiveRun(callbackCtx, queueText, nil)
		}
		if errors.Is(err, runtime.ErrNoActiveLiveRun) {
			return serverapi.ErrRuntimeNoActiveRun
		}
		if err != nil {
			return err
		}
		if !accepted {
			return serverapi.ErrRuntimeNoActiveRun
		}
		displayText, displayErr := item.DisplayText()
		if displayErr != nil {
			return displayErr
		}
		resp = &runtimepb.LiveSteerSuccess{
			QueueItemId: item.ID,
			Text:        displayText,
		}
		if s != nil && s.promptStore != nil {
			if _, err := s.recordPromptHistory(callbackCtx, sessionID.String(), queueText); err != nil {
				engine.ReportPromptHistoryPersistError(err.Error())
			}
		}
		return nil
	})
	return resp, err
}

func (s *Service) LiveStop(ctx context.Context, req *runtimepb.LiveStopRequest) (response *runtimepb.LiveStopSuccess, err error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	identity, err := s.liveTarget(ctx, req.SessionId, req.CallerSessionId)
	if errors.Is(err, session.ErrSessionNotFound) {
		return &runtimepb.LiveStopSuccess{Status: runtimepb.LiveStopStatus_RUNTIME_LIVE_STOP_STATUS_IDLE}, nil
	}
	if err != nil {
		return nil, err
	}
	defer retainLiveTargetFailure(identity, &err)
	if s == nil || s.authority == nil {
		return nil, errors.New("session runtime authority is required")
	}
	resp := &runtimepb.LiveStopSuccess{Status: runtimepb.LiveStopStatus_RUNTIME_LIVE_STOP_STATUS_IDLE}
	stopped, err := s.authority.InterruptSession(ctx, identity.ID)
	if err != nil {
		return nil, err
	}
	if stopped {
		resp.Status = runtimepb.LiveStopStatus_RUNTIME_LIVE_STOP_STATUS_STOPPED
	}
	return resp, nil
}

func (s *Service) captureLiveRun(ctx context.Context, id runtimeids.SessionID) (*runtime.LiveRunWaitHandle, *string, error) {
	var handle *runtime.LiveRunWaitHandle
	var name *string
	err := s.withLiveExecutionRuntime(ctx, id, func(callbackCtx context.Context, engine *runtime.Engine) error {
		var err error
		handle, err = engine.CaptureActiveRunResult(callbackCtx)
		if err == nil {
			name = engine.SessionName()
		}
		return err
	})
	if errors.Is(err, runtime.ErrNoActiveLiveRun) {
		return nil, nil, serverapi.ErrRuntimeNoActiveRun
	}
	return handle, name, err
}

func (s *Service) LiveWait(ctx context.Context, req *runtimepb.LiveWaitRequest) (resp *runtimepb.LiveWaitSuccess, err error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	identity, err := s.liveTarget(ctx, req.SessionId, req.CallerSessionId)
	if err != nil {
		return nil, err
	}
	defer retainLiveTargetFailure(identity, &err)
	sessionID := identity.ID
	waitHandle, sessionName, err := s.captureLiveRun(ctx, sessionID)
	if err != nil {
		return resp, err
	}
	result, err := waitHandle.Wait()
	if errors.Is(err, runtime.ErrLiveRunNoFinalAnswer) {
		return resp, serverapi.ErrRuntimeNoFinalAnswer
	}
	if err != nil {
		return resp, err
	}
	if result.AssistantMessage.Content == nil {
		return nil, errors.New("live run final answer content is required")
	}
	resp = &runtimepb.LiveWaitSuccess{
		ShortId:   protoapi.SessionShortIDToProto(identity.ShortID),
		SessionId: sessionID.String(), SessionName: sessionName,
		Result: &runtimepb.LiveWaitSuccess_AssistantFinalAnswer{
			AssistantFinalAnswer: &runtimepb.LiveWaitAssistantFinalAnswer{Result: *result.AssistantMessage.Content},
		},
		Duration:       durationpb.New(result.FinishedAt.Sub(result.StartedAt)),
		LiveRunGroupId: result.GroupID.String(), TerminalStepId: result.StepID.String(),
		TerminalStatus: string(result.Status),
	}
	return resp, err
}

func (s *Service) pendingWatchQuestion(identity runtimeids.SessionIdentity) (*serverapi.ObservationQuestion, error) {
	sessionID := identity.ID
	if s.pendingPrompts == nil {
		return nil, nil
	}
	var asks []clientui.PendingAsk
	var approvals []clientui.PendingApproval
	for _, snapshot := range s.pendingPrompts.ListPendingPrompts(sessionID.String()) {
		if snapshot.Request.Approval {
			approval, err := registry.PendingApprovalFromSnapshot(sessionID, snapshot)
			if err != nil {
				return nil, err
			}
			approval.ShortID = identity.ShortID
			approvals = append(approvals, approval)
		} else {
			ask, err := registry.PendingAskFromSnapshot(sessionID, snapshot)
			if err != nil {
				return nil, err
			}
			ask.ShortID = identity.ShortID
			asks = append(asks, ask)
		}
	}
	prompt, ok := serverapi.FirstPendingPromptObservation(asks, approvals)
	if !ok {
		return nil, nil
	}
	return &prompt.Question, nil
}

func liveWatchQuestionResult(id runtimeids.SessionID, question *serverapi.ObservationQuestion) (*promptpb.LiveWatchSuccess, error) {
	projected, err := protoapi.ObservationQuestionToProto(*question)
	if err != nil {
		return nil, err
	}
	return &promptpb.LiveWatchSuccess{SessionId: id.String(), Outcome: &promptpb.LiveWatchOutcome{
		Outcome: &promptpb.LiveWatchOutcome_Question{Question: projected},
	}}, nil
}

func (s *Service) LiveWatch(ctx context.Context, req *promptpb.LiveWatchRequest) (response *promptpb.LiveWatchSuccess, err error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	identity, err := s.liveTarget(ctx, req.SessionId, req.CallerSessionId)
	if err != nil {
		return nil, err
	}
	defer retainLiveTargetFailure(identity, &err)
	id := identity.ID
	if s.attention == nil {
		return nil, errors.New("attention notification service is required")
	}
	defer func() {
		if response != nil {
			response.ShortId = protoapi.SessionShortIDToProto(identity.ShortID)
		}
	}()
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	sub, err := s.attention.SubscribeSessionAttentionNotifications(ctx, &attentionpb.SubscribeRequest{
		SessionId: id.String(),
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = sub.Close() }()
	handle, name, captureErr := s.captureLiveRun(watchCtx, id)
	if errors.Is(captureErr, serverapi.ErrRuntimeNoActiveRun) {
		question, err := s.pendingWatchQuestion(identity)
		if err != nil {
			return nil, err
		}
		if question == nil {
			return nil, serverapi.ErrRuntimeNoActiveRun
		}
		return liveWatchQuestionResult(id, question)
	}
	if captureErr != nil {
		return nil, captureErr
	}
	return s.observeCapturedLiveRun(watchCtx, cancel, identity, name, handle, sub)
}

// ObserveLiveRun observes a run already captured by its owner. cancel must
// cancel the context used to capture handle, so a pending prompt can release
// the observer's terminal waiter without affecting the execution itself.
func (s *Service) ObserveLiveRun(
	ctx context.Context,
	cancel context.CancelFunc,
	id runtimeids.SessionID,
	handle *runtime.LiveRunWaitHandle,
) (*promptpb.LiveWatchSuccess, error) {
	identity, err := s.metadata.SessionIdentity(ctx, id)
	if err != nil {
		cancel()
		return nil, err
	}
	return s.observeCapturedLiveRun(ctx, cancel, identity, nil, handle, nil)
}

func (s *Service) observeCapturedLiveRun(
	ctx context.Context,
	cancel context.CancelFunc,
	identity runtimeids.SessionIdentity,
	name *string,
	handle *runtime.LiveRunWaitHandle,
	sub serverapi.SessionAttentionNotificationSubscription,
) (*promptpb.LiveWatchSuccess, error) {
	id := identity.ID
	if cancel == nil {
		return nil, errors.New("live run observation cancel function is required")
	}
	if handle == nil {
		cancel()
		return nil, errors.New("live run observation handle is required")
	}
	type terminal struct {
		result runtime.LiveRunResult
		err    error
	}
	terminalCh := make(chan terminal, 1)
	questionCh := make(chan *serverapi.ObservationQuestion, 1)
	attentionErrCh := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		result, err := handle.Wait()
		terminalCh <- terminal{result, err}
	}()
	ownsSubscription := sub == nil
	if ownsSubscription {
		if s.attention == nil {
			cancel()
			wg.Wait()
			return nil, errors.New("attention notification service is required")
		}
		var err error
		sub, err = s.attention.SubscribeSessionAttentionNotifications(ctx, &attentionpb.SubscribeRequest{
			SessionId: id.String(),
		})
		if err != nil {
			cancel()
			wg.Wait()
			return nil, err
		}
	}
	defer func() {
		cancel()
		if ownsSubscription {
			_ = sub.Close()
		}
		wg.Wait()
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			if _, err := sub.Next(ctx); err != nil {
				if ctx.Err() == nil {
					attentionErrCh <- liveWatchAttentionStreamError(err)
				}
				return
			}
			question, err := s.pendingWatchQuestion(identity)
			if err != nil {
				if ctx.Err() == nil {
					attentionErrCh <- liveWatchAttentionStreamError(err)
				}
				return
			}
			if question != nil {
				questionCh <- question
				return
			}
		}
	}()
	if question, err := s.pendingWatchQuestion(identity); err != nil {
		return nil, err
	} else if question != nil {
		return liveWatchQuestionResult(id, question)
	}
	select {
	case question := <-questionCh:
		return liveWatchQuestionResult(id, question)
	case terminal := <-terminalCh:
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return liveWatchResult(id, name, terminal.result, terminal.err)
	case err := <-attentionErrCh:
		if ctx.Err() == nil {
			select {
			case terminal := <-terminalCh:
				return liveWatchResult(id, name, terminal.result, terminal.err)
			default:
			}
		}
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type liveWatchAttentionStreamFailure struct {
	cause error
}

func (e *liveWatchAttentionStreamFailure) Error() string {
	return fmt.Sprintf("%s: %v", serverapi.ErrStreamFailed, e.cause)
}

func (e *liveWatchAttentionStreamFailure) Unwrap() error {
	return serverapi.ErrStreamFailed
}

func liveWatchAttentionStreamError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return &liveWatchAttentionStreamFailure{cause: err}
	}
	return serverapi.NormalizeStreamError(err)
}

type liveRunTerminal struct {
	result        runtime.LiveRunResult
	err           error
	noFinal       bool
	noFinalReason runtime.LiveRunNoFinalAnswerReason
	status        runtime.RunStatus
	reason        *string
	diagnostic    *string
}

func classifyLiveRun(result runtime.LiveRunResult, err error) liveRunTerminal {
	terminal := liveRunTerminal{
		result: result,
		err:    err,
		status: result.Status,
	}
	if errors.Is(err, runtime.ErrLiveRunNoFinalAnswer) {
		terminal.noFinal = true
		terminal.noFinalReason = result.NoFinalReason
		return terminal
	}
	if terminal.err == nil {
		terminal.err = result.Error
	}
	if terminal.err != nil {
		if terminal.status == runtime.RunStatusInterrupted {
			reason := strings.TrimSpace(string(terminal.status))
			terminal.reason = &reason
			diagnosticErr := result.Error
			if diagnosticErr == nil {
				diagnosticErr = terminal.err
			}
			diagnostic := strings.TrimSpace(diagnosticErr.Error())
			if diagnostic != "" && diagnostic != reason {
				terminal.diagnostic = &diagnostic
			}
			return terminal
		}
		reason := strings.TrimSpace(terminal.err.Error())
		if reason == "" {
			reason = strings.TrimSpace(string(result.Status))
		}
		if reason == "" {
			reason = "execution error"
		}
		terminal.reason = &reason
	}
	if result.Error != nil && (terminal.reason == nil || result.Error.Error() != *terminal.reason) {
		value := result.Error.Error()
		terminal.diagnostic = &value
	}
	return terminal
}

func liveWatchResult(id runtimeids.SessionID, name *string, result runtime.LiveRunResult, err error) (*promptpb.LiveWatchSuccess, error) {
	terminal := classifyLiveRun(result, err)
	if terminal.noFinal {
		reason := strings.TrimSpace(string(terminal.noFinalReason))
		if reason == "" {
			reason = string(runtime.LiveRunNoFinalAnswerReasonUnknown)
		}
		return &promptpb.LiveWatchSuccess{SessionId: id.String(), Outcome: &promptpb.LiveWatchOutcome{
			Outcome: &promptpb.LiveWatchOutcome_NoFinalResult{NoFinalResult: &promptpb.LiveWatchFailure{Reason: reason}},
		}}, nil
	}
	if terminal.err != nil {
		reason := "execution error"
		if terminal.reason != nil {
			reason = *terminal.reason
		}
		failure := &promptpb.LiveWatchFailure{Reason: reason, Diagnostic: terminal.diagnostic}
		outcome := &promptpb.LiveWatchOutcome{Outcome: &promptpb.LiveWatchOutcome_ExecutionError{ExecutionError: failure}}
		if terminal.status == runtime.RunStatusInterrupted {
			outcome.Outcome = &promptpb.LiveWatchOutcome_Interrupted{Interrupted: failure}
		}
		return &promptpb.LiveWatchSuccess{SessionId: id.String(), Outcome: outcome}, nil
	}
	return &promptpb.LiveWatchSuccess{SessionId: id.String(), Outcome: &promptpb.LiveWatchOutcome{
		Outcome: &promptpb.LiveWatchOutcome_FinalAnswer{FinalAnswer: &promptpb.LiveWatchFinal{
			Result: textutil.Pointer(result.AssistantMessage.Content), SessionName: name,
			Duration: durationpb.New(result.FinishedAt.Sub(result.StartedAt)),
		}},
	}}, nil
}
