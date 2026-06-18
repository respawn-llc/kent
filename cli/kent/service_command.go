package main

import (
	"context"
	"core/shared/config"
	"core/shared/protocol"
	"core/shared/sessionenv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type serviceAction string

const (
	serviceActionStatus    serviceAction = "status"
	serviceActionInstall   serviceAction = "install"
	serviceActionUninstall serviceAction = "uninstall"
	serviceActionStart     serviceAction = "start"
	serviceActionStop      serviceAction = "stop"
	serviceActionRestart   serviceAction = "restart"
)

type serviceCommandOptions struct {
	JSON        bool
	Force       bool
	NoStart     bool
	KeepRunning bool
	IfInstalled bool
}

const servicePersistenceRootFlagUsage = "config and data root directory (overrides KENT_PERSISTENCE_ROOT and the default ~/.kent)"

// commitServicePersistenceRoot publishes a --persistence-root flag value to
// KENT_PERSISTENCE_ROOT so every service operation resolves the same config+data
// root (install bakes it into the launched unit; status/start/stop target the
// matching instance). It returns (exitCode, false) when the value is invalid.
func commitServicePersistenceRoot(value string, stderr io.Writer) (int, bool) {
	if err := publishPersistenceRootEnv(value); err != nil {
		fmt.Fprintln(stderr, err)
		return 2, false
	}
	return 0, true
}

func serviceSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fs := newCommandFlagSet(config.Command+" service", stderr, serviceUsage)
		fs.Usage()
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	action := serviceAction(strings.TrimSpace(args[0]))
	switch action {
	case serviceActionStatus:
		return serviceStatusSubcommand(args[1:], stdout, stderr)
	case serviceActionInstall:
		return serviceInstallSubcommand(args[1:], stdout, stderr)
	case serviceActionUninstall:
		return serviceUninstallSubcommand(args[1:], stdout, stderr)
	case serviceActionStart:
		return serviceLifecycleSubcommand(action, args[1:], stdout, stderr)
	case serviceActionStop:
		return serviceLifecycleSubcommand(action, args[1:], stdout, stderr)
	case serviceActionRestart:
		return serviceRestartSubcommand(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown service command: %s\n\n", args[0])
		fs := newCommandFlagSet(config.Command+" service", stderr, serviceUsage)
		serviceUsage.write(fs)
		return 2
	}
}

func serviceStatusSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" service status", stderr, serviceStatusUsage)
	jsonOut := fs.Bool("json", false, "print machine-readable JSON")
	persistenceRoot := fs.String("persistence-root", "", servicePersistenceRootFlagUsage)
	if ok, exitCode := parseCommandFlags(fs, args); !ok {
		return exitCode
	}
	if len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "service status does not accept positional arguments")
		return 2
	}
	if code, ok := commitServicePersistenceRoot(*persistenceRoot, stderr); !ok {
		return code
	}
	return runServiceCommandAction(context.Background(), serviceActionStatus, serviceCommandOptions{JSON: *jsonOut}, stdout, stderr)
}

func serviceInstallSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" service install", stderr, serviceInstallUsage)
	force := fs.Bool("force", false, "rewrite existing service registration")
	noStart := fs.Bool("no-start", false, "install service without starting it")
	persistenceRoot := fs.String("persistence-root", "", servicePersistenceRootFlagUsage)
	if ok, exitCode := parseCommandFlags(fs, args); !ok {
		return exitCode
	}
	if len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "service install does not accept positional arguments")
		return 2
	}
	if code, ok := commitServicePersistenceRoot(*persistenceRoot, stderr); !ok {
		return code
	}
	return runServiceCommandAction(context.Background(), serviceActionInstall, serviceCommandOptions{Force: *force, NoStart: *noStart}, stdout, stderr)
}

func serviceUninstallSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" service uninstall", stderr, serviceUninstallUsage)
	keepRunning := fs.Bool("keep-running", false, "remove service registration without stopping current server process")
	persistenceRoot := fs.String("persistence-root", "", servicePersistenceRootFlagUsage)
	if ok, exitCode := parseCommandFlags(fs, args); !ok {
		return exitCode
	}
	if len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "service uninstall does not accept positional arguments")
		return 2
	}
	if code, ok := commitServicePersistenceRoot(*persistenceRoot, stderr); !ok {
		return code
	}
	return runServiceCommandAction(context.Background(), serviceActionUninstall, serviceCommandOptions{KeepRunning: *keepRunning}, stdout, stderr)
}

func serviceLifecycleSubcommand(action serviceAction, args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" service "+string(action), stderr, commandUsage{
		title: "Usage of " + config.Command + " service " + string(action) + ":",
		lines: []string{"  " + config.Command + " service " + string(action)},
	})
	persistenceRoot := fs.String("persistence-root", "", servicePersistenceRootFlagUsage)
	if ok, exitCode := parseCommandFlags(fs, args); !ok {
		return exitCode
	}
	if len(fs.Args()) != 0 {
		fmt.Fprintf(stderr, "service %s does not accept positional arguments\n", action)
		return 2
	}
	if code, ok := commitServicePersistenceRoot(*persistenceRoot, stderr); !ok {
		return code
	}
	return runServiceCommandAction(context.Background(), action, serviceCommandOptions{}, stdout, stderr)
}

func serviceRestartSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" service restart", stderr, serviceRestartUsage)
	ifInstalled := fs.Bool("if-installed", false, "exit successfully without action when service is not installed")
	persistenceRoot := fs.String("persistence-root", "", servicePersistenceRootFlagUsage)
	if ok, exitCode := parseCommandFlags(fs, args); !ok {
		return exitCode
	}
	if len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "service restart does not accept positional arguments")
		return 2
	}
	if code, ok := commitServicePersistenceRoot(*persistenceRoot, stderr); !ok {
		return code
	}
	return runServiceCommandAction(context.Background(), serviceActionRestart, serviceCommandOptions{IfInstalled: *ifInstalled}, stdout, stderr)
}

func runServiceCommandAction(ctx context.Context, action serviceAction, opts serviceCommandOptions, stdout io.Writer, stderr io.Writer) int {
	if err := ensureServiceLifecycleAllowed(action, opts); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	spec, err := loadServiceSpec()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	backend := serviceBackendFactory()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	switch action {
	case serviceActionStatus:
		status, err := readServiceStatus(ctx, backend, spec)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if opts.JSON {
			encoder := json.NewEncoder(stdout)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(status); err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			return 0
		}
		writeServiceStatus(stdout, status)
		return 0
	case serviceActionInstall:
		if err := ensureNoUnmanagedServerConflictForAction(ctx, backend, spec, serviceActionInstall); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := backend.Install(ctx, spec, opts.Force, !opts.NoStart); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "Installed %s.\n", serviceDisplayName)
		if !opts.NoStart {
			fmt.Fprintln(stdout, "Started: yes")
		} else {
			fmt.Fprintln(stdout, "Started: no")
		}
	case serviceActionUninstall:
		if err := ensureServiceRootMatch(ctx, backend, spec); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := backend.Uninstall(ctx, spec, !opts.KeepRunning); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "Uninstalled %s.\n", serviceDisplayName)
	case serviceActionStart:
		if err := ensureNoUnmanagedServerConflictForAction(ctx, backend, spec, serviceActionStart); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := backend.Start(ctx, spec); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "Started %s.\n", serviceDisplayName)
	case serviceActionStop:
		if err := ensureServiceRootMatch(ctx, backend, spec); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := backend.Stop(ctx, spec); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "Stopped %s.\n", serviceDisplayName)
	case serviceActionRestart:
		if !opts.IfInstalled {
			if err := ensureNoUnmanagedServerConflictForAction(ctx, backend, spec, action); err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
		} else {
			status, err := backend.Status(ctx, spec)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			if !status.Installed {
				return 0
			}
			// A registration for a different root is "not installed" from this
			// invocation's perspective, so --if-installed exits as a quiet no-op.
			if rootMismatchError(status, spec) != nil {
				return 0
			}
			if err := ensureNoUnmanagedServerConflictForAction(ctx, backend, spec, action); err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			fmt.Fprintf(stdout, "%s is installed. Restarting it after update; sessions may fail briefly.\n", serviceDisplayName)
			if err := backend.Install(ctx, spec, true, true); err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			fmt.Fprintf(stdout, "Restarted %s.\n", serviceDisplayName)
			return 0
		}
		if err := backend.Restart(ctx, spec); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "Restarted %s.\n", serviceDisplayName)
	}
	return 0
}

