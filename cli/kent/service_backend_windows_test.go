//go:build windows

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"core/shared/brand"
	"core/shared/config"
)

func TestWindowsInstallWithoutForceRejectsExistingDifferentScript(t *testing.T) {
	spec := windowsServiceTestSpec(t)
	if err := os.MkdirAll(filepath.Dir(windowsTaskScriptPath(spec)), 0o755); err != nil {
		t.Fatalf("mkdir task script dir: %v", err)
	}
	if err := os.WriteFile(windowsTaskScriptPath(spec), []byte("old script"), 0o644); err != nil {
		t.Fatalf("write existing task script: %v", err)
	}
	calls := captureWindowsServiceCommands(t, func(ctx context.Context, name string, args ...string) (serviceCommandResult, error) {
		return serviceCommandResult{}, errors.New("unexpected command")
	})

	err := (scheduledTaskServiceBackend{}).Install(context.Background(), spec, false, false)
	if err == nil {
		t.Fatal("expected existing script rejection")
	}
	if string(mustReadFile(t, windowsTaskScriptPath(spec))) != "old script" {
		t.Fatal("expected existing script to remain unchanged")
	}
	if len(*calls) != 0 {
		t.Fatalf("commands = %+v, want none", *calls)
	}
}

func TestWindowsInstallWithoutForceReRegistersOrphanScript(t *testing.T) {
	spec := windowsServiceTestSpec(t)
	if err := os.MkdirAll(filepath.Dir(windowsTaskScriptPath(spec)), 0o755); err != nil {
		t.Fatalf("mkdir task script dir: %v", err)
	}
	if err := os.WriteFile(windowsTaskScriptPath(spec), []byte("old script"), 0o644); err != nil {
		t.Fatalf("write existing task script: %v", err)
	}
	calls := captureWindowsServiceCommands(t, func(ctx context.Context, name string, args ...string) (serviceCommandResult, error) {
		switch name {
		case "schtasks":
			if len(args) > 0 && args[0] == "/Query" {
				return serviceCommandResult{}, errors.New("task missing")
			}
			return serviceCommandResult{}, nil
		default:
			return serviceCommandResult{}, errors.New("unexpected command")
		}
	})

	if err := (scheduledTaskServiceBackend{}).Install(context.Background(), spec, false, false); err != nil {
		t.Fatalf("install with orphan script: %v", err)
	}
	if string(mustReadFile(t, windowsTaskScriptPath(spec))) == "old script" {
		t.Fatal("expected orphan script to be rewritten")
	}
	if len(*calls) != 2 || (*calls)[1][0] != "schtasks" || (*calls)[1][1] != "/Create" {
		t.Fatalf("calls = %+v, want query then create", *calls)
	}
}

func TestWindowsInstallRemovesStartupFallbackAfterScheduledTaskRegistration(t *testing.T) {
	spec := windowsServiceTestSpec(t)
	if err := os.MkdirAll(filepath.Dir(windowsStartupItemPath()), 0o755); err != nil {
		t.Fatalf("mkdir startup dir: %v", err)
	}
	if err := os.WriteFile(windowsStartupItemPath(), []byte("fallback"), 0o644); err != nil {
		t.Fatalf("write startup item: %v", err)
	}
	calls := captureWindowsServiceCommands(t, func(ctx context.Context, name string, args ...string) (serviceCommandResult, error) {
		switch name {
		case "schtasks":
			if len(args) > 0 && args[0] == "/Query" {
				return serviceCommandResult{}, errors.New("task missing")
			}
			return serviceCommandResult{}, nil
		default:
			return serviceCommandResult{}, errors.New("unexpected command")
		}
	})

	if err := (scheduledTaskServiceBackend{}).Install(context.Background(), spec, true, false); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := os.Stat(windowsStartupItemPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("startup fallback stat err = %v, want not exist", err)
	}
	if len(*calls) != 2 || (*calls)[1][0] != "schtasks" || (*calls)[1][1] != "/Create" {
		t.Fatalf("calls = %+v, want query then create", *calls)
	}
}

func TestWindowsStopStartupFallbackKillsTaskScriptProcess(t *testing.T) {
	spec := windowsServiceTestSpec(t)
	if err := os.MkdirAll(filepath.Dir(windowsStartupItemPath()), 0o755); err != nil {
		t.Fatalf("mkdir startup dir: %v", err)
	}
	if err := os.WriteFile(windowsStartupItemPath(), []byte("launcher"), 0o644); err != nil {
		t.Fatalf("write startup item: %v", err)
	}
	calls := captureWindowsServiceCommands(t, func(ctx context.Context, name string, args ...string) (serviceCommandResult, error) {
		switch name {
		case "schtasks":
			return serviceCommandResult{}, errors.New("task missing")
		case "powershell":
			return serviceCommandResult{Stdout: "123\r\n"}, nil
		case "taskkill":
			return serviceCommandResult{}, nil
		default:
			return serviceCommandResult{}, errors.New("unexpected command")
		}
	})

	if err := (scheduledTaskServiceBackend{}).Stop(context.Background(), spec); err != nil {
		t.Fatalf("stop fallback: %v", err)
	}
	want := []string{"taskkill", "/T", "/F", "/PID", "123"}
	if len(*calls) != 3 || !reflect.DeepEqual((*calls)[2], want) {
		t.Fatalf("calls = %+v, want final %v", *calls, want)
	}
}

