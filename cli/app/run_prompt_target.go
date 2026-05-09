package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	serverbootstrap "builder/server/bootstrap"
	"builder/server/serve"
	"builder/server/session"
	"builder/shared/client"
	"builder/shared/config"
	"builder/shared/protocol"
	"builder/shared/serverapi"
	"builder/shared/sessionenv"
)

var launchRunPromptDaemon = startLocalRunPromptDaemon
var dialConfiguredRemote = client.DialConfiguredRemoteForProjectWorkspaceID
var dialConfiguredProjectViewRemote = func(ctx context.Context, cfg config.App) (configuredProjectViewRemote, error) {
	return client.DialConfiguredRemote(ctx, cfg)
}
var resolveDaemonExecutablePath = daemonExecutablePath
var buildServeArgsFunc = func(_ string, opts Options) []string { return buildServeArgs(opts) }
var buildServeEnvFunc = buildServeEnv
var terminateOwnedDaemonProcess = func(process *os.Process) error {
	if process == nil {
		return nil
	}
	if goruntime.GOOS == "windows" {
		return process.Kill()
	}
	return process.Signal(os.Interrupt)
}
var forceKillOwnedDaemonProcess = func(process *os.Process) error {
	if process == nil {
		return nil
	}
	return process.Kill()
}

const launchedDaemonShutdownTimeout = 5 * time.Second

var configuredRemoteAttachTimeout = 500 * time.Millisecond
var configuredRemoteWorkspaceDiscoveryTimeout = 5 * time.Second

var errWorkspaceNotRegistered = serverapi.ErrWorkspaceNotRegistered

type configuredProjectViewRemote interface {
	client.ProjectViewClient
	Close() error
	Identity() protocol.ServerIdentity
}

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
	if remote, ok, err := tryDialMatchingConfiguredRunPromptRemoteWithConfig(ctx, opts, cfg, nil); err != nil {
		return nil, nil, err
	} else if ok {
		if err := ensureRemoteAuthReady(ctx, remote, cfg.Settings, newHeadlessAuthInteractor()); err != nil {
			_ = remote.Close()
			return nil, nil, err
		}
		return remote, remote.Close, nil
	}
	launchErr := error(nil)
	if remote, closeFn, ok, err := launchRunPromptDaemon(ctx, opts); err != nil {
		launchErr = err
	} else if ok {
		if err := ensureRemoteAuthReady(ctx, remote, cfg.Settings, newHeadlessAuthInteractor()); err != nil {
			_ = closeFn()
			return nil, nil, err
		}
		if strings.TrimSpace(remote.ProjectID()) == "" {
			_ = closeFn()
			return nil, nil, headlessWorkspaceRegistrationError(cfg.WorkspaceRoot)
		}
		return remote, closeFn, nil
	}
	server, err := startEmbeddedServer(ctx, opts, newHeadlessAuthInteractor())
	if err != nil {
		if launchErr != nil {
			return nil, nil, errors.Join(launchErr, err)
		}
		return nil, nil, err
	}
	if strings.TrimSpace(server.ProjectID()) == "" {
		_ = server.Close()
		return nil, nil, headlessWorkspaceRegistrationError(cfg.WorkspaceRoot)
	}
	return server.RunPromptClient(), server.Close, nil
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

