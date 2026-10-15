package chatmutation

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"core/server/promptcommands"
	"core/server/runtimecontrol"
	"core/shared/protoapi"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	chatshellpb "core/shared/protoapi/gen/kent/api/chat_shell"
	promptcommandpb "core/shared/protoapi/gen/kent/api/prompt_command"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
	"core/shared/serverapi"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

type TargetResolutionService interface {
	Resolve(context.Context, TargetResolutionRequest) (ResolvedTarget, error)
	SelectPlacement(context.Context, ResolvedTarget, promptcommands.Placement) (ResolvedTarget, error)
}

type RuntimeOpeningService interface {
	Open(context.Context, runtimeids.SessionID, *serverapi.SessionRuntimePreparationOptions) error
}

type RuntimeAdmissionService interface {
	AdmitUserShellCommand(context.Context, runtimeids.SessionID, string) error
	PrepareUserTurn(context.Context, string, runtimeinput.Input) (runtimecontrol.PreparedUserTurn, error)
	AdmitChatUserTurn(
		context.Context,
		string,
		runtimecontrol.PreparedUserTurn,
	) (serverapi.ChatInputAdmissionResult, error)
	AdmitChatQueuedUserInput(
		context.Context,
		string,
		runtimecontrol.PreparedUserTurn,
	) (serverapi.ChatInputAdmissionResult, error)
	AdmitManualCompaction(
		context.Context,
		*runtimepb.CompactContextRequest,
	) (bool, error)
}