// ensureServiceRootMatch reads the current registration and returns an error
// when the installed service serves a different config+data root than the
// resolved spec (or when the status read itself fails). It returns nil when
// there is no conflict (or the installed root cannot be determined). Used by
// stop/uninstall, which otherwise do not read status; start/restart reuse the
// status fetched by ensureNoUnmanagedServerConflictForAction instead of paying a
// second read.
func ensureServiceRootMatch(ctx context.Context, backend serviceBackend, spec serviceSpec) error {
	status, err := backend.Status(ctx, spec)
	if err != nil {
		return err
	}
	return rootMismatchError(status, spec)
}

// rootMismatchError compares the persistence root baked into an installed
// registration's command against the resolved spec root. Lifecycle actions
// target the single global OS registration, so acting on a registration that
// serves a different root is a cross-root footgun. It returns nil when they
// match, when nothing is installed, or when the requested root is the default and
// the installed command carries no explicit --persistence-root (a legitimate
// default-root service). Every service this binary installs now bakes
// --persistence-root, and backends report the actual registration command (the
// Windows backend resolves it from the registered scheduled-task action or the
// Startup-folder launcher, never a path under the requested root), so the real
// cross-root footguns — a default/other-root registration targeted with a
// different --persistence-root, or an unpinned legacy/manual registration
// targeted with an explicit non-default root — are caught.
func rootMismatchError(status serviceStatus, spec serviceSpec) error {
	if !status.Installed || len(status.Command) == 0 {
		return nil
	}
	installedRoot, ok := persistenceRootFromServiceCommand(status.Command)
	if !ok {
		// A registration with no --persistence-root predates root isolation or was
		// installed by hand; its root is the default/indeterminate one. Acting on it
		// for an explicit non-default root would target the wrong (single, global)
		// registration, so refuse unless the requested root is itself the default.
		requestedIsDefault, err := config.IsDefaultPersistenceRoot(spec.Config.PersistenceRoot)
		if err != nil {
			return err
		}
		if requestedIsDefault {
			return nil
		}
		return fmt.Errorf("no %s is installed for persistence root %s; the installed service declares no persistence root (it predates root isolation or was installed manually) and is treated as the default root. Reinstall with `%s service install --persistence-root %s` or manage the default root instead", serviceDisplayName, spec.Config.PersistenceRoot, config.Command, spec.Config.PersistenceRoot)
	}
	if config.PersistenceRootHash(installedRoot) == config.PersistenceRootHash(spec.Config.PersistenceRoot) {
		return nil
	}
	return fmt.Errorf("no %s is installed for persistence root %s; the installed service targets %s. Reinstall with `%s service install --persistence-root %s` or manage the matching root instead", serviceDisplayName, spec.Config.PersistenceRoot, installedRoot, config.Command, spec.Config.PersistenceRoot)
}

// persistenceRootFromServiceCommand extracts the --persistence-root value baked
// into an installed service command. It scans structured argv tokens (both the
// "--persistence-root <root>" and "--persistence-root=<root>" forms) rather than
// matching substrings.
func persistenceRootFromServiceCommand(command []string) (string, bool) {
	const flag = "--persistence-root"
	for i, arg := range command {
		if arg == flag {
			if i+1 < len(command) {
				return command[i+1], true
			}
			return "", false
		}
		if value, ok := strings.CutPrefix(arg, flag+"="); ok {
			return value, true
		}
	}
	return "", false
}

