package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"core/cli/app/internal/remoteattach"
	"core/cli/app/internal/serverattach"
	"core/cli/app/internal/startupconfig"
	"core/shared/client"
	"core/shared/config"
	"core/shared/protocol"
)

var dialConfiguredRemote = client.DialConfiguredRemoteForProjectWorkspaceID
var dialConfiguredProjectViewRemote = func(ctx context.Context, cfg config.App) (remoteattach.ProjectViewRemote, error) {
	return client.DialConfiguredRemote(ctx, cfg)
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
	// kent run is a pure client: it attaches to an already-running server and
	// never starts one of its own (embedded or launched daemon). Omitting
	// LaunchDaemon and StartEmbedded makes Resolve return
	// serverattach.ErrNoServerAvailable when no server can be attached, which we
	// translate into errRunRequiresServer below. This keeps concurrent kent run
	// invocations safe: they share one standing server instead of each owning a
	// daemon that gets killed when the first run exits.
	target, err := serverattach.Resolve[serverattach.RunPromptTarget](ctx, serverattach.Request[serverattach.RunPromptTarget]{
		Mode:   serverattach.ModeHeadless,
		Remote: serverAttachRemotePolicy(cfg, remoteattach.SupportsRunPrompt, true),
		WrapRemote: func(remote *client.Remote, cfg config.App, closeFn func() error, _ serverattach.OwnershipState) (serverattach.Target[serverattach.RunPromptTarget], error) {
			target := serverattach.RunPromptRemoteWithClose(remote, cfg, closeFn)
			return serverattach.Target[serverattach.RunPromptTarget]{Value: target.Value, Close: target.Close}, nil
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
		if errors.Is(err, serverattach.ErrNoServerAvailable) {
			return nil, nil, errRunRequiresServer
		}
		return nil, nil, err
	}
	return target.Value.Client, target.Close, nil
}

// errRunRequiresServer is returned when `kent run` cannot attach to a server
// because none is running. kent run is a pure client and never starts a server
// of its own, so a server must already be available.
var errRunRequiresServer = errors.New("`kent run` can only be used when a server is already running. Start a server with `kent serve` or install a service with `kent service install` to prevent subagents and scripted runs from exiting abruptly if running concurrently with each other")

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
			ConfigRoot:          opts.ConfigRoot,
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
		RootID:           explicitPersistenceRootID(cfg),
	}
}

// explicitPersistenceRootID returns the persistence-root id an attached server
// must report when the operator explicitly selected a non-default root (via the
// --persistence-root flag or KENT_PERSISTENCE_ROOT). For the default root it
// returns "" so attach behavior is unchanged. This prevents an isolated-root
// invocation from silently attaching to a different instance that happens to be
// listening on the same configured TCP endpoint. The source label is set by
// config.Load (see resolveConfigRoot): "default", "flag", or "env".
func explicitPersistenceRootID(cfg config.App) string {
	switch cfg.Source.Sources["persistence_root"] {
	case "flag", "env":
		return config.PersistenceRootHash(cfg.PersistenceRoot)
	default:
		return ""
	}
}
