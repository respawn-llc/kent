package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"core/shared/config"
	"core/shared/protoapi"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

type Options struct {
	WorkspaceRoot             string
	WorkspaceRootExplicit     bool
	SessionID                 string
	WorkspaceContextSessionID string
	AgentRole                 *string
	RunAgentFlag              RunAgentFlag
	Model                     string
	ThinkingLevel             string
	Theme                     string
	ModelTimeoutSeconds       int
	Tools                     string
	ConfigRoot                string
}

func Run(ctx context.Context, opts Options) (runErr error) {
	interactor := newInteractiveAuthInteractor()
	server, err := startSessionServer(ctx, opts, interactor, true)
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

	intent, overrides, err := sessionLaunchOptionsFor(opts)
	if err != nil {
		return err
	}
	invocationOverrides := runPromptOverridesFromOptions(opts)
	invocationOverrides.AgentRole = nil
	return runSessionLifecycleWithOptions(ctx, server, interactor, sessionLifecycleOptions{
		Intent:              intent,
		Overrides:           overrides,
		InvocationOverrides: invocationOverrides,
	})
}

func RunPrompt(ctx context.Context, opts Options, prompt string, timeout time.Duration, progress serverapi.RunPromptProgressSink) (RunPromptResult, error) {
	workspaceConfig, err := resolveRunPromptWorkspaceConfig(opts)
	if err != nil {
		return RunPromptResult{}, err
	}
	runClient, closeFn, err := startRunPromptClientWithWorkspaceConfig(ctx, workspaceConfig)
	if err != nil {
		return RunPromptResult{}, err
	}
	defer func() {
		if closeFn != nil {
			_ = closeFn()
		}
	}()
	return runPrompt(ctx, runClient, workspaceConfig.Options, strings.TrimSpace(opts.SessionID), prompt, timeout, progress)
}

func sessionLaunchOptionsFor(opts Options) (*sessionlaunchpb.SessionLaunchRequestIntent, serverapi.RunPromptOverrides, error) {
	var agentRole *string
	if opts.AgentRole != nil {
		value := strings.TrimSpace(*opts.AgentRole)
		if value == "" {
			return nil, serverapi.RunPromptOverrides{}, errors.New("agent role must not be blank")
		}
		agentRole = &value
	}
	overrides := serverapi.RunPromptOverrides{AgentRole: agentRole}
	if opts.SessionID != "" && opts.WorkspaceContextSessionID != "" {
		return nil, serverapi.RunPromptOverrides{}, errors.New("session ID and workspace context session ID cannot both be set")
	}
	if opts.SessionID != "" {
		sessionID, err := runtimeids.ParseSessionSelector(opts.SessionID)
		if err != nil {
			return nil, serverapi.RunPromptOverrides{}, err
		}
		intent := &sessionlaunchpb.SessionLaunchRequestIntent{Intent: &sessionlaunchpb.SessionLaunchRequestIntent_OpenExistingSessionId{OpenExistingSessionId: protoapi.SessionSelectorToProto(sessionID)}}
		return intent, overrides, nil
	}
	if opts.WorkspaceContextSessionID != "" {
		parentID, err := runtimeids.ParseSessionID(opts.WorkspaceContextSessionID)
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