func (s *Service) SubmitUserShellCommand(ctx context.Context, request *chatshellpb.ShellCommandRequest) (*chatshellpb.ShellCommandSuccess, error) {
	if err := protoapi.Validate(request); err != nil {
		return nil, err
	}
	var result *chatshellpb.ShellCommandSuccess
	operation, err := s.operations.Start(ctx, func(scope OperationScope) error {
		target, err := s.prepareRuntimeWithDraft(scope, request.Target, request.InitialInputDraft)
		if err != nil && target.SessionID.IsZero() {
			return err
		}
		if err == nil {
			err = s.admissions.AdmitUserShellCommand(scope.Context(), target.SessionID, request.Command)
		}
		result = &chatshellpb.ShellCommandSuccess{Session: &chatpb.ExistingSessionTarget{SessionId: target.SessionID.String()}}
		if err != nil {
			result.Outcome = &chatshellpb.ShellCommandSuccess_Failed{Failed: inputNotAcceptedForCause("chat_shell", err)}
		} else {
			result.Outcome = &chatshellpb.ShellCommandSuccess_Completed{Completed: &emptypb.Empty{}}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := operation.Await(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

type ResolvedGoalSetInvoker interface {
	SetResolvedGoal(context.Context, *runtimepb.GoalSetRequest) (serverapi.ResolvedGoalSetCommit, error)
}

type Service struct {
	operations *OperationOwner
	targets    TargetResolutionService
	runtimes   RuntimeOpeningService
	admissions RuntimeAdmissionService
	goals      ResolvedGoalSetInvoker
}

func NewService(
	operations *OperationOwner,
	targets TargetResolutionService,
	runtimes RuntimeOpeningService,
	admissions RuntimeAdmissionService,
	goals ResolvedGoalSetInvoker,
) *Service {
	return &Service{
		operations: operations,
		targets:    targets,
		runtimes:   runtimes,
		admissions: admissions,
		goals:      goals,
	}
}

func (s *Service) SetGoal(
	ctx context.Context,
	request *runtimepb.GoalSetRequest,
) (*runtimepb.GoalSetSuccess, error) {
	if err := protoapi.Validate(request); err != nil {
		return nil, err
	}
	if s == nil || s.operations == nil {
		return nil, errors.New("Chat operation owner is required")
	}
	var result *runtimepb.GoalSetSuccess
	operation, err := s.operations.Start(ctx, func(scope OperationScope) error {
		var operationErr error
		result, operationErr = s.setGoal(scope, request)
		return operationErr
	})
	if err != nil {
		return nil, err
	}
	if err := operation.Await(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) setGoal(
	scope OperationScope,
	request *runtimepb.GoalSetRequest,
) (*runtimepb.GoalSetSuccess, error) {
	target, err := s.resolveTarget(scope, request.Target, request.InitialInputDraft)
	if err != nil {
		return settleGoalSetError(target, err)
	}
	if request.ExecutionPolicy == runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_PRESERVE_RUNTIME_STATE {
		return s.settleGoalSet(scope, request, target)
	}
	_, err = s.openRuntime(scope, target)
	if err != nil {
		return settleGoalSetError(target, err)
	}
	return s.settleGoalSet(scope, request, target)
}

func (s *Service) settleGoalSet(
	scope OperationScope,
	request *runtimepb.GoalSetRequest,
	target ResolvedTarget,
) (*runtimepb.GoalSetSuccess, error) {
	invocation, invocationErr := s.invokeGoal(scope, request, target)
	if invocationErr != nil {
		return settleGoalSetError(target, invocationErr)
	}
	return goalSetSuccess(
		target.SessionID,
		invocation.mutation,
		goalSetTargetError(target, invocation.diagnostic),
	), nil
}

func settleGoalSetError(
	target ResolvedTarget,
	err error,
) (*runtimepb.GoalSetSuccess, error) {
	if target.SessionID.IsZero() {
		return nil, err
	}
	return goalSetSuccessRejected(target.SessionID, goalSetTargetError(target, err)), nil
}

func goalSetTargetError(target ResolvedTarget, err error) error {
	if err == nil {
		return nil
	}
	return serverapi.WithSessionTarget(serverapi.ProjectedSessionTarget{
		Identity: runtimeids.SessionIdentity{ID: target.SessionID, ShortID: target.ShortID},
	}, err)
}

type goalSetInvocation struct {
	mutation   *runtimepb.GoalMutationSuccess
	diagnostic error
}

func (s *Service) invokeGoal(
	scope OperationScope,
	request *runtimepb.GoalSetRequest,
	target ResolvedTarget,
) (goalSetInvocation, error) {
	if s.goals == nil {
		return goalSetInvocation{}, errors.New("Goal Set service is required")
	}
	resolvedRequest := &runtimepb.GoalSetRequest{
		Target: &chatpb.ChatTarget{
			Target: &chatpb.ChatTarget_Session{
				Session: &chatpb.ExistingSessionRequestTarget{SessionId: protoapi.SessionSelectorToProto(target.SessionID)},
			},
		},
		Objective:       strings.TrimSpace(request.Objective),
		Actor:           strings.TrimSpace(request.Actor),
		ExecutionPolicy: request.ExecutionPolicy,
	}
	if request.StepId != nil {
		value := strings.TrimSpace(request.GetStepId())
		resolvedRequest.StepId = &value
	}
	response, err := s.goals.SetResolvedGoal(scope.Context(), resolvedRequest)
	if err != nil {
		return goalSetInvocation{}, err
	}
	if response.Mutation == nil {
		return goalSetInvocation{}, errors.New("resolved Goal Set commit requires a mutation")
	}
	if err := protoapi.Validate(response.Mutation); err != nil {
		return goalSetInvocation{}, fmt.Errorf("validate resolved Goal Set mutation: %w", err)
	}
	return goalSetInvocation{
		mutation:   proto.Clone(response.Mutation).(*runtimepb.GoalMutationSuccess),
		diagnostic: response.Diagnostic,
	}, nil
}

func goalSetSuccess(
	sessionID runtimeids.SessionID,
	mutation *runtimepb.GoalMutationSuccess,
	diagnostic error,
) *runtimepb.GoalSetSuccess {
	success := &runtimepb.GoalSetSuccess{
		Session: &chatpb.ExistingSessionTarget{SessionId: sessionID.String()},
		Outcome: &runtimepb.GoalSetSuccess_Mutation{Mutation: mutation},
	}
	if diagnostic != nil {
		success.Diagnostic = protoapi.GoalSetErrorFromError(diagnostic)
	}
	return success
}

func (s *Service) prepareRuntimeWithDraft(
	scope OperationScope,
	targetRequest *chatpb.ChatTarget,
	initialDraft *string,
) (ResolvedTarget, error) {
	target, err := s.resolveTarget(scope, targetRequest, initialDraft)
	if err != nil {
		return target, err
	}
	return s.openRuntime(scope, target)
}

func (s *Service) resolveTarget(
	scope OperationScope,
	targetRequest *chatpb.ChatTarget,
	initialDraft *string,
) (ResolvedTarget, error) {
	if s == nil || s.targets == nil {
		return ResolvedTarget{}, errors.New("Chat target resolver is required")
	}
	target, err := s.targets.Resolve(scope.Context(), TargetResolutionRequest{
		Target:       targetRequest,
		InitialDraft: initialDraft,
	})
	if err != nil {
		return target, err
	}
	return target, nil
}

func (s *Service) openRuntime(
	scope OperationScope,
	target ResolvedTarget,
) (ResolvedTarget, error) {
	if s == nil || s.runtimes == nil {
		return target, errors.New("Session Runtime planner is required")
	}
	return target, s.runtimes.Open(scope.Context(), target.SessionID, target.Preparation)
}

func goalSetSuccessRejected(
	sessionID runtimeids.SessionID,
	err error,
) *runtimepb.GoalSetSuccess {
	return &runtimepb.GoalSetSuccess{
		Session: &chatpb.ExistingSessionTarget{SessionId: sessionID.String()},
		Outcome: &runtimepb.GoalSetSuccess_Rejected{
			Rejected: protoapi.GoalSetErrorFromError(err),
		},
	}
}

func (s *Service) Steer(
	ctx context.Context,
	request *chatpb.SteerRequest,
) (*chatpb.InputMutationSuccess, error) {
	if err := protoapi.Validate(request); err != nil {
		return nil, err
	}
	return s.runInputMutation(ctx, request.Target, request.Activation, request.QueueItemId, inputMutationSteer)
}

func (s *Service) Queue(
	ctx context.Context,
	request *chatpb.QueueRequest,
) (*chatpb.InputMutationSuccess, error) {
	if err := protoapi.Validate(request); err != nil {
		return nil, err
	}
	return s.runInputMutation(ctx, request.Target, request.Activation, request.QueueItemId, inputMutationQueue)
}

func (s *Service) Compact(
	ctx context.Context,
	request *chatpb.CompactRequest,
) (*chatpb.CompactionMutationSuccess, error) {
	if err := protoapi.Validate(request); err != nil {
		return nil, err
	}
	draft, admission, err := compactionInvocation(request.Invocation)
	if err != nil {
		return nil, err
	}
	if s == nil || s.operations == nil {
		return nil, errors.New("Chat operation owner is required")
	}
	var result *chatpb.CompactionMutationSuccess
	operation, err := s.operations.Start(ctx, func(scope OperationScope) error {
		var operationErr error
		result, operationErr = s.compact(scope, request, draft, admission)
		return operationErr
	})
	if err != nil {
		return nil, err
	}
	if err := operation.Await(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) compact(
	scope OperationScope,
	request *chatpb.CompactRequest,
	draft string,
	admission runtimeinput.ManualCompactionAdmission,
) (*chatpb.CompactionMutationSuccess, error) {
	ctx := scope.Context()
	target, err := s.prepareRuntime(scope, request.Target, draft)
	if err != nil {
		if target.SessionID.IsZero() {
			return nil, err
		}
		return compactionNotAccepted(target.SessionID, err), nil
	}
	if s.admissions == nil {
		return compactionNotAcceptedInternal(
			target.SessionID,
			"chat_compact",
			errors.New("manual-compaction admission service is required"),
		), nil
	}
	accepted, admissionErr := s.admissions.AdmitManualCompaction(ctx, &runtimepb.CompactContextRequest{
		SessionId: target.SessionID.String(),
		Admission: protoapi.ManualCompactionAdmissionToProto(admission),
	})
	if !accepted {
		return compactionNotAccepted(target.SessionID, admissionErr), nil
	}
	acceptedResult := &chatpb.CompactionAccepted{}
	if admissionErr != nil {
		acceptedResult.Diagnostic = acceptedDiagnostic("chat_compact", admissionErr)
	}
	return &chatpb.CompactionMutationSuccess{
		Session: &chatpb.ExistingSessionTarget{SessionId: target.SessionID.String()},
		Outcome: &chatpb.CompactionMutationSuccess_Accepted{Accepted: acceptedResult},
	}, nil
}

type inputMutationOperation string

const (
	inputMutationSteer inputMutationOperation = "chat_steer"
	inputMutationQueue inputMutationOperation = "chat_queue"
)

func (s *Service) mutateInput(
	scope OperationScope,
	targetRequest *chatpb.ChatTarget,
	activation *chatpb.Activation,
	queueItemID string,
	operation inputMutationOperation,
) (*chatpb.InputMutationSuccess, error) {
	ctx := scope.Context()
	draft, input, err := activationInput(activation)
	if err != nil {
		return nil, err
	}
	target, err := s.resolveTarget(scope, targetRequest, &draft)
	if err != nil {
		if target.SessionID.IsZero() {
			return nil, err
		}
		return inputNotAccepted(target.SessionID, operation, err), nil
	}
	if s.admissions == nil {
		return inputNotAccepted(target.SessionID, operation, errors.New("user-turn admission service is required")), nil
	}
	prepared, err := s.admissions.PrepareUserTurn(ctx, target.SessionID.String(), input)
	if err != nil {
		return inputNotAccepted(target.SessionID, operation, err), nil
	}
	prepared.QueueItemID = queueItemID
	target, err = s.targets.SelectPlacement(ctx, target, prepared.Placement)
	if err != nil {
		return inputNotAccepted(target.SessionID, operation, err), nil
	}
	if prepared.Placement == promptcommands.PlacementFresh {
		operation = inputMutationSteer
	}
	target, err = s.openRuntime(scope, target)
	if err != nil {
		return inputNotAccepted(target.SessionID, operation, err), nil
	}
	admissionResult, admissionErr := s.admitInput(ctx, operation, target.SessionID.String(), prepared)
	if !admissionResult.Accepted {
		return inputNotAccepted(target.SessionID, operation, admissionErr), nil
	}
	if admissionResult.QueueItemID.IsZero() {
		return nil, errors.Join(
			fmt.Errorf("accepted Chat %s Queue Item identity is required", operation),
			admissionErr,
		)
	}
	acceptedResult := &chatpb.InputAccepted{
		QueueItem: &chatpb.QueueItemIdentity{Id: admissionResult.QueueItemID.String()},
	}
	switch admissionResult.Disposition {
	case runtimeinput.InputAdmissionStartedTurn:
		acceptedResult.Disposition = chatpb.InputAdmissionDisposition_INPUT_ADMISSION_DISPOSITION_STARTED_TURN
	case runtimeinput.InputAdmissionPendingSteer:
		acceptedResult.Disposition = chatpb.InputAdmissionDisposition_INPUT_ADMISSION_DISPOSITION_PENDING_STEER
	case runtimeinput.InputAdmissionPendingQueue:
		acceptedResult.Disposition = chatpb.InputAdmissionDisposition_INPUT_ADMISSION_DISPOSITION_PENDING_QUEUE
	default:
		return nil, fmt.Errorf("accepted Chat %s admission disposition is invalid: %q", operation, admissionResult.Disposition)
	}
	switch {
	case admissionResult.PromptHistoryFailure != nil:
		acceptedResult.Diagnostic = promptHistoryFailureDiagnostic(
			errors.Join(admissionResult.PromptHistoryFailure, admissionErr),
		)
	case admissionErr != nil:
		acceptedResult.Diagnostic = acceptedDiagnostic(string(operation), admissionErr)
	}
	return &chatpb.InputMutationSuccess{
		Session: &chatpb.ExistingSessionTarget{SessionId: target.SessionID.String()},
		Outcome: &chatpb.InputMutationSuccess_Accepted{Accepted: acceptedResult},
	}, nil
}

func (s *Service) runInputMutation(
	ctx context.Context,
	target *chatpb.ChatTarget,
	activation *chatpb.Activation,
	queueItemID string,
	kind inputMutationOperation,
) (*chatpb.InputMutationSuccess, error) {
	if s == nil || s.operations == nil {
		return nil, errors.New("Chat operation owner is required")
	}
	var result *chatpb.InputMutationSuccess
	operation, err := s.operations.Start(ctx, func(scope OperationScope) error {
		var operationErr error
		result, operationErr = s.mutateInput(scope, target, activation, queueItemID, kind)
		return operationErr
	})
	if err != nil {
		return nil, err
	}
	if err := operation.Await(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) prepareRuntime(
	scope OperationScope,
	targetRequest *chatpb.ChatTarget,
	initialDraft string,
) (ResolvedTarget, error) {
	return s.prepareRuntimeWithDraft(scope, targetRequest, &initialDraft)
}

func (s *Service) admitInput(
	ctx context.Context,
	operation inputMutationOperation,
	sessionID string,
	prepared runtimecontrol.PreparedUserTurn,
) (serverapi.ChatInputAdmissionResult, error) {
	switch operation {
	case inputMutationSteer:
		return s.admissions.AdmitChatUserTurn(ctx, sessionID, prepared)
	case inputMutationQueue:
		return s.admissions.AdmitChatQueuedUserInput(ctx, sessionID, prepared)
	default:
		return serverapi.ChatInputAdmissionResult{}, fmt.Errorf(
			"unsupported Chat input mutation %q",
			operation,
		)
	}
}

func activationInput(activation *chatpb.Activation) (string, runtimeinput.Input, error) {
	if err := protoapi.Validate(activation); err != nil {
		return "", runtimeinput.Input{}, err
	}
	if text, ok := activation.Input.(*chatpb.Activation_Text); ok {
		return text.Text, runtimeinput.Text(text.Text), nil
	}
	command := activation.GetCommand()
	if command == nil {
		return "", runtimeinput.Input{}, errors.New("Chat activation selection is required")
	}
	draft := command.Token + command.SeparatorWhitespace + command.Arguments
	return draft, runtimeinput.Command(command.CatalogIdentity, command.Arguments), nil
}

func compactionInvocation(
	invocation *chatpb.CompactionInvocation,
) (string, runtimeinput.ManualCompactionAdmission, error) {
	if err := protoapi.Validate(invocation); err != nil {
		return "", runtimeinput.ManualCompactionAdmission{}, err
	}
	draft := invocation.Token + invocation.SeparatorWhitespace + invocation.RawGuidance
	normalizedGuidance := runtimeinput.NormalizePendingWorkArgument(invocation.RawGuidance)
	admission := runtimeinput.ManualCompactionAdmission{}
	if normalizedGuidance != "" {
		admission.Guidance = &normalizedGuidance
	}
	return draft, admission, nil
}

func compactionNotAccepted(
	sessionID runtimeids.SessionID,
	cause error,
) *chatpb.CompactionMutationSuccess {
	if cause == nil {
		cause = errors.New("manual-compaction admission completed without accepting work")
	}
	return &chatpb.CompactionMutationSuccess{
		Session: &chatpb.ExistingSessionTarget{SessionId: sessionID.String()},
		Outcome: &chatpb.CompactionMutationSuccess_NotAccepted{
			NotAccepted: compactionNotAcceptedForCause(cause),
		},
	}
}

func compactionNotAcceptedInternal(
	sessionID runtimeids.SessionID,
	operation string,
	cause error,
) *chatpb.CompactionMutationSuccess {
	return &chatpb.CompactionMutationSuccess{
		Session: &chatpb.ExistingSessionTarget{SessionId: sessionID.String()},
		Outcome: &chatpb.CompactionMutationSuccess_NotAccepted{
			NotAccepted: &chatpb.CompactionNotAccepted{
				Reason: &chatpb.CompactionNotAccepted_InternalFailure{
					InternalFailure: internalFailure(operation, cause),
				},
			},
		},
	}
}

func compactionNotAcceptedForCause(cause error) *chatpb.CompactionNotAccepted {
	switch {
	case errors.Is(cause, context.Canceled), errors.Is(cause, context.DeadlineExceeded):
		return &chatpb.CompactionNotAccepted{
			Reason: &chatpb.CompactionNotAccepted_Canceled{Canceled: &emptypb.Empty{}},
		}
	case errors.Is(cause, serverapi.ErrRuntimeUnavailable):
		return &chatpb.CompactionNotAccepted{
			Reason: &chatpb.CompactionNotAccepted_RuntimeUnavailable{
				RuntimeUnavailable: &chatpb.RuntimeUnavailableDetails{},
			},
		}
	case errors.Is(cause, serverapi.ErrPendingWorkCapacity):
		return &chatpb.CompactionNotAccepted{
			Reason: &chatpb.CompactionNotAccepted_PendingWorkCapacity{
				PendingWorkCapacity: &chatpb.PendingWorkCapacityDetails{},
			},
		}
	case errors.Is(cause, serverapi.ErrManualCompactionTooSoon):
		return &chatpb.CompactionNotAccepted{
			Reason: &chatpb.CompactionNotAccepted_TooSoon{
				TooSoon: &chatpb.ManualCompactionTooSoonDetails{},
			},
		}
	case errors.Is(cause, serverapi.ErrManualCompactionDisabled):
		return &chatpb.CompactionNotAccepted{
			Reason: &chatpb.CompactionNotAccepted_Disabled{
				Disabled: &chatpb.ManualCompactionDisabledDetails{},
			},
		}
	case errors.Is(cause, serverapi.ErrManualCompactionActive):
		return &chatpb.CompactionNotAccepted{
			Reason: &chatpb.CompactionNotAccepted_Active{
				Active: &chatpb.ManualCompactionActiveDetails{},
			},
		}
	default:
		return &chatpb.CompactionNotAccepted{
			Reason: &chatpb.CompactionNotAccepted_InternalFailure{
				InternalFailure: internalFailure("chat_compact", cause),
			},
		}
	}
}

func inputNotAccepted(
	sessionID runtimeids.SessionID,
	operation inputMutationOperation,
	cause error,
) *chatpb.InputMutationSuccess {
	if cause == nil {
		cause = errors.New("user-turn admission completed without accepting work")
	}
	return &chatpb.InputMutationSuccess{
		Session: &chatpb.ExistingSessionTarget{SessionId: sessionID.String()},
		Outcome: &chatpb.InputMutationSuccess_NotAccepted{
			NotAccepted: inputNotAcceptedForCause(operation, cause),
		},
	}
}

func inputNotAcceptedForCause(
	operation inputMutationOperation,
	cause error,
) *chatpb.InputNotAccepted {
	switch {
	case errors.Is(cause, context.Canceled), errors.Is(cause, context.DeadlineExceeded):
		return &chatpb.InputNotAccepted{
			Reason: &chatpb.InputNotAccepted_Canceled{Canceled: &emptypb.Empty{}},
		}
	case errors.Is(cause, serverapi.ErrRuntimeUnavailable):
		return &chatpb.InputNotAccepted{
			Reason: &chatpb.InputNotAccepted_RuntimeUnavailable{
				RuntimeUnavailable: &chatpb.RuntimeUnavailableDetails{},
			},
		}
	case errors.Is(cause, serverapi.ErrPendingWorkCapacity):
		return &chatpb.InputNotAccepted{
			Reason: &chatpb.InputNotAccepted_PendingWorkCapacity{
				PendingWorkCapacity: &chatpb.PendingWorkCapacityDetails{},
			},
		}
	}
	var promptErr *serverapi.PromptCommandError
	if errors.As(cause, &promptErr) {
		switch promptErr.Kind {
		case serverapi.PromptCommandErrorKindCatalogRead:
			return &chatpb.InputNotAccepted{
				Reason: &chatpb.InputNotAccepted_PromptCatalogRead{
					PromptCatalogRead: &promptcommandpb.CatalogReadDetails{Command: promptErr.Command},
				},
			}
		case serverapi.PromptCommandErrorKindCommandNotFound:
			if promptErr.Command != nil {
				return &chatpb.InputNotAccepted{
					Reason: &chatpb.InputNotAccepted_PromptCommandNotFound{
						PromptCommandNotFound: &promptcommandpb.CommandNotFoundDetails{Command: *promptErr.Command},
					},
				}
			}
		case serverapi.PromptCommandErrorKindCommandRead:
			if promptErr.Command != nil {
				return &chatpb.InputNotAccepted{
					Reason: &chatpb.InputNotAccepted_PromptCommandRead{
						PromptCommandRead: &promptcommandpb.CommandReadDetails{Command: *promptErr.Command},
					},
				}
			}
		}
	}
	return &chatpb.InputNotAccepted{
		Reason: &chatpb.InputNotAccepted_InternalFailure{
			InternalFailure: internalFailure(string(operation), cause),
		},
	}
}

func acceptedDiagnostic(operation string, cause error) *chatpb.AcceptedDiagnostic {
	return &chatpb.AcceptedDiagnostic{
		Detail: &chatpb.AcceptedDiagnostic_InternalFailure{
			InternalFailure: internalFailure(operation, cause),
		},
	}
}

func promptHistoryFailureDiagnostic(cause error) *chatpb.AcceptedDiagnostic {
	return &chatpb.AcceptedDiagnostic{
		Detail: &chatpb.AcceptedDiagnostic_PromptHistoryFailure{
			PromptHistoryFailure: internalFailure("record_prompt_history", cause),
		},
	}
}

func internalFailure(operation string, cause error) *sharedpb.InternalFailureDetails {
	details := &sharedpb.InternalFailureDetails{}
	if operation != "" {
		details.Operation = &operation
	}
	if cause != nil {
		message := cause.Error()
		details.Cause = &message
	}
	return details
}
