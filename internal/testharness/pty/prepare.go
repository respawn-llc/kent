package pty

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// PrepareAppFixture builds the app test process before the consuming package's
// testing.M.Run starts its test deadline.
func PrepareAppFixture() (string, error) {
	root, err := fixtureModuleRoot()
	if err != nil {
		return "", err
	}
	binary := filepath.Join(root, ".generated", "test-binaries", "app.test")
	if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
		return "", fmt.Errorf("create app fixture directory: %w", err)
	}
	command := exec.Command("go", "test", "-c", "-ldflags=-s -w", "-o", binary, "core/cli/app")
	if err := prepareFixture(root, command); err != nil {
		return "", err
	}
	return binary, nil
}

// PrepareKentFixture builds the production binary through its native build recipe.
func PrepareKentFixture() (string, error) {
	root, err := fixtureModuleRoot()
	if err != nil {
		return "", err
	}
	if err := prepareFixture(root, exec.Command("just", "--quiet", "_prepare-pty-kent")); err != nil {
		return "", err
	}
	return filepath.Join(root, ".generated", "test-binaries", "pty", "kent"), nil
}

func fixtureModuleRoot() (string, error) {
	command := exec.Command("go", "env", "GOMOD")
	command.Stderr = os.Stderr
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("run %s: %w", command, err)
	}
	module := strings.TrimSpace(string(output))
	if !filepath.IsAbs(module) || module == os.DevNull {
		return "", fmt.Errorf("go env GOMOD did not locate a module: %q", module)
	}
	return filepath.Dir(module), nil
}

func prepareFixture(root string, command *exec.Cmd) error {
	command.Dir = root
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if directory, configured := os.LookupEnv("KENT_TEST_COMPILER_TMPDIR"); configured {
		if strings.TrimSpace(directory) == "" {
			return fmt.Errorf("compiler temporary directory is required")
		}
		command.Env = append(os.Environ(), "GOTMPDIR="+directory)
	}
	if err := command.Run(); err != nil {
		return fmt.Errorf("prepare PTY fixture with %s: %w", command, err)
	}
	return nil
}