func TestWindowsStatusReportsRegisteredServerPID(t *testing.T) {
	spec := windowsServiceTestSpec(t)
	if err := os.MkdirAll(filepath.Dir(windowsTaskScriptPath(spec)), 0o755); err != nil {
		t.Fatalf("mkdir task script dir: %v", err)
	}
	if err := os.WriteFile(windowsTaskScriptPath(spec), []byte(renderWindowsTaskScript(spec)), 0o644); err != nil {
		t.Fatalf("write task script: %v", err)
	}
	captureWindowsServiceCommands(t, func(ctx context.Context, name string, args ...string) (serviceCommandResult, error) {
		switch name {
		case "schtasks":
			return serviceCommandResult{Stdout: "Status: Running\r\n"}, nil
		case "powershell":
			script := strings.Join(args, " ")
			if strings.Contains(script, windowsTaskScriptPath(spec)) {
				return serviceCommandResult{Stdout: "111\r\n"}, nil
			}
			if strings.Contains(script, spec.Executable) {
				return serviceCommandResult{Stdout: "222\r\n"}, nil
			}
			return serviceCommandResult{}, nil
		default:
			return serviceCommandResult{}, errors.New("unexpected command")
		}
	})

	status, err := (scheduledTaskServiceBackend{}).Status(context.Background(), spec)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.PID != 222 {
		t.Fatalf("status PID = %d, want registered server PID 222", status.PID)
	}
}

func TestWindowsStatusDoesNotTreatBareServerProcessAsServiceRunning(t *testing.T) {
	spec := windowsServiceTestSpec(t)
	if err := os.MkdirAll(filepath.Dir(windowsTaskScriptPath(spec)), 0o755); err != nil {
		t.Fatalf("mkdir task script dir: %v", err)
	}
	if err := os.WriteFile(windowsTaskScriptPath(spec), []byte(renderWindowsTaskScript(spec)), 0o644); err != nil {
		t.Fatalf("write task script: %v", err)
	}
	captureWindowsServiceCommands(t, func(ctx context.Context, name string, args ...string) (serviceCommandResult, error) {
		switch name {
		case "schtasks":
			return serviceCommandResult{Stdout: "Status: Ready\r\n"}, nil
		case "powershell":
			script := strings.Join(args, " ")
			if strings.Contains(script, windowsTaskScriptPath(spec)) {
				return serviceCommandResult{}, nil
			}
			if strings.Contains(script, spec.Executable) {
				return serviceCommandResult{Stdout: "222\r\n"}, nil
			}
			return serviceCommandResult{}, nil
		default:
			return serviceCommandResult{}, errors.New("unexpected command")
		}
	})

	status, err := (scheduledTaskServiceBackend{}).Status(context.Background(), spec)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.Running || status.PID != 0 {
		t.Fatalf("status running=%v pid=%d, want stopped with no service PID", status.Running, status.PID)
	}
}

func TestParseWindowsCommandLinePreservesPathBackslashes(t *testing.T) {
	got := parseWindowsCommandLine(`"C:\Users\Nek\AppData\Local\Builder\builder.exe" serve`)
	want := []string{`C:\Users\Nek\AppData\Local\Builder\builder.exe`, "serve"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseWindowsCommandLine = %#v, want %#v", got, want)
	}
}

func windowsServiceTestSpec(t *testing.T) serviceSpec {
	t.Helper()
	temp := t.TempDir()
	t.Setenv("APPDATA", filepath.Join(temp, "AppData", "Roaming"))
	return serviceSpec{
		Config:        config.App{PersistenceRoot: filepath.Join(temp, brand.ConfigDirName)},
		Executable:    filepath.Join(temp, "builder.exe"),
		Arguments:     []string{"serve"},
		LogDir:        filepath.Join(temp, brand.ConfigDirName, "logs"),
		StdoutLogPath: filepath.Join(temp, brand.ConfigDirName, "logs", "server.log"),
		StderrLogPath: filepath.Join(temp, brand.ConfigDirName, "logs", "server.err.log"),
		Endpoint:      "http://127.0.0.1:53082",
	}
}

func captureWindowsServiceCommands(t *testing.T, fn func(context.Context, string, ...string) (serviceCommandResult, error)) *[][]string {
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

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file %s: %v", path, err)
	}
	return data
}
