package architectureguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckNoProductionRecoverRejectsProductionCall(t *testing.T) {
	root := t.TempDir()
	writeGuardFixture(t, root, "bad.go", "package fixture\nfunc bad() { recover() }\n")

	err := CheckNoProductionRecover(root)
	if err == nil || !strings.Contains(err.Error(), "bad.go:2") {
		t.Fatalf("CheckNoProductionRecover error = %v, want bad.go violation", err)
	}
}

func TestCheckNoProductionRecoverAllowsTestCall(t *testing.T) {
	root := t.TempDir()
	writeGuardFixture(t, root, "allowed_test.go", "package fixture\nfunc allowed() { recover() }\n")

	if err := CheckNoProductionRecover(root); err != nil {
		t.Fatalf("CheckNoProductionRecover: %v", err)
	}
}

func TestCheckNoProductionRecoverRespectsNestedModuleBoundary(t *testing.T) {
	root := t.TempDir()
	writeGuardFixture(t, root, "go.mod", "module fixture\n")
	writeGuardFixture(t, root, "allowed.go", "package fixture\n")
	nested := filepath.Join(root, "dependency")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	writeGuardFixture(t, nested, "go.mod", "module dependency\n")
	writeGuardFixture(t, nested, "bad.go", "package dependency\nfunc bad(){ recover() }\n")
	if err := CheckNoProductionRecover(root); err != nil {
		t.Fatalf("parent module checked dependency source: %v", err)
	}
	if err := CheckNoProductionRecover(nested); err == nil {
		t.Fatal("selected module's own violations were skipped")
	}
}

func writeGuardFixture(t *testing.T, root string, name string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
