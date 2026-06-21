package runtime

import (
	"strings"
	"time"
)

type WorkflowCompletionSource string

const (
	WorkflowCompletionSourceTool             WorkflowCompletionSource = "tool"
	WorkflowCompletionSourceStructuredOutput WorkflowCompletionSource = "structured_output"
	WorkflowCompletionSourceUnstructured     WorkflowCompletionSource = "unstructured_output"
	WorkflowCompletionSourceObserved         WorkflowCompletionSource = "observed"
)

type WorkflowTerminalState struct {
	Completed   bool
	RunID       string
	Generation  int64
	Source      WorkflowCompletionSource
	CompletedAt time.Time
}

type WorkflowSessionState struct {
	RunID      string
	TaskID     string
	WorkflowID string
}

func (e *Engine) WorkflowSessionState() WorkflowSessionState {
	if e == nil {
		return WorkflowSessionState{}
	}
	if e.workflowRunActive() {
		return WorkflowSessionState{
			RunID:      strings.TrimSpace(string(e.cfg.WorkflowRun.Contract.RunID)),
			TaskID:     strings.TrimSpace(e.cfg.WorkflowRun.Instructions.TaskID),
			WorkflowID: strings.TrimSpace(e.cfg.WorkflowRun.Instructions.WorkflowID),
		}
	}
	if e.store == nil {
		return WorkflowSessionState{}
	}
	meta := e.store.Meta()
	workflowSession := meta.WorkflowSession
	if workflowSession == nil {
		return WorkflowSessionState{}
	}
	return WorkflowSessionState{
		RunID:      strings.TrimSpace(workflowSession.RunID),
		TaskID:     strings.TrimSpace(workflowSession.TaskID),
		WorkflowID: strings.TrimSpace(workflowSession.WorkflowID),
	}
}

func (e *Engine) WorkflowTerminalState() WorkflowTerminalState {
	if e == nil {
		return WorkflowTerminalState{}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.workflowTerminal
}

func (e *Engine) setWorkflowTerminalState(source WorkflowCompletionSource) {
	if e == nil || !e.workflowRunActive() {
		return
	}
	transitioned := e.recordWorkflowTerminalState(source)
	if transitioned {
		// Soft cascade: a valid workflow completion auto-completes an ACTIVE self-set goal in the
		// same step (actor=system). This runs here — after recordWorkflowTerminalState releases
		// e.mu — so it covers every terminal source (structured/unstructured/tool/observed) exactly
		// once on the false->true transition, without re-taking e.mu while SetGoalStatus holds
		// controlMutationMu/steer (the R1 lock-order deadlock).
		e.cascadeCompleteActiveGoalOnWorkflowCompletion()
	}
}

// recordWorkflowTerminalState commits the terminal state under e.mu and reports whether this call
// is the one that transitioned the run to completed (false otherwise, incl. repeat calls).
func (e *Engine) recordWorkflowTerminalState(source WorkflowCompletionSource) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.workflowTerminal.Completed {
		return false
	}
	e.workflowTerminal = WorkflowTerminalState{
		Completed:   true,
		RunID:       strings.TrimSpace(string(e.cfg.WorkflowRun.Contract.RunID)),
		Generation:  e.cfg.WorkflowRun.Contract.ExpectedGeneration,
		Source:      source,
		CompletedAt: time.Now(),
	}
	return true
}
