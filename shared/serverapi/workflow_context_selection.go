package serverapi

import (
	"encoding/json"
	"errors"
	"strings"

	"core/shared/protocol"
)

type WorkflowTaskContextSelectionRequiredError struct {
	TaskID string
}

func (e *WorkflowTaskContextSelectionRequiredError) Error() string {
	return "workflow task context selection required"
}

func (e *WorkflowTaskContextSelectionRequiredError) RPCErrorCode() int {
	return protocol.ErrCodeWorkflowTaskContextSelectionRequired
}

func (e *WorkflowTaskContextSelectionRequiredError) RPCErrorData() json.RawMessage {
	return marshalRPCErrorData(struct {
		Type   string `json:"type"`
		TaskID string `json:"task_id"`
	}{"workflow_task_context_selection_required", e.TaskID})
}

func DecodeWorkflowTaskContextSelectionRequiredError(data json.RawMessage, message string) error {
	var envelope struct {
		Type   string `json:"type"`
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil ||
		envelope.Type != "workflow_task_context_selection_required" ||
		strings.TrimSpace(envelope.TaskID) == "" || strings.TrimSpace(envelope.TaskID) != envelope.TaskID {
		return errors.New(strings.TrimSpace(message))
	}
	return &WorkflowTaskContextSelectionRequiredError{TaskID: envelope.TaskID}
}
