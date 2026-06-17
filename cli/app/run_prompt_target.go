package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"core/cli/app/internal/daemonlaunch"
	"core/cli/app/internal/remoteattach"
	"core/cli/app/internal/serverattach"
	"core/cli/app/internal/startupconfig"
	serverstartup "core/server/startup"
	"core/shared/client"
	"core/shared/config"
	"core/shared/protocol"
)

var launchRunPromptDaemon = startLocalRunPromptDaemon
var dialConfiguredRemote = client.DialConfiguredRemoteForProjectWorkspaceID
var dialConfiguredProjectViewRemote = func(ctx context.Context, cfg config.App) (remoteattach.ProjectViewRemote, error) {
	return client.DialConfiguredRemote(ctx, cfg)
}
var resolveDaemonExecutablePath = startupconfig.ServeExecutablePath
var buildServeArgsFunc = func(_ string, _ Options) []string { return startupconfig.ServeArgs() }
var buildServeEnvFunc = startupconfig.ServeEnv
var releaseServeReservationFunc = func(cfg config.App) {
	serverstartup.ReleaseTestListenReservation(net.JoinHostPort(cfg.Settings.ServerHost, strconv.Itoa(cfg.Settings.ServerPort)))
}

var configuredRemoteAttachTimeout = 500 * time.Millisecond
var configuredRemoteWorkspaceDiscoveryTimeout = 5 * time.Second

type configuredProjectViewRemote = remoteattach.ProjectViewRemote

type runPromptWorkspaceConfig struct {
	Options          Options
	Config           config.App
	ContextAgentRole string
}

