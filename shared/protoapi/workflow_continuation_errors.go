package protoapi

import (
	"errors"
	"fmt"
	"strings"

	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/serverapi"
)

func WorkflowContinuationRejectionToProto(
	rejection *serverapi.WorkflowContinuationRejectionError,
) (*sharedpb.WorkflowContinuationRejectionDetails, error) {
	if rejection == nil {
		return nil, errors.New("workflow continuation rejection is required")
	}
	if strings.TrimSpace(rejection.TaskID) == "" {
		return nil, errors.New("workflow continuation rejection Task id is required")
	}
	detail := &sharedpb.WorkflowContinuationRejectionDetails{TaskId: rejection.TaskID}
	switch rejection.Reason {
	case serverapi.WorkflowContinuationWaitingForApproval:
		detail.Reason = sharedpb.WorkflowContinuationRejectionReason_WORKFLOW_CONTINUATION_REJECTION_REASON_WAITING_FOR_TRANSITION_APPROVAL
	case serverapi.WorkflowContinuationNotCurrentNode:
		detail.Reason = sharedpb.WorkflowContinuationRejectionReason_WORKFLOW_CONTINUATION_REJECTION_REASON_NOT_CURRENT_WORKFLOW_NODE
	default:
		return nil, fmt.Errorf("workflow continuation rejection reason %q is invalid", rejection.Reason)
	}
	return detail, nil
}

func WorkflowContinuationRejectionFromProto(
	detail *sharedpb.WorkflowContinuationRejectionDetails,
) error {
	if err := Validate(detail); err != nil {
		return err
	}
	rejection := &serverapi.WorkflowContinuationRejectionError{TaskID: detail.TaskId}
	switch detail.Reason {
	case sharedpb.WorkflowContinuationRejectionReason_WORKFLOW_CONTINUATION_REJECTION_REASON_WAITING_FOR_TRANSITION_APPROVAL:
		rejection.Reason = serverapi.WorkflowContinuationWaitingForApproval
	case sharedpb.WorkflowContinuationRejectionReason_WORKFLOW_CONTINUATION_REJECTION_REASON_NOT_CURRENT_WORKFLOW_NODE:
		rejection.Reason = serverapi.WorkflowContinuationNotCurrentNode
	default:
		return fmt.Errorf("workflow continuation rejection reason %q is invalid", detail.Reason)
	}
	return rejection
}
