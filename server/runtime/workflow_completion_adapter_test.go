package runtime

import (
	"testing"

	"core/server/workflowruntime"
	"core/shared/config"
)

func TestEvaluateWorkflowOutputCompletion(t *testing.T) {
	contract := testWorkflowConfig(&fakeWorkflowController{}, config.WorkflowCompletionModeUnstructured).Contract

	eval, ok := evaluateWorkflowOutputCompletion(workflowruntime.CompletionModeStructuredOutput, contract, `{"commentary":"complete","summary":"done"}`)
	if !ok || eval.Invalid != nil || eval.Source != WorkflowCompletionSourceStructuredOutput {
		t.Fatalf("structured valid = %+v ok=%v, want valid structured-output completion", eval, ok)
	}
	if eval.Parsed.OutputValues["summary"] != "done" {
		t.Fatalf("structured parsed summary = %q, want done", eval.Parsed.OutputValues["summary"])
	}

	eval, ok = evaluateWorkflowOutputCompletion(workflowruntime.CompletionModeStructuredOutput, contract, `{"summary":""}`)
	if !ok || eval.Invalid == nil {
		t.Fatalf("structured invalid = %+v ok=%v, want an Invalid decode error", eval, ok)
	}

	eval, ok = evaluateWorkflowOutputCompletion(workflowruntime.CompletionModeUnstructuredOutput, contract, `{"commentary":"complete","summary":"done"}`)
	if !ok || eval.Invalid != nil || eval.Source != WorkflowCompletionSourceUnstructured {
		t.Fatalf("unstructured valid = %+v ok=%v, want valid unstructured completion", eval, ok)
	}

	for _, mode := range []workflowruntime.CompletionMode{workflowruntime.CompletionModeTool, workflowruntime.CompletionModeShellCommand} {
		if _, ok := evaluateWorkflowOutputCompletion(mode, contract, "anything"); ok {
			t.Fatalf("mode %s reported as output-completion, want not applicable", mode)
		}
	}
}