func startRunPromptClient(ctx context.Context, opts Options) (client.RunPromptClient, func() error, error) {
	workspaceConfig, err := resolveRunPromptWorkspaceConfig(opts)
	if err != nil {
		return nil, nil, err
	}
	opts = workspaceConfig.Options
	cfg := workspaceConfig.Config
	kentSessionCaller := strings.TrimSpace(opts.WorkspaceContextSessionID) != ""
	contextAgentRole := strings.TrimSpace(workspaceConfig.ContextAgentRole)
	if err := validateRunPromptAgentRole(cfg.Settings, opts.AgentRole, kentSessionCaller, contextAgentRole); err != nil {
		return nil, nil, err
	}
	target, err := serverattach.Resolve[serverattach.RunPromptTarget](ctx, serverattach.Request[serverattach.RunPromptTarget]{
		Mode:   serverattach.ModeHeadless,
		Remote: serverAttachRemotePolicy(cfg, remoteattach.SupportsRunPrompt, true),
		LaunchDaemon: func(ctx context.Context, _ serverattach.LaunchedRemoteDialer) (serverattach.DaemonTarget[*client.Remote], bool, error) {
			remote, closeFn, ok, err := launchRunPromptDaemon(ctx, opts)
			if err != nil || !ok {
				return serverattach.DaemonTarget[*client.Remote]{}, ok, err
			}
			return serverattach.DaemonTarget[*client.Remote]{Value: remote, Close: closeFn}, true, nil
		},
		WrapRemote: func(remote *client.Remote, cfg config.App, closeFn func() error, _ serverattach.OwnershipState) (serverattach.Target[serverattach.RunPromptTarget], error) {
			target := serverattach.RunPromptRemoteWithClose(remote, cfg, closeFn)
			return serverattach.Target[serverattach.RunPromptTarget]{Value: target.Value, Close: target.Close}, nil
		},
		StartEmbedded: func(ctx context.Context) (serverattach.Target[serverattach.RunPromptTarget], error) {
			server, err := startEmbeddedServer(ctx, opts, newHeadlessAuthInteractor(), false)
			if err != nil {
				return serverattach.Target[serverattach.RunPromptTarget]{}, err
			}
			return runPromptTargetForEmbeddedAttachment(server)
		},
		Validate: func(ctx context.Context, resolution serverattach.Resolution[serverattach.RunPromptTarget]) (serverattach.AuthReadiness, error) {
			if err := serverattach.ValidateRunPromptTarget(ctx, serverattach.RunPromptValidateRequest{
				Target: resolution.Value,
				Config: cfg,
				EnsureAuthReady: func(ctx context.Context, auth client.AuthBootstrapClient) error {
					return ensureRemoteAuthReady(ctx, auth, cfg.Settings, newHeadlessAuthInteractor(), false)
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

const nonCallableSubagentRoleMessage = "User has disallowed calling this agent by other agents like you. Do not try to circumvent this, pick another suitable agent or do the work manually and let the user know your desire to use the subagent at the end of the task"

// errNonCallableSubagentRole and errUnrecognizedSubagentRole classify
// run-prompt agent-role validation failures. Callers and tests match these with
// errors.Is rather than comparing rendered message text.
var (
	errNonCallableSubagentRole  = errors.New(nonCallableSubagentRoleMessage)
	errUnrecognizedSubagentRole = errors.New("unrecognized subagent role")
)

func validateRunPromptAgentRole(settings config.Settings, rawRole string, kentSessionCaller bool, contextAgentRole string) error {
	roleName := config.NormalizeSubagentSelector(rawRole)
	if roleName == "" {
		if strings.TrimSpace(rawRole) != "" && !config.IsReservedSubagentRoleName(rawRole) {
			return errors.New("invalid agent role " + strconv.Quote(rawRole))
		}
		if kentSessionCaller {
			if err := validateContextAgentRoleCallable(settings, contextAgentRole); err != nil {
				return err
			}
		}
		return nil
	}
	role, exists := settings.Subagents[roleName]
	if !exists && roleName != config.BuiltInSubagentRoleFast {
		return fmt.Errorf("%w: %s. It may have been removed by the user during the session. Available roles: [%s]", errUnrecognizedSubagentRole, strconv.Quote(roleName), strings.Join(config.AvailableSubagentRoleNames(settings, kentSessionCaller), ", "))
	}
	if kentSessionCaller && !config.SubagentRoleCallable(role) {
		return errNonCallableSubagentRole
	}
	return nil
}

func validateContextAgentRoleCallable(settings config.Settings, rawRole string) error {
	roleName := config.NormalizeSubagentSelector(rawRole)
	if roleName == "" {
		return nil
	}
	role, exists := settings.Subagents[roleName]
	if !exists && roleName != config.BuiltInSubagentRoleFast {
		return fmt.Errorf("%w: %s. It may have been removed by the user during the session. Available roles: [%s]", errUnrecognizedSubagentRole, strconv.Quote(roleName), strings.Join(config.AvailableSubagentRoleNames(settings, true), ", "))
	}
	if !config.SubagentRoleCallable(role) {
		return errNonCallableSubagentRole
	}
	return nil
}

type embeddedRunPromptAttachment interface {
	RunPromptClient() client.RunPromptClient
	ProjectID() string
	Close() error
}

func runPromptTargetForEmbeddedAttachment(server embeddedRunPromptAttachment) (serverattach.Target[serverattach.RunPromptTarget], error) {
	if server == nil {
		return serverattach.Target[serverattach.RunPromptTarget]{}, errors.New("embedded run prompt attachment is required")
	}
	runPrompt := server.RunPromptClient()
	if runPrompt == nil {
		return serverattach.Target[serverattach.RunPromptTarget]{}, errors.New("embedded run prompt client is required")
	}
	target := serverattach.RunPromptEmbedded(runPrompt, server.ProjectID, server.Close)
	return serverattach.Target[serverattach.RunPromptTarget]{Value: target.Value, Close: target.Close}, nil
}

func tryDialMatchingConfiguredRunPromptRemote(ctx context.Context, opts Options, accept func(protocol.ServerIdentity) bool) (*client.Remote, bool, error) {
	workspaceConfig, err := resolveRunPromptWorkspaceConfig(opts)
	if err != nil {
		return nil, false, err
	}
	return serverattach.DialRemote(ctx, serverattach.ModeHeadless, serverAttachRemotePolicy(workspaceConfig.Config, remoteattach.SupportsRunPrompt, true), accept)
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
			return serverattach.DialRemote(ctx, serverattach.ModeHeadless, serverAttachRemotePolicy(cfg, remoteattach.SupportsRunPrompt, true), func(identity protocol.ServerIdentity) bool {
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
	return runPromptWorkspaceConfig{Options: resolvedOpts, Config: result.Config, ContextAgentRole: result.ContextAgentRole}, nil
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
