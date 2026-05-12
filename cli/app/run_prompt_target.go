package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"builder/cli/app/internal/daemonlaunch"
	"builder/cli/app/internal/remoteattach"
	"builder/cli/app/internal/runprompttarget"
	"builder/cli/app/internal/servecommand"
	"builder/cli/app/internal/serverattach"
	"builder/cli/app/internal/startupconfig"
	"builder/shared/client"
	"builder/shared/config"
	"builder/shared/protocol"
)

var launchRunPromptDaemon = startLocalRunPromptDaemon
var dialConfiguredRemote = client.DialConfiguredRemoteForProjectWorkspaceID
var dialConfiguredProjectViewRemote = func(ctx context.Context, cfg config.App) (remoteattach.ProjectViewRemote, error) {
	return client.DialConfiguredRemote(ctx, cfg)
}
var resolveDaemonExecutablePath = servecommand.ExecutablePath
var buildServeArgsFunc = func(_ string, _ Options) []string { return servecommand.Args() }
var buildServeEnvFunc = servecommand.Env
var releaseServeReservationFunc = servecommand.ReleaseReservation

var configuredRemoteAttachTimeout = 500 * time.Millisecond
var configuredRemoteWorkspaceDiscoveryTimeout = 5 * time.Second

type configuredProjectViewRemote = remoteattach.ProjectViewRemote

type runPromptWorkspaceConfig struct {
	Options Options
	Config  config.App
}

func startRunPromptClient(ctx context.Context, opts Options) (client.RunPromptClient, func() error, error) {
	workspaceConfig, err := resolveRunPromptWorkspaceConfig(opts)
	if err != nil {
		return nil, nil, err
	}
	opts = workspaceConfig.Options
	cfg := workspaceConfig.Config
	target, err := serverattach.Resolve[runprompttarget.Target](ctx, serverattach.Request[runprompttarget.Target]{
		Mode:   serverattach.ModeHeadless,
		Remote: serverAttachRemotePolicy(cfg, remoteattach.SupportsRunPrompt, true),
		LaunchDaemon: func(ctx context.Context, _ serverattach.LaunchedRemoteDialer) (serverattach.DaemonTarget[*client.Remote], bool, error) {
			remote, closeFn, ok, err := launchRunPromptDaemon(ctx, opts)
			if err != nil || !ok {
				return serverattach.DaemonTarget[*client.Remote]{}, ok, err
			}
			return serverattach.DaemonTarget[*client.Remote]{Value: remote, Close: closeFn}, true, nil
		},
		WrapRemote: func(remote *client.Remote, cfg config.App, closeFn func() error, _ serverattach.OwnershipState) (serverattach.Target[runprompttarget.Target], error) {
			target := runprompttarget.RemoteWithClose(remote, cfg, closeFn)
			return serverattach.Target[runprompttarget.Target]{Value: target.Value, Close: target.Close}, nil
		},
		StartEmbedded: func(ctx context.Context) (serverattach.Target[runprompttarget.Target], error) {
			server, err := startEmbeddedServer(ctx, opts, newHeadlessAuthInteractor())
			if err != nil {
				return serverattach.Target[runprompttarget.Target]{}, err
			}
			return runPromptTargetForEmbeddedAttachment(server)
		},
		Validate: func(ctx context.Context, resolution serverattach.Resolution[runprompttarget.Target]) (serverattach.AuthReadiness, error) {
			if err := runprompttarget.Validate(ctx, runprompttarget.ValidateRequest{
				Target: resolution.Value,
				Config: cfg,
				EnsureAuthReady: func(ctx context.Context, auth client.AuthBootstrapClient) error {
					return ensureRemoteAuthReady(ctx, auth, cfg.Settings, newHeadlessAuthInteractor())
				},
			}); err != nil {
				return serverattach.AuthReadinessUnchecked, err
			}
			if resolution.Value.Auth == nil {
				return serverattach.AuthReadinessUnchecked, nil
			}
			return serverattach.AuthReadinessValidated, nil
		},
	})
	if err != nil {
		return nil, nil, err
	}
	return target.Value.Client, target.Close, nil
}

type embeddedRunPromptAttachment interface {
	RunPromptClient() client.RunPromptClient
	ProjectID() string
	Close() error
}

func runPromptTargetForEmbeddedAttachment(server embeddedRunPromptAttachment) (serverattach.Target[runprompttarget.Target], error) {
	if server == nil {
		return serverattach.Target[runprompttarget.Target]{}, errors.New("embedded run prompt attachment is required")
	}
	runPrompt := server.RunPromptClient()
	if runPrompt == nil {
		return serverattach.Target[runprompttarget.Target]{}, errors.New("embedded run prompt client is required")
	}
	target := runprompttarget.Embedded(runPrompt, server.ProjectID, server.Close)
	return serverattach.Target[runprompttarget.Target]{Value: target.Value, Close: target.Close}, nil
}

func tryDialConfiguredRunPromptRemote(ctx context.Context, opts Options) (*client.Remote, bool, error) {
	return tryDialMatchingConfiguredRunPromptRemote(ctx, opts, nil)
}

func tryDialMatchingConfiguredRunPromptRemote(ctx context.Context, opts Options, accept func(protocol.ServerIdentity) bool) (*client.Remote, bool, error) {
	workspaceConfig, err := resolveRunPromptWorkspaceConfig(opts)
	if err != nil {
		return nil, false, err
	}
	return tryDialMatchingConfiguredRunPromptRemoteWithConfig(ctx, workspaceConfig.Options, workspaceConfig.Config, accept)
}

