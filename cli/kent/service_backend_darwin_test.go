//go:build darwin

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func newLaunchdHealthTestServer(t *testing.T, health func() (string, int)) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		body, code := health()
		if code != http.StatusOK {
			http.Error(w, body, code)
			return
		}
		_, _ = fmt.Fprint(w, body)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestLaunchdInstallReloadsLoadedServiceBeforeBootstrap(t *testing.T) {
	spec := newLaunchdTestSpec(t)
	var calls *[][]string
	calls = captureLaunchdServiceCommands(t, func(_ context.Context, name string, args ...string) (serviceCommandResult, error) {
		switch strings.Join(append([]string{name}, args...), "\x00") {
		case "launchctl\x00print\x00gui/" + currentUIDText() + "/" + serviceLaunchdLabel:
			return serviceCommandResult{Stdout: "state = running\npid = 42\n"}, nil
		case "launchctl\x00bootout\x00gui/" + currentUIDText() + "/" + serviceLaunchdLabel:
			return serviceCommandResult{}, nil
		case "launchctl\x00bootstrap\x00gui/" + currentUIDText() + "\x00" + mustLaunchdPlistPath(t):
			return serviceCommandResult{}, nil
		default:
			return serviceCommandResult{}, errors.New("unexpected command")
		}
	})

	if err := (launchdServiceBackend{}).Install(context.Background(), spec, true, true); err != nil {
		t.Fatalf("install: %v", err)
	}

	want := [][]string{
		{"launchctl", "print", "gui/" + currentUIDText() + "/" + serviceLaunchdLabel},
		{"launchctl", "bootout", "gui/" + currentUIDText() + "/" + serviceLaunchdLabel},
		{"launchctl", "bootstrap", "gui/" + currentUIDText(), mustLaunchdPlistPath(t)},
	}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("calls = %#v, want %#v", *calls, want)
	}
}

func TestLaunchdStartBootstrapsUnloadedServiceWithoutKickstart(t *testing.T) {
	spec := newLaunchdTestSpec(t)
	path := writeLaunchdTestPlist(t, spec)
	var calls *[][]string
	calls = captureLaunchdServiceCommands(t, func(_ context.Context, name string, args ...string) (serviceCommandResult, error) {
		switch strings.Join(append([]string{name}, args...), "\x00") {
		case "launchctl\x00print\x00gui/" + currentUIDText() + "/" + serviceLaunchdLabel:
			return serviceCommandResult{Stderr: "not found", Code: 113}, serviceCommandError{Name: name, Args: args, Result: serviceCommandResult{Stderr: "not found", Code: 113}}
		case "launchctl\x00bootstrap\x00gui/" + currentUIDText() + "\x00" + path:
			return serviceCommandResult{}, nil
		default:
			return serviceCommandResult{}, errors.New("unexpected command")
		}
	})

	if err := (launchdServiceBackend{}).Start(context.Background(), spec); err != nil {
		t.Fatalf("start: %v", err)
	}

	want := [][]string{
		{"launchctl", "print", "gui/" + currentUIDText() + "/" + serviceLaunchdLabel},
		{"launchctl", "bootstrap", "gui/" + currentUIDText(), path},
	}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("calls = %#v, want %#v", *calls, want)
	}
}

func TestLaunchdRestartReloadsLoadedServiceBeforeBootstrap(t *testing.T) {
	spec := newLaunchdTestSpec(t)
	path := writeLaunchdTestPlist(t, spec)
	calls := captureLaunchdServiceCommands(t, func(_ context.Context, name string, args ...string) (serviceCommandResult, error) {
		switch strings.Join(append([]string{name}, args...), "\x00") {
		case "launchctl\x00print\x00gui/" + currentUIDText() + "/" + serviceLaunchdLabel:
			return serviceCommandResult{Stdout: "state = running\npid = 42\n"}, nil
		case "launchctl\x00bootout\x00gui/" + currentUIDText() + "/" + serviceLaunchdLabel:
			return serviceCommandResult{}, nil
		case "launchctl\x00bootstrap\x00gui/" + currentUIDText() + "\x00" + path:
			return serviceCommandResult{}, nil
		default:
			return serviceCommandResult{}, errors.New("unexpected command")
		}
	})

	if err := (launchdServiceBackend{}).Restart(context.Background(), spec); err != nil {
		t.Fatalf("restart: %v", err)
	}

	want := [][]string{
		{"launchctl", "print", "gui/" + currentUIDText() + "/" + serviceLaunchdLabel},
		{"launchctl", "print", "gui/" + currentUIDText() + "/" + serviceLaunchdLabel},
		{"launchctl", "bootout", "gui/" + currentUIDText() + "/" + serviceLaunchdLabel},
		{"launchctl", "bootstrap", "gui/" + currentUIDText(), path},
	}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("calls = %#v, want %#v", *calls, want)
	}
}

