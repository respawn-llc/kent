package runtimecontrol

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"core/prompts"
	"core/server/goalview"
	"core/server/runtime"
	"core/server/session"
	"core/server/sessionruntime"
	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeids"
)

func (s *Service) ShowGoal(ctx context.Context, req *runtimepb.GoalShowRequest) (*runtimepb.GoalShowSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	if s == nil || s.persisted == nil {
		return nil, errors.New("persisted session resolver is required")
	}
	sessionID := strings.TrimSpace(req.SessionId)
	record, err := session.ResolvePersistedSessionRecord(ctx, s.persisted, sessionID)
	if err != nil {
		return nil, fmt.Errorf("resolve persisted session %q: %w", sessionID, err)
	}
	if record.Meta == nil {
		return nil, fmt.Errorf("persisted session %q metadata is required", sessionID)
	}
	availability, err := session.GoalAvailabilityFromMeta(*record.Meta)
	if err != nil {
		return nil, err
	}
	goal, err := goalview.CoreFromSessionState(record.Meta.Goal)
	if err != nil {
		return nil, err
	}
	return &runtimepb.GoalShowSuccess{
		Goal:         goal,
		Availability: goalview.AvailabilityFromSession(availability),
	}, nil
}

func (s *Service) SetGoal(ctx context.Context, req *runtimepb.GoalSetRequest) (*runtimepb.GoalMutationSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	sessionID, err := runtimeids.ParseSessionID(strings.TrimSpace(req.SessionId))
	if err != nil {
		return nil, err
	}
	mutation := goalMutation{
		kind:      goalMutationSet,
		Objective: strings.TrimSpace(req.Objective),
		Actor:     session.GoalActor(strings.TrimSpace(req.Actor)),
	}
	return s.mutateGoal(ctx, sessionID, req.GetRunId(), req.GetStepId(), mutation)
}

func (s *Service) PauseGoal(ctx context.Context, req *runtimepb.GoalMutationRequest) (*runtimepb.GoalMutationSuccess, error) {
	return s.setGoalStatus(ctx, req, session.GoalStatusPaused)
}

func (s *Service) ResumeGoal(ctx context.Context, req *runtimepb.GoalMutationRequest) (*runtimepb.GoalMutationSuccess, error) {
	return s.setGoalStatus(ctx, req, session.GoalStatusActive)
}

func (s *Service) CompleteGoal(ctx context.Context, req *runtimepb.GoalMutationRequest) (*runtimepb.GoalMutationSuccess, error) {
	return s.setGoalStatus(ctx, req, session.GoalStatusComplete)
}

func (s *Service) setGoalStatus(
	ctx context.Context,
	req *runtimepb.GoalMutationRequest,
	status session.GoalStatus,
) (*runtimepb.GoalMutationSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	sessionID, err := runtimeids.ParseSessionID(strings.TrimSpace(req.SessionId))
	if err != nil {
		return nil, err
	}
	return s.mutateGoal(ctx, sessionID, req.GetRunId(), req.GetStepId(), goalMutation{
		kind:   goalMutationStatus,
		Status: status,
		Actor:  session.GoalActor(strings.TrimSpace(req.Actor)),
	})
}

func (s *Service) ClearGoal(ctx context.Context, req *runtimepb.GoalClearRequest) (*runtimepb.GoalMutationSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	sessionID, err := runtimeids.ParseSessionID(strings.TrimSpace(req.SessionId))
	if err != nil {
		return nil, err
	}
	return s.mutateGoal(ctx, sessionID, "", "", goalMutation{
		kind:  goalMutationClear,
		Actor: session.GoalActor(strings.TrimSpace(req.Actor)),
	})
}

type goalMutationKind uint8

const (
	goalMutationSet goalMutationKind = iota + 1
	goalMutationStatus
	goalMutationClear
)

type goalMutation struct {
	kind      goalMutationKind
	Objective string
	Status    session.GoalStatus
	Actor     session.GoalActor
}

