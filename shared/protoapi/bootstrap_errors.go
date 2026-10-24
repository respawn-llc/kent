package protoapi

import (
	"context"
	"errors"
	"fmt"

	onboardingpb "core/shared/protoapi/gen/kent/api/onboarding"
	serverpb "core/shared/protoapi/gen/kent/api/server"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/serverapi"
)

func ServerNotReadyToProto(err *serverapi.ServerNotReadyError) (*serverpb.ServerNotReadyDetails, error) {
	if err == nil {
		return nil, errors.New("server-not-ready error is required")
	}
	reason, conversionErr := serverNotReadyReasonToProto(err.Reason)
	if conversionErr != nil {
		return nil, conversionErr
	}
	details := &serverpb.ServerNotReadyDetails{Reason: reason}
	switch value := err.Details.(type) {
	case nil:
	case serverapi.ServerNotReadyDetails:
		details.OnboardingCompleted = value.OnboardingCompleted
		details.SettingsPath = cloneBootstrapErrorPointer(value.SettingsPath)
		details.Diagnostic = cloneBootstrapErrorPointer(value.Diagnostic)
	default:
		return nil, fmt.Errorf("server-not-ready details type %T is unsupported", err.Details)
	}
	return details, nil
}

func ServerNotReadyFromProto(details *serverpb.ServerNotReadyDetails) error {
	reason := serverNotReadyReasonValues[details.Reason]
	return serverapi.NewServerNotReadyError(reason, serverapi.ServerNotReadyDetails{
		OnboardingCompleted: details.OnboardingCompleted,
		SettingsPath:        cloneBootstrapErrorPointer(details.SettingsPath),
		Diagnostic:          cloneBootstrapErrorPointer(details.Diagnostic),
	}, nil)
}

func OnboardingFinalizeErrorToProto(err *serverapi.OnboardingFinalizeError) (*onboardingpb.FinalizeError, error) {
	if err == nil {
		return nil, errors.New("onboarding finalize error is required")
	}
	result := &onboardingpb.FinalizeError{Code: string(err.Code)}
	switch err.Code {
	case serverapi.OnboardingFinalizeInvalidRequest:
		details, ok := err.Details.(serverapi.OnboardingInvalidRequestDetails)
		if !ok {
			return nil, onboardingDetailsTypeError(err, details)
		}
		result.Detail = &onboardingpb.FinalizeError_InvalidRequest{InvalidRequest: invalidRequestDetailsToProto(details)}
	case serverapi.OnboardingFinalizeConfigAlreadyExists:
		details, ok := err.Details.(serverapi.OnboardingConfigAlreadyExistsDetails)
		if !ok {
			return nil, onboardingDetailsTypeError(err, details)
		}
		result.Detail = &onboardingpb.FinalizeError_ConfigAlreadyExists{
			ConfigAlreadyExists: &onboardingpb.ConfigAlreadyExistsDetails{SettingsPath: details.SettingsPath},
		}
	case serverapi.OnboardingFinalizeImportUnavailable:
		details, ok := err.Details.(serverapi.OnboardingImportUnavailableDetails)
		if !ok {
			return nil, onboardingDetailsTypeError(err, details)
		}
		converted, conversionErr := importUnavailableDetailsToProto(details)
		if conversionErr != nil {
			return nil, conversionErr
		}
		result.Detail = &onboardingpb.FinalizeError_ImportUnavailable{ImportUnavailable: converted}
	case serverapi.OnboardingFinalizeImportFailed:
		details, ok := err.Details.(serverapi.OnboardingImportFailedDetails)
		if !ok {
			return nil, onboardingDetailsTypeError(err, details)
		}
		converted, conversionErr := importFailedDetailsToProto(details)
		if conversionErr != nil {
			return nil, conversionErr
		}
		result.Detail = &onboardingpb.FinalizeError_ImportFailed{ImportFailed: converted}
	case serverapi.OnboardingFinalizeConfigWriteFailed:
		details, ok := err.Details.(serverapi.OnboardingConfigWriteFailedDetails)
		if !ok {
			return nil, onboardingDetailsTypeError(err, details)
		}
		result.Detail = &onboardingpb.FinalizeError_ConfigWriteFailed{
			ConfigWriteFailed: &onboardingpb.ConfigWriteFailedDetails{
				SettingsPath: details.SettingsPath,
				Operation:    details.Operation,
				Cause:        details.Cause,
			},
		}
	case serverapi.OnboardingFinalizeRollbackFailed:
		details, ok := err.Details.(serverapi.OnboardingRollbackFailedDetails)
		if !ok {
			return nil, onboardingDetailsTypeError(err, details)
		}
		converted, conversionErr := rollbackFailedDetailsToProto(details)
		if conversionErr != nil {
			return nil, conversionErr
		}
		result.Detail = &onboardingpb.FinalizeError_RollbackFailed{RollbackFailed: converted}
	case serverapi.OnboardingFinalizeCanceled:
		details, ok := err.Details.(serverapi.OnboardingCanceledDetails)
		if !ok {
			return nil, onboardingDetailsTypeError(err, details)
		}
		phase, conversionErr := cancelPhaseToProto(details.Phase)
		if conversionErr != nil {
			return nil, conversionErr
		}
		result.Detail = &onboardingpb.FinalizeError_Canceled{
			Canceled: &onboardingpb.CanceledDetails{Phase: phase},
		}
	default:
		return nil, fmt.Errorf("onboarding finalize error code %q is unsupported", err.Code)
	}
	return result, nil
}