func TestLaunchdRestartReloadsUnloadedHealthyServerBeforeBootstrap(t *testing.T) {
	serverRequests := 0
	serverStopped := false
	bootstrapped := false
	server := newLaunchdHealthTestServer(t, func() (string, int) {
		serverRequests++
		if serverStopped && !bootstrapped {
			return "stopped", http.StatusServiceUnavailable
		}
		if bootstrapped {
			return `{"status":"ok","pid":77}`, http.StatusOK
		}
		return `{"status":"ok","pid":42}`, http.StatusOK
	})
	spec := newLaunchdTestSpec(t)
	spec.Endpoint = server.URL
	path := writeLaunchdTestPlist(t, spec)
	originalSignal := signalLaunchdServiceProcess
	signaledPID := 0
	signalLaunchdServiceProcess = func(pid int) error {
		signaledPID = pid
		serverStopped = true
		return nil
	}
	t.Cleanup(func() { signalLaunchdServiceProcess = originalSignal })
	originalAlive := launchdServiceProcessAlive
	launchdServiceProcessAlive = func(pid int) (bool, error) {
		return !serverStopped, nil
	}
	t.Cleanup(func() { launchdServiceProcessAlive = originalAlive })
	var calls *[][]string
	calls = captureLaunchdServiceCommands(t, func(_ context.Context, name string, args ...string) (serviceCommandResult, error) {
		switch strings.Join(append([]string{name}, args...), "\x00") {
		case "launchctl\x00print\x00gui/" + currentUIDText() + "/" + serviceLaunchdLabel:
			if countLaunchdCommand(*calls, "bootstrap") > 0 {
				return serviceCommandResult{Stdout: "state = running\npid = 77\n"}, nil
			}
			return serviceCommandResult{Stderr: "not found", Code: 113}, serviceCommandError{Name: name, Args: args, Result: serviceCommandResult{Stderr: "not found", Code: 113}}
		case "launchctl\x00bootstrap\x00gui/" + currentUIDText() + "\x00" + path:
			if serverRequests < 2 {
				t.Fatalf("bootstrap happened before old server health went down")
			}
			bootstrapped = true
			return serviceCommandResult{}, nil
		default:
			return serviceCommandResult{}, errors.New("unexpected command")
		}
	})

	if err := (launchdServiceBackend{}).Restart(context.Background(), spec); err != nil {
		t.Fatalf("restart: %v", err)
	}

	if signaledPID != 42 {
		t.Fatalf("signaled pid = %d, want 42", signaledPID)
	}
	want := [][]string{
		{"launchctl", "print", "gui/" + currentUIDText() + "/" + serviceLaunchdLabel},
		{"launchctl", "print", "gui/" + currentUIDText() + "/" + serviceLaunchdLabel},
		{"launchctl", "bootstrap", "gui/" + currentUIDText(), path},
		{"launchctl", "print", "gui/" + currentUIDText() + "/" + serviceLaunchdLabel},
	}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("calls = %#v, want %#v", *calls, want)
	}
}