func (s *Service) mutateGoal(
	ctx context.Context,
	sessionID runtimeids.SessionID,
	rawRunID string,
	rawStepID string,
	mutation goalMutation,
) (*runtimepb.GoalMutationSuccess, error) {
	if s == nil || s.authority == nil {
		return nil, errors.New("session runtime authority is required")
	}
	runText, stepText := strings.TrimSpace(rawRunID), strings.TrimSpace(rawStepID)
	if mutation.Actor == session.GoalActorAgent && stepText != "" {
		if runText == "" {
			return nil, runtime.ErrAgentGoalStepInactive
		}
		runID, err := runtimeids.ParseRunID(runText)
		if err != nil {
			return nil, err
		}
		stepID, err := runtimeids.ParseStepID(stepText)
		if err != nil {
			return nil, err
		}
		result, err := s.applyExactAgentGoalMutation(ctx, sessionID, runID, stepID, mutation)
		return goalResponseFromRuntimeResult(result, mutation, goalMutationError(err))
	}
	if runText != "" || stepText != "" {
		return nil, errors.New("Goal execution identity requires an agent Step")
	}
	descriptor, err := session.NewOpenSessionDescriptor(sessionID)
	if err != nil {
		return nil, err
	}
	var dormant runtime.GoalCommandResult
	var dormantErr error
	admission, err := s.authority.WithDormantSessionStore(ctx, descriptor, func(_ context.Context, store *session.Store) error {
		dormant, dormantErr = applyDormantGoalMutation(store, mutation)
		if goalResultAccepted(dormant) {
			return nil
		}
		return dormantErr
	})
	if err != nil {
		return nil, goalMutationError(err)
	}
	if !admission.RuntimeAvailable {
		return goalResponseFromRuntimeResult(dormant, mutation, goalMutationError(dormantErr))
	}
	result, err := s.applyLiveGoalMutation(ctx, sessionID, mutation)
	return goalResponseFromRuntimeResult(result, mutation, goalMutationError(err))
}

func (s *Service) applyLiveGoalMutation(
	ctx context.Context,
	sessionID runtimeids.SessionID,
	mutation goalMutation,
) (runtime.GoalCommandResult, error) {
	var result runtime.GoalCommandResult
	apply := func(runtimeCtx context.Context, engine *runtime.Engine) error {
		var err error
		result, err = applyLiveGoalMutation(runtimeCtx, engine, mutation)
		return err
	}
	descriptor, err := session.NewOpenSessionDescriptor(sessionID)
	if err != nil {
		return result, err
	}
	err = s.authority.RunCurrentTurn(ctx, descriptor, func(commit func() (bool, error)) (bool, error) {
		return commit()
	}, func(ctx context.Context, engine *runtime.Engine, accept runtime.CommandAcceptance) error {
		_, err := accept(func() (bool, error) {
			err := apply(ctx, engine)
			return goalResultAccepted(result), err
		})
		return err
	})
	if errors.Is(err, sessionruntime.ErrSessionWorkflowActivationActive) {
		// Retained Workflow activation owns its next execution. Goal control
		// may update it, but must not launch an ordinary Goal execution.
		err = s.authority.WithRetainedWorkflowRuntime(ctx, sessionID, apply)
	}
	if goalResultAccepted(result) {
		return result, err
	}
	return runtime.GoalCommandResult{}, err
}

