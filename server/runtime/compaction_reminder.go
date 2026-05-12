package runtime

import (
	"context"
	"strings"

	"builder/prompts"
	"builder/server/llm"
	"builder/shared/toolspec"
)

type compactionReminderCoordinator struct {
	engine *Engine
}

func newCompactionReminderCoordinator(engine *Engine) compactionReminderCoordinator {
	return compactionReminderCoordinator{engine: engine}
}

func (e *Engine) compactionSoonReminderLimit(ctx context.Context) int {
	return e.compactionPlannerState().soonReminderLimit(e.compactionPlanningSnapshot())
}

func (e *Engine) maybeAppendCompactionSoonReminder(ctx context.Context, stepID string) error {
	return newCompactionReminderCoordinator(e).maybeAppend(ctx, stepID)
}

func (c compactionReminderCoordinator) maybeAppend(ctx context.Context, stepID string) error {
	e := c.engine
	planningSnapshot := e.compactionPlanningSnapshot()
	planner := e.compactionPlannerState()
	if !planner.autoCompactionAvailable(planningSnapshot) {
		return nil
	}
	limit := planner.soonReminderLimit(planningSnapshot)
	if limit <= 0 {
		return nil
	}
	if !e.usageAtOrAboveLimit(ctx, limit) {
		return nil
	}
	content := prompts.RenderCompactionSoonReminderPrompt(e.triggerHandoffConfigured())
	if content == "" {
		return nil
	}
	if e.shouldAutoCompactWithContext(ctx) {
		return nil
	}
	if e.compactionRuntimeState().SoonReminderIssued() {
		return nil
	}
	if err := e.appendMessage(stepID, llm.Message{
		Role:        llm.RoleDeveloper,
		MessageType: llm.MessageTypeCompactionSoonReminder,
		Content:     content,
	}); err != nil {
		return err
	}
	return e.persistCompactionSoonReminderIssued(true)
}

func (e *Engine) handoffToolEnabled() bool {
	return e.compactionRuntimeState().SoonReminderIssued()
}

func (e *Engine) setCompactionSoonReminderIssued(issued bool) {
	e.compactionRuntimeState().SetSoonReminderIssued(issued)
}

func (e *Engine) persistCompactionSoonReminderIssued(issued bool) error {
	e.setCompactionSoonReminderIssued(issued)
	return e.store.SetCompactionSoonReminderIssued(issued)
}

func (e *Engine) syncCompactionSoonReminderIssuedFromMessages(messages []llm.Message) {
	issued := false
	for _, message := range messages {
		if message.Role == llm.RoleDeveloper && message.MessageType == llm.MessageTypeCompactionSoonReminder && strings.TrimSpace(message.Content) != "" {
			issued = true
		}
	}
	e.setCompactionSoonReminderIssued(issued)
}

func (e *Engine) syncCompactionSoonReminderIssuedFromItems(items []llm.ResponseItem) {
	e.syncCompactionSoonReminderIssuedFromMessages(llm.MessagesFromItems(items))
}

func (e *Engine) triggerHandoffConfigured() bool {
	for _, id := range e.cfg.EnabledTools {
		if id == toolspec.ToolTriggerHandoff {
			return true
		}
	}
	return false
}