func OnboardingFinalizeErrorFromProto(message *onboardingpb.FinalizeError) error {
	code := serverapi.OnboardingFinalizeErrorCode(message.Code)
	var details any
	switch detail := message.Detail.(type) {
	case *onboardingpb.FinalizeError_InvalidRequest:
		details = invalidRequestDetailsFromProto(detail.InvalidRequest)
	case *onboardingpb.FinalizeError_ConfigAlreadyExists:
		details = serverapi.OnboardingConfigAlreadyExistsDetails{SettingsPath: detail.ConfigAlreadyExists.SettingsPath}
	case *onboardingpb.FinalizeError_ImportUnavailable:
		converted, err := importUnavailableDetailsFromProto(detail.ImportUnavailable)
		if err != nil {
			return err
		}
		details = converted
	case *onboardingpb.FinalizeError_ImportFailed:
		converted, err := importFailedDetailsFromProto(detail.ImportFailed)
		if err != nil {
			return err
		}
		details = converted
	case *onboardingpb.FinalizeError_ConfigWriteFailed:
		details = serverapi.OnboardingConfigWriteFailedDetails{
			SettingsPath: detail.ConfigWriteFailed.SettingsPath,
			Operation:    detail.ConfigWriteFailed.Operation,
			Cause:        detail.ConfigWriteFailed.Cause,
		}
	case *onboardingpb.FinalizeError_RollbackFailed:
		converted, err := rollbackFailedDetailsFromProto(detail.RollbackFailed)
		if err != nil {
			return err
		}
		details = converted
	case *onboardingpb.FinalizeError_Canceled:
		phase := cancelPhaseValues[detail.Canceled.Phase]
		details = serverapi.OnboardingCanceledDetails{Phase: phase}
	default:
		return fmt.Errorf("onboarding finalize error detail %T is unsupported", detail)
	}
	typed := serverapi.NewOnboardingFinalizeError(code, details, nil)
	if code == serverapi.OnboardingFinalizeCanceled {
		return errors.Join(typed, context.Canceled)
	}
	return typed
}

func InternalFailureToProto(err error) *sharedpb.InternalFailureDetails {
	cause := err.Error()
	return &sharedpb.InternalFailureDetails{Cause: &cause, SessionTarget: SessionTargetErrorContext(err)}
}

