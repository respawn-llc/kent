package runtime

import (
	"context"
	"strings"
	"time"

	"builder/server/llm"
	"builder/shared/transcript"
)

// Request orchestration calls this coordinator when model-visible runtime
// context must be prepared. Individual prompt families are owned here and by
// meta_context.go; request entry points must not append those prompts directly.
func (e *Engine) ensureMetaContextForRequest(ctx context.Context, stepID string) error {
	if err := e.steerBaseMetaContextIfNeeded(stepID); err != nil {
		return err
	}
	if err := e.steerHeadlessModeTransitionIfNeeded(stepID); err != nil {
		return err
	}
	if err := e.steerWorkflowModeIfNeeded(ctx, stepID); err != nil {
		return err
	}
	return e.materializePendingWorktreeReminder(stepID)
}

func (e *Engine) ensureMetaContextForCompaction(ctx context.Context, stepID string) error {
	return e.steerBaseMetaContextIfNeeded(stepID)
}

// steerBaseMetaContextIfNeeded injects base meta context (AGENTS.md, skills,
// subagents, environment) exactly once per conversation. A resumed transcript
// that already carries this context marks the guard during restore, so this is
// a no-op on resume. Compaction reinjects base meta directly into the rebuilt
// active list, so the guard stays set across compactions.
func (e *Engine) steerBaseMetaContextIfNeeded(stepID string) error {
	if e.baseMetaInjected {
		return nil
	}
	// The in-process guard is only a fast path; the active transcript is the
	// source of truth. A resumed conversation (or any path that left the guard
	// out of sync with persisted history) must never re-inject base meta on top
	// of context the transcript already carries. Treat presence as already
	// injected so duplication is structurally impossible across restarts.
	if baseMetaContextPresent(e.snapshotMessages()) {
		e.baseMetaInjected = true
		return nil
	}
	builder := e.newActiveBaseMetaContextBuilder(e.cfg.Model, time.Now())
	metaResult, err := builder.Build(baseMetaContextBuildOptions(true))
	if err != nil {
		return err
	}
	intents := make([]steeringIntent, 0, 2)
	if combined := strings.TrimSpace(strings.Join(metaResult.SkillWarnings, "\n")); combined != "" {
		intents = append(intents, steerLocalEntryIntent(storedLocalEntry{
			Visibility: transcript.EntryVisibilityAll,
			Role:       "warning",
			Text:       combined,
		}))
	}
	intents = append(intents, steerRuntimeContextMessagesIntent(metaResult.OrderedInjectionMessages()))
	if err := e.steer(stepID, intents...); err != nil {
		return err
	}
	e.baseMetaInjected = true
	return nil
}

func (e *Engine) steerHeadlessModeTransitionIfNeeded(stepID string) error {
	if e.workflowRunActive() {
		return nil
	}
	builder := newMetaContextBuilder(e.store.Meta().WorkspaceRoot, e.cfg.Model, e.ThinkingLevel(), e.cfg.DisabledSkills, time.Now())
	headlessActive := e.transcriptRuntimeState().HeadlessActive()
	if e.cfg.HeadlessMode {
		if !shouldInjectHeadlessModePromptForState(headlessActive) {
			return nil
		}
		metaResult, err := builder.Build(metaContextBuildOptions{IncludeHeadless: true})
		if err != nil {
			return err
		}
		return e.steer(stepID, steerRuntimeContextMessagesIntent(metaResult.Headless))
	}
	if !headlessActive {
		return nil
	}
	metaResult, err := builder.Build(metaContextBuildOptions{IncludeHeadlessExit: true})
	if err != nil {
		return err
	}
	return e.steer(stepID, steerRuntimeContextMessagesIntent(metaResult.HeadlessExit))
}

func (e *Engine) steerWorkflowModeIfNeeded(ctx context.Context, stepID string) error {
	if !e.workflowRunActive() {
		return nil
	}
	runID := strings.TrimSpace(string(e.cfg.WorkflowRun.Contract.RunID))
	for _, msg := range e.snapshotMessages() {
		if msg.Role == llm.RoleDeveloper && msg.MessageType == llm.MessageTypeWorkflowMode && strings.TrimSpace(msg.SourcePath) == runID {
			return nil
		}
	}
	mode, err := e.workflowCompletionMode(ctx)
	if err != nil {
		return err
	}
	message, ok, renderErr := workflowModeMetaMessage(mode, e.cfg.WorkflowRun)
	if renderErr != nil {
		return renderErr
	}
	if !ok {
		return nil
	}
	message.SourcePath = runID
	return e.steer(stepID, steerRuntimeContextMessagesIntent([]llm.Message{message}))
}

func (e *Engine) compactionReinjectedMetaMessages(ctx context.Context) ([]llm.Message, error) {
	builder := e.newActiveBaseMetaContextBuilder(e.currentModel(), time.Now())
	opts := baseMetaContextBuildOptions(false)
	if e.workflowRunActive() {
		mode, err := e.workflowCompletionMode(ctx)
		if err != nil {
			return nil, err
		}
		opts.IncludeWorkflow = true
		opts.WorkflowCompletionMode = mode
		opts.WorkflowRun = e.cfg.WorkflowRun
	}
	metaResult, err := builder.Build(opts)
	if err != nil {
		return nil, err
	}
	return metaResult.OrderedMetaMessages(), nil
}
