package serverapi

import (
	"errors"
	"testing"

	"core/shared/protocol"
)

func TestWorkflowContextSelectionRestrictionRoundTrip(t *testing.T) {
	source := &WorkflowTaskContextSelectionRequiredError{TaskID: "task-restricted"}
	if source.RPCErrorCode() != protocol.ErrCodeWorkflowTaskContextSelectionRequired {
		t.Fatal("incorrect error classification")
	}
	decoded := DecodeWorkflowTaskContextSelectionRequiredError(source.RPCErrorData(), source.Error())
	var restriction *WorkflowTaskContextSelectionRequiredError
	if !errors.As(decoded, &restriction) || restriction.TaskID != source.TaskID {
		t.Fatalf("lost restriction: %v", decoded)
	}
}
