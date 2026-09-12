package client

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"core/shared/clientui"
	"core/shared/protoapi"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/serverapi"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func goalMethod(name string) protoreflect.MethodDescriptor {
	return runtimepb.File_kent_api_runtime_runtime_proto.Services().
		ByName("GoalService").
		Methods().
		ByName(protoreflect.Name(name))
}

func (c *Remote) SetGoal(
	ctx context.Context,
	request serverapi.RuntimeGoalSetRequest,
) (serverapi.RuntimeGoalSetResponse, error) {
	if err := request.Validate(); err != nil {
		return serverapi.RuntimeGoalSetResponse{}, err
	}
	var executionPolicy runtimepb.GoalExecutionPolicy
	switch request.ExecutionPolicy {
	case serverapi.RuntimeGoalExecutionPolicyStartOrContinue:
		executionPolicy = runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_START_OR_CONTINUE
	case serverapi.RuntimeGoalExecutionPolicyPreserveRuntimeState:
		executionPolicy = runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_PRESERVE_RUNTIME_STATE
	default:
		return serverapi.RuntimeGoalSetResponse{}, errors.New("execution_policy is required")
	}
	generatedRequest := &runtimepb.GoalSetRequest{
		Target: &chatpb.ChatTarget{
			Target: &chatpb.ChatTarget_Session{
				Session: &chatpb.ExistingSessionTarget{SessionId: strings.TrimSpace(request.SessionID)},
			},
		},
		Objective:       request.Objective,
		Actor:           request.Actor,
		ExecutionPolicy: executionPolicy,
	}
	if strings.TrimSpace(request.RunID) != "" {
		value := request.RunID
		generatedRequest.RunId = &value
	}
	if strings.TrimSpace(request.StepID) != "" {
		value := request.StepID
		generatedRequest.StepId = &value
	}
	success, err := callGoalSetBinary(c, ctx, generatedRequest)
	if err != nil {
		return serverapi.RuntimeGoalSetResponse{}, err
	}
	if success == nil || success.Session == nil {
		return serverapi.RuntimeGoalSetResponse{}, errors.New("Goal Set response Session is required")
	}
	if success.Session.SessionId != strings.TrimSpace(request.SessionID) {
		return serverapi.RuntimeGoalSetResponse{}, fmt.Errorf(
			"Goal Set response Session %q does not match requested Session %q",
			success.Session.SessionId,
			request.SessionID,
		)
	}
	if rejected := success.GetRejected(); rejected != nil {
		return serverapi.RuntimeGoalSetResponse{}, goalSetGeneratedError(rejected)
	}
	mutation := success.GetMutation()
	if mutation == nil {
		return serverapi.RuntimeGoalSetResponse{}, errors.New("Goal Set response mutation is required")
	}
	result, err := goalMutationFromProto(mutation)
	if err != nil {
		return serverapi.RuntimeGoalSetResponse{}, err
	}
	response := serverapi.RuntimeGoalSetResponse{Result: result}
	if diagnostic := success.GetDiagnostic(); diagnostic != nil {
		response.Diagnostic = goalSetGeneratedError(diagnostic)
	}
	return response, nil
}

func callGoalSetBinary(
	c *Remote,
	ctx context.Context,
	request *runtimepb.GoalSetRequest,
) (*runtimepb.GoalSetSuccess, error) {
	method := goalMethod("Set")
	result := &runtimepb.GoalSetResult{}
	if err := c.callBinary(ctx, method, request, result); err != nil {
		return nil, err
	}
	classified, err := protoapi.ClassifyResult(result)
	if err != nil {
		return nil, fmt.Errorf("classify %s result: %w", method.FullName(), err)
	}
	switch classified.Outcome {
	case protoapi.OperationSuccess:
		return result.GetSuccess(), nil
	case protoapi.OperationKnownFailure, protoapi.OperationGenericFailure:
		failure := result.GetError()
		if failure == nil {
			return nil, fmt.Errorf("%s classified a failure without an error value", method.FullName())
		}
		return nil, goalSetGeneratedError(failure)
	default:
		return nil, fmt.Errorf("classify %s result: unknown outcome %d", method.FullName(), classified.Outcome)
	}
}