// windowsRegisteredTaskRunPath extracts the action path registered for the
// scheduled task from `schtasks /Query /V /FO LIST` output (its "Task To Run"
// field). That value reflects the actual global registration regardless of the
// requested persistence root, so the resolved command can be compared against
// the requested root instead of trusting a script path under it. Defined here
// rather than in the build-tagged Windows backend so the parsing is unit-testable
// on every platform.
func windowsRegisteredTaskRunPath(taskQueryOutput string) (string, bool) {
	const field = "task to run:"
	for _, raw := range strings.Split(taskQueryOutput, "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(strings.ToLower(line), field) {
			continue
		}
		value := strings.Trim(strings.TrimSpace(line[len(field):]), "\"")
		if value == "" {
			return "", false
		}
		return value, true
	}
	return "", false
}

// windowsRegisteredScriptPath resolves the server.cmd path the OS actually has
// registered, independent of any requested persistence root. It prefers the
// scheduled task action ("Task To Run") and falls back to the script path
// embedded in the Startup-folder launcher. It returns false when neither source
// carries a path, in which case the installed root is indeterminate and callers
// must not substitute a path derived from the requested root: lifecycle actions
// target the single global registration, so a requested-root guess would either
// fabricate a false root match or mask a real cross-root mismatch. Defined here
// rather than in the build-tagged Windows backend so the parsing is unit-testable
// on every platform.
func windowsRegisteredScriptPath(taskQueryOutput string, startupLauncher string) (string, bool) {
	if path, ok := windowsRegisteredTaskRunPath(taskQueryOutput); ok {
		return path, true
	}
	return windowsStartupItemScriptPath(startupLauncher)
}

// windowsStartupItemScriptPath extracts the server.cmd path the Windows
// Startup-folder fallback launcher invokes. The launcher line has the shape
// `start "" /min cmd.exe /d /c "<script path>"`; the script path is the final
// token after the `/d /c ` marker, optionally quoted. It returns false when no
// launcher line is present, mirroring an absent scheduled-task action.
func windowsStartupItemScriptPath(startupLauncher string) (string, bool) {
	const marker = "/d /c "
	for _, raw := range strings.Split(startupLauncher, "\n") {
		line := strings.TrimSpace(raw)
		idx := strings.LastIndex(strings.ToLower(line), marker)
		if idx < 0 {
			continue
		}
		candidate := strings.Trim(strings.TrimSpace(line[idx+len(marker):]), "\"")
		if candidate == "" {
			continue
		}
		return candidate, true
	}
	return "", false
}

func ensureNoUnmanagedServerConflictForAction(ctx context.Context, backend serviceBackend, spec serviceSpec, action serviceAction) error {
	status, err := backend.Status(ctx, spec)
	if err != nil {
		return err
	}
	// start/restart must not act on a registration installed for a different
	// root. install (re)writes the registration, so it is exempt.
	if action == serviceActionStart || action == serviceActionRestart {
		if mismatch := rootMismatchError(status, spec); mismatch != nil {
			return mismatch
		}
	}
	healthStatus, healthPID := probeServiceHealth(ctx, spec)
	healthRunning := healthStatus == protocol.HealthStatusOK
	pidProof := status.PID > 0 && healthPID > 0 && status.PID == healthPID
	commandProof := len(status.Command) > 0 && commandArgsEqual(status.Command, serviceCommand(spec))
	backendOwnsHealthyServer := healthRunning && status.Running && status.Loaded && (pidProof || commandProof)
	if healthRunning && !backendOwnsHealthyServer {
		if action == serviceActionRestart && status.Installed && (!status.Loaded || !status.Running) {
			return nil
		}
		pidText := ""
		if healthPID > 0 {
			pidText = fmt.Sprintf(" (pid %d)", healthPID)
		}
		return fmt.Errorf(config.Product+" server is already running outside the background service on %s%s. Stop it before changing the service", spec.Endpoint, pidText)
	}
	if status.Running && status.Installed && !status.Loaded {
		return fmt.Errorf(config.Product+" server is already running on %s, but the background service is not loaded. Stop the manual server or run `"+config.Command+" service restart` after fixing service state", spec.Endpoint)
	}
	if !healthRunning {
		if status.Installed && status.Loaded && status.Running {
			return nil
		}
		dialer := net.Dialer{Timeout: 500 * time.Millisecond}
		conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(spec.Config.Settings.ServerHost, strconv.Itoa(spec.Config.Settings.ServerPort)))
		if err == nil {
			_ = conn.Close()
			return fmt.Errorf("server port %s is already in use, but it is not responding as "+config.Product+". Stop the process using that port before installing the background service", net.JoinHostPort(spec.Config.Settings.ServerHost, strconv.Itoa(spec.Config.Settings.ServerPort)))
		}
	}
	return nil
}