func tryDialMatchingConfiguredRunPromptRemoteWithConfig(ctx context.Context, opts Options, cfg config.App, accept func(protocol.ServerIdentity) bool) (*client.Remote, bool, error) {
	attachCtx, cancel := context.WithTimeout(ctx, configuredRemoteAttachTimeout)
	defer cancel()
	projectViews, err := dialConfiguredProjectViewRemote(attachCtx, cfg)
	if err != nil {
		return nil, false, nil
	}
	if accept != nil && !accept(projectViews.Identity()) {
		_ = projectViews.Close()
		return nil, false, nil
	}
	if !configuredRemoteSupportsRunPrompt(projectViews.Identity().Capabilities) {
		_ = projectViews.Close()
		return nil, false, nil
	}
	discoveryCtx, discoveryCancel := context.WithTimeout(ctx, configuredRemoteWorkspaceDiscoveryTimeout)
	plan, err := projectViews.PlanWorkspaceBinding(discoveryCtx, serverapi.ProjectBindingPlanRequest{Path: cfg.WorkspaceRoot, Mode: serverapi.ProjectBindingPlanModeHeadless})
	discoveryCancel()
	if err != nil {
		_ = projectViews.Close()
		return nil, true, err
	}
	switch plan.Kind {
	case serverapi.ProjectBindingPlanKindBound:
		if plan.Binding == nil {
			_ = projectViews.Close()
			return nil, true, errors.New("resolved project binding is required")
		}
		_ = projectViews.Close()
		remote, err := dialConfiguredRemoteWorkspace(ctx, cfg, plan.Binding.ProjectID, plan.Binding.WorkspaceID)
		if err != nil {
			return nil, true, err
		}
		return remote, true, nil
	case serverapi.ProjectBindingPlanKindLocalUnbound:
		_ = projectViews.Close()
		return nil, true, headlessWorkspaceRegistrationError(cfg.WorkspaceRoot)
	case serverapi.ProjectBindingPlanKindHeadlessRemoteAmbiguous:
		_ = projectViews.Close()
		return nil, true, headlessRemoteWorkspaceSelectionError()
	case serverapi.ProjectBindingPlanKindHeadlessRemoteSelected:
		if plan.Workspace == nil {
			_ = projectViews.Close()
			return nil, true, errors.New("resolved remote workspace is required")
		}
		_ = projectViews.Close()
		remote, err := dialConfiguredRemoteWorkspace(ctx, cfg, plan.Workspace.ProjectID, plan.Workspace.WorkspaceID)
		if err != nil {
			return nil, true, err
		}
		return remote, true, nil
	default:
		_ = projectViews.Close()
		return nil, true, fmt.Errorf("unsupported headless project binding plan %q", plan.Kind)
	}
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
	attachCtx, cancel := context.WithTimeout(ctx, configuredRemoteAttachTimeout)
	defer cancel()
	projectViews, err := dialConfiguredProjectViewRemote(attachCtx, cfg)
	if err != nil {
		return nil, false
	}
	if accept != nil && !accept(projectViews.Identity()) {
		_ = projectViews.Close()
		return nil, false
	}
	if supports != nil && !supports(projectViews.Identity().Capabilities) {
		_ = projectViews.Close()
		return nil, false
	}
	binding, resolveErr := resolveRemoteWorkspaceBinding(attachCtx, projectViews, cfg.WorkspaceRoot)
	if resolveErr != nil {
		_ = projectViews.Close()
		return nil, false
	}
	if binding == nil {
		if requireRegistered {
			_ = projectViews.Close()
			return nil, false
		}
		remote, ok := projectViews.(*client.Remote)
		if !ok {
			_ = projectViews.Close()
			return nil, false
		}
		return remote, true
	}
	_ = projectViews.Close()
	remote, err := dialConfiguredRemoteWorkspace(ctx, cfg, binding.ProjectID, binding.WorkspaceID)
	if err != nil {
		return nil, false
	}
	return remote, true
}

func dialConfiguredRemoteWorkspace(ctx context.Context, cfg config.App, projectID string, workspaceID string) (*client.Remote, error) {
	attachCtx, cancel := context.WithTimeout(ctx, configuredRemoteAttachTimeout)
	defer cancel()
	return dialConfiguredRemote(attachCtx, cfg, projectID, workspaceID)
}

func configuredRemoteSupportsRunPrompt(flags protocol.CapabilityFlags) bool {
	return flags.RunPrompt && flags.AuthBootstrap && flags.ProjectAttach
}

func configuredRemoteSupportsInteractiveSession(flags protocol.CapabilityFlags) bool {
	return flags.AuthBootstrap &&
		flags.ProjectAttach &&
		flags.SessionPlan &&
		flags.SessionLifecycle &&
		flags.SessionTranscriptPaging &&
		flags.SessionRuntime &&
		flags.RuntimeControl &&
		flags.PromptControl &&
		flags.PromptActivity &&
		flags.SessionActivity &&
		flags.ProcessOutput
}

func resolveCLIWorkspaceRoot(opts Options) (string, error) {
	trimmed := strings.TrimSpace(opts.WorkspaceRoot)
	if trimmed == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		trimmed = cwd
	}
	return filepath.Abs(trimmed)
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
		if remote, ok, err := tryDialMatchingConfiguredRunPromptRemoteWithConfig(ctx, opts, cfg, func(identity protocol.ServerIdentity) bool {
			return identity.PID == childPID
		}); err != nil {
			_ = failureClose()
			return nil, nil, false, err
		} else if ok {
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

func loadRemoteAttachConfig(opts Options) (config.App, error) {
	workspaceConfig, err := resolveRunPromptWorkspaceConfig(opts)
	return workspaceConfig.Config, err
}

func resolveRunPromptWorkspaceConfig(opts Options) (runPromptWorkspaceConfig, error) {
	workspaceRoot, err := resolveCLIWorkspaceRoot(opts)
	if err != nil {
		return runPromptWorkspaceConfig{}, err
	}
	sessionID := strings.TrimSpace(opts.SessionID)
	contextSessionID := strings.TrimSpace(opts.WorkspaceContextSessionID)
	if sessionID == "" && !opts.WorkspaceRootExplicit {
		sessionID = contextSessionID
	}
	plan, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{
		WorkspaceRoot:         workspaceRoot,
		WorkspaceRootExplicit: opts.WorkspaceRootExplicit,
		SessionID:             sessionID,
		OpenAIBaseURL:         opts.OpenAIBaseURL,
		OpenAIBaseURLExplicit: opts.OpenAIBaseURLExplicit,
	})
	if err != nil {
		if sessionID != "" && sessionID == contextSessionID {
			return runPromptWorkspaceConfig{}, workspaceContextSessionError(contextSessionID, err)
		}
		return runPromptWorkspaceConfig{}, err
	}
	resolvedOpts := opts
	if strings.TrimSpace(plan.Config.WorkspaceRoot) != "" && plan.Config.WorkspaceRoot != workspaceRoot {
		resolvedOpts.WorkspaceRoot = plan.Config.WorkspaceRoot
	}
	return runPromptWorkspaceConfig{Options: resolvedOpts, Config: plan.Config}, nil
}

