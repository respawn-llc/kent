package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"builder/prompts"
	"builder/server/llm"
	"builder/server/session"
	"builder/shared/toolspec"
	"builder/shared/transcript"
)

const goalObjectivePreviewMaxRunes = 120
const goalLoopBusyRetryDelay = 50 * time.Millisecond

var ErrGoalRequiresAskQuestion = errors.New("active goal requires ask_question to be enabled; enable ask_question or pause/clear the goal")
var errGoalLoopInactive = errors.New("goal loop inactive")

func (e *Engine) Goal() *session.GoalState {
	if e == nil || e.store == nil {
		return nil
	}
	return cloneRuntimeGoal(e.store.Meta().Goal)
}

func (e *Engine) GoalLoopSuspended() bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.goalLoopSuspended && e.goalActiveLocked()
}

func (e *Engine) SetGoal(objective string, actor session.GoalActor) (session.GoalState, error) {
	if e == nil || e.store == nil {
		return session.GoalState{}, fmt.Errorf("runtime engine is required")
	}
	msg := e.goalDeveloperMessage(prompts.RenderGoalSetPrompt(strings.TrimSpace(objective)), goalSetCompactText(objective))
	goal, err := e.store.SetGoalWithEvents(objective, actor, []session.EventInput{{Kind: "message", Payload: msg}})
	if err != nil {
		return session.GoalState{}, err
	}
	e.appendPersistedGoalDeveloperMessage("", msg)
	return goal, nil
}

func (e *Engine) SetGoalStatus(status session.GoalStatus, actor session.GoalActor) (session.GoalState, error) {
	if e == nil || e.store == nil {
		return session.GoalState{}, fmt.Errorf("runtime engine is required")
	}
	transcriptWorkingDir := e.transcriptWorkingDir()
	var msg llm.Message
	goal, err := e.store.SetGoalStatusWithEventBuilder(status, actor, func(goal session.GoalState) ([]session.EventInput, error) {
		msg = goalDeveloperMessageForWorkingDir(goalStatusPrompt(goal), goalStatusCompactText(goal), transcriptWorkingDir)
		return []session.EventInput{{Kind: "message", Payload: msg}}, nil
	})
	if err != nil {
		return session.GoalState{}, err
	}
	e.appendPersistedGoalDeveloperMessage("", msg)
	return goal, nil
}

func (e *Engine) ClearGoal(actor session.GoalActor) (session.GoalState, error) {
	if e == nil || e.store == nil {
		return session.GoalState{}, fmt.Errorf("runtime engine is required")
	}
	msg := e.goalDeveloperMessage(prompts.GoalClearPrompt, "Goal cleared")
	goal, err := e.store.ClearGoalWithEvents(actor, []session.EventInput{{Kind: "message", Payload: msg}})
	if err != nil {
		return session.GoalState{}, err
	}
	e.appendPersistedGoalDeveloperMessage("", msg)
	return goal, nil
}

func (e *Engine) StartGoalLoop() error {
	return e.startGoalLoop(true)
}

func (e *Engine) startGoalLoop(firstTurnAlreadyPrompted bool) error {
	if e == nil {
		return nil
	}
	e.ensureOrchestrationCollaborators()
	e.mu.Lock()
	if !e.goalActiveLocked() {
		e.mu.Unlock()
		return nil
	}
	if err := e.requireAskQuestionForGoalLoopStart(); err != nil {
		if errors.Is(err, ErrGoalRequiresAskQuestion) {
			e.goalLoopSuspended = true
		}
		e.mu.Unlock()
		return err
	}
	e.goalLoopSuspended = false
	if e.goalLoopRunning {
		e.mu.Unlock()
		return nil
	}
	e.goalLoopRunning = true
	e.mu.Unlock()

	launched := e.launchLifecycleTask(func(ctx context.Context) {
		defer e.finishGoalLoop()
		e.runGoalLoop(ctx, firstTurnAlreadyPrompted)
	})
	if !launched {
		e.finishGoalLoop()
	}
	return nil
}

func (e *Engine) finishGoalLoop() {
	e.mu.Lock()
	e.goalLoopRunning = false
	e.mu.Unlock()
}

func (e *Engine) runGoalLoop(ctx context.Context, firstTurnAlreadyPrompted bool) {
	appendNudge := !firstTurnAlreadyPrompted
	for {
		if !e.shouldContinueGoalLoop() {
			return
		}
		if _, err := e.runGoalTurn(ctx, appendNudge); err != nil {
			if errors.Is(err, errExclusiveStepBusy) {
				if !e.waitBeforeGoalLoopBusyRetry(ctx) {
					return
				}
				continue
			}
			e.recordGoalLoopError(err)
			return
		}
		appendNudge = true
	}
}

func (e *Engine) runGoalTurn(ctx context.Context, appendNudge bool) (assistant llm.Message, err error) {
	e.ensureOrchestrationCollaborators()
	err = e.stepLifecycle.Run(ctx, exclusiveStepOptions{EmitRunState: true, PersistRunLifecycle: true, GoalLoop: true}, func(stepCtx context.Context, stepID string) error {
		if err := e.injectAgentsIfNeeded(stepID); err != nil {
			return err
		}
		if err := e.injectHeadlessModeTransitionPromptIfNeeded(stepID); err != nil {
			return err
		}
		goal := e.Goal()
		if goal == nil || goal.Status != session.GoalStatusActive {
			return errGoalLoopInactive
		}
		if appendNudge {
			if err := e.appendGoalDeveloperMessage(stepID, prompts.RenderGoalNudgePrompt(goal.Objective, string(goal.Status)), goalNudgeCompactText(*goal)); err != nil {
				return err
			}
		}
		msg, runErr := e.runStepLoop(stepCtx, stepID)
		assistant = msg
		return runErr
	})
	if errors.Is(err, errGoalLoopInactive) {
		return llm.Message{}, nil
	}
	return assistant, err
}

