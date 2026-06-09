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

func (e *Engine) steerBaseMetaContextIfNeeded(stepID string) error {
	meta := e.store.Meta()
	if meta.AgentsInjected {
		return nil
	}
	builder := e.newActiveBaseMetaContextBuilder(e.cfg.Model, time.Now())
	metaResult, err := builder.Build(baseMetaContextBuildOptions(true))
	if err != nil {
		return err
	}
	missingMessages := missingBaseMetaContextMessages(metaResult.OrderedInjectionMessages(), e.snapshotMessages())
	intents := make([]steeringIntent, 0, len(metaResult.SkillWarnings)+1)
	if len(missingMessages) > 0 {
		for _, warning := range metaResult.SkillWarnings {
			intents = append(intents, steerLocalEntryIntent(storedLocalEntry{
				Visibility: transcript.EntryVisibilityAll,
				Role:       "warning",
				Text:       warning,
			}))
		}
		intents = append(intents, steerRuntimeContextMessagesIntent(missingMessages))
	}
	if err := e.steer(stepID, intents...); err != nil {
		return err
	}
	return e.store.MarkAgentsInjected()
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
