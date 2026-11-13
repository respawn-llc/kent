package chatmutation

import (
	"context"
	"errors"

	"core/server/agentruntime"
	"core/server/launch"
	"core/server/sessionlaunch"
	"core/server/sessionruntime"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

type SessionRuntimePlannerResolver func(
	context.Context,
	runtimeids.SessionID,
) (PersistedSessionPlanner, error)

type PersistedSessionPlanner interface {
	PlanLaunchSession(context.Context, sessionlaunch.PlanRequest) (sessionlaunch.PlanResult, error)
}

type RuntimePlanner struct {
	authority     *sessionruntime.Authority
	sessionLaunch SessionRuntimePlannerResolver
	runtimeAPI    *sessionruntime.API
}

func NewRuntimePlanner(
	authority *sessionruntime.Authority,
	sessionLaunch SessionRuntimePlannerResolver,
	runtimeAPI *sessionruntime.API,
) *RuntimePlanner {
	return &RuntimePlanner{
		authority:     authority,
		sessionLaunch: sessionLaunch,
		runtimeAPI:    runtimeAPI,
	}
}

func (p *RuntimePlanner) Open(
	ctx context.Context,
	sessionID runtimeids.SessionID,
	invocation *serverapi.RunPromptOverrides,
) error {
	if p == nil || p.authority == nil {
		return errors.New("session Runtime authority is required")
	}
	if invocation == nil || invocation.AgentRole == nil {
		err := p.authority.WithCurrentRuntime(ctx, sessionID, func(ctx context.Context, engine *runtime.Engine) error {
			if invocation != nil && invocation.ThinkingLevel != "" {
				return engine.SetThinkingLevel(ctx, invocation.ThinkingLevel)
			}
			return nil
		})
		if err == nil {
			return nil
		}
		if !errors.Is(err, serverapi.ErrRuntimeUnavailable) {
			return err
		}
	}
	if p.sessionLaunch == nil {
		return errors.New("persisted Session launch resolver is required")
	}
	service, err := p.sessionLaunch(ctx, sessionID)
	if err != nil {
		return err
	}
	if service == nil {
		return errors.New("persisted Session launch service is required")
	}
	request := sessionlaunch.PlanRequest{
		Mode:   launch.ModeInteractive,
		Intent: serverapi.OpenExistingSessionLaunchIntent(sessionID),
	}
	if invocation != nil {
		request.Overrides = *invocation
	}
	planned, err := service.PlanLaunchSession(ctx, request)
	if err != nil {
		return err
	}
	if p.runtimeAPI == nil {
		return errors.New("Session Runtime service is required")
	}
	return p.runtimeAPI.ActivateSessionRuntime(ctx, planned.Plan)
}