func TestLaunchdRestartIfInstalledReplacesStaleLoadedServiceAfterTransientBootstrapError(t *testing.T) {
	spec := newLaunchdTestSpec(t)
	withLaunchdServiceCommandSpec(t, spec)
	path := writeLaunchdTestPlist(t, spec)
	printCalls := 0
	var calls *[][]string
	calls = captureLaunchdServiceCommands(t, func(_ context.Context, name string, args ...string) (serviceCommandResult, error) {
		switch strings.Join(append([]string{name}, args...), "\x00") {
		case "launchctl\x00print\x00gui/" + currentUIDText() + "/" + serviceLaunchdLabel:
			printCalls++
			if printCalls <= 2 {
				return serviceCommandResult{Stdout: "state = running\npid = 42\narguments = {\n\t/old/kent\n\tserve\n}\n"}, nil
			}
			return serviceCommandResult{Stderr: "not found", Code: 113}, serviceCommandError{Name: name, Args: args, Result: serviceCommandResult{Stderr: "not found", Code: 113}}
		case "launchctl\x00bootstrap\x00gui/" + currentUIDText() + "\x00" + path:
			if countLaunchdCommand(*calls, "bootstrap") == 1 {
				return serviceCommandResult{Stderr: "Bootstrap failed: 5: Input/output error", Code: 5}, serviceCommandError{Name: name, Args: args, Result: serviceCommandResult{Stderr: "Bootstrap failed: 5: Input/output error", Code: 5}}
			}
			return serviceCommandResult{}, nil
		case "launchctl\x00bootout\x00gui/" + currentUIDText() + "/" + serviceLaunchdLabel:
			return serviceCommandResult{}, nil
		default:
			return serviceCommandResult{}, errors.New("unexpected command")
		}
	})

	var stdout strings.Builder
	var stderr strings.Builder
	code := serviceSubcommand([]string{"restart", "--if-installed"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}

	want := [][]string{
		{"launchctl", "print", "gui/" + currentUIDText() + "/" + serviceLaunchdLabel},
		{"launchctl", "print", "gui/" + currentUIDText() + "/" + serviceLaunchdLabel},
		{"launchctl", "print", "gui/" + currentUIDText() + "/" + serviceLaunchdLabel},
		{"launchctl", "bootstrap", "gui/" + currentUIDText(), path},
		{"launchctl", "bootout", "gui/" + currentUIDText() + "/" + serviceLaunchdLabel},
		{"launchctl", "bootstrap", "gui/" + currentUIDText(), path},
	}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("calls = %#v, want %#v", *calls, want)
	}
	wantStdout := serviceDisplayName + " is installed. Restarting it after update; sessions may fail briefly.\nRestarted " + serviceDisplayName + ".\n"
	if stdout.String() != wantStdout {
		t.Fatalf("stdout = %q, want %q", stdout.String(), wantStdout)
	}
}

