package runner

import (
	"context"
	"errors"
	"strings"

	"core/shared/config"
	"core/shared/protoapi"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

func RunInteractive(ctx context.Context, req Request, deps Dependencies) (runErr error) {
	if deps.StartSessionServer == nil {
		return errors.New("session server starter is required")
	}
	if deps.RunSessionLifecycle == nil {
		return errors.New("session lifecycle runner is required")
	}
	server, err := deps.StartSessionServer(ctx)
	if err != nil {
		return err
	}
	defer func() {
		closeErr := server.Close()
		if closeErr == nil {
			return
		}
		if runErr == nil || !errors.Is(closeErr, runErr) {
			runErr = errors.Join(runErr, closeErr)
		}
	}()
	intent, overrides, err := SessionLifecycleOptionsFor(req)
	if err != nil {
		return err
	}
	runErr = deps.RunSessionLifecycle(ctx, server, intent, overrides)
	return runErr
}

func SessionLifecycleOptionsFor(req Request) (*sessionlaunchpb.SessionLaunchRequestIntent, serverapi.RunPromptOverrides, error) {
	var agentRole *string
	if req.AgentRole != nil {
		value := strings.TrimSpace(*req.AgentRole)
		if value == "" {
			return nil, serverapi.RunPromptOverrides{}, errors.New("agent role must not be blank")
		}
		agentRole = &value
	}
	overrides := serverapi.RunPromptOverrides{AgentRole: agentRole}
	if req.SessionID != "" && req.WorkspaceContextSessionID != "" {
		return nil, serverapi.RunPromptOverrides{}, errors.New("session ID and workspace context session ID cannot both be set")
	}
	if req.SessionID != "" {
		sessionID, err := runtimeids.ParseSessionSelector(req.SessionID)
		if err != nil {
			return nil, serverapi.RunPromptOverrides{}, err
		}
		intent := &sessionlaunchpb.SessionLaunchRequestIntent{Intent: &sessionlaunchpb.SessionLaunchRequestIntent_OpenExistingSessionId{OpenExistingSessionId: protoapi.SessionSelectorToProto(sessionID)}}
		return intent, overrides, nil
	}
	if req.WorkspaceContextSessionID != "" {
		parentID, err := runtimeids.ParseSessionID(req.WorkspaceContextSessionID)
		if err != nil {
			return nil, serverapi.RunPromptOverrides{}, err
		}
		intent, err := protoapi.SessionLaunchRequestIntentToProto(serverapi.CreateNewSessionLaunchIntent(serverapi.ParentAgentSessionCreateOrigin(parentID)))
		return intent, overrides, err
	}
	if agentRole != nil && *agentRole != config.DefaultSubagentRole {
		intent, err := protoapi.SessionLaunchRequestIntentToProto(serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()))
		return intent, overrides, err
	}
	return nil, overrides, nil
}