func workspaceContextSessionError(sessionID string, err error) error {
	if errors.Is(err, session.ErrSessionNotFound) {
		return fmt.Errorf("%s points to missing Builder session %q; unset %s or run from a live Builder shell: %w", sessionenv.BuilderSessionID, strings.TrimSpace(sessionID), sessionenv.BuilderSessionID, err)
	}
	return fmt.Errorf("resolve %s workspace context %q: %w", sessionenv.BuilderSessionID, strings.TrimSpace(sessionID), err)
}

func resolveRemoteWorkspaceBinding(ctx context.Context, projectViews client.ProjectViewClient, workspaceRoot string) (*serverapi.ProjectBinding, error) {
	resp, err := projectViews.PlanWorkspaceBinding(ctx, serverapi.ProjectBindingPlanRequest{Path: workspaceRoot, Mode: serverapi.ProjectBindingPlanModeInteractive})
	if err != nil {
		return nil, err
	}
	if resp.Kind != serverapi.ProjectBindingPlanKindBound {
		return nil, nil
	}
	return resp.Binding, nil
}

func headlessWorkspaceRegistrationError(workspaceRoot string) error {
	trimmedRoot := strings.TrimSpace(workspaceRoot)
	if trimmedRoot == "" {
		trimmedRoot = "current workspace"
	}
	return fmt.Errorf("%w: %s is not attached to a project. Run `builder project` in a workspace that already belongs to the target project, then run `builder attach <path>` from there or `builder attach --project <project-id> <path>`", errWorkspaceNotRegistered, trimmedRoot)
}

func headlessRemoteWorkspaceSelectionError() error {
	return errors.New("remote server could not resolve the current workspace and no single server workspace could be chosen automatically. Run `builder project list`, `builder project create --path <server-path> --name <project-name>`, or `builder attach --project <project-id> <server-path>` against the configured server, or start interactive Builder to choose an existing server project/workspace")
}

func newOwnedDaemonClose(remote *client.Remote, cmd *exec.Cmd, errCh <-chan error) func() error {
	var once sync.Once
	return func() error {
		var closeErr error
		once.Do(func() {
			if remote != nil {
				closeErr = errors.Join(closeErr, remote.Close())
			}
			if cmd == nil || cmd.Process == nil || errCh == nil {
				return
			}
			select {
			case <-errCh:
				return
			default:
			}
			if err := terminateOwnedDaemonProcess(cmd.Process); err != nil && !errors.Is(err, os.ErrProcessDone) {
				if killErr := forceKillOwnedDaemonProcess(cmd.Process); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
					closeErr = errors.Join(closeErr, killErr)
				}
				<-errCh
				return
			}
			timer := time.NewTimer(launchedDaemonShutdownTimeout)
			defer timer.Stop()
			select {
			case <-errCh:
			case <-timer.C:
				if err := forceKillOwnedDaemonProcess(cmd.Process); err != nil && !errors.Is(err, os.ErrProcessDone) {
					closeErr = errors.Join(closeErr, err)
				}
				<-errCh
			}
		})
		return closeErr
	}
}

func daemonExecutablePath() (string, bool) {
	execPath, err := os.Executable()
	if err != nil {
		return "", false
	}
	if strings.HasSuffix(filepath.Base(execPath), ".test") {
		return "", false
	}
	return execPath, true
}

func buildServeArgs(opts Options) []string {
	return []string{"serve"}
}

func buildServeEnv(cfg config.App) []string {
	env := os.Environ()
	if strings.TrimSpace(cfg.PersistenceRoot) != "" {
		env = append(env, "BUILDER_PERSISTENCE_ROOT="+cfg.PersistenceRoot)
	}
	if strings.TrimSpace(cfg.Settings.ServerHost) != "" {
		env = append(env, "BUILDER_SERVER_HOST="+cfg.Settings.ServerHost)
	}
	if cfg.Settings.ServerPort > 0 {
		env = append(env, "BUILDER_SERVER_PORT="+strconv.Itoa(cfg.Settings.ServerPort))
	}
	return env
}
