package main

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"core/server/workflow"
	"core/shared/serverapi"
)

func TestWorkflowValidationForCLIFormatsSessionReferenceCodes(t *testing.T) {
	placeholder := ".Params.review.session_id"
	const serverMessage = "server-message-sentinel"
	for _, code := range []workflow.ValidationErrorCode{
		workflow.CodeSessionSourceCannotOwnSession,
		workflow.CodeSessionTransitionMissing,
		workflow.CodeSessionTransitionNotGuaranteed,
		workflow.CodeSessionTransitionAmbiguous,
	} {
		t.Run(string(code), func(t *testing.T) {
			projected := workflowValidationForCLI(serverapi.WorkflowValidateResponse{
				Errors: []serverapi.WorkflowValidationError{{
					Code:    string(code),
					Message: serverMessage,
					Details: &serverapi.WorkflowValidationErrorDetails{
						Placeholder: placeholder,
					},
				}},
			})

			message := projected.Errors[0].Message
			if message == serverMessage {
				t.Fatalf("message = %q, want client-formatted diagnostic", message)
			}
			if projected.Errors[0].Code != string(code) ||
				projected.Errors[0].Details == nil ||
				projected.Errors[0].Details.Placeholder != placeholder {
				t.Fatalf("projected validation error = %+v, want code and placeholder details", projected.Errors[0])
			}
			var stdout bytes.Buffer
			writeWorkflowValidationError(&stdout, projected.Errors[0])
			if !slices.Contains(strings.Fields(stdout.String()), placeholder) {
				t.Fatalf("validation output = %q, want placeholder detail", stdout.String())
			}
		})
	}
}

func TestWorkflowGraphApplyHumanOutputFormatsSessionReferenceCodes(t *testing.T) {
	const placeholder = ".Params.review.session_id"
	const serverMessage = "server-message-sentinel"
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
						Code:    string(workflow.CodeSessionTransitionMissing),
						Message: serverMessage,
						Details: &serverapi.WorkflowValidationErrorDetails{
							Placeholder: placeholder,
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
