package runtimewire

import (
	"context"
	"strings"
	"sync"

	"core/server/runtime"
)

type RuntimeRegistry interface {
	Register(sessionID string, engine *runtime.Engine)
	Unregister(sessionID string, engine *runtime.Engine)
}

type RuntimeRegistryHookRegistrar interface {
	RegisterRuntimeHooks(sessionID string, engine *runtime.Engine, rebind func(string) error)
}

type RuntimeRegistryCloseDrainer interface {
	CloseRuntimeWithDrain(ctx context.Context, sessionID string, engine *runtime.Engine, drain func(context.Context) error) error
}

type BackgroundRouter interface {
	SetActiveSession(sessionID string, engine *runtime.Engine)
	ClearActiveSession(sessionID string, engine *runtime.Engine)
}

type RuntimeRegistration struct {
	once    sync.Once
	cleanup func()
	drain   func(context.Context, func(context.Context) error) error
}

type RuntimeRegistrationOption func(*runtimeRegistrationOptions)

type runtimeRegistrationOptions struct {
	rebind func(string) error
}

func WithRuntimeRebind(rebind func(string) error) RuntimeRegistrationOption {
	return func(options *runtimeRegistrationOptions) {
		if options != nil {
			options.rebind = rebind
		}
	}
}

func RuntimeRebindFunc(localRebind func(string) error, engine *runtime.Engine) func(string) error {
	return func(workdir string) error {
		if localRebind != nil {
			if err := localRebind(workdir); err != nil {
				return err
			}
		}
		if engine != nil {
			engine.SetTranscriptWorkingDir(workdir)
		}
		return nil
	}
}

func RegisterSessionRuntime(sessionID string, engine *runtime.Engine, registry RuntimeRegistry, router BackgroundRouter, opts ...RuntimeRegistrationOption) *RuntimeRegistration {
	trimmedSessionID := strings.TrimSpace(sessionID)
	if trimmedSessionID == "" || engine == nil {
		return &RuntimeRegistration{}
	}
	options := runtimeRegistrationOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}
	if registry != nil {
		if hookRegistry, ok := registry.(RuntimeRegistryHookRegistrar); ok {
			hookRegistry.RegisterRuntimeHooks(trimmedSessionID, engine, options.rebind)
		} else {
			registry.Register(trimmedSessionID, engine)
		}
	}
	if router != nil {
		router.SetActiveSession(trimmedSessionID, engine)
	}
	cleanup := func() {
		if registry != nil {
			registry.Unregister(trimmedSessionID, engine)
		}
		if router != nil {
			router.ClearActiveSession(trimmedSessionID, engine)
		}
	}
	drainClose := func(ctx context.Context, drain func(context.Context) error) error {
		if drainer, ok := registry.(RuntimeRegistryCloseDrainer); ok {
			err := drainer.CloseRuntimeWithDrain(ctx, trimmedSessionID, engine, drain)
			if router != nil {
				router.ClearActiveSession(trimmedSessionID, engine)
			}
			return err
		}
		if drain != nil {
			if err := drain(ctx); err != nil {
				cleanup()
				return err
			}
		}
		cleanup()
		return nil
	}
	return &RuntimeRegistration{cleanup: cleanup, drain: drainClose}
}

func (r *RuntimeRegistration) Close() {
	_ = r.CloseWithDrain(context.Background(), nil)
}

func (r *RuntimeRegistration) CloseWithDrain(ctx context.Context, drain func(context.Context) error) error {
	if r == nil || r.cleanup == nil {
		return nil
	}
	var err error
	r.once.Do(func() {
		if r.drain != nil {
			err = r.drain(ctx, drain)
			return
		}
		if drain != nil {
			err = drain(ctx)
		}
		if r.cleanup != nil {
			r.cleanup()
		}
	})
	return err
}
