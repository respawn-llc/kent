package runtime

import (
	"core/server/llm"
)

func (e *Engine) assertWorkflowInstructionsPresent(stepNo int) {
	if stepNo <= 1 {
		return
	}
	if !e.isWorkflowAgent() {
		return
	}
	for _, item := range e.transcriptRuntimeState().SnapshotItems() {
		if item.Type == llm.ResponseItemTypeMessage &&
			item.MessageType != nil &&
			*item.MessageType == llm.MessageTypeWorkflowMode {
			return
		}
	}
	panic("workflow mode skipped prompt")
}

func (e *Engine) isWorkflowAgent() bool {
	return e != nil && e.currentNodeExecutionActive()
}
