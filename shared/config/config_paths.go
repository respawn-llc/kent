package config

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

var processStartHome = os.Getenv("HOME")
var processStartAccountHome = accountHomeDir()

func accountHomeDir() string {
	current, err := user.Current()
	if err != nil || current == nil {
		return ""
	}
	return strings.TrimSpace(current.HomeDir)
}

func currentHomeDir() (string, error) {
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return home, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return home, nil
}

func expandTildePath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" || !strings.HasPrefix(trimmed, "~") {
		return trimmed, nil
	}
	home, err := currentHomeDir()
	if err != nil {
		return "", err
	}
	if trimmed == "~" {
		return home, nil
	}
	if strings.HasPrefix(trimmed, "~/") {
		return filepath.Join(home, strings.TrimPrefix(trimmed, "~/")), nil
	}
	if strings.HasPrefix(trimmed, "~\\") {
		return filepath.Join(home, strings.TrimPrefix(trimmed, "~\\")), nil
	}
	return trimmed, nil
}

func preparePersistenceRoot(path string) (string, error) {
	expanded, err := expandTildePath(path)
	if err != nil {
		return "", fmt.Errorf("expand persistence root: %w", err)
	}
	absRoot, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("resolve persistence root: %w", err)
	}
	if err := refuseRealPersistenceRootUnderGoTest(absRoot); err != nil {
		return "", err
	}
	if err := os.MkdirAll(absRoot, 0o755); err != nil {
		return "", fmt.Errorf("create persistence root: %w", err)
	}
	return absRoot, nil
}

func refuseRealPersistenceRootUnderGoTest(absRoot string) error {
	if os.Getenv("BUILDER_ALLOW_REAL_PERSISTENCE_ROOT_IN_TESTS") == "1" {
		return nil
	}
	if !testing.Testing() {
		return nil
	}
	for _, home := range protectedPersistenceRootHomes() {
		realRoot, err := filepath.Abs(filepath.Join(home, ".builder"))
		if err != nil {
			return fmt.Errorf("resolve protected persistence root: %w", err)
		}
		if filepath.Clean(absRoot) == filepath.Clean(realRoot) {
			return fmt.Errorf("refusing to use protected persistence root %s from a Go test binary; tests must provide an isolated config root before calling Load", absRoot)
		}
	}
	return nil
}

func protectedPersistenceRootHomes() []string {
	accountHome := strings.TrimSpace(processStartAccountHome)
	envHome := strings.TrimSpace(processStartHome)
	if accountHome == "" {
		if envHome == "" {
			return nil
		}
		return []string{envHome}
	}
	if envHome == "" || filepath.Clean(envHome) == filepath.Clean(accountHome) {
		return []string{accountHome}
	}
	if isPathInsideTempDir(envHome) {
		return []string{accountHome}
	}
	return []string{accountHome, envHome}
}

func isPathInsideTempDir(path string) bool {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return false
	}
	absPath, err := filepath.Abs(trimmed)
	if err != nil {
		return false
	}
	absTemp, err := filepath.Abs(os.TempDir())
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absTemp, absPath)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

func prepareWorktreeBaseDir(persistenceRoot string, path string) (string, error) {
	raw := strings.TrimSpace(path)
	if raw == "" {
		raw = filepath.Join(persistenceRoot, "worktrees")
	}
	expanded, err := expandTildePath(raw)
	if err != nil {
		return "", fmt.Errorf("expand worktree base dir: %w", err)
	}
	resolved := expanded
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(persistenceRoot, resolved)
	}
	absRoot, err := filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("resolve worktree base dir: %w", err)
	}
	if err := os.MkdirAll(absRoot, 0o755); err != nil {
		return "", fmt.Errorf("create worktree base dir: %w", err)
	}
	return absRoot, nil
}