func (s *Service) applyExactAgentGoalMutation(
	ctx context.Context,
	sessionID runtimeids.SessionID,
	runID runtimeids.RunID,
	stepID runtimeids.StepID,
	mutation goalMutation,
) (runtime.GoalCommandResult, error) {
	var result runtime.GoalCommandResult
	err := s.authority.WithCurrentRuntime(ctx, sessionID, func(_ context.Context, engine *runtime.Engine) error {
		active := engine.ActiveRun()
		if active == nil || active.RunID != runID.String() || active.StepID != stepID.String() {
			return runtime.ErrAgentGoalStepInactive
		}
		var operation runtime.CurrentGoalOperation
		switch mutation.kind {
		case goalMutationSet:
			operation = runtime.CurrentGoalSet{Objective: mutation.Objective, Actor: mutation.Actor}
		case goalMutationStatus:
			operation = runtime.CurrentGoalStatus{Status: mutation.Status, Actor: mutation.Actor}
		default:
			return errors.New("agent Goal mutation kind is invalid")
		}
		var err error
		result, err = engine.ApplyGoalForStep(stepID.String(), operation)
		return err
	})
	return result, err
}

func applyLiveGoalMutation(ctx context.Context, engine *runtime.Engine, mutation goalMutation) (runtime.GoalCommandResult, error) {
	switch mutation.kind {
	case goalMutationSet:
		if err := engine.ValidateGoalSet(mutation.Objective, mutation.Actor); err != nil {
			return runtime.GoalCommandResult{}, err
		}
		if err := engine.RequireGoalLoopStartAllowed(); err != nil {
			return runtime.GoalCommandResult{}, err
		}
		return engine.SetGoalAndStartLoop(ctx, mutation.Objective, mutation.Actor)
	case goalMutationStatus:
		if mutation.Status == session.GoalStatusActive {
			return engine.SetGoalStatusAndStartLoop(ctx, mutation.Status, mutation.Actor)
		}
		return engine.SetGoalStatus(ctx, mutation.Status, mutation.Actor)
	case goalMutationClear:
		return engine.ClearGoal(ctx, mutation.Actor)
	default:
		return runtime.GoalCommandResult{}, fmt.Errorf("unsupported Goal mutation kind %d", mutation.kind)
	}
}

func applyDormantGoalMutation(store *session.Store, mutation goalMutation) (runtime.GoalCommandResult, error) {
	if store == nil {
		return runtime.GoalCommandResult{}, errors.New("session store is required")
	}
	availability, err := store.GoalAvailability()
	if err != nil {
		return runtime.GoalCommandResult{}, err
	}
	switch mutation.kind {
	case goalMutationSet:
		goal, metadataReceipt, err := store.SetGoal(mutation.Objective, mutation.Actor)
		result := runtimeGoalResult(goal, false, runtime.GoalCommandApplied, metadataReceipt, session.CommitReceipt{})
		result.Availability = &availability
		if err != nil || !metadataReceipt.Committed {
			return result, err
		}
		noticeReceipt, noticeErr := runtime.SteerPersistedGoalNotice(store, runtime.GoalNoticeSet, &goal)
		result.NoticeReceipt = noticeReceipt
		return result, noticeErr
	case goalMutationStatus:
		if current := store.Meta().Goal; current != nil && current.Status == mutation.Status {
			result := runtimeGoalResult(*current, false, runtime.GoalCommandNoop, session.CommitReceipt{}, session.CommitReceipt{})
			result.Availability = &availability
			return result, nil
		}
		goal, transitioned, metadataReceipt, err := store.SetGoalStatus(mutation.Status, mutation.Actor)
		disposition := runtime.GoalCommandApplied
		if err == nil && !transitioned {
			disposition = runtime.GoalCommandNoop
		}
		result := runtimeGoalResult(goal, false, disposition, metadataReceipt, session.CommitReceipt{})
		result.Availability = &availability
		if err != nil || !transitioned || !metadataReceipt.Committed {
			return result, err
		}
		noticeReceipt, noticeErr := runtime.SteerPersistedGoalNotice(store, runtime.GoalNoticeStatus, &goal)
		result.NoticeReceipt = noticeReceipt
		return result, noticeErr
	case goalMutationClear:
		goal, metadataReceipt, err := store.ClearGoal(mutation.Actor)
		result := runtimeGoalResult(goal, true, runtime.GoalCommandApplied, metadataReceipt, session.CommitReceipt{})
		result.Availability = &availability
		if err != nil || !metadataReceipt.Committed {
			return result, err
		}
		noticeReceipt, noticeErr := runtime.SteerPersistedGoalNotice(store, runtime.GoalNoticeClear, nil)
		result.NoticeReceipt = noticeReceipt
		return result, noticeErr
	default:
		return runtime.GoalCommandResult{}, fmt.Errorf("unsupported Goal mutation kind %d", mutation.kind)
	}
}