const serviceLifecycleCurrentSessionError = "you may not manage the service now as this may kill your current session, halting your work. Ask the user to manage the service manually."

func serviceLifecycleGuardApplies(action serviceAction, opts serviceCommandOptions) bool {
	switch action {
	case serviceActionRestart:
		return true
	case serviceActionInstall:
		return !opts.NoStart
	case serviceActionUninstall:
		return !opts.KeepRunning
	case serviceActionStart:
		return true
	case serviceActionStop:
		return true
	default:
		return false
	}
}

func ensureServiceLifecycleAllowed(action serviceAction, opts serviceCommandOptions) error {
	if !serviceLifecycleGuardApplies(action, opts) {
		return nil
	}
	if _, ok := sessionenv.LookupSessionID(os.LookupEnv); !ok {
		return nil
	}
	return errors.New(serviceLifecycleCurrentSessionError)
}

func readServiceStatus(ctx context.Context, backend serviceBackend, spec serviceSpec) (serviceStatus, error) {
	status, err := backend.Status(ctx, spec)
	if err != nil {
		return serviceStatus{}, err
	}
	// Evaluate the root match against the raw registration command before the
	// substitution below replaces an empty command with the requested-root one,
	// which would otherwise mask a registration that serves a different root.
	mismatched := rootMismatchError(status, spec) != nil
	status.Backend = backend.Name()
	status.Endpoint = spec.Endpoint
	status.Logs = []string{spec.StdoutLogPath, spec.StderrLogPath}
	if len(status.Command) == 0 {
		status.Command = serviceCommand(spec)
	}
	if mismatched {
		// A registration for a different (or unconfirmable) root is "not
		// installed" from the requested root's perspective, so status never
		// reports another root's service as installed/running for this one.
		status.Installed = false
		status.Loaded = false
		status.Running = false
		status.PID = 0
	}
	return applyHealthProbe(ctx, status, spec), nil
}

func writeServiceStatus(stdout io.Writer, status serviceStatus) {
	state := "not installed"
	if status.Installed && status.Running {
		state = "running"
	} else if status.Installed {
		state = "stopped"
	} else if status.HealthStatus == protocol.HealthStatusOK {
		state = "not installed (server running manually)"
	}
	fmt.Fprintf(stdout, config.ServiceDisplayName+": %s\n", state)
	fmt.Fprintf(stdout, "Backend: %s\n", status.Backend)
	if status.PID > 0 {
		fmt.Fprintf(stdout, "PID: %d\n", status.PID)
	} else if status.HealthPID > 0 {
		fmt.Fprintf(stdout, "PID: %d\n", status.HealthPID)
	}
	if len(status.Command) > 0 {
		fmt.Fprintf(stdout, "Command: %s\n", commandString(status.Command))
	}
	fmt.Fprintf(stdout, "Endpoint: %s\n", status.Endpoint)
	if len(status.Logs) > 0 {
		fmt.Fprintf(stdout, "Logs: %s\n", strings.Join(status.Logs, ", "))
	}
	for _, hint := range status.Hints {
		fmt.Fprintf(stdout, "Hint: %s\n", hint)
	}
	if strings.TrimSpace(status.Detail) != "" {
		fmt.Fprintf(stdout, "Detail: %s\n", status.Detail)
	}
}
