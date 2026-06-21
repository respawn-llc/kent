package runtime

import (
	"core/server/workflowruntime"
)

// workflowOutputCompletion is the decode-and-classify decision for a model FINAL answer in an
// output-completion workflow mode (structured or unstructured). It is the structured-output
// completion adapter of the objective/continuation seam: it judges only whether a final message is
// a valid workflow completion. Side effects (completing the run, setting terminal state, the active
// goal cascade) and continuation-nudge emission stay with the caller, so the adapter is a pure,
// independently testable decision.
type workflowOutputCompletion struct {
	Parsed  workflowruntime.ParsedCompletion
	Source  WorkflowCompletionSource
	Invalid error
}

// evaluateWorkflowOutputCompletion decodes an already-trimmed final answer for an output-completion
// mode and returns the completion decision. ok is false when the mode does not complete via model
// output (tool and shell-command modes complete through their own channels), letting the caller
// fall through to those paths unchanged.
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
