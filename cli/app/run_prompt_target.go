package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"core/cli/app/internal/remoteattach"
	"core/cli/app/internal/serverattach"
	"core/cli/app/internal/startupconfig"
	"core/shared/apicontract"
	"core/shared/client"
	"core/shared/config"
)

var dialConfiguredRemote = client.DialConfiguredRemoteForProjectWorkspaceID
var dialConfiguredProjectViewRemote = func(ctx context.Context, cfg config.Connection) (remoteattach.ProjectViewRemote, error) {
	return client.DialConfiguredRemote(ctx, cfg)
}
var dialConfiguredRuntimeLiveControlRemote = client.DialConfiguredRemote

var configuredRemoteAttachTimeout = 500 * time.Millisecond
var configuredRemoteWorkspaceDiscoveryTimeout = 5 * time.Second

type runPromptWorkspaceConfig struct {
	Options Options
	Config  config.Connection
}

func startRunPromptClient(ctx context.Context, opts Options) (apicontract.RunPromptService, func() error, error) {
	workspaceConfig, err := resolveRunPromptWorkspaceConfig(opts)
	if err != nil {
		return nil, nil, err
	}
	return startRunPromptClientWithWorkspaceConfig(ctx, workspaceConfig)
}

func startRunPromptClientWithWorkspaceConfig(ctx context.Context, workspaceConfig runPromptWorkspaceConfig) (apicontract.RunPromptService, func() error, error) {
	cfg := workspaceConfig.Config
	opts := workspaceConfig.Options
	sessionID := strings.TrimSpace(opts.SessionID)
	inherited := sessionID == "" && !opts.WorkspaceRootExplicit
	if inherited {
		sessionID = strings.TrimSpace(opts.WorkspaceContextSessionID)
	}
	if sessionID != "" {
		remote, err := attachConfiguredStartupRemote(ctx, cfg)
		if err != nil {
			return nil, nil, err
		}
		bound, err := remoteattach.BindSession(ctx, remote, cfg, sessionID, config.ExplicitPersistenceRootID(cfg))
		if err != nil {
			err = errors.Join(err, remote.Close())
			if inherited {
				err = startupconfig.WorkspaceContextSessionError(sessionID, err)
			}
			return nil, nil, err
		}
		return bound, bound.Close, nil
	}
	runPrompt, closeFn, err := serverattach.AttachRunPrompt(ctx, serverattach.AttachRunPromptRequest{
		Config:           cfg,
		AttachTimeout:    configuredRemoteAttachTimeout,
		DiscoveryTimeout: configuredRemoteWorkspaceDiscoveryTimeout,
		DialProjectView:  dialConfiguredProjectViewRemote,
		DialWorkspace:    dialConfiguredRemote,
	})
	if err != nil {
		var rootMismatch *serverattach.RootMismatchServerError
		if errors.As(err, &rootMismatch) {
			if reason := strings.TrimSpace(rootMismatch.Reason); reason != "" {
				return nil, nil, fmt.Errorf("%w (%s)", errRunServerRootMismatch, reason)
			}
			return nil, nil, errRunServerRootMismatch
		}
		if errors.Is(err, serverattach.ErrNoServerAvailable) {
			return nil, nil, errRunRequiresServer
		}
		return nil, nil, err
	}
	return runPrompt, closeFn, nil
}

func startRuntimeLiveControlClient(ctx context.Context, opts Options) (apicontract.RuntimeLiveControlService, func() error, error) {
	cfg, err := loadRemoteAttachConfig(opts)
	if err != nil {
		return nil, nil, err
	}
	attachCtx, cancel := context.WithTimeout(ctx, configuredRemoteAttachTimeout)
	defer cancel()
	remote, err := dialConfiguredRuntimeLiveControlRemote(attachCtx, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", errRunRequiresServer, err)
	}
	if err := remote.RequireRoot(config.ExplicitPersistenceRootID(cfg)); err != nil {
		return nil, remote.Close, errRunServerRootMismatch
	}
	return remote, remote.Close, nil
}

// errRunRequiresServer is returned when no server is reachable for `kent run`.
var errRunRequiresServer = errors.New("`kent run` can only be used when a server is already running. Start a server with `kent serve` or install a service with `kent service install` to prevent subagents and scripted runs from exiting abruptly if running concurrently with each other")

// errRunServerRootMismatch is returned when a reachable server on the configured
// endpoint serves a different persistence root than the one selected. Starting
// another server on the same address would only hit a bind conflict, so the
// operator must stop/reconfigure the other-root server or target a matching
// endpoint.
var errRunServerRootMismatch = errors.New("a Kent server is running on the configured endpoint but serves a different persistence root than the selected one. Stop or reconfigure that server, or point `--persistence-root`/the configured endpoint at the matching instance, instead of starting another server which would conflict on the same address")

func loadRemoteAttachConfig(opts Options) (config.Connection, error) {
	workspaceConfig, err := resolveRunPromptWorkspaceConfig(opts)
	return workspaceConfig.Config, err
}

func resolveRunPromptWorkspaceConfig(opts Options) (runPromptWorkspaceConfig, error) {
	result, err := startupconfig.ResolveRunPromptConfig(startupConfigRequest(opts))
	if err != nil {
		return runPromptWorkspaceConfig{}, err
	}
	resolvedOpts := opts
	resolvedOpts.WorkspaceRoot = result.WorkspaceRoot
	return runPromptWorkspaceConfig{
		Options: resolvedOpts,
		Config:  result,
	}, nil
}

func startupConfigRequest(opts Options) startupconfig.Request {
	return startupconfig.Request{
		WorkspaceRoot: opts.WorkspaceRoot,
		LoadOptions: config.LoadOptions{
			Theme:      opts.Theme,
			ConfigRoot: opts.ConfigRoot,
		},
	}
}
