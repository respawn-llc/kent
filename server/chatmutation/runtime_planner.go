package chatmutation

import (
	"context"
	"errors"

	"core/server/agentruntime"
	"core/server/launch"
	"core/server/sessionlaunch"
	"core/server/sessionruntime"
	"core/shared/apicontract"
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
	runtimeAPI    apicontract.SessionRuntimeService
}

func NewRuntimePlanner(
	authority *sessionruntime.Authority,
	sessionLaunch SessionRuntimePlannerResolver,
	runtimeAPI apicontract.SessionRuntimeService,
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
	options *serverapi.SessionRuntimePreparationOptions,
) error {
	if p == nil || p.authority == nil {
		return errors.New("session Runtime authority is required")
	}
	var activation serverapi.SessionRuntimeActivateRequest
	if options != nil {
		activation = serverapi.SessionRuntimeActivateRequest{
			SessionID:                        sessionID,
			SessionRuntimePreparationOptions: *options,
		}
	} else {
		err := p.authority.WithCurrentRuntime(ctx, sessionID, func(context.Context, *runtime.Engine) error { return nil })
		if err == nil {
			return nil
		}
		if !errors.Is(err, serverapi.ErrRuntimeUnavailable) {
			return err
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
		planned, err := service.PlanLaunchSession(ctx, sessionlaunch.PlanRequest{
			Mode:   launch.ModeInteractive,
			Intent: serverapi.OpenExistingSessionLaunchIntent(sessionID),
		})
		if err != nil {
			return err
		}
		activation, err = sessionruntime.ActivationRequestFromSessionPlan(planned.Plan)
		if err != nil {
			return err
		}
	}
	if p.runtimeAPI == nil {
		return errors.New("Session Runtime service is required")
	}
	return p.runtimeAPI.ActivateSessionRuntime(ctx, activation)
}