func TestLaunchdRestartIfInstalledBootstrapRecoveryFailsWhenBootoutFails(t *testing.T) {
	spec := newLaunchdTestSpec(t)
	withLaunchdServiceCommandSpec(t, spec)
	path := writeLaunchdTestPlist(t, spec)
	printCalls := 0
	var calls *[][]string
	calls = captureLaunchdServiceCommands(t, func(_ context.Context, name string, args ...string) (serviceCommandResult, error) {
		switch strings.Join(append([]string{name}, args...), "\x00") {
		case "launchctl\x00print\x00gui/" + currentUIDText() + "/" + serviceLaunchdLabel:
			printCalls++
			if printCalls <= 2 {
				return serviceCommandResult{Stdout: "state = running\npid = 42\narguments = {\n\t/old/kent\n\tserve\n}\n"}, nil
			}
			return serviceCommandResult{Stderr: "not found", Code: 113}, serviceCommandError{Name: name, Args: args, Result: serviceCommandResult{Stderr: "not found", Code: 113}}
		case "launchctl\x00bootstrap\x00gui/" + currentUIDText() + "\x00" + path:
			return serviceCommandResult{Stderr: "Bootstrap failed: 5: Input/output error", Code: 5}, serviceCommandError{Name: name, Args: args, Result: serviceCommandResult{Stderr: "Bootstrap failed: 5: Input/output error", Code: 5}}
		case "launchctl\x00bootout\x00gui/" + currentUIDText() + "/" + serviceLaunchdLabel:
			return serviceCommandResult{Stderr: "bootout failed", Code: 5}, serviceCommandError{Name: name, Args: args, Result: serviceCommandResult{Stderr: "bootout failed", Code: 5}}
		default:
			return serviceCommandResult{}, errors.New("unexpected command")
		}
	})

	var stdout strings.Builder
	var stderr strings.Builder
	code := serviceSubcommand([]string{"restart", "--if-installed"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if countLaunchdCommand(*calls, "bootstrap") != 1 || countLaunchdCommand(*calls, "bootout") != 1 {
		t.Fatalf("calls = %#v, want one bootstrap then failed bootout", *calls)
	}
	if !strings.Contains(stderr.String(), "bootout failed") {
		t.Fatalf("stderr = %q, want bootout failure", stderr.String())
	}
}

func TestLaunchdRestartIfInstalledBootstrapRecoveryFailsWhenRetryBootstrapFails(t *testing.T) {
	spec := newLaunchdTestSpec(t)
	withLaunchdServiceCommandSpec(t, spec)
	path := writeLaunchdTestPlist(t, spec)
	printCalls := 0
	var calls *[][]string
	calls = captureLaunchdServiceCommands(t, func(_ context.Context, name string, args ...string) (serviceCommandResult, error) {
		switch strings.Join(append([]string{name}, args...), "\x00") {
		case "launchctl\x00print\x00gui/" + currentUIDText() + "/" + serviceLaunchdLabel:
			printCalls++
			if printCalls <= 2 {
				return serviceCommandResult{Stdout: "state = running\npid = 42\narguments = {\n\t/old/kent\n\tserve\n}\n"}, nil
			}
			return serviceCommandResult{Stderr: "not found", Code: 113}, serviceCommandError{Name: name, Args: args, Result: serviceCommandResult{Stderr: "not found", Code: 113}}
		case "launchctl\x00bootstrap\x00gui/" + currentUIDText() + "\x00" + path:
			if countLaunchdCommand(*calls, "bootstrap") == 1 {
				return serviceCommandResult{Stderr: "Bootstrap failed: 5: Input/output error", Code: 5}, serviceCommandError{Name: name, Args: args, Result: serviceCommandResult{Stderr: "Bootstrap failed: 5: Input/output error", Code: 5}}
			}
			return serviceCommandResult{Stderr: "retry bootstrap failed", Code: 5}, serviceCommandError{Name: name, Args: args, Result: serviceCommandResult{Stderr: "retry bootstrap failed", Code: 5}}
		case "launchctl\x00bootout\x00gui/" + currentUIDText() + "/" + serviceLaunchdLabel:
			return serviceCommandResult{}, nil
		default:
			return serviceCommandResult{}, errors.New("unexpected command")
		}
	})

	var stdout strings.Builder
	var stderr strings.Builder
	code := serviceSubcommand([]string{"restart", "--if-installed"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if countLaunchdCommand(*calls, "bootstrap") != 2 || countLaunchdCommand(*calls, "bootout") != 1 {
		t.Fatalf("calls = %#v, want failed bootstrap, bootout, failed retry bootstrap", *calls)
	}
	if !strings.Contains(stderr.String(), "retry bootstrap failed") {
		t.Fatalf("stderr = %q, want retry bootstrap failure", stderr.String())
	}
}

func TestLaunchdReloadWaitsForOldServerBeforeBootstrap(t *testing.T) {
	serverRequests := 0
	server := newLaunchdHealthTestServer(t, func() (string, int) {
		serverRequests++
		if serverRequests == 1 {
			return `{"status":"ok","pid":42}`, http.StatusOK
		}
		return "stopped", http.StatusServiceUnavailable
	})
	spec := newLaunchdTestSpec(t)
	spec.Endpoint = server.URL
	path := mustLaunchdPlistPath(t)
	calls := captureLaunchdServiceCommands(t, func(_ context.Context, name string, args ...string) (serviceCommandResult, error) {
		switch strings.Join(append([]string{name}, args...), "\x00") {
		case "launchctl\x00print\x00gui/" + currentUIDText() + "/" + serviceLaunchdLabel:
			return serviceCommandResult{Stdout: "state = running\npid = 42\n"}, nil
		case "launchctl\x00bootout\x00gui/" + currentUIDText() + "/" + serviceLaunchdLabel:
			return serviceCommandResult{}, nil
		case "launchctl\x00bootstrap\x00gui/" + currentUIDText() + "\x00" + path:
			if serverRequests < 2 {
				t.Fatalf("bootstrap happened before old server health went down")
			}
			return serviceCommandResult{}, nil
		default:
			return serviceCommandResult{}, errors.New("unexpected command")
		}
	})

	if err := reloadLaunchdService(context.Background(), spec, path); err != nil {
		t.Fatalf("reload: %v", err)
	}

	want := [][]string{
		{"launchctl", "print", "gui/" + currentUIDText() + "/" + serviceLaunchdLabel},
		{"launchctl", "bootout", "gui/" + currentUIDText() + "/" + serviceLaunchdLabel},
		{"launchctl", "print", "gui/" + currentUIDText() + "/" + serviceLaunchdLabel},
		{"launchctl", "bootstrap", "gui/" + currentUIDText(), path},
	}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("calls = %#v, want %#v", *calls, want)
	}
}

func TestLaunchdReloadStopsUnloadedHealthyServerBeforeBootstrap(t *testing.T) {
	serverRequests := 0
	serverStopped := false
	bootstrapped := false
	server := newLaunchdHealthTestServer(t, func() (string, int) {
		serverRequests++
		if serverStopped && !bootstrapped {
			return "stopped", http.StatusServiceUnavailable
		}
		if bootstrapped {
			return `{"status":"ok","pid":77}`, http.StatusOK
		}
		return `{"status":"ok","pid":42}`, http.StatusOK
	})
	spec := newLaunchdTestSpec(t)
	spec.Endpoint = server.URL
	path := mustLaunchdPlistPath(t)
	originalSignal := signalLaunchdServiceProcess
	signaledPID := 0
	signalLaunchdServiceProcess = func(pid int) error {
		signaledPID = pid
		serverStopped = true
		return nil
	}
	t.Cleanup(func() { signalLaunchdServiceProcess = originalSignal })
	originalAlive := launchdServiceProcessAlive
	launchdServiceProcessAlive = func(pid int) (bool, error) {
		return !serverStopped, nil
	}
	t.Cleanup(func() { launchdServiceProcessAlive = originalAlive })
	var calls *[][]string
	calls = captureLaunchdServiceCommands(t, func(_ context.Context, name string, args ...string) (serviceCommandResult, error) {
		switch strings.Join(append([]string{name}, args...), "\x00") {
		case "launchctl\x00print\x00gui/" + currentUIDText() + "/" + serviceLaunchdLabel:
			if countLaunchdCommand(*calls, "bootstrap") > 0 {
				return serviceCommandResult{Stdout: "state = running\npid = 77\n"}, nil
			}
			return serviceCommandResult{Stderr: "not found", Code: 113}, serviceCommandError{Name: name, Args: args, Result: serviceCommandResult{Stderr: "not found", Code: 113}}
		case "launchctl\x00bootstrap\x00gui/" + currentUIDText() + "\x00" + path:
			if serverRequests < 2 {
				t.Fatalf("bootstrap happened before old server health went down")
			}
			bootstrapped = true
			return serviceCommandResult{}, nil
		default:
			return serviceCommandResult{}, errors.New("unexpected command")
		}
	})

	if err := reloadLaunchdService(context.Background(), spec, path); err != nil {
		t.Fatalf("reload: %v", err)
	}

	if signaledPID != 42 {
		t.Fatalf("signaled pid = %d, want 42", signaledPID)
	}
	want := [][]string{
		{"launchctl", "print", "gui/" + currentUIDText() + "/" + serviceLaunchdLabel},
		{"launchctl", "bootstrap", "gui/" + currentUIDText(), path},
		{"launchctl", "print", "gui/" + currentUIDText() + "/" + serviceLaunchdLabel},
	}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("calls = %#v, want %#v", *calls, want)
	}
}

func TestLaunchdReloadDoesNotAcceptLaunchdPIDWithoutHealthyServer(t *testing.T) {
	originalTimeout := launchdServiceShutdownTimeout
	originalInterval := launchdServiceShutdownPollInterval
	launchdServiceShutdownTimeout = time.Millisecond
	launchdServiceShutdownPollInterval = time.Millisecond
	t.Cleanup(func() {
		launchdServiceShutdownTimeout = originalTimeout
		launchdServiceShutdownPollInterval = originalInterval
	})
	serverStopped := false
	server := newLaunchdHealthTestServer(t, func() (string, int) {
		if serverStopped {
			return "starting", http.StatusServiceUnavailable
		}
		return `{"status":"ok","pid":42}`, http.StatusOK
	})
	spec := newLaunchdTestSpec(t)
	spec.Endpoint = server.URL
	path := mustLaunchdPlistPath(t)
	originalSignal := signalLaunchdServiceProcess
	signalLaunchdServiceProcess = func(pid int) error {
		serverStopped = true
		return nil
	}
	t.Cleanup(func() { signalLaunchdServiceProcess = originalSignal })
	originalAlive := launchdServiceProcessAlive
	launchdServiceProcessAlive = func(pid int) (bool, error) {
		return !serverStopped, nil
	}
	t.Cleanup(func() { launchdServiceProcessAlive = originalAlive })
	var calls *[][]string
	calls = captureLaunchdServiceCommands(t, func(_ context.Context, name string, args ...string) (serviceCommandResult, error) {
		switch strings.Join(append([]string{name}, args...), "\x00") {
		case "launchctl\x00print\x00gui/" + currentUIDText() + "/" + serviceLaunchdLabel:
			if countLaunchdCommand(*calls, "bootstrap") > 0 {
				return serviceCommandResult{Stdout: "state = running\npid = 77\n"}, nil
			}
			return serviceCommandResult{Stderr: "not found", Code: 113}, serviceCommandError{Name: name, Args: args, Result: serviceCommandResult{Stderr: "not found", Code: 113}}
		case "launchctl\x00bootstrap\x00gui/" + currentUIDText() + "\x00" + path:
			return serviceCommandResult{}, nil
		default:
			return serviceCommandResult{}, errors.New("unexpected command")
		}
	})

	err := reloadLaunchdService(context.Background(), spec, path)
	if err == nil {
		t.Fatal("expected reload to wait for healthy launchd server")
	}
	if !errors.Is(err, errLaunchdServerNotHealthy) {
		t.Fatalf("error = %v, want healthy startup timeout", err)
	}
}

func TestLaunchdReloadExplainsOldServerStillRunningInsteadOfBootstrapCodeFive(t *testing.T) {
	originalTimeout := launchdServiceShutdownTimeout
	originalInterval := launchdServiceShutdownPollInterval
	launchdServiceShutdownTimeout = time.Millisecond
	launchdServiceShutdownPollInterval = time.Millisecond
	originalSignal := signalLaunchdServiceProcess
	originalKill := killLaunchdServiceProcess
	originalAlive := launchdServiceProcessAlive
	signalLaunchdServiceProcess = func(pid int) error { return nil }
	killLaunchdServiceProcess = func(pid int) error { return nil }
	launchdServiceProcessAlive = func(pid int) (bool, error) { return true, nil }
	t.Cleanup(func() {
		launchdServiceShutdownTimeout = originalTimeout
		launchdServiceShutdownPollInterval = originalInterval
		signalLaunchdServiceProcess = originalSignal
		killLaunchdServiceProcess = originalKill
		launchdServiceProcessAlive = originalAlive
	})
	server := newServiceHealthTestServer(t, `{"status":"ok","pid":42}`)
	spec := newLaunchdTestSpec(t)
	spec.Endpoint = server.URL
	path := mustLaunchdPlistPath(t)
	calls := captureLaunchdServiceCommands(t, func(_ context.Context, name string, args ...string) (serviceCommandResult, error) {
		switch strings.Join(append([]string{name}, args...), "\x00") {
		case "launchctl\x00print\x00gui/" + currentUIDText() + "/" + serviceLaunchdLabel:
			return serviceCommandResult{Stdout: "state = running\npid = 42\n"}, nil
		case "launchctl\x00bootout\x00gui/" + currentUIDText() + "/" + serviceLaunchdLabel:
			return serviceCommandResult{}, nil
		default:
			return serviceCommandResult{}, errors.New("unexpected command")
		}
	})

	err := reloadLaunchdService(context.Background(), spec, path)
	if err == nil {
		t.Fatal("expected reload to fail while old server is still healthy")
	}
	if !errors.Is(err, errLaunchdOldServerNotExited) || !errors.Is(err, errLaunchdServerProcessNotExited) {
		t.Fatalf("error = %v, want actionable old-server and process-exit failures", err)
	}
	if countLaunchdCommand(*calls, "bootstrap") != 0 {
		t.Fatalf("bootstrap should not run while old server still owns port, calls=%#v", *calls)
	}
}

func TestLaunchdRestartIfInstalledRepeatedIntegration(t *testing.T) {
	if os.Getenv("KENT_LAUNCHD_INTEGRATION") != "1" {
		t.Skip("set KENT_LAUNCHD_INTEGRATION=1 to run real launchd service restart integration")
	}
	kentPath := strings.TrimSpace(os.Getenv("KENT_LAUNCHD_INTEGRATION_BIN"))
	if kentPath == "" {
		var err error
		kentPath, err = exec.LookPath("kent")
		if err != nil {
			t.Fatalf("find kent binary: %v", err)
		}
	}
	freePort := reserveFreeLocalPort(t)
	root := t.TempDir()
	wrapperPath := filepath.Join(root, "kent")
	wrapper := fmt.Sprintf("#!/bin/sh\nexport KENT_SERVER_PORT=%d\nexport KENT_PERSISTENCE_ROOT=%s\nexec -a \"$0\" %s \"$@\"\n", freePort, shellQuote(filepath.Join(root, "persist")), shellQuote(kentPath))
	if err := os.WriteFile(wrapperPath, []byte(wrapper), 0o755); err != nil {
		t.Fatalf("write kent wrapper: %v", err)
	}
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	env := append(os.Environ(),
		"HOME="+home,
		fmt.Sprintf("KENT_SERVER_PORT=%d", freePort),
		"KENT_PERSISTENCE_ROOT="+filepath.Join(root, "persist"),
	)
	runKent := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(wrapperPath, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %s failed: %v\n%s", wrapperPath, strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	t.Cleanup(func() {
		cmd := exec.Command(wrapperPath, "service", "uninstall")
		cmd.Env = env
		_ = cmd.Run()
	})

	runKent("service", "install", "--force")
	lastPID := 0
	for i := 0; i < 3; i++ {
		output := runKent("service", "restart", "--if-installed")
		if !strings.Contains(output, "Restarted Kent background service.") {
			t.Fatalf("restart output = %q, want restart confirmation", output)
		}
		status := runKent("service", "status", "--json")
		var decoded serviceStatus
		if err := json.Unmarshal([]byte(status), &decoded); err != nil {
			t.Fatalf("decode status JSON: %v; raw=%q", err, status)
		}
		if !decoded.Installed || !decoded.Loaded || !decoded.Running || decoded.PID <= 0 {
			t.Fatalf("status after restart %d = %+v, want installed/loaded/running with pid", i+1, decoded)
		}
		if lastPID > 0 && decoded.PID == lastPID {
			t.Fatalf("pid did not change after restart %d: %d", i+1, decoded.PID)
		}
		lastPID = decoded.PID
	}
}

func TestLaunchdStartReplacesStaleLoadedServiceAfterTransientBootstrapError(t *testing.T) {
	spec := newLaunchdTestSpec(t)
	path := writeLaunchdTestPlist(t, spec)
	var calls *[][]string
	calls = captureLaunchdServiceCommands(t, func(_ context.Context, name string, args ...string) (serviceCommandResult, error) {
		switch strings.Join(append([]string{name}, args...), "\x00") {
		case "launchctl\x00print\x00gui/" + currentUIDText() + "/" + serviceLaunchdLabel:
			return serviceCommandResult{Stderr: "not found", Code: 113}, serviceCommandError{Name: name, Args: args, Result: serviceCommandResult{Stderr: "not found", Code: 113}}
		case "launchctl\x00bootstrap\x00gui/" + currentUIDText() + "\x00" + path:
			if countLaunchdCommand(*calls, "bootstrap") == 1 {
				return serviceCommandResult{Stderr: "Bootstrap failed: 5: Input/output error", Code: 5}, serviceCommandError{Name: name, Args: args, Result: serviceCommandResult{Stderr: "Bootstrap failed: 5: Input/output error", Code: 5}}
			}
			return serviceCommandResult{}, nil
		case "launchctl\x00bootout\x00gui/" + currentUIDText() + "/" + serviceLaunchdLabel:
			return serviceCommandResult{}, nil
		default:
			return serviceCommandResult{}, errors.New("unexpected command")
		}
	})

	if err := (launchdServiceBackend{}).Start(context.Background(), spec); err != nil {
		t.Fatalf("start: %v", err)
	}

	want := [][]string{
		{"launchctl", "print", "gui/" + currentUIDText() + "/" + serviceLaunchdLabel},
		{"launchctl", "bootstrap", "gui/" + currentUIDText(), path},
		{"launchctl", "bootout", "gui/" + currentUIDText() + "/" + serviceLaunchdLabel},
		{"launchctl", "bootstrap", "gui/" + currentUIDText(), path},
	}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("calls = %#v, want %#v", *calls, want)
	}
}

