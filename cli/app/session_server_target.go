package app

import (
	"context"
	"io"
	"os/exec"
	"time"

	"builder/server/auth"
	serverbootstrap "builder/server/bootstrap"
	"builder/server/serve"
	"builder/shared/client"
	"builder/shared/config"
	"builder/shared/protocol"
)

var launchSessionServerDaemon = startLocalInteractiveSessionDaemon
var startInteractiveEmbeddedSessionServer = startEmbeddedServer
var dialInteractiveRemoteSessionServer = tryDialConfiguredRemoteServer

func remoteAuthHooks(interactor authInteractor) (func(string) string, func(auth.Store) auth.Store) {
	if interactor == nil {
		return nil, nil
	}
	return interactor.LookupEnv, interactor.WrapStore
}

func startSessionServer(ctx context.Context, opts Options, interactor authInteractor) (embeddedServer, error) {
	bypassRemote, err := shouldBypassRemoteStartupForInteractiveOnboarding(opts, interactor)
	if err != nil {
		return nil, err
	}
	if bypassRemote {
		return startInteractiveEmbeddedSessionServer(ctx, opts, interactor)
	}
	if remote, ok, err := dialInteractiveRemoteSessionServer(ctx, opts, interactor); err != nil {
		return nil, err
	} else if ok {
		if err := remote.Reauthenticate(ctx, interactor); err != nil {
			_ = remote.Close()
			return nil, err
		}
		return remote, nil
	}
	lookupEnv, wrapStore := remoteAuthHooks(interactor)
	if remote, closeFn, ok, err := launchSessionServerDaemon(ctx, opts); err == nil && ok {
		cfg, cfgErr := loadSessionServerConfig(opts)
		if cfgErr != nil {
			if closeFn != nil {
				_ = closeFn()
			} else {
				_ = remote.Close()
			}
			return nil, cfgErr
		}
		appServer := newRemoteAppServerWithAuth(remote, cfg, closeFn, lookupEnv, wrapStore)
		if err := appServer.Reauthenticate(ctx, interactor); err != nil {
			_ = appServer.Close()
			return nil, err
		}
		return appServer, nil
	}
	return startInteractiveEmbeddedSessionServer(ctx, opts, interactor)
}

func shouldBypassRemoteStartupForInteractiveOnboarding(opts Options, interactor authInteractor) (bool, error) {
	if interactor == nil || !interactor.Interactive() {
		return false, nil
	}
	cfg, err := loadSessionServerConfig(opts)
	if err != nil {
		return false, err
	}
	return !cfg.Source.SettingsFileExists, nil
}

func tryDialConfiguredRemoteServer(ctx context.Context, opts Options, interactor authInteractor) (*remoteAppServer, bool, error) {
	remote, ok := tryDialMatchingConfiguredRemoteAllowUnregistered(ctx, opts, configuredRemoteSupportsInteractiveSession, nil)
	if !ok {
		return nil, false, nil
	}
	cfg, err := loadSessionServerConfig(opts)
	if err != nil {
		_ = remote.Close()
		return nil, false, err
	}
	lookupEnv, wrapStore := remoteAuthHooks(interactor)
	return newRemoteAppServerWithAuth(remote, cfg, nil, lookupEnv, wrapStore), true, nil
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
	serve.ReleaseTestListenReservation(config.ServerListenAddress(cfg))
	args := append([]string{execPath}, buildServeArgsFunc("", opts)...)
	cmd := exec.CommandContext(context.Background(), args[0], args[1:]...)
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.Env = buildServeEnvFunc(cfg)
	if err := cmd.Start(); err != nil {
		return nil, nil, false, err
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Wait()
	}()
	failureClose := newOwnedDaemonClose(nil, cmd, errCh)
	childPID := cmd.Process.Pid
	deadline := time.Now().Add(10 * time.Second)
	for {
		if remote, ok := tryDialMatchingConfiguredRemoteAllowUnregistered(ctx, opts, configuredRemoteSupportsInteractiveSession, func(identity protocol.ServerIdentity) bool {
			return identity.PID == childPID
		}); ok {
			return remote, newOwnedDaemonClose(remote, cmd, errCh), true, nil
		}
		select {
		case <-ctx.Done():
			_ = failureClose()
			return nil, nil, false, ctx.Err()
		case err := <-errCh:
			return nil, nil, false, err
		default:
		}
		if time.Now().After(deadline) {
			_ = failureClose()
			return nil, nil, false, context.DeadlineExceeded
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func loadSessionServerConfig(opts Options) (config.App, error) {
	workspaceRoot, err := resolveCLIWorkspaceRoot(opts)
	if err != nil {
		return config.App{}, err
	}
	plan, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{
		WorkspaceRoot:         workspaceRoot,
		WorkspaceRootExplicit: opts.WorkspaceRootExplicit,
		SessionID:             opts.SessionID,
		OpenAIBaseURL:         opts.OpenAIBaseURL,
		OpenAIBaseURLExplicit: opts.OpenAIBaseURLExplicit,
		LoadOptions: config.LoadOptions{
			Model:               opts.Model,
			ProviderOverride:    opts.ProviderOverride,
			ThinkingLevel:       opts.ThinkingLevel,
			Theme:               opts.Theme,
			ModelTimeoutSeconds: opts.ModelTimeoutSeconds,
			Tools:               opts.Tools,
		},
	})
	if err != nil {
		return config.App{}, err
	}
	return plan.Config, nil
}
