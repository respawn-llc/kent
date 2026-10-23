package sessionruntime

import (
	"context"
	"core/server/agentruntime"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"errors"
	"fmt"
)

func (a *Authority) WithRuntime(ctx context.Context, ref runtimeids.SessionResourceRef, callback func(context.Context, *runtime.Engine) error) error {
	if a == nil {
		return errors.New("session runtime authority is required")
	}
	if err := context.Cause(ctx); err != nil {
		return err
	}
	if err := ref.Validate(); err != nil {
		return err
	}
	a.mu.Lock()
	resource := a.resources[ref.SessionID()]
	a.mu.Unlock()
	if resource == nil {
		return errors.Join(
			serverapi.ErrRuntimeUnavailable,
			fmt.Errorf("agent resource %s generation %d is unavailable", ref.SessionID(), ref.Generation()),
		)
	}
	return resource.withEngine(ctx, ref, callback)
}

func (a *Authority) WithCurrentRuntime(ctx context.Context, sessionID runtimeids.SessionID, callback func(context.Context, *runtime.Engine) error) error {
	if a == nil {
		return errors.New("session runtime authority is required")
	}
	if sessionID.IsZero() {
		return errors.New("session id is required")
	}
	a.mu.Lock()
	resource := a.resources[sessionID]
	a.mu.Unlock()
	if resource == nil {
		return errors.Join(
			serverapi.ErrRuntimeUnavailable,
			fmt.Errorf("session %s has no active runtime available", sessionID),
		)
	}
	return resource.withEngine(ctx, resource.ref, callback)
}

func (a *Authority) WithLiveExecutionRuntime(
	ctx context.Context,
	sessionID runtimeids.SessionID,
	callback func(context.Context, *runtime.Engine) error,
) error {
	if a == nil {
		return errors.New("session runtime authority is required")
	}
	execution, ok := a.SessionExecution(sessionID)
	if !ok {
		err := a.WithCurrentRuntime(ctx, sessionID, func(context.Context, *runtime.Engine) error {
			return nil
		})
		if err != nil {
			return err
		}
		return serverapi.ErrRuntimeNoActiveRun
	}
	resource, ok := execution.Scope().Resource()
	if !ok {
		return errors.New("agent execution scope has no runtime resource")
	}
	return a.WithRuntime(ctx, resource, callback)
}

// WithRetainedWorkflowRuntime admits a callback only while the Session keeps
// Workflow activation without requiring an Exact Execution Scope to be live.
func (a *Authority) WithRetainedWorkflowRuntime(
	ctx context.Context,
	sessionID runtimeids.SessionID,
	callback func(context.Context, *runtime.Engine) error,
) error {
	if callback == nil {
		return errors.New("retained workflow runtime callback is required")
	}
	return a.WithCurrentRuntime(ctx, sessionID, func(ctx context.Context, engine *runtime.Engine) error {
		if !engine.CurrentNodeExecutionConfigured() {
			return errors.Join(
				serverapi.ErrRuntimeNoActiveRun,
				fmt.Errorf("session %s has no retained workflow activation", sessionID),
			)
		}
		return callback(ctx, engine)
	})
}

func (a *Authority) withInterruptibleAgentTurn(
	ctx context.Context,
	sessionID runtimeids.SessionID,
	withoutExecution func() error,
	callback func(context.Context, *runtime.Engine, *execution) error,
) error {
	if a == nil {
		return errors.New("session runtime authority is required")
	}
	if sessionID.IsZero() {
		return errors.New("session id is required")
	}
	if callback == nil {
		return errors.New("interruptible Agent Turn mutation is required")
	}
	runWithoutExecution := func(missing error) error {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		if withoutExecution != nil {
			return withoutExecution()
		}
		return missing
	}
	a.mu.Lock()
	resource := a.resources[sessionID]
	if resource == nil {
		defer a.mu.Unlock()
		return runWithoutExecution(errors.Join(serverapi.ErrRuntimeUnavailable, fmt.Errorf("session %s has no active runtime available", sessionID)))
	}
	resource.mu.Lock()
	execution := resource.current
	if execution == nil {
		defer a.mu.Unlock()
		defer resource.mu.Unlock()
		return runWithoutExecution(ErrExecutionNoLongerLive)
	}
	resource.mu.Unlock()
	a.mu.Unlock()
	execution.prompts.mu.RLock()
	defer execution.prompts.mu.RUnlock()
	resource.mu.Lock()
	defer resource.mu.Unlock()
	if resource.current != execution {
		return ErrExecutionNoLongerLive
	}
	if resource.rejectsNewUseLocked() || resource.engine == nil {
		return errors.Join(serverapi.ErrRuntimeUnavailable, fmt.Errorf("session %s has no active runtime available", sessionID))
	}
	if err := context.Cause(ctx); err != nil {
		return err
	}
	return callback(ctx, resource.engine, execution)
}

