package runtime

import (
	"context"
	"errors"
	"time"

	"core/server/llm"
)

const queuedUserSubmissionBusyRetryDelay = 25 * time.Millisecond

// SubmitQueuedUserMessages starts a fresh step from already-queued injected user
// messages or background notices. This is used when a non-turn busy operation
// (for example manual compaction) completes while queued steering is waiting.
func (e *Engine) SubmitQueuedUserMessages(ctx context.Context) (assistant llm.Message, err error) {
	e.ensureOrchestrationCollaborators()
	for {
		err = e.stepLifecycle.Run(ctx, exclusiveStepOptions{EmitRunState: true, PersistRunLifecycle: true}, func(stepCtx context.Context, stepID string) error {
			if err := e.ensureMetaContextForRequest(stepCtx, stepID); err != nil {
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

func (e *Engine) DrainQueuedUserMessagesBeforeClose(ctx context.Context) error {
	if e == nil {
		return nil
	}
	if e.WorkflowTerminalState().Completed {
		e.FailQueuedUserMessages(QueuedUserMessageFailureTerminalWorkflowCompletion)
		return nil
	}
	if !e.HasQueuedUserWork() {
		return nil
	}
	_, err := e.SubmitQueuedUserMessages(ctx)
	if err != nil {
		if e.WorkflowTerminalState().Completed {
			e.FailQueuedUserMessages(QueuedUserMessageFailureTerminalWorkflowCompletion)
			return nil
		}
		e.FailQueuedUserMessages(QueuedUserMessageFailureClosing)
		return err
	}
	return nil
}
