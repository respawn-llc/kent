package ptyfixture

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"core/internal/testharness/pty"
	"core/internal/testharness/pty/appfixture"
	"core/internal/testharness/testsetup"
)

const (
	ptyFixtureCommandTimeout = 30 * time.Second
	ptyFixtureTestTimeout    = 45 * time.Second
)

var ptyFixtureBinary string

func TestMain(m *testing.M) {
	var err error
	ptyFixtureBinary, err = pty.PrepareAppFixture()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
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
	testsetup.PrepareMetadataPersistenceRoot(t, persistenceRoot)
	configPath := filepath.Join(root, "process.json")
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
