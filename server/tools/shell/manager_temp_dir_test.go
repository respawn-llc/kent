package shell

import (
	"context"
	"core/internal/testharness/testsetup"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"core/internal/testharness/postprocessfixture"
	"core/server/tools/shell/postprocess"
	"core/shared/config"
)

func sessionLogRequest(t *testing.T) ExecRequest {
	t.Helper()
	return ExecRequest{
		OwnerSessionShortID: testsetup.ShellLogSessionShortID(t),
		Postprocessor:       postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}),
		Command:             []string{"/bin/sh", "-c", "printf first"},
		Workdir:             t.TempDir(),
		YieldTime:           15 * time.Second,
	}
}

func TestManagerLogUsesNumericSessionDirectory(t *testing.T) {
	manager := newShellTestManager(t, 15*time.Second)
	request := sessionLogRequest(t)
	result, err := manager.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	root := "/tmp"
	if runtime.GOOS == "windows" {
		root = os.TempDir()
	}
	want := filepath.Join(root, "kent", request.OwnerSessionShortID.String(), result.SessionID+".log")
	if result.OutputPath != want {
		t.Fatalf("log path = %q, want %q", result.OutputPath, want)
	}
	if log, err := os.ReadFile(want); err != nil || string(log) != "first" {
		t.Fatalf("log = %q, error = %v", log, err)
	}
}

func TestManagerRejectsMissingSessionIdentityWithoutExecuting(t *testing.T) {
	manager := newShellTestManager(t, 15*time.Second)
	request := sessionLogRequest(t)
	request.OwnerSessionShortID = nil
	marker := filepath.Join(request.Workdir, "executed")
	request.Command = []string{"/bin/sh", "-c", "touch executed"}
	if _, err := manager.Start(context.Background(), request); err == nil {
		t.Fatal("expected missing Session short ID to reject command")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("command side effect = %v, want no execution", err)
	}
}

func TestManagerLogUsesUUIDForSessionWithoutShortID(t *testing.T) {
	ownerID := testsetup.ShellLogSessionID(t)
	manager := newShellTestManager(t, 15*time.Second)
	request := sessionLogRequest(t)
	request.OwnerSessionShortID = nil
	request.OwnerSessionID = ownerID.String()
	result, err := manager.Start(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(filepath.Dir(result.OutputPath)) != ownerID.String() {
		t.Fatalf("legacy Session log path = %s", result.OutputPath)
	}
	if log, err := os.ReadFile(result.OutputPath); err != nil || string(log) != "first" {
		t.Fatalf("output=%q err=%v", log, err)
	}
}

func TestManagersDoNotOverwriteSameSessionLogs(t *testing.T) {
	firstManager := newShellTestManager(t, 15*time.Second)
	secondManager := newShellTestManager(t, 15*time.Second)
	request := sessionLogRequest(t)
	first, err := firstManager.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	request.Command = []string{"/bin/sh", "-c", "printf second"}
	second, err := secondManager.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.OutputPath == second.OutputPath {
		t.Fatalf("managers reused log path %q", first.OutputPath)
	}
	for path, want := range map[string]string{first.OutputPath: "first", second.OutputPath: "second"} {
		if log, err := os.ReadFile(path); err != nil || string(log) != want {
			t.Fatalf("log %q = %q, want %q, error = %v", path, log, want, err)
		}
	}
}

func TestManagerRunsCommandAfterTemporaryDirectoryIsDeleted(t *testing.T) {
	manager := newShellTestManager(t, 15*time.Second)
	request := sessionLogRequest(t)
	first, err := manager.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Dir(first.OutputPath)
	if err := os.Remove(first.OutputPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(directory); err != nil {
		t.Fatalf("remove empty session directory: %v", err)
	}
	result, err := manager.Start(context.Background(), request)
	if err != nil {
		t.Fatalf("start command after directory deletion: %v", err)
	}
	if result.ExitCode == nil || *result.ExitCode != 0 {
		t.Fatalf("command result = %+v, want successful completion", result)
	}
	if filepath.Dir(result.OutputPath) != directory {
		t.Fatalf("log path = %q, want original session directory %q", result.OutputPath, directory)
	}
	if _, err := os.Stat(result.OutputPath); err != nil {
		t.Fatalf("read command log in recreated directory: %v", err)
	}
}