func (e *Engine) waitBeforeGoalLoopBusyRetry(ctx context.Context) bool {
	timer := time.NewTimer(goalLoopBusyRetryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (e *Engine) recordGoalLoopError(err error) {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, errGoalLoopInactive) {
		return
	}
	message := "Goal loop stopped: " + err.Error()
	if appendErr := e.appendPersistedLocalEntry("", string(transcript.EntryRoleDeveloperErrorFeedback), message); appendErr != nil {
		e.SetOngoingError(message + " (also failed to persist error: " + appendErr.Error() + ")")
		return
	}
	e.SetOngoingError(message)
}

func (e *Engine) shouldContinueGoalLoop() bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return !e.goalLoopSuspended && e.goalActiveLocked()
}

func (e *Engine) goalActiveLocked() bool {
	if e == nil || e.store == nil {
		return false
	}
	goal := e.store.Meta().Goal
	return goal != nil && goal.Status == session.GoalStatusActive
}

func (e *Engine) goalActive() bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.goalActiveLocked()
}

func (e *Engine) appendGoalDeveloperMessage(stepID string, content string, compact string) error {
	return e.appendMessage(stepID, e.goalDeveloperMessage(content, compact))
}

func (e *Engine) goalDeveloperMessage(content string, compact string) llm.Message {
	return goalDeveloperMessageForWorkingDir(content, compact, e.transcriptWorkingDir())
}

func goalDeveloperMessageForWorkingDir(content string, compact string, workingDir string) llm.Message {
	return normalizeMessageForTranscript(llm.Message{
		Role:           llm.RoleDeveloper,
		MessageType:    llm.MessageTypeGoal,
		Content:        content,
		CompactContent: compact,
	}, workingDir)
}

func (e *Engine) appendPersistedGoalDeveloperMessage(stepID string, msg llm.Message) {
	previousCommittedCount := e.CommittedTranscriptEntryCount()
	e.markCurrentRequestShapeDirty()
	e.chat.appendMessage(msg)
	if shouldEmitCommittedTranscriptAdvancedForAppendedMessage(msg, previousCommittedCount, e.CommittedTranscriptEntryCount()) {
		e.emitCommittedMessageTranscriptAdvanced(stepID, msg)
	}
}

func (e *Engine) requireAskQuestionForActiveGoal() error {
	goal := e.Goal()
	if goal == nil || goal.Status != session.GoalStatusActive {
		return nil
	}
	return e.requireAskQuestionForGoalLoopStart()
}

func (e *Engine) RequireGoalLoopStartAllowed() error {
	return e.requireAskQuestionForGoalLoopStart()
}

func (e *Engine) requireAskQuestionForGoalLoopStart() error {
	for _, id := range e.cfg.EnabledTools {
		if id == toolspec.ToolAskQuestion {
			return nil
		}
	}
	return ErrGoalRequiresAskQuestion
}

func goalSetCompactText(objective string) string {
	return "Goal set: " + strconvQuoteForGoalPreview(objective)
}

func goalStatusPrompt(goal session.GoalState) string {
	switch goal.Status {
	case session.GoalStatusPaused:
		return prompts.GoalPausePrompt
	case session.GoalStatusActive:
		return prompts.RenderGoalResumePrompt(goal.Objective)
	case session.GoalStatusComplete:
		return prompts.GoalCompletePrompt
	default:
		return ""
	}
}

func goalStatusCompactText(goal session.GoalState) string {
	switch goal.Status {
	case session.GoalStatusPaused:
		return "Goal paused"
	case session.GoalStatusActive:
		return "Goal resumed: " + strconvQuoteForGoalPreview(goal.Objective)
	case session.GoalStatusComplete:
		return "Goal complete. Cooked for " + formatGoalDuration(goal.UpdatedAt.Sub(goal.CreatedAt))
	default:
		return "Goal updated"
	}
}

func goalNudgeCompactText(goal session.GoalState) string {
	return "Continue active goal: " + strconvQuoteForGoalPreview(goal.Objective)
}

func formatGoalDuration(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}
	totalSeconds := int64(duration / time.Second)
	hours := totalSeconds / int64(time.Hour/time.Second)
	minutes := totalSeconds % int64(time.Hour/time.Second) / int64(time.Minute/time.Second)
	seconds := totalSeconds % int64(time.Minute/time.Second)
	var out strings.Builder
	if hours > 0 {
		out.WriteString(fmt.Sprintf("%dh", hours))
	}
	if minutes > 0 {
		out.WriteString(fmt.Sprintf("%dm", minutes))
	}
	if seconds > 0 || out.Len() == 0 {
		out.WriteString(fmt.Sprintf("%ds", seconds))
	}
	return out.String()
}

func strconvQuoteForGoalPreview(objective string) string {
	preview := strings.Join(strings.Fields(strings.TrimSpace(objective)), " ")
	runes := []rune(preview)
	if len(runes) > goalObjectivePreviewMaxRunes {
		preview = string(runes[:goalObjectivePreviewMaxRunes]) + "..."
	}
	return fmt.Sprintf("%q", preview)
}

func cloneRuntimeGoal(goal *session.GoalState) *session.GoalState {
	if goal == nil {
		return nil
	}
	copyGoal := *goal
	return &copyGoal
}