func InternalFailureFromProto(details *sharedpb.InternalFailureDetails) error {
	cause := errors.New("server operation failed")
	if details.Cause != nil {
		cause = errors.New(*details.Cause)
	}
	if details.SessionTarget != nil {
		return SessionTargetContextErrorFromProto(details.SessionTarget, cause)
	}
	return cause
}

func invalidRequestDetailsToProto(details serverapi.OnboardingInvalidRequestDetails) *onboardingpb.InvalidRequestDetails {
	return &onboardingpb.InvalidRequestDetails{
		FieldErrors: mapBootstrapErrorSlice(details.FieldErrors, func(field serverapi.OnboardingFinalizeFieldError) *onboardingpb.FieldError {
			return &onboardingpb.FieldError{Field: field.Field, Code: field.Code}
		}),
	}
}

func invalidRequestDetailsFromProto(details *onboardingpb.InvalidRequestDetails) serverapi.OnboardingInvalidRequestDetails {
	return serverapi.OnboardingInvalidRequestDetails{
		FieldErrors: mapBootstrapErrorSlice(details.FieldErrors, func(field *onboardingpb.FieldError) serverapi.OnboardingFinalizeFieldError {
			return serverapi.OnboardingFinalizeFieldError{Field: field.Field, Code: field.Code}
		}),
	}
}

func importUnavailableDetailsToProto(details serverapi.OnboardingImportUnavailableDetails) (*onboardingpb.ImportUnavailableDetails, error) {
	kind, err := importKindToProto(details.ImportKind)
	if err != nil {
		return nil, err
	}
	mode, err := onboardingImportModeToProto(details.Mode)
	if err != nil {
		return nil, err
	}
	reason, err := importUnavailableReasonToProto(details.ReasonCode)
	if err != nil {
		return nil, err
	}
	return &onboardingpb.ImportUnavailableDetails{
		ImportKind:       kind,
		Mode:             mode,
		ImportProviderId: cloneBootstrapErrorPointer(details.ImportProviderID),
		SourceRootPath:   cloneBootstrapErrorPointer(details.SourceRootPath),
		Reason:           reason,
	}, nil
}

func importUnavailableDetailsFromProto(details *onboardingpb.ImportUnavailableDetails) (serverapi.OnboardingImportUnavailableDetails, error) {
	kind := importKindValues[details.ImportKind]
	mode := onboardingImportModeValues[details.Mode]
	reason := importUnavailableReasonValues[details.Reason]
	return serverapi.OnboardingImportUnavailableDetails{
		ImportKind:       kind,
		Mode:             mode,
		ImportProviderID: cloneBootstrapErrorPointer(details.ImportProviderId),
		SourceRootPath:   cloneBootstrapErrorPointer(details.SourceRootPath),
		ReasonCode:       reason,
	}, nil
}

func importFailedDetailsToProto(details serverapi.OnboardingImportFailedDetails) (*onboardingpb.ImportFailedDetails, error) {
	var kind *onboardingpb.ImportKind
	if details.ImportKind != "" {
		value, err := importKindToProto(details.ImportKind)
		if err != nil {
			return nil, err
		}
		kind = &value
	}
	operation, err := importOperationToProto(details.Operation)
	if err != nil {
		return nil, err
	}
	return &onboardingpb.ImportFailedDetails{
		ImportKind:       kind,
		ImportProviderId: cloneBootstrapErrorPointer(details.ImportProviderID),
		SourceRootPath:   cloneBootstrapErrorPointer(details.SourceRootPath),
		Operation:        operation,
		Cause:            details.Cause,
	}, nil
}

