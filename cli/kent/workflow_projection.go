package main

import (
	"errors"
	"fmt"

	workflowdefinitionpb "core/shared/protoapi/gen/kent/api/workflow_definition"
	"core/shared/serverapi"
)

func workflowRecordForCLI(record serverapi.WorkflowRecord) (serverapi.WorkflowRecord, error) {
	if record.ID.IsZero() {
		return serverapi.WorkflowRecord{}, errors.New("workflow_id is required")
	}
	return record, nil
}

func workflowRecordsForCLI(records []serverapi.WorkflowRecord) ([]serverapi.WorkflowRecord, error) {
	projected := make([]serverapi.WorkflowRecord, len(records))
	for i := range records {
		record, err := workflowRecordForCLI(records[i])
		if err != nil {
			return nil, err
		}
		projected[i] = record
	}
	return projected, nil
}

func workflowDeleteResponseForCLI(response serverapi.WorkflowDeleteResponse) (serverapi.WorkflowDeleteResponse, error) {
	if response.Impact.WorkflowID.IsZero() {
		return serverapi.WorkflowDeleteResponse{}, errors.New("workflow_id is required")
	}
	return response, nil
}

func workflowTaskSummaryForCLI(summary serverapi.WorkflowTaskSummary) (serverapi.WorkflowTaskSummary, error) {
	if summary.WorkflowID.IsZero() {
		return serverapi.WorkflowTaskSummary{}, errors.New("workflow_id is required")
	}
	return summary, nil
}

func workflowDefinitionForCLI(definition serverapi.WorkflowDefinition) (serverapi.WorkflowDefinition, error) {
	workflow, err := workflowRecordForCLI(definition.Workflow)
	if err != nil {
		return serverapi.WorkflowDefinition{}, err
	}
	projected := definition
	projected.Workflow = workflow
	projected.DerivedWiring.Diagnostics, err = workflowValidationErrorsForCLI(definition.DerivedWiring.Diagnostics)
	if err != nil {
		return serverapi.WorkflowDefinition{}, err
	}
	return projected, nil
}

func projectWorkflowLinkForCLI(link serverapi.ProjectWorkflowLink) (serverapi.ProjectWorkflowLink, error) {
	if link.WorkflowID.IsZero() {
		return serverapi.ProjectWorkflowLink{}, errors.New("workflow_id is required")
	}
	return link, nil
}

func workflowValidationForCLI(response serverapi.WorkflowValidateResponse) (serverapi.WorkflowValidateResponse, error) {
	projected := response
	errors, err := workflowValidationErrorsForCLI(response.Errors)
	if err != nil {
		return serverapi.WorkflowValidateResponse{}, err
	}
	projected.Errors = errors
	return projected, nil
}

func workflowValidationErrorsForCLI(errors []serverapi.WorkflowValidationError) ([]serverapi.WorkflowValidationError, error) {
	projected := append([]serverapi.WorkflowValidationError(nil), errors...)
	for i := range projected {
		message, err := workflowValidationErrorMessageForCLI(projected[i])
		if err != nil {
			return nil, err
		}
		projected[i].Message = message
	}
	return projected, nil
}

func workflowValidationErrorMessageForCLI(err serverapi.WorkflowValidationError) (string, error) {
	if err.Details == nil || err.Details.Reason == nil {
		return err.Message, nil
	}
	var message string
	switch *err.Details.Reason {
	case workflowdefinitionpb.ValidationErrorReason_VALIDATION_ERROR_REASON_SESSION_SOURCE_CANNOT_OWN_SESSION:
		message = "This prompt references a source node that cannot own a Session. Use an agent source node for this placeholder."
	case workflowdefinitionpb.ValidationErrorReason_VALIDATION_ERROR_REASON_SESSION_TRANSITION_MISSING:
		message = "This prompt references an unknown transition. Correct the transition key or define the transition before using this placeholder."
	case workflowdefinitionpb.ValidationErrorReason_VALIDATION_ERROR_REASON_SESSION_TRANSITION_NOT_GUARANTEED:
		message = "This prompt references a transition that is not guaranteed to run before the prompt. Reference a transition that runs on every incoming path."
	case workflowdefinitionpb.ValidationErrorReason_VALIDATION_ERROR_REASON_SESSION_TRANSITION_AMBIGUOUS:
		message = "This prompt references more than one matching transition. Make the Session-producing transition unambiguous before using this placeholder."
	default:
		return "", fmt.Errorf("workflow validation reason %q is unsupported", *err.Details.Reason)
	}
	return message, nil
}

func workflowTaskDetailForCLI(detail serverapi.WorkflowTaskDetail) (serverapi.WorkflowTaskDetail, error) {
	projected := detail
	summary, err := workflowTaskSummaryForCLI(detail.Summary)
	if err != nil {
		return serverapi.WorkflowTaskDetail{}, err
	}
	projected.Summary = summary
	return projected, nil
}
