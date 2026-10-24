package protoapi

import (
	"context"
	"errors"
	"fmt"

	chatpb "core/shared/protoapi/gen/kent/api/chat"
	promptcommandpb "core/shared/protoapi/gen/kent/api/prompt_command"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/serverapi"
	"google.golang.org/protobuf/types/known/emptypb"
)

func RuntimeUnavailableFromProto(details *runtimepb.RuntimeUnavailableDetails) error {
	if details.SessionTarget == nil {
		return serverapi.ErrRuntimeUnavailable
	}
	return SessionTargetContextErrorFromProto(details.SessionTarget, serverapi.ErrRuntimeUnavailable)
}

func SessionSelfTargetToProto(err error, self *serverapi.SessionSelfTargetError) *sharedpb.SessionSelfTargetDetails {
	captured := serverapi.WithSessionTarget(serverapi.CanonicalSessionTarget{ID: self.SessionID}, err)
	return &sharedpb.SessionSelfTargetDetails{SessionTarget: SessionTargetErrorContext(captured)}
}

func SessionSelfTargetFromProto(details *sharedpb.SessionSelfTargetDetails) error {
	self := &serverapi.SessionSelfTargetError{}
	captured := SessionTargetContextErrorFromProto(details.SessionTarget, self)
	target := captured.(*serverapi.SessionTargetError)
	self.SessionID = *target.SessionID()
	return captured
}

func RuntimeCommandNotAcceptedToProto(target *sharedpb.SessionTargetContext, err *serverapi.RuntimeCommandNotAcceptedError) *runtimepb.RuntimeCommandNotAcceptedDetails {
	details := &runtimepb.RuntimeCommandNotAcceptedDetails{}
	var commandErr *serverapi.PromptCommandError
	switch {
	case errors.Is(err.Cause, context.Canceled):
		details.Cause = &runtimepb.RuntimeCommandNotAcceptedDetails_Canceled{Canceled: &emptypb.Empty{}}
	case errors.Is(err.Cause, serverapi.ErrRuntimeUnavailable):
		details.Cause = &runtimepb.RuntimeCommandNotAcceptedDetails_RuntimeUnavailable{
			RuntimeUnavailable: &runtimepb.RuntimeUnavailableDetails{SessionTarget: target},
		}
	case errors.Is(err.Cause, serverapi.ErrRuntimeNoActiveRun):
		details.Cause = &runtimepb.RuntimeCommandNotAcceptedDetails_NoActiveRun{NoActiveRun: &emptypb.Empty{}}
	case errors.Is(err.Cause, serverapi.ErrManualCompactionTooSoon):
		details.Cause = &runtimepb.RuntimeCommandNotAcceptedDetails_ManualCompactionTooSoon{ManualCompactionTooSoon: &chatpb.ManualCompactionTooSoonDetails{}}
	case errors.Is(err.Cause, serverapi.ErrManualCompactionDisabled):
		details.Cause = &runtimepb.RuntimeCommandNotAcceptedDetails_ManualCompactionDisabled{ManualCompactionDisabled: &chatpb.ManualCompactionDisabledDetails{}}
	case errors.Is(err.Cause, serverapi.ErrManualCompactionActive):
		details.Cause = &runtimepb.RuntimeCommandNotAcceptedDetails_ManualCompactionActive{ManualCompactionActive: &chatpb.ManualCompactionActiveDetails{}}
	case errors.Is(err.Cause, serverapi.ErrPendingWorkCapacity):
		details.Cause = &runtimepb.RuntimeCommandNotAcceptedDetails_PendingWorkCapacity{PendingWorkCapacity: &chatpb.PendingWorkCapacityDetails{}}
	case errors.As(err.Cause, &commandErr):
		converted, conversionErr := PromptCommandErrorToProto(commandErr)
		if conversionErr != nil {
			cause := errors.Join(err, conversionErr).Error()
			details.Cause = &runtimepb.RuntimeCommandNotAcceptedDetails_InternalFailure{InternalFailure: &sharedpb.InternalFailureDetails{Cause: &cause}}
			break
		}
		switch value := converted.(type) {
		case *promptcommandpb.CatalogReadDetails:
			details.Cause = &runtimepb.RuntimeCommandNotAcceptedDetails_PromptCatalogRead{PromptCatalogRead: value}
		case *promptcommandpb.CommandNotFoundDetails:
			details.Cause = &runtimepb.RuntimeCommandNotAcceptedDetails_PromptCommandNotFound{PromptCommandNotFound: value}
		case *promptcommandpb.CommandReadDetails:
			details.Cause = &runtimepb.RuntimeCommandNotAcceptedDetails_PromptCommandRead{PromptCommandRead: value}
		}
	default:
		cause := serverapi.ErrRuntimeCommandNotAccepted.Error()
		if err.Cause != nil {
			cause = err.Cause.Error()
		}
		details.Cause = &runtimepb.RuntimeCommandNotAcceptedDetails_InternalFailure{
			InternalFailure: &sharedpb.InternalFailureDetails{Cause: &cause},
		}
	}
	return details
}

func RuntimeCommandNotAcceptedFromProto(details *runtimepb.RuntimeCommandNotAcceptedDetails) error {
	var cause error
	switch selected := details.GetCause().(type) {
	case *runtimepb.RuntimeCommandNotAcceptedDetails_Canceled:
		cause = context.Canceled
	case *runtimepb.RuntimeCommandNotAcceptedDetails_RuntimeUnavailable:
		cause = RuntimeUnavailableFromProto(selected.RuntimeUnavailable)
	case *runtimepb.RuntimeCommandNotAcceptedDetails_NoActiveRun:
		cause = serverapi.ErrRuntimeNoActiveRun
	case *runtimepb.RuntimeCommandNotAcceptedDetails_InternalFailure:
		cause = InternalFailureFromProto(selected.InternalFailure)
	case *runtimepb.RuntimeCommandNotAcceptedDetails_ManualCompactionTooSoon:
		cause = serverapi.ErrManualCompactionTooSoon
	case *runtimepb.RuntimeCommandNotAcceptedDetails_ManualCompactionDisabled:
		cause = serverapi.ErrManualCompactionDisabled
	case *runtimepb.RuntimeCommandNotAcceptedDetails_ManualCompactionActive:
		cause = serverapi.ErrManualCompactionActive
	case *runtimepb.RuntimeCommandNotAcceptedDetails_PendingWorkCapacity:
		cause = &serverapi.PendingWorkCapacityError{}
	case *runtimepb.RuntimeCommandNotAcceptedDetails_PromptCatalogRead:
		cause = PromptCommandErrorFromProto(selected.PromptCatalogRead)
	case *runtimepb.RuntimeCommandNotAcceptedDetails_PromptCommandNotFound:
		cause = PromptCommandErrorFromProto(selected.PromptCommandNotFound)
	case *runtimepb.RuntimeCommandNotAcceptedDetails_PromptCommandRead:
		cause = PromptCommandErrorFromProto(selected.PromptCommandRead)
	default:
		return fmt.Errorf("runtime command rejection cause is required")
	}
	return serverapi.NewRuntimeCommandNotAcceptedError(cause)
}