func importFailedDetailsFromProto(details *onboardingpb.ImportFailedDetails) (serverapi.OnboardingImportFailedDetails, error) {
	var kind serverapi.OnboardingImportKind
	if details.ImportKind != nil {
		kind = importKindValues[*details.ImportKind]
	}
	operation := importOperationValues[details.Operation]
	return serverapi.OnboardingImportFailedDetails{
		ImportKind:       kind,
		ImportProviderID: cloneBootstrapErrorPointer(details.ImportProviderId),
		SourceRootPath:   cloneBootstrapErrorPointer(details.SourceRootPath),
		Operation:        operation,
		Cause:            details.Cause,
	}, nil
}

func rollbackFailedDetailsToProto(details serverapi.OnboardingRollbackFailedDetails) (*onboardingpb.RollbackFailedDetails, error) {
	primary, err := rollbackPrimaryToProto(details.Primary)
	if err != nil {
		return nil, err
	}
	return &onboardingpb.RollbackFailedDetails{
		Primary: primary,
		Rollback: &onboardingpb.RollbackFailureFact{
			Operation: details.Rollback.Operation,
			Cause:     details.Rollback.Cause,
		},
	}, nil
}

func rollbackPrimaryToProto(primary serverapi.OnboardingRollbackPrimaryFailure) (*onboardingpb.RollbackPrimaryFailure, error) {
	message := &onboardingpb.RollbackPrimaryFailure{}
	switch {
	case primary.InvalidRequest != nil:
		message.Failure = &onboardingpb.RollbackPrimaryFailure_InvalidRequest{InvalidRequest: invalidRequestDetailsToProto(*primary.InvalidRequest)}
	case primary.ConfigAlreadyExists != nil:
		message.Failure = &onboardingpb.RollbackPrimaryFailure_ConfigAlreadyExists{
			ConfigAlreadyExists: &onboardingpb.ConfigAlreadyExistsDetails{SettingsPath: primary.ConfigAlreadyExists.SettingsPath},
		}
	case primary.ImportUnavailable != nil:
		details, err := importUnavailableDetailsToProto(*primary.ImportUnavailable)
		if err != nil {
			return nil, err
		}
		message.Failure = &onboardingpb.RollbackPrimaryFailure_ImportUnavailable{ImportUnavailable: details}
	case primary.ImportFailed != nil:
		details, err := importFailedDetailsToProto(*primary.ImportFailed)
		if err != nil {
			return nil, err
		}
		message.Failure = &onboardingpb.RollbackPrimaryFailure_ImportFailed{ImportFailed: details}
	case primary.ConfigWriteFailed != nil:
		message.Failure = &onboardingpb.RollbackPrimaryFailure_ConfigWriteFailed{
			ConfigWriteFailed: &onboardingpb.ConfigWriteFailedDetails{
				SettingsPath: primary.ConfigWriteFailed.SettingsPath,
				Operation:    primary.ConfigWriteFailed.Operation,
				Cause:        primary.ConfigWriteFailed.Cause,
			},
		}
	case primary.Canceled != nil:
		phase, err := cancelPhaseToProto(primary.Canceled.Phase)
		if err != nil {
			return nil, err
		}
		message.Failure = &onboardingpb.RollbackPrimaryFailure_Canceled{
			Canceled: &onboardingpb.CanceledDetails{Phase: phase},
		}
	case primary.InternalFailure != nil:
		message.Failure = &onboardingpb.RollbackPrimaryFailure_InternalFailure{
			InternalFailure: &sharedpb.InternalFailureDetails{Cause: cloneBootstrapErrorPointer(primary.InternalFailure.Cause)},
		}
	default:
		return nil, errors.New("onboarding rollback primary failure is required")
	}
	return message, nil
}

func rollbackFailedDetailsFromProto(details *onboardingpb.RollbackFailedDetails) (serverapi.OnboardingRollbackFailedDetails, error) {
	primary, err := rollbackPrimaryFromProto(details.Primary)
	if err != nil {
		return serverapi.OnboardingRollbackFailedDetails{}, err
	}
	return serverapi.OnboardingRollbackFailedDetails{
		Primary: primary,
		Rollback: serverapi.OnboardingRollbackFailureFact{
			Operation: details.Rollback.Operation,
			Cause:     details.Rollback.Cause,
		},
	}, nil
}