func TestLaunchdStartDoesNotHideNonTransientBootstrapError(t *testing.T) {
	spec := newLaunchdTestSpec(t)
	path := writeLaunchdTestPlist(t, spec)
	calls := captureLaunchdServiceCommands(t, func(_ context.Context, name string, args ...string) (serviceCommandResult, error) {
		switch strings.Join(append([]string{name}, args...), "\x00") {
		case "launchctl\x00print\x00gui/" + currentUIDText() + "/" + serviceLaunchdLabel:
			return serviceCommandResult{Stderr: "not found", Code: 113}, serviceCommandError{Name: name, Args: args, Result: serviceCommandResult{Stderr: "not found", Code: 113}}
		case "launchctl\x00bootstrap\x00gui/" + currentUIDText() + "\x00" + path:
			return serviceCommandResult{Stderr: "invalid property list", Code: 78}, serviceCommandError{Name: name, Args: args, Result: serviceCommandResult{Stderr: "invalid property list", Code: 78}}
		default:
			return serviceCommandResult{}, errors.New("unexpected command")
		}
	})

	err := (launchdServiceBackend{}).Start(context.Background(), spec)
	var cmdErr serviceCommandError
	if !errors.As(err, &cmdErr) || cmdErr.Result.Code != 78 {
		t.Fatalf("start error = %v, want surfaced non-transient bootstrap command error", err)
	}
	want := [][]string{
		{"launchctl", "print", "gui/" + currentUIDText() + "/" + serviceLaunchdLabel},
		{"launchctl", "bootstrap", "gui/" + currentUIDText(), path},
	}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("calls = %#v, want %#v", *calls, want)
	}
}

