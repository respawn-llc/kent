package sessionpath

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"core/shared/sessioncontract"
)

// ErrOutsideWorkspaceContainer is returned when a resolved session directory
// escapes the workspace container.
var ErrOutsideWorkspaceContainer = errors.New("outside workspace container")

func ResolveScopedSessionDir(containerDir string, sessionID string) (string, error) {
	absContainerDir, candidateSessionDir, err := candidateScopedSessionDir(containerDir, sessionID)
	if err != nil {
		return "", err
	}
	realContainerDir, realSessionDir, err := resolveRealSessionPath(absContainerDir, candidateSessionDir)
	if err != nil {
		return "", err
	}
	if !isDescendantPath(realContainerDir, realSessionDir) {
		return "", fmt.Errorf("session %q is %w", strings.TrimSpace(sessionID), ErrOutsideWorkspaceContainer)
	}
	return realSessionDir, nil
}

func candidateScopedSessionDir(containerDir string, sessionID string) (string, string, error) {
	trimmedContainerDir := strings.TrimSpace(containerDir)
	trimmedSessionID := strings.TrimSpace(sessionID)
	if trimmedContainerDir == "" {
		return "", "", fmt.Errorf("workspace container dir is required")
	}
	if trimmedSessionID == "" {
		return "", "", fmt.Errorf("session id is required")
	}
	if filepath.IsAbs(trimmedSessionID) || trimmedSessionID == "." || trimmedSessionID == ".." {
		return "", "", fmt.Errorf("session id %q is invalid", trimmedSessionID)
	}
	if strings.Contains(trimmedSessionID, "/") || strings.Contains(trimmedSessionID, "\\") {
		return "", "", fmt.Errorf("session id %q is invalid", trimmedSessionID)
	}
	if cleaned := filepath.Clean(trimmedSessionID); cleaned != trimmedSessionID {
		return "", "", fmt.Errorf("session id %q is invalid", trimmedSessionID)
	}
	absContainerDir, err := filepath.Abs(trimmedContainerDir)
	if err != nil {
		return "", "", fmt.Errorf("resolve workspace container dir: %w", err)
	}
	return absContainerDir, filepath.Join(absContainerDir, trimmedSessionID), nil
}

func resolveRealSessionPath(containerDir string, sessionDir string) (string, string, error) {
	realContainerDir, err := filepath.EvalSymlinks(containerDir)
	if err != nil {
		return "", "", fmt.Errorf("resolve workspace container dir: %w", err)
	}
	realSessionDir, err := filepath.EvalSymlinks(sessionDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", fmt.Errorf("%w: session dir %q not found: %w", sessioncontract.ErrSessionNotFound, filepath.Base(sessionDir), err)
		}
		return "", "", fmt.Errorf("resolve session dir: %w", err)
	}
	return realContainerDir, realSessionDir, nil
}

func isDescendantPath(parent string, child string) bool {
	cleanParent := filepath.Clean(strings.TrimSpace(parent))
	cleanChild := filepath.Clean(strings.TrimSpace(child))
	if cleanParent == "" || cleanChild == "" {
		return false
	}
	if cleanParent == cleanChild {
		return true
	}
	return strings.HasPrefix(cleanChild, cleanParent+string(filepath.Separator))
}