func rollbackPrimaryFromProto(message *onboardingpb.RollbackPrimaryFailure) (serverapi.OnboardingRollbackPrimaryFailure, error) {
	switch failure := message.Failure.(type) {
	case *onboardingpb.RollbackPrimaryFailure_InvalidRequest:
		details := invalidRequestDetailsFromProto(failure.InvalidRequest)
		return serverapi.OnboardingRollbackPrimaryFailure{InvalidRequest: &details}, nil
	case *onboardingpb.RollbackPrimaryFailure_ConfigAlreadyExists:
		details := serverapi.OnboardingConfigAlreadyExistsDetails{SettingsPath: failure.ConfigAlreadyExists.SettingsPath}
		return serverapi.OnboardingRollbackPrimaryFailure{ConfigAlreadyExists: &details}, nil
	case *onboardingpb.RollbackPrimaryFailure_ImportUnavailable:
		details, err := importUnavailableDetailsFromProto(failure.ImportUnavailable)
		return serverapi.OnboardingRollbackPrimaryFailure{ImportUnavailable: &details}, err
	case *onboardingpb.RollbackPrimaryFailure_ImportFailed:
		details, err := importFailedDetailsFromProto(failure.ImportFailed)
		return serverapi.OnboardingRollbackPrimaryFailure{ImportFailed: &details}, err
	case *onboardingpb.RollbackPrimaryFailure_ConfigWriteFailed:
		details := serverapi.OnboardingConfigWriteFailedDetails{
			SettingsPath: failure.ConfigWriteFailed.SettingsPath,
			Operation:    failure.ConfigWriteFailed.Operation,
			Cause:        failure.ConfigWriteFailed.Cause,
		}
		return serverapi.OnboardingRollbackPrimaryFailure{ConfigWriteFailed: &details}, nil
	case *onboardingpb.RollbackPrimaryFailure_Canceled:
		phase := cancelPhaseValues[failure.Canceled.Phase]
		details := serverapi.OnboardingCanceledDetails{Phase: phase}
		return serverapi.OnboardingRollbackPrimaryFailure{Canceled: &details}, nil
	case *onboardingpb.RollbackPrimaryFailure_InternalFailure:
		details := serverapi.OnboardingInternalFailureDetails{Cause: cloneBootstrapErrorPointer(failure.InternalFailure.Cause)}
		return serverapi.OnboardingRollbackPrimaryFailure{InternalFailure: &details}, nil
	default:
		return serverapi.OnboardingRollbackPrimaryFailure{}, fmt.Errorf("onboarding rollback primary failure %T is unsupported", failure)
	}
}

func onboardingDetailsTypeError(err *serverapi.OnboardingFinalizeError, details any) error {
	return fmt.Errorf("onboarding finalize error %q has unsupported details type %T", err.Code, details)
}

func serverNotReadyReasonToProto(reason serverapi.ServerNotReadyReason) (serverpb.ServerNotReadyReason, error) {
	return enumToProto(serverNotReadyReasonValues, reason, "server-not-ready reason")
}

var serverNotReadyReasonValues = map[serverpb.ServerNotReadyReason]serverapi.ServerNotReadyReason{
	serverpb.ServerNotReadyReason_SERVER_NOT_READY_REASON_ONBOARDING_REQUIRED: serverapi.ServerNotReadyOnboardingRequired,
	serverpb.ServerNotReadyReason_SERVER_NOT_READY_REASON_ACTIVATION_FAILED:   serverapi.ServerNotReadyActivationFailed,
}

func importKindToProto(kind serverapi.OnboardingImportKind) (onboardingpb.ImportKind, error) {
	return enumToProto(importKindValues, kind, "onboarding import kind")
}

