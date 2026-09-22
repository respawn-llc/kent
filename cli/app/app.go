package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"core/shared/config"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

type Options struct {
	WorkspaceRoot             string
	WorkspaceRootExplicit     bool
	SessionID                 string
	WorkspaceContextSessionID string
	AgentRole                 *string
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
	return runSessionLifecycleWithOptions(ctx, server, interactor, sessionLifecycleOptions{
		Intent:    intent,
		Overrides: overrides,
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
	return runPrompt(ctx, runClient, workspaceConfig.Options, workspaceConfig.CallerContext, strings.TrimSpace(opts.SessionID), prompt, timeout, progress)
}

func sessionLaunchOptionsFor(opts Options) (*serverapi.SessionLaunchIntent, serverapi.RunPromptOverrides, error) {
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
		sessionID, err := runtimeids.ParseSessionID(opts.SessionID)
		if err != nil {
			return nil, serverapi.RunPromptOverrides{}, err
		}
		intent := serverapi.OpenExistingSessionLaunchIntent(sessionID)
		return &intent, overrides, nil
	}
	if opts.WorkspaceContextSessionID != "" {
		parentID, err := runtimeids.ParseSessionID(opts.WorkspaceContextSessionID)
		if err != nil {
			return nil, serverapi.RunPromptOverrides{}, err
		}
		intent := serverapi.CreateNewSessionLaunchIntent(serverapi.ParentAgentSessionCreateOrigin(parentID))
		return &intent, overrides, nil
	}
	if agentRole != nil && *agentRole != config.DefaultSubagentRole {
		intent := serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin())
		return &intent, overrides, nil
	}
	return nil, overrides, nil
}
