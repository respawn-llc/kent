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
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.workflowTerminal.Completed {
		return
	}
	e.workflowTerminal = WorkflowTerminalState{
		Completed:   true,
		RunID:       strings.TrimSpace(string(e.cfg.WorkflowRun.Contract.RunID)),
		Generation:  e.cfg.WorkflowRun.Contract.ExpectedGeneration,
		Source:      source,
		CompletedAt: time.Now(),
	}
}
