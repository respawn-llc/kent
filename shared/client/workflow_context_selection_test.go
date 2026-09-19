package client

import (
	"errors"
	"testing"

	"core/shared/protocol"
	"core/shared/serverapi"
)

func TestProtocolErrorPreservesWorkflowContextSelectionRestriction(t *testing.T) {
	source := &serverapi.WorkflowTaskContextSelectionRequiredError{TaskID: "task-restricted"}
	decoded := protocolError(&protocol.ResponseError{
		Code: source.RPCErrorCode(), Message: source.Error(), Data: source.RPCErrorData(),
	})
	var restriction *serverapi.WorkflowTaskContextSelectionRequiredError
	if !errors.As(decoded, &restriction) || restriction.TaskID != source.TaskID {
		t.Fatalf("restriction lost across transport: %v", decoded)
	}
}
