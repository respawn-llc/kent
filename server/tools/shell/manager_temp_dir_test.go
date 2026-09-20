package shell

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"core/internal/testharness/postprocessfixture"
	"core/server/tools/shell/postprocess"
	"core/shared/config"
)

func TestManagerRunsCommandAfterTemporaryDirectoryIsDeleted(t *testing.T) {
	manager := newShellTestManager(t, 15*time.Second)
	directory := manager.TempDir()
	if err := os.Remove(directory); err != nil {
		t.Fatalf("remove empty manager directory: %v", err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	result, err := manager.Start(context.Background(), ExecRequest{
		Postprocessor: postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}),
		Command:       []string{executable, "-test.run=^$"},
		Workdir:       t.TempDir(),
		YieldTime:     15 * time.Second,
	})
	if err != nil {
		t.Fatalf("start command after directory deletion: %v", err)
	}
	if result.ExitCode == nil || *result.ExitCode != 0 {
		t.Fatalf("command result = %+v, want successful completion", result)
	}
	if filepath.Dir(result.OutputPath) != directory {
		t.Fatalf("log path = %q, want original manager directory %q", result.OutputPath, directory)
	}
	if _, err := os.Stat(result.OutputPath); err != nil {
		t.Fatalf("read command log in recreated directory: %v", err)
	}
}
