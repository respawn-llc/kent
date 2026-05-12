package app

import (
	"context"

	"builder/cli/app/internal/daemonlaunch"
	"builder/cli/app/internal/remoteattach"
	"builder/cli/app/internal/serverattach"
	"builder/cli/app/internal/sessiontarget"
	"builder/cli/app/internal/startupconfig"
	"builder/shared/client"
	"builder/shared/config"
	"builder/shared/protocol"
)

var launchSessionServerDaemon = startLocalInteractiveSessionDaemon
var startInteractiveEmbeddedSessionServer = startEmbeddedServer

func startSessionServer(ctx context.Context, opts Options, interactor authInteractor) (interactiveSessionServer, error) {
	cfg, err := loadSessionServerConfig(opts)
	if err != nil {
		return nil, err
	}
	target, err := serverattach.Resolve[interactiveSessionServer](ctx, serverattach.Request[interactiveSessionServer]{
		Mode:   serverattach.ModeInteractive,
		Remote: serverAttachRemotePolicy(cfg, remoteattach.SupportsInteractiveSession, false),
		BypassRemote: func(context.Context) (bool, error) {
			return shouldBypassRemoteStartupForInteractiveOnboardingWithConfig(cfg, interactor), nil
		},
		LaunchDaemon: func(ctx context.Context, _ serverattach.LaunchedRemoteDialer) (serverattach.DaemonTarget[*client.Remote], bool, error) {
			remote, closeFn, ok, err := launchSessionServerDaemon(ctx, opts)
			if err != nil || !ok {
				return serverattach.DaemonTarget[*client.Remote]{}, ok, err
			}
			return serverattach.DaemonTarget[*client.Remote]{Value: remote, Close: closeFn}, true, nil
		},
		WrapRemote: func(remote *client.Remote, cfg config.App, closeFn func() error, ownership serverattach.OwnershipState) (serverattach.Target[interactiveSessionServer], error) {
			server := newRemoteAppServerWithAuth(remote, cfg, closeFn, ownership == serverattach.OwnershipLaunchedDaemon)
			return serverattach.Target[interactiveSessionServer]{Value: server, Close: server.Close}, nil
		},
		StartEmbedded: func(ctx context.Context) (serverattach.Target[interactiveSessionServer], error) {
			server, err := startInteractiveEmbeddedSessionServer(ctx, opts, interactor)
			if err != nil {
				return serverattach.Target[interactiveSessionServer]{}, err
			}
			return serverattach.Target[interactiveSessionServer]{Value: server, Close: server.Close}, nil
		},
		Validate: func(ctx context.Context, resolution serverattach.Resolution[interactiveSessionServer]) (serverattach.AuthReadiness, error) {
			if resolution.Source == serverattach.SourceEmbeddedFallback {
				return serverattach.AuthReadinessUnchecked, nil
			}
			if err := resolution.Value.Reauthenticate(ctx, interactor); err != nil {
				return serverattach.AuthReadinessUnchecked, err
			}
			return serverattach.AuthReadinessValidated, nil
		},
	})
	if err != nil {
		return nil, err
	}
	return target.Value, nil
}

func shouldBypassRemoteStartupForInteractiveOnboarding(opts Options, interactor authInteractor) (bool, error) {
	cfg, err := loadSessionServerConfig(opts)
	if err != nil {
		return false, err
	}
	return shouldBypassRemoteStartupForInteractiveOnboardingWithConfig(cfg, interactor), nil
}

func shouldBypassRemoteStartupForInteractiveOnboardingWithConfig(cfg config.App, interactor authInteractor) bool {
	if interactor == nil || !interactor.Interactive() {
		return false
	}
	return sessiontarget.ShouldBypassRemoteForFirstRun(interactor.Interactive(), cfg.Source.SettingsFileExists)
}

func startLocalInteractiveSessionDaemon(ctx context.Context, opts Options) (*client.Remote, func() error, bool, error) {
	cfg, err := loadRemoteAttachConfig(opts)
	if err != nil {
		return nil, nil, false, err
	}
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
			remote, ok := tryDialMatchingConfiguredRemoteAllowUnregistered(ctx, opts, remoteattach.SupportsInteractiveSession, func(identity protocol.ServerIdentity) bool {
				return identity.PID == childPID
			})
			return remote, ok, nil
		},
		CloseTarget: func(remote *client.Remote) error {
			if remote == nil {
				return nil
			}
			return remote.Close()
		},
	})
}

func loadSessionServerConfig(opts Options) (config.App, error) {
	return startupconfig.ResolveSessionConfig(startupConfigRequest(opts))
}