func TestLaunchdStatusUsesLoadedCommandAndRunningStateFromPrint(t *testing.T) {
	spec := newLaunchdTestSpec(t)
	writeLaunchdTestPlist(t, spec)
	captureLaunchdServiceCommands(t, func(_ context.Context, name string, args ...string) (serviceCommandResult, error) {
		if strings.Join(append([]string{name}, args...), "\x00") != "launchctl\x00print\x00gui/"+currentUIDText()+"/"+serviceLaunchdLabel {
			return serviceCommandResult{}, errors.New("unexpected command")
		}
		return serviceCommandResult{Stdout: "state = running\narguments = {\n\t/usr/local/bin/kent\n\tserve\n}\n"}, nil
	})

	status, err := (launchdServiceBackend{}).Status(context.Background(), spec)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.Running {
		t.Fatalf("running = false, want true from launchd state")
	}
	wantCommand := []string{"/usr/local/bin/kent", "serve"}
	if !reflect.DeepEqual(status.Command, wantCommand) {
		t.Fatalf("command = %#v, want %#v", status.Command, wantCommand)
	}
}

func newLaunchdTestSpec(t *testing.T) serviceSpec {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	return testLaunchdServiceSpec(t)
}

func writeLaunchdTestPlist(t *testing.T, spec serviceSpec) string {
	t.Helper()
	path := mustLaunchdPlistPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir launch agents: %v", err)
	}
	if err := os.WriteFile(path, []byte(renderLaunchdPlist(spec)), 0o644); err != nil {
		t.Fatalf("write plist: %v", err)
	}
	return path
}

