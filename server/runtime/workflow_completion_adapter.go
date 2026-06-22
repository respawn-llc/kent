package runtime

import (
	"core/server/workflowruntime"
)

type workflowOutputCompletion struct {
	Parsed  workflowruntime.ParsedCompletion
	Source  WorkflowCompletionSource
	Invalid error
}

func evaluateWorkflowOutputCompletion(mode workflowruntime.CompletionMode, contract workflowruntime.CompletionContract, content string) (workflowOutputCompletion, bool) {
	switch mode {
	case workflowruntime.CompletionModeStructuredOutput:
		parsed, err := workflowruntime.DecodeCompletion([]byte(content), contract)
		return workflowOutputCompletion{Parsed: parsed, Source: WorkflowCompletionSourceStructuredOutput, Invalid: err}, true
	case workflowruntime.CompletionModeUnstructuredOutput:
		parsed, err := workflowruntime.DecodeUnstructuredCompletion(content, contract)
		return workflowOutputCompletion{Parsed: parsed, Source: WorkflowCompletionSourceUnstructured, Invalid: err}, true
	default:
		return workflowOutputCompletion{}, false
	}
}
