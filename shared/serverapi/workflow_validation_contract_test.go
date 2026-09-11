package serverapi

import (
	"testing"

	workflowdefinitionpb "core/shared/protoapi/gen/kent/api/workflow_definition"
	"core/shared/workflowcontract"
)

func TestWorkflowValidationReasonFromDomainReturnsErrorForUnsupportedReason(t *testing.T) {
	t.Setenv("KENT_INVARIANT_MODE", "diagnostic")

	reason, err := WorkflowValidationErrorReasonFromDomain(workflowcontract.ValidationErrorReason("unsupported"))
	if err == nil {
		t.Fatal("unsupported workflow validation reason returned nil error")
	}
	if reason != workflowdefinitionpb.ValidationErrorReason_VALIDATION_ERROR_REASON_UNSPECIFIED {
		t.Fatalf("unsupported workflow validation reason = %v, want unspecified on error", reason)
	}
}

func TestWorkflowValidationReasonFromDomainFailsFastInPanicMode(t *testing.T) {
	t.Setenv("KENT_INVARIANT_MODE", "panic")

	defer func() {
		if recover() == nil {
			t.Fatal("unsupported workflow validation reason did not panic in panic mode")
		}
	}()
	_, _ = WorkflowValidationErrorReasonFromDomain(workflowcontract.ValidationErrorReason("unsupported"))
}
