package app

import (
	"context"

	"builder/cli/app/internal/daemonlaunch"
	"builder/cli/app/internal/remoteattach"
	"builder/cli/app/internal/sessiontarget"
	"builder/cli/app/internal/startupconfig"
	"builder/cli/app/internal/targetstartup"
	"builder/shared/client"
	"builder/shared/config"
	"builder/shared/protocol"
)

var launchSessionServerDaemon = startLocalInteractiveSessionDaemon
var startInteractiveEmbeddedSessionServer = startEmbeddedServer
var dialInteractiveRemoteSessionServer = tryDialConfiguredRemoteServer

func startSessionServer(ctx context.Context, opts Options, interactor authInteractor) (interactiveSessionServer, error) {
	target, err := targetstartup.Resolve[interactiveSessionServer, *client.Remote](ctx, targetstartup.Request[interactiveSessionServer, *client.Remote]{
		BypassRemote: func(context.Context) (bool, error) {
			return shouldBypassRemoteStartupForInteractiveOnboarding(opts, interactor)
		},
		DialRemote: func(ctx context.Context) (targetstartup.Target[interactiveSessionServer], bool, error) {
			remote, ok, err := dialInteractiveRemoteSessionServer(ctx, opts, interactor)
			if err != nil || !ok {
				return targetstartup.Target[interactiveSessionServer]{}, ok, err
			}
			return interactiveRemoteSessionTarget(remote), true, nil
		},
		LaunchDaemon: func(ctx context.Context) (targetstartup.DaemonTarget[*client.Remote], bool, error) {
			remote, closeFn, ok, err := launchSessionServerDaemon(ctx, opts)
			if err != nil || !ok {
				return targetstartup.DaemonTarget[*client.Remote]{}, ok, err
			}
			return targetstartup.DaemonTarget[*client.Remote]{Value: remote, Close: closeFn}, true, nil
		},
		WrapDaemon: func(_ context.Context, daemon targetstartup.DaemonTarget[*client.Remote]) (targetstartup.Target[interactiveSessionServer], error) {
			return sessiontarget.WrapDaemon(daemon, sessiontarget.WrapDaemonRequest[interactiveSessionServer, *client.Remote]{
				LoadConfig: func() (config.App, error) {
					return loadSessionServerConfig(opts)
				},
				NewRemote: func(remote *client.Remote, cfg config.App, closeFn func() error) interactiveSessionServer {
					return newRemoteAppServerWithAuth(remote, cfg, closeFn)
				},
			})
		},
		StartEmbedded: func(ctx context.Context) (targetstartup.Target[interactiveSessionServer], error) {
			server, err := startInteractiveEmbeddedSessionServer(ctx, opts, interactor)
			if err != nil {
				return targetstartup.Target[interactiveSessionServer]{}, err
			}
			return targetstartup.Target[interactiveSessionServer]{Value: server, Close: server.Close}, nil
		},
		Validate: func(ctx context.Context, source targetstartup.Source, target interactiveSessionServer) error {
			return sessiontarget.Validate(ctx, source, target, func(ctx context.Context, server interactiveSessionServer) error {
				return server.Reauthenticate(ctx, interactor)
			})
		},
	})
	if err != nil {
		return nil, err
	}
	return target.Value, nil
}

func interactiveRemoteSessionTarget(server interactiveSessionServer) targetstartup.Target[interactiveSessionServer] {
	return sessiontarget.Remote(server)
}

func shouldBypassRemoteStartupForInteractiveOnboarding(opts Options, interactor authInteractor) (bool, error) {
	if interactor == nil || !interactor.Interactive() {
		return false, nil
	}
	cfg, err := loadSessionServerConfig(opts)
	if err != nil {
		return false, err
	}
	return sessiontarget.ShouldBypassRemoteForFirstRun(interactor.Interactive(), cfg.Source.SettingsFileExists), nil
}

func tryDialConfiguredRemoteServer(ctx context.Context, opts Options, _ authInteractor) (*remoteAppServer, bool, error) {
	remote, ok := tryDialMatchingConfiguredRemoteAllowUnregistered(ctx, opts, remoteattach.SupportsInteractiveSession, nil)
	if !ok {
		return nil, false, nil
	}
	cfg, err := loadSessionServerConfig(opts)
	if err != nil {
		_ = remote.Close()
		return nil, false, err
	}
	return newRemoteAppServerWithAuth(remote, cfg, nil), true, nil
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
