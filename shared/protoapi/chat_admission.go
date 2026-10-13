package protoapi

import (
	"context"
	"errors"

	chatpb "core/shared/protoapi/gen/kent/api/chat"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/serverapi"
	"google.golang.org/protobuf/types/known/emptypb"
)

type chatAdmissionFailure interface {
	GetCanceled() *emptypb.Empty
	GetRuntimeUnavailable() *chatpb.RuntimeUnavailableDetails
	GetPendingWorkCapacity() *chatpb.PendingWorkCapacityDetails
	GetInternalFailure() *sharedpb.InternalFailureDetails
}

func ChatNotAcceptedFromProto(failure chatAdmissionFailure) error {
	var cause error
	switch {
	case failure.GetCanceled() != nil:
		cause = context.Canceled
	case failure.GetRuntimeUnavailable() != nil:
		cause = serverapi.ErrRuntimeUnavailable
	case failure.GetPendingWorkCapacity() != nil:
		cause = &serverapi.PendingWorkCapacityError{}
	case failure.GetInternalFailure() != nil:
		cause = InternalFailureFromProto(failure.GetInternalFailure())
	default:
		cause = errors.New("Chat rejection reason is required")
		switch failure := failure.(type) {
		case *chatpb.InputNotAccepted:
			switch reason := failure.Reason.(type) {
			case *chatpb.InputNotAccepted_PromptCatalogRead:
				cause = PromptCommandErrorFromProto(reason.PromptCatalogRead)
			case *chatpb.InputNotAccepted_PromptCommandNotFound:
				cause = PromptCommandErrorFromProto(reason.PromptCommandNotFound)
			case *chatpb.InputNotAccepted_PromptCommandRead:
				cause = PromptCommandErrorFromProto(reason.PromptCommandRead)
			}
		case *chatpb.CompactionNotAccepted:
			switch failure.Reason.(type) {
			case *chatpb.CompactionNotAccepted_TooSoon:
				cause = serverapi.ErrManualCompactionTooSoon
			case *chatpb.CompactionNotAccepted_Disabled:
				cause = serverapi.ErrManualCompactionDisabled
			case *chatpb.CompactionNotAccepted_Active:
				cause = serverapi.ErrManualCompactionActive
			}
		}
	}
	return serverapi.NewRuntimeCommandNotAcceptedError(cause)
}

func ChatAcceptedDiagnosticFromProto(diagnostic *chatpb.AcceptedDiagnostic) error {
	if diagnostic == nil {
		return nil
	}
	switch detail := diagnostic.Detail.(type) {
	case *chatpb.AcceptedDiagnostic_InternalFailure:
		return InternalFailureFromProto(detail.InternalFailure)
	case *chatpb.AcceptedDiagnostic_PromptHistoryFailure:
		return InternalFailureFromProto(detail.PromptHistoryFailure)
	default:
		return errors.New("accepted Chat diagnostic detail is required")
	}
}
