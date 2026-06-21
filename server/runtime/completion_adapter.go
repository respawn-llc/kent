package runtime

import (
	"context"
	"strings"

	"core/server/llm"
)

// CompletionAdapter is the completion policy of a continuation objective. The engine drives two
// kinds of objective to completion — a self-declared goal (across runs) and a workflow node (within
// one run). Their loops differ in lifecycle level and cannot share a body, but both make the same
// post-turn decision: is the objective complete (run its side effect and stop) or not (nudge and
// take another turn). That decision is the unified architecture: each driver consults its
// CompletionAdapter after a model turn. The adapter owns the completion decision and its side
// effect; the driver keeps its own loop level and continuation-nudge emission.
type CompletionAdapter interface {
	// Evaluate decides the objective's state after a model turn produced final (the goal ignores
	// final and reads its out-of-band status; the workflow decodes final as a completion answer).
	Evaluate(ctx context.Context, final llm.Message) (objectiveOutcome, error)
}

// objectiveOutcome is a CompletionAdapter's decision for one turn.
type objectiveOutcome struct {
	// Applicable is false when the adapter has no completion opinion about this turn (e.g. a workflow
	// final answer in a tool/shell completion mode, or a non-final message). The driver then falls
	// through to its own handling.
	Applicable bool
	// Done reports the objective is finished; the driver runs Complete (if set) and stops.
	Done bool
	// Complete runs the completion side effect (e.g. complete the workflow run + set terminal state,
	// which soft-cascades an active goal). Nil when completion has no side effect — a goal completes
	// out-of-band before the driver observes it.
	Complete func(context.Context) error
	// Continue carries the cause to surface in the driver's continuation nudge when Applicable and
	// not Done (the workflow decode error). Nil for objectives whose nudge derives from state.
	Continue error
}

// workflowCompletionAdapter is the output-completion adapter: a workflow node completes when the
// model emits a valid structured/unstructured final answer for the node's completion contract. It
// is the counterpart to goalContinuation (the self-declared adapter).
type workflowCompletionAdapter struct {
	executor *defaultStepExecutor
}

func (s *defaultStepExecutor) workflowCompletionAdapter() workflowCompletionAdapter {
	return workflowCompletionAdapter{executor: s}
}

func (a workflowCompletionAdapter) Evaluate(ctx context.Context, final llm.Message) (objectiveOutcome, error) {
	e := a.executor.engine
	if final.Phase != llm.MessagePhaseFinal {
		return objectiveOutcome{}, nil
	}
	mode, err := e.workflowCompletionMode(ctx)
	if err != nil {
		return objectiveOutcome{}, err
	}
	eval, ok := evaluateWorkflowOutputCompletion(mode, e.cfg.WorkflowRun.Contract, strings.TrimSpace(final.Content))
	if !ok {
		// Tool/shell completion modes do not complete from a normal final answer; the driver applies
		// its mode-specific protocol nudge instead.
		return objectiveOutcome{}, nil
	}
	if eval.Invalid != nil {
		return objectiveOutcome{Applicable: true, Continue: eval.Invalid}, nil
	}
	return objectiveOutcome{Applicable: true, Done: true, Complete: func(ctx context.Context) error {
		if completeErr := a.executor.completeWorkflowRunFromParsed(ctx, eval.Parsed); completeErr != nil {
			return completeErr
		}
		e.setWorkflowTerminalState(eval.Source)
		return nil
	}}, nil
}

var _ CompletionAdapter = workflowCompletionAdapter{}
var _ CompletionAdapter = goalContinuation{}