var importKindValues = map[onboardingpb.ImportKind]serverapi.OnboardingImportKind{
	onboardingpb.ImportKind_IMPORT_KIND_SKILLS:   serverapi.OnboardingImportKindSkills,
	onboardingpb.ImportKind_IMPORT_KIND_COMMANDS: serverapi.OnboardingImportKindCommands,
}

func onboardingImportModeToProto(mode serverapi.OnboardingImportMode) (onboardingpb.ImportMode, error) {
	return enumToProto(onboardingImportModeValues, mode, "onboarding import mode")
}

var onboardingImportModeValues = map[onboardingpb.ImportMode]serverapi.OnboardingImportMode{
	onboardingpb.ImportMode_IMPORT_MODE_NONE:           serverapi.OnboardingImportModeNone,
	onboardingpb.ImportMode_IMPORT_MODE_SYMLINK_SOURCE: serverapi.OnboardingImportModeSymlinkSource,
}

func importUnavailableReasonToProto(reason serverapi.OnboardingImportUnavailableReason) (onboardingpb.ImportUnavailableReason, error) {
	return enumToProto(importUnavailableReasonValues, reason, "onboarding import-unavailable reason")
}

var importUnavailableReasonValues = map[onboardingpb.ImportUnavailableReason]serverapi.OnboardingImportUnavailableReason{
	onboardingpb.ImportUnavailableReason_IMPORT_UNAVAILABLE_REASON_NOT_DISCOVERED:       serverapi.OnboardingImportReasonNotDiscovered,
	onboardingpb.ImportUnavailableReason_IMPORT_UNAVAILABLE_REASON_TARGET_EXISTS:        serverapi.OnboardingImportReasonTargetExists,
	onboardingpb.ImportUnavailableReason_IMPORT_UNAVAILABLE_REASON_UNSUPPORTED_PROVIDER: serverapi.OnboardingImportReasonUnsupportedProvider,
}

func importOperationToProto(operation serverapi.OnboardingImportOperation) (onboardingpb.ImportOperation, error) {
	return enumToProto(importOperationValues, operation, "onboarding import operation")
}

var importOperationValues = map[onboardingpb.ImportOperation]serverapi.OnboardingImportOperation{
	onboardingpb.ImportOperation_IMPORT_OPERATION_DISCOVER:       serverapi.OnboardingImportOperationDiscover,
	onboardingpb.ImportOperation_IMPORT_OPERATION_PREPARE_TARGET: serverapi.OnboardingImportOperationPrepareTarget,
	onboardingpb.ImportOperation_IMPORT_OPERATION_CREATE_SYMLINK: serverapi.OnboardingImportOperationCreateSymlink,
}

func cancelPhaseToProto(phase serverapi.OnboardingCancelPhase) (onboardingpb.CancelPhase, error) {
	return enumToProto(cancelPhaseValues, phase, "onboarding cancel phase")
}

var cancelPhaseValues = map[onboardingpb.CancelPhase]serverapi.OnboardingCancelPhase{
	onboardingpb.CancelPhase_CANCEL_PHASE_WAITING_FOR_LOCK:    serverapi.OnboardingCancelWaitingForLock,
	onboardingpb.CancelPhase_CANCEL_PHASE_VALIDATING:          serverapi.OnboardingCancelValidating,
	onboardingpb.CancelPhase_CANCEL_PHASE_DISCOVERING_IMPORTS: serverapi.OnboardingCancelDiscoveringImports,
	onboardingpb.CancelPhase_CANCEL_PHASE_IMPORTING:           serverapi.OnboardingCancelImporting,
}

func cloneBootstrapErrorPointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func mapBootstrapErrorSlice[A, B any](values []A, convert func(A) B) []B {
	if values == nil {
		return nil
	}
	result := make([]B, len(values))
	for index, value := range values {
		result[index] = convert(value)
	}
	return result
}