func testLaunchdServiceSpec(t *testing.T) serviceSpec {
	t.Helper()
	root := t.TempDir()
	return serviceSpec{
		Executable:    "/usr/local/bin/kent",
		Arguments:     []string{"serve"},
		LogDir:        filepath.Join(root, "logs"),
		StdoutLogPath: filepath.Join(root, "logs", "server.log"),
		StderrLogPath: filepath.Join(root, "logs", "server.err.log"),
		Endpoint:      "http://127.0.0.1:1",
	}
}

func captureLaunchdServiceCommands(t *testing.T, fn func(context.Context, string, ...string) (serviceCommandResult, error)) *[][]string {
	t.Helper()
	original := runServiceCommand
	calls := [][]string{}
	runServiceCommand = func(ctx context.Context, name string, args ...string) (serviceCommandResult, error) {
		calls = append(calls, append([]string{name}, args...))
		return fn(ctx, name, args...)
	}
	t.Cleanup(func() { runServiceCommand = original })
	return &calls
}

func withLaunchdServiceCommandSpec(t *testing.T, spec serviceSpec) {
	t.Helper()
	originalLoadSpec := loadServiceSpec
	originalBackendFactory := serviceBackendFactory
	loadServiceSpec = func() (serviceSpec, error) { return spec, nil }
	serviceBackendFactory = func() serviceBackend { return launchdServiceBackend{} }
	t.Cleanup(func() {
		loadServiceSpec = originalLoadSpec
		serviceBackendFactory = originalBackendFactory
	})
}

func currentUIDText() string {
	return strconv.Itoa(os.Getuid())
}

func mustLaunchdPlistPath(t *testing.T) string {
	t.Helper()
	path, err := launchdPlistPath()
	if err != nil {
		t.Fatalf("launchd plist path: %v", err)
	}
	return path
}

func countLaunchdCommand(calls [][]string, name string) int {
	count := 0
	for _, call := range calls {
		if len(call) >= 2 && call[0] == "launchctl" && call[1] == name {
			count++
		}
	}
	return count
}

func reserveFreeLocalPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve local port: %v", err)
	}
	defer func() { _ = listener.Close() }()
	return listener.Addr().(*net.TCPAddr).Port
}