func tryDialMatchingConfiguredRunPromptRemoteWithConfig(ctx context.Context, _ Options, cfg config.App, accept func(protocol.ServerIdentity) bool) (*client.Remote, bool, error) {
	return serverattach.DialRemote(ctx, serverattach.ModeHeadless, serverAttachRemotePolicy(cfg, remoteattach.SupportsRunPrompt, true), accept)
}

func tryDialConfiguredRemote(ctx context.Context, opts Options, supports func(protocol.CapabilityFlags) bool) (*client.Remote, bool) {
	return tryDialMatchingConfiguredRemoteWithRequirement(ctx, opts, supports, nil, true)
}

func tryDialMatchingConfiguredRemoteAllowUnregistered(ctx context.Context, opts Options, supports func(protocol.CapabilityFlags) bool, accept func(protocol.ServerIdentity) bool) (*client.Remote, bool) {
	return tryDialMatchingConfiguredRemoteWithRequirement(ctx, opts, supports, accept, false)
}

func tryDialMatchingConfiguredRemote(ctx context.Context, opts Options, supports func(protocol.CapabilityFlags) bool, accept func(protocol.ServerIdentity) bool) (*client.Remote, bool) {
	return tryDialMatchingConfiguredRemoteWithRequirement(ctx, opts, supports, accept, true)
}

func tryDialMatchingConfiguredRemoteWithRequirement(ctx context.Context, opts Options, supports func(protocol.CapabilityFlags) bool, accept func(protocol.ServerIdentity) bool, requireRegistered bool) (*client.Remote, bool) {
	cfg, err := loadRemoteAttachConfig(opts)
	if err != nil {
		return nil, false
	}
	remote, ok, err := serverattach.DialRemote(ctx, serverattach.ModeInteractive, serverAttachRemotePolicy(cfg, supports, requireRegistered), accept)
	if err != nil {
		return nil, false
	}
	return remote, ok
}

func startLocalRunPromptDaemon(ctx context.Context, opts Options) (*client.Remote, func() error, bool, error) {
	workspaceConfig, err := resolveRunPromptWorkspaceConfig(opts)
	if err != nil {
		return nil, nil, false, err
	}
	opts = workspaceConfig.Options
	cfg := workspaceConfig.Config
	execPath, ok := resolveDaemonExecutablePath()
	if !ok {
		return nil, nil, false, nil
	}
	releaseServeReservationFunc(cfg)
	return daemonlaunch.Launch[*client.Remote](ctx, daemonlaunch.Request[*client.Remote]{
		ExecutablePath: execPath,
		Args:           buildServeArgsFunc("", opts),
		Env:            buildServeEnvFunc(cfg),
		Dial: func(ctx context.Context, childPID int) (*client.Remote, bool, error) {
			return tryDialMatchingConfiguredRunPromptRemoteWithConfig(ctx, opts, cfg, func(identity protocol.ServerIdentity) bool {
				return identity.PID == childPID
			})
		},
		CloseTarget: func(remote *client.Remote) error {
			if remote == nil {
				return nil
			}
			return remote.Close()
		},
	})
}

func loadRemoteAttachConfig(opts Options) (config.App, error) {
	workspaceConfig, err := resolveRunPromptWorkspaceConfig(opts)
	return workspaceConfig.Config, err
}

func resolveRunPromptWorkspaceConfig(opts Options) (runPromptWorkspaceConfig, error) {
	result, err := startupconfig.ResolveRunPromptConfig(startupConfigRequest(opts))
	if err != nil {
		return runPromptWorkspaceConfig{}, err
	}
	resolvedOpts := opts
	if strings.TrimSpace(result.ResolvedWorkspaceRoot) != "" && result.ResolvedWorkspaceRoot != opts.WorkspaceRoot {
		resolvedOpts.WorkspaceRoot = result.ResolvedWorkspaceRoot
	}
	return runPromptWorkspaceConfig{Options: resolvedOpts, Config: result.Config}, nil
}

func startupConfigRequest(opts Options) startupconfig.Request {
	return startupconfig.Request{
		WorkspaceRoot:             opts.WorkspaceRoot,
		WorkspaceRootExplicit:     opts.WorkspaceRootExplicit,
		SessionID:                 opts.SessionID,
		WorkspaceContextSessionID: opts.WorkspaceContextSessionID,
		OpenAIBaseURL:             opts.OpenAIBaseURL,
		OpenAIBaseURLExplicit:     opts.OpenAIBaseURLExplicit,
		LoadOptions: config.LoadOptions{
			Model:               opts.Model,
			ProviderOverride:    opts.ProviderOverride,
			ThinkingLevel:       opts.ThinkingLevel,
			Theme:               opts.Theme,
			ModelTimeoutSeconds: opts.ModelTimeoutSeconds,
			Tools:               opts.Tools,
		},
	}
}

func serverAttachRemotePolicy(cfg config.App, supports remoteattach.Supports, requireBound bool) serverattach.RemotePolicy {
	return serverattach.RemotePolicy{
		Config:           cfg,
		AttachTimeout:    configuredRemoteAttachTimeout,
		DiscoveryTimeout: configuredRemoteWorkspaceDiscoveryTimeout,
		DialProjectView:  dialConfiguredProjectViewRemote,
		DialWorkspace:    dialConfiguredRemote,
		Supports:         supports,
		RequireBound:     requireBound,
	}
}
