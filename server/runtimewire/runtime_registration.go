package runtimewire

import (
	"strings"
	"sync"

	"core/server/runtime"
)

type RuntimeRegistry interface {
	Register(sessionID string, engine *runtime.Engine)
	Unregister(sessionID string, engine *runtime.Engine)
}

type BackgroundRouter interface {
	SetActiveSession(sessionID string, engine *runtime.Engine)
	ClearActiveSession(sessionID string)
}

type RuntimeRegistration struct {
	once    sync.Once
	cleanup func()
}

func RegisterSessionRuntime(sessionID string, engine *runtime.Engine, registry RuntimeRegistry, router BackgroundRouter) *RuntimeRegistration {
	trimmedSessionID := strings.TrimSpace(sessionID)
	if trimmedSessionID == "" || engine == nil {
		return &RuntimeRegistration{}
	}
	if registry != nil {
		registry.Register(trimmedSessionID, engine)
	}
	if router != nil {
		router.SetActiveSession(trimmedSessionID, engine)
	}
	return &RuntimeRegistration{cleanup: func() {
		if registry != nil {
			registry.Unregister(trimmedSessionID, engine)
		}
		if router != nil {
			router.ClearActiveSession(trimmedSessionID)
		}
	}}
}

func (r *RuntimeRegistration) Close() {
	if r == nil || r.cleanup == nil {
		return
	}
	r.once.Do(r.cleanup)
}
