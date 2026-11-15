package testsetup

import (
	"crypto/rand"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"core/shared/runtimeids"
)

// ShellLogSessionShortID isolates shell logs and removes only this fixture's session directory.
func ShellLogSessionShortID(t testing.TB) runtimeids.SessionShortID {
	t.Helper()
	value, err := rand.Int(rand.Reader, big.NewInt(runtimeids.MaxSessionShortID))
	if err != nil {
		t.Fatal(err)
	}
	id, err := runtimeids.NewSessionShortID(value.Int64() + 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		root := "/tmp"
		if runtime.GOOS == "windows" {
			root = os.TempDir()
		}
		if err := os.RemoveAll(filepath.Join(root, "kent", id.String())); err != nil {
			t.Errorf("remove test session logs: %v", err)
		}
	})
	return id
}