// WithInterruptibleAgentTurn prevents Question admission across one exact current-execution mutation.
func (a *Authority) WithInterruptibleAgentTurn(
	ctx context.Context,
	sessionID runtimeids.SessionID,
	withoutExecution func() error,
	callback func(context.Context, *runtime.Engine) error,
) error {
	if callback == nil {
		return errors.New("interruptible Agent Turn mutation is required")
	}
	return a.withInterruptibleAgentTurn(
		ctx,
		sessionID,
		withoutExecution,
		func(ctx context.Context, engine *runtime.Engine, _ *execution) error {
			return callback(ctx, engine)
		},
	)
}

func (a *Authority) InterruptSession(
	ctx context.Context,
	sessionID runtimeids.SessionID,
) (bool, error) {
	if a == nil {
		return false, errors.New("session runtime authority is required")
	}
	if sessionID.IsZero() {
		return false, errors.New("session id is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	a.mu.Lock()
	resource := a.resources[sessionID]
	if resource == nil {
		a.mu.Unlock()
		return false, context.Cause(ctx)
	}
	resource.mu.Lock()
	execution := resource.current
	resource.mu.Unlock()
	a.mu.Unlock()
	if execution == nil {
		var interrupted bool
		err := resource.withEngine(ctx, resource.ref, func(_ context.Context, engine *runtime.Engine) error {
			gate, releaseGate := a.gateFor(sessionID)
			defer releaseGate()
			if err := gate.lock.LockContext(ctx); err != nil {
				return err
			}
			defer gate.lock.Unlock()
			resource.mu.Lock()
			successor := resource.current != nil
			resource.mu.Unlock()
			if successor {
				return nil
			}
			var err error
			interrupted, err = engine.TryInterruptActiveRun()
			return err
		})
		return interrupted, err
	}

	execution.exactMu.Lock()
	defer execution.exactMu.Unlock()
	a.mu.Lock()
	live := a.byScope[execution.scope.ID()] == execution
	a.mu.Unlock()
	if !live {
		return false, nil
	}
	alreadyCanceled := context.Cause(execution.ctx) != nil
	if err := context.Cause(ctx); err != nil {
		return false, err
	}
	execution.prompts.mu.Lock()
	closure := execution.prompts.closeLocked(context.Canceled)
	execution.prompts.mu.Unlock()
	publicationErr := execution.prompts.publishClosure(closure)
	execution.prompts.releaseClosure(closure)
	publicationErr = errors.Join(
		publicationErr,
		execution.prompts.closeApprovals(closure, true),
	)

	resource.mu.Lock()
	if resource.current != execution {
		resource.mu.Unlock()
		return false, publicationErr
	}
	if resource.rejectsNewUseLocked() || resource.engine == nil {
		resource.mu.Unlock()
		return false, errors.Join(
			publicationErr,
			serverapi.ErrRuntimeUnavailable,
			fmt.Errorf("session %s has no active runtime available", sessionID),
		)
	}
	engine := resource.engine
	execution.cancel()
	interrupted, interruptErr := engine.TryInterruptActiveRun()
	resource.mu.Unlock()
	if interruptErr == nil && !interrupted && !alreadyCanceled {
		interruptErr = engine.PersistInterruption()
	}
	return interrupted || !alreadyCanceled, errors.Join(publicationErr, interruptErr)
}
