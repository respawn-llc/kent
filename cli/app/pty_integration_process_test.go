package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"core/internal/testharness/pty/appfixture"
)

const (
	ptyFixtureCommandTimeout = 30 * time.Second
	ptyFixtureTestTimeout    = 45 * time.Second
)

func ptyFixtureExecutable(t *testing.T) string {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatalf("locate current app test executable: %v", err)
	}
	return binary
}

func ptyFixtureProcessEnv(
	t *testing.T,
	root string,
	workspaceRoot string,
	persistenceRoot string,
	scriptPath string,
	observationPath string,
) string {
	t.Helper()
	configPath := filepath.Join(root, "process.json")
	reportFixtureDiagnosticsOnFailure(t, configPath)
	if err := appfixture.WriteProcessConfig(configPath, appfixture.ProcessConfig{
		WorkspaceRoot:   workspaceRoot,
		PersistenceRoot: persistenceRoot,
		ScriptPath:      scriptPath,
		ObservationPath: observationPath,
	}); err != nil {
		t.Fatalf("write fixture process config: %v", err)
	}
	return appfixture.ProcessConfigEnvName + "=" + configPath
}

func reportFixtureDiagnosticsOnFailure(t *testing.T, configPath string) {
	t.Helper()
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		path := appfixture.ProcessDiagnosticLogPath(configPath)
		diagnostics, err := os.ReadFile(path)
		if err != nil {
			t.Logf("read fixture diagnostics %s: %v", path, err)
			return
		}
		t.Logf("fixture diagnostics %s:\n%s", path, diagnostics)
	})
}
