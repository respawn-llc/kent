package main

import (
	"bytes"
	"strings"
	"testing"

	"core/server/workflow"
	"core/shared/serverapi"
	"core/shared/workflowcontract"
)

func TestWorkflowValidationForCLIFormatsSessionReferenceReasons(t *testing.T) {
	placeholder := ".Params.review.session_id"
	for _, reason := range []serverapi.WorkflowValidationErrorReason{
		workflowcontract.ValidationErrorReasonSessionSourceCannotOwnSession,
		workflowcontract.ValidationErrorReasonSessionTransitionMissing,
		workflowcontract.ValidationErrorReasonSessionTransitionNotGuaranteed,
		workflowcontract.ValidationErrorReasonSessionTransitionAmbiguous,
	} {
		t.Run(string(reason), func(t *testing.T) {
			projected, err := workflowValidationForCLI(serverapi.WorkflowValidateResponse{
				Errors: []serverapi.WorkflowValidationError{{
					Code:    string(workflow.CodeInvalidTemplatePlaceholder),
					Message: string(reason),
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
			if message == string(reason) || strings.Contains(message, string(reason)) {
				t.Fatalf("message = %q, contains internal reason %q", message, reason)
			}
			if !strings.Contains(message, placeholder) {
				t.Fatalf("message = %q, does not identify placeholder %q", message, placeholder)
			}
		})
	}
}

func TestWorkflowGraphApplyHumanOutputFormatsSessionReferenceReasons(t *testing.T) {
	const placeholder = ".Params.review.session_id"
	reason := serverapi.WorkflowValidationErrorReason(workflowcontract.ValidationErrorReasonSessionTransitionMissing)
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
						Message: string(reason),
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
	if strings.Contains(output, string(reason)) {
		t.Fatalf("output = %q, contains internal reason %q", output, reason)
	}
	if !strings.Contains(output, placeholder) {
		t.Fatalf("output = %q, does not identify placeholder %q", output, placeholder)
	}
}
