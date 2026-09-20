package transport

import (
	"errors"
	"fmt"
	"testing"

	"core/shared/protocol"
	"core/shared/serverapi"
)

func TestWorkflowContextSelectionErrorRetainsStructuredTransportData(t *testing.T) {
	source := &serverapi.WorkflowTaskContextSelectionRequiredError{TaskID: "task-restricted"}
	response := responseForError("request", fmt.Errorf("resume failed: %w", source))
	if response.Error == nil || response.Error.Code != protocol.ErrCodeWorkflowTaskContextSelectionRequired {
		t.Fatalf("lost structured error classification: %+v", response)
	}
	decoded := serverapi.DecodeWorkflowTaskContextSelectionRequiredError(response.Error.Data, response.Error.Message)
	var restriction *serverapi.WorkflowTaskContextSelectionRequiredError
	if !errors.As(decoded, &restriction) || restriction.TaskID != source.TaskID {
		t.Fatalf("lost structured error data: %v", decoded)
	}
}
