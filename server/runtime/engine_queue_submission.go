package runtime

import (
	"context"
	"errors"
	"time"

	"builder/server/llm"
)

const queuedUserSubmissionBusyRetryDelay = 25 * time.Millisecond

// SubmitQueuedUserMessages starts a fresh step from already-queued injected user
// messages or background notices. This is used when a non-turn busy operation
// (for example manual compaction) completes while queued steering is waiting.
func (e *Engine) SubmitQueuedUserMessages(ctx context.Context) (assistant llm.Message, err error) {
	e.ensureOrchestrationCollaborators()
	for {
		err = e.stepLifecycle.Run(ctx, exclusiveStepOptions{EmitRunState: true, PersistRunLifecycle: true}, func(stepCtx context.Context, stepID string) error {
			if err := e.injectAgentsIfNeeded(stepID); err != nil {
				return err
			}
			if err := e.injectHeadlessModeTransitionPromptIfNeeded(stepID); err != nil {
				return err
			}
			if err := e.injectWorkflowModePromptIfNeeded(stepCtx, stepID); err != nil {
				return err
			}
			flushed, err := e.flushPendingUserInjections(stepID)
			if err != nil {
				return err
			}
			if flushed == 0 {
				return nil
			}
			msg, runErr := e.runStepLoop(stepCtx, stepID)
			assistant = msg
			return runErr
		})
		if !errors.Is(err, errExclusiveStepBusy) {
			return assistant, err
		}

		select {
		case <-ctx.Done():
			return llm.Message{}, ctx.Err()
		case <-time.After(queuedUserSubmissionBusyRetryDelay):
		}
	}
}

func (e *Engine) HasQueuedUserWork() bool {
	e.ensureOrchestrationCollaborators()
	if e.messageFlow.HasPendingUserInjections() {
		return true
	}
	if e.backgroundFlow != nil && e.backgroundFlow.HasPendingNotices() {
		return true
	}
	return false
}