func runtimeGoalResult(
	goal session.GoalState,
	cleared bool,
	disposition runtime.GoalCommandDisposition,
	metadataReceipt session.CommitReceipt,
	noticeReceipt session.CommitReceipt,
) runtime.GoalCommandResult {
	return runtime.GoalCommandResult{
		GoalState:       goal,
		Cleared:         cleared,
		Disposition:     disposition,
		MetadataReceipt: metadataReceipt,
		NoticeReceipt:   noticeReceipt,
	}
}

func goalResultAccepted(result runtime.GoalCommandResult) bool {
	return result.Disposition == runtime.GoalCommandNoop ||
		result.MetadataReceipt.Committed ||
		result.NoticeReceipt.Committed
}

func goalResponseFromRuntimeResult(
	result runtime.GoalCommandResult,
	mutation goalMutation,
	err error,
) (*runtimepb.GoalMutationSuccess, error) {
	if err != nil {
		return nil, err
	}
	var availability *runtimepb.GoalAvailability
	if result.Availability != nil {
		value := goalview.AvailabilityFromSession(*result.Availability)
		availability = &value
	}
	requestedStatus, hasRequestedStatus := goalMutationRequestedStatus(mutation)
	var response *runtimepb.GoalMutationSuccess
	switch result.Disposition {
	case runtime.GoalCommandApplied, runtime.GoalCommandNoop:
		if mutation.kind == goalMutationClear {
			if !result.Cleared || result.Disposition != runtime.GoalCommandApplied {
				return nil, errors.New("Goal Clear result is not authoritative")
			}
			response = &runtimepb.GoalMutationSuccess{
				Kind:         runtimepb.GoalMutationResultKind_GOAL_MUTATION_RESULT_KIND_AUTHORITATIVE_CLEAR,
				Availability: availability,
			}
			break
		}
		if result.Cleared || !hasRequestedStatus || result.GoalState.Status != requestedStatus {
			return nil, errors.New("authoritative Goal result does not match the requested state")
		}
		goal, err := goalview.CoreFromSessionState(&result.GoalState)
		if err != nil {
			return nil, err
		}
		response = &runtimepb.GoalMutationSuccess{
			Kind:         runtimepb.GoalMutationResultKind_GOAL_MUTATION_RESULT_KIND_AUTHORITATIVE_GOAL,
			Goal:         goal,
			Availability: availability,
		}
	default:
		return nil, errors.New("Goal mutation is missing an authoritative result")
	}
	if err := protoapi.Validate(response); err != nil {
		return nil, err
	}
	return response, nil
}

func goalMutationRequestedStatus(mutation goalMutation) (session.GoalStatus, bool) {
	switch mutation.kind {
	case goalMutationSet:
		return session.GoalStatusActive, true
	case goalMutationStatus:
		return mutation.Status, true
	default:
		return "", false
	}
}

type goalAgentOverwriteDeniedError struct {
	Objective string
	Status    string
}

func (e goalAgentOverwriteDeniedError) Error() string {
	return strings.TrimSpace(prompts.RenderGoalAgentDuplicateSetDeniedPrompt(e.Objective, e.Status))
}

func goalMutationError(err error) error {
	if err == nil {
		return nil
	}
	var blocked session.GoalAgentOverwriteBlockedError
	if errors.As(err, &blocked) {
		return goalAgentOverwriteDeniedError{Objective: blocked.Goal.Objective, Status: string(blocked.Goal.Status)}
	}
	return err
}