type GoalSetGeneratedError struct {
	Failure *runtimepb.GoalSetError
}

func (e *GoalSetGeneratedError) Error() string {
	if e == nil || e.Failure == nil || e.Failure.Code == "" {
		return "Goal Set failed"
	}
	return fmt.Sprintf("Goal Set failed with code %q", e.Failure.Code)
}

func goalSetGeneratedError(failure *runtimepb.GoalSetError) error {
	if failure == nil {
		return errors.New("Goal Set error is required")
	}
	if err := protoapi.Validate(failure); err != nil {
		return fmt.Errorf("validate Goal Set error: %w", err)
	}
	switch failure.Code {
	case "runtime_unavailable":
		return serverapi.ErrRuntimeUnavailable
	case "internal_failure":
		return protoapi.InternalFailureFromProto(failure.GetInternalFailure())
	default:
		return &GoalSetGeneratedError{Failure: failure}
	}
}

func goalMutationFromProto(
	success *runtimepb.GoalMutationSuccess,
) (clientui.GoalMutationResult, error) {
	if success == nil {
		return clientui.GoalMutationResult{}, errors.New("Goal mutation success is required")
	}
	result := clientui.GoalMutationResult{}
	switch success.Kind {
	case runtimepb.GoalMutationResultKind_GOAL_MUTATION_RESULT_KIND_AUTHORITATIVE_GOAL:
		if success.Goal == nil {
			return clientui.GoalMutationResult{}, errors.New("authoritative Goal result requires Goal")
		}
		status, err := goalStatusFromProto(success.Goal.Status)
		if err != nil {
			return clientui.GoalMutationResult{}, err
		}
		result.Kind = clientui.GoalMutationResultAuthoritativeGoal
		result.Goal = &clientui.Goal{
			ID:        success.Goal.Id,
			Objective: success.Goal.Objective,
			Status:    status,
			CreatedAt: success.Goal.CreatedAt.AsTime(),
			UpdatedAt: success.Goal.UpdatedAt.AsTime(),
		}
	case runtimepb.GoalMutationResultKind_GOAL_MUTATION_RESULT_KIND_AUTHORITATIVE_CLEAR:
		result.Kind = clientui.GoalMutationResultAuthoritativeClear
	default:
		return clientui.GoalMutationResult{}, fmt.Errorf("unknown Goal mutation kind %q", success.Kind)
	}
	if success.Availability != nil {
		availability, err := goalAvailabilityFromProto(*success.Availability)
		if err != nil {
			return clientui.GoalMutationResult{}, err
		}
		result.Availability = &availability
	}
	if err := result.Validate(); err != nil {
		return clientui.GoalMutationResult{}, err
	}
	return result, nil
}

func goalStatusFromProto(status runtimepb.GoalStatus) (clientui.RuntimeGoalStatus, error) {
	switch status {
	case runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE:
		return clientui.RuntimeGoalStatusActive, nil
	case runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_PAUSED:
		return clientui.RuntimeGoalStatusPaused, nil
	case runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_COMPLETE:
		return clientui.RuntimeGoalStatusComplete, nil
	default:
		return "", fmt.Errorf("unknown Goal status %q", status)
	}
}

func goalAvailabilityFromProto(
	availability runtimepb.GoalAvailability,
) (clientui.GoalAvailability, error) {
	switch availability {
	case runtimepb.GoalAvailability_GOAL_AVAILABILITY_AVAILABLE:
		return clientui.GoalAvailabilityAvailable, nil
	case runtimepb.GoalAvailability_GOAL_AVAILABILITY_AGENT_CAPABILITY_MISSING:
		return clientui.GoalAvailabilityAgentCapabilityMissing, nil
	default:
		return "", fmt.Errorf("unknown Goal availability %q", availability)
	}
}
