package main

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"core/server/workflow"
	workflowdefinitionpb "core/shared/protoapi/gen/kent/api/workflow_definition"
	"core/shared/serverapi"
)

func TestWorkflowValidationForCLIFormatsSessionReferenceReasons(t *testing.T) {
	placeholder := ".Params.review.session_id"
	const serverMessage = "server-message-sentinel"
	for _, reason := range []serverapi.WorkflowValidationErrorReason{
		workflowdefinitionpb.ValidationErrorReason_VALIDATION_ERROR_REASON_SESSION_SOURCE_CANNOT_OWN_SESSION,
		workflowdefinitionpb.ValidationErrorReason_VALIDATION_ERROR_REASON_SESSION_TRANSITION_MISSING,
		workflowdefinitionpb.ValidationErrorReason_VALIDATION_ERROR_REASON_SESSION_TRANSITION_NOT_GUARANTEED,
		workflowdefinitionpb.ValidationErrorReason_VALIDATION_ERROR_REASON_SESSION_TRANSITION_AMBIGUOUS,
	} {
		t.Run(reason.String(), func(t *testing.T) {
			projected, err := workflowValidationForCLI(serverapi.WorkflowValidateResponse{
				Errors: []serverapi.WorkflowValidationError{{
					Code:    string(workflow.CodeInvalidTemplatePlaceholder),
					Message: serverMessage,
					Details: &serverapi.WorkflowValidationErrorDetails{
						Placeholder: placeholder,
						Reason:      &reason,
					},
				}},
			})
			if err != nil {
				t.Fatalf("project validation errors: %v", err)
			}

			message := projected.Errors[0].Message
			if message == serverMessage {
				t.Fatalf("message = %q, want client-formatted diagnostic", message)
			}
			if projected.Errors[0].Details == nil || projected.Errors[0].Details.Reason == nil ||
				*projected.Errors[0].Details.Reason != reason {
				t.Fatalf("projected validation reason = %+v, want %v", projected.Errors[0].Details, reason)
			}
			var stdout bytes.Buffer
			writeWorkflowValidationError(&stdout, projected.Errors[0])
			if !slices.Contains(strings.Fields(stdout.String()), placeholder) {
				t.Fatalf("validation output = %q, want placeholder detail", stdout.String())
			}
		})
	}
}

func TestWorkflowGraphApplyHumanOutputFormatsSessionReferenceReasons(t *testing.T) {
	const placeholder = ".Params.review.session_id"
	const serverMessage = "server-message-sentinel"
	reason := serverapi.WorkflowValidationErrorReason(
		workflowdefinitionpb.ValidationErrorReason_VALIDATION_ERROR_REASON_SESSION_TRANSITION_MISSING,
	)
	var stderr bytes.Buffer
	err := writeWorkflowGraphApplyHumanOutcome(
		&bytes.Buffer{},
		&stderr,
		workflowGraphApplyOutcome{
			Outcome: workflowGraphApplyBlocked,
			Blockers: []serverapi.WorkflowGraphSaveBlocker{{
				Code:    "validation_failed",
				Message: "validation failed",
			}},
			ValidationResults: map[serverapi.WorkflowValidationMode]serverapi.WorkflowValidateResponse{
				serverapi.WorkflowValidationModeExecution: {
					Errors: []serverapi.WorkflowValidationError{{
						Code:    string(workflow.CodeInvalidTemplatePlaceholder),
						Message: serverMessage,
						Details: &serverapi.WorkflowValidationErrorDetails{
							Placeholder: placeholder,
							Reason:      &reason,
						},
					}},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("write graph apply output: %v", err)
	}
	output := stderr.String()
	if slices.Contains(strings.Fields(output), serverMessage) {
		t.Fatalf("output = %q, want client-formatted diagnostic", output)
	}
	if !slices.Contains(strings.Fields(output), placeholder) {
		t.Fatalf("output = %q, does not identify placeholder %q", output, placeholder)
	}
}
