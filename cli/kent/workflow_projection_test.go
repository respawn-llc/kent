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
					Message: "server-provided message",
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
			if message == "server-provided message" {
				t.Fatalf("message = %q, want client-formatted diagnostic", message)
			}
			var stdout bytes.Buffer
			writeWorkflowValidationError(&stdout, projected.Errors[0])
			if !slices.Contains(strings.Split(stdout.String(), "\n"), "  placeholder: "+placeholder) {
				t.Fatalf("validation output = %q, want placeholder detail", stdout.String())
			}
		})
	}
}

func TestWorkflowGraphApplyHumanOutputFormatsSessionReferenceReasons(t *testing.T) {
	const placeholder = ".Params.review.session_id"
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
						Message: "server-provided message",
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
	if slices.Contains(strings.Split(output, "\n"), "  - ["+string(workflow.CodeInvalidTemplatePlaceholder)+"] server-provided message") {
		t.Fatalf("output = %q, want client-formatted diagnostic", output)
	}
	if !slices.Contains(strings.Split(output, "\n"), "    placeholder: "+placeholder) {
		t.Fatalf("output = %q, does not identify placeholder %q", output, placeholder)
	}
}
