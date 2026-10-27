package sessionruntime

import (
	"context"
	"errors"

	"core/server/agentruntime"
	"core/server/sessionstore"
	"core/shared/runtimeids"
)

// Preflight validates before Stop; commit revalidates afterward. Admission stays
// closed until the old runtime has retired.
func (a *Authority) WithSessionAgentChange(ctx context.Context, sessionID runtimeids.SessionID, preflight func(context.Context, *session.Store, *runtime.Engine) (bool, error), commit func(context.Context, *session.Store, *runtime.Engine) (bool, error)) error {
	if err := context.Cause(ctx); err != nil {
		return err
	}
	result, err := a.AcceptLifecycleTask(func(runCtx context.Context) (resultErr error) {
		block, err := a.TryBlockSessionStarts(runCtx, []runtimeids.SessionID{sessionID}, SessionStartBlockMaintenance)
		if err != nil {
			return err
		}
		defer func() { resultErr = errors.Join(resultErr, block.Close(context.Background())) }()
		runCtx = block.AuthorizeMaintenance(runCtx)
		var change bool
		err = a.withMaintenanceResourceAdmission(runCtx, sessionID, maintenanceAdmissionAuthorized, false, func(ctx context.Context, store *session.Store, _ *agentResource, engine *runtime.Engine) (bool, error) {
			var err error
			change, err = preflight(ctx, store, engine)
			return false, err
		})
		if err != nil || !change {
			return err
		}
		// Do not retain an engine callback while waiting: abort finalization
		// may need to close that engine before execution completion.
		a.mu.Lock()
		resource := a.resources[sessionID]
		var current *execution
		if resource != nil {
			resource.mu.Lock()
			current = resource.current
			resource.mu.Unlock()
		}
		a.mu.Unlock()
		if _, err := a.InterruptSession(runCtx, sessionID); err != nil {
			return err
		}
		if current != nil {
			if err := current.awaitDone(runCtx); err != nil {
				return err
			}
			if err := current.stopError(); err != nil {
				return err
			}
		}
		return a.withMaintenanceResourceAdmission(runCtx, sessionID, maintenanceAdmissionAuthorized, false, func(ctx context.Context, store *session.Store, _ *agentResource, engine *runtime.Engine) (bool, error) {
			if engine == nil {
				return commit(ctx, store, nil)
			}
			var retire bool
			err := engine.RunWhenIdleBeforeQueuedUserWork(ctx, runtime.ActiveKindRuntimeMaintenance, func() error {
				var err error
				retire, err = commit(ctx, store, engine)
				return err
			})
			return retire, err
		})
	})
	if err != nil {
		return err
	}
	return <-result
}
