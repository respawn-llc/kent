package startupconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"core/shared/config"
	"core/shared/sessioncontract"
	"core/shared/sessionenv"
)

type Request struct {
	WorkspaceRoot string
	LoadOptions   config.LoadOptions
}

type SessionConfigResult struct {
	Config config.Connection
	Local  config.LocalPreferences
}

func ResolveSessionConfig(req Request) (SessionConfigResult, error) {
	workspaceRoot, err := ResolveWorkspaceRoot(req.WorkspaceRoot)
	if err != nil {
		return SessionConfigResult{}, err
	}
	connection, local, err := config.LoadInteractiveConnectionDiscovery(workspaceRoot, req.LoadOptions)
	if err != nil {
		return SessionConfigResult{}, err
	}
	return SessionConfigResult{Config: connection, Local: local}, nil
}

func ResolveRunPromptConfig(req Request) (config.Connection, error) {
	workspaceRoot, err := ResolveWorkspaceRoot(req.WorkspaceRoot)
	if err != nil {
		return config.Connection{}, err
	}
	return config.LoadConnectionDiscovery(workspaceRoot, req.LoadOptions)
}

func ResolveWorkspaceRoot(workspaceRoot string) (string, error) {
	trimmed := strings.TrimSpace(workspaceRoot)
	if trimmed == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		trimmed = cwd
	}
	return filepath.Abs(trimmed)
}

// ErrWorkspaceContextSessionMissing marks the implicit workspace-context
// session lookup that failed because the referenced session no longer exists.
// It wraps sessioncontract.ErrSessionNotFound so callers and tests can
// distinguish the workspace-context guidance path from a strict explicit
// session lookup with errors.Is rather than matching rendered message text.
var ErrWorkspaceContextSessionMissing = errors.New("workspace context session is missing")

func WorkspaceContextSessionError(sessionID string, err error) error {
	if errors.Is(err, sessioncontract.ErrSessionNotFound) {
		return fmt.Errorf("%s points to missing Kent session %q; unset %s or run from a live Kent shell: %w: %w", sessionenv.SessionIDEnv, strings.TrimSpace(sessionID), sessionenv.SessionIDEnv, ErrWorkspaceContextSessionMissing, err)
	}
	return fmt.Errorf("resolve %s workspace context %q: %w", sessionenv.SessionIDEnv, strings.TrimSpace(sessionID), err)
}
