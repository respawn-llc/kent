package runtime

import (
	"errors"

	"core/server/llm"
	"core/shared/invariant"
)

var errWorkflowInstructionsMissing = errors.New("workflow instructions are missing")

func (e *Engine) validateWorkflowInstructions() error {
	if !e.isWorkflowAgent() {
		return nil
	}
	for _, item := range e.transcriptRuntimeState().SnapshotItems() {
		if item.Type == llm.ResponseItemTypeMessage &&
			item.MessageType != nil &&
			*item.MessageType == llm.MessageTypeWorkflowMode {
			return nil
		}
	}
	return invariant.OperationalError(
		errWorkflowInstructionsMissing,
		e.cfg.Debug,
		"validate_workflow_instructions",
		errors.New("active Workflow has no workflow-mode context item"),
	)
}

func (e *Engine) isWorkflowAgent() bool {
	return e != nil && e.currentNodeExecutionActive()
}
