package startupconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	serverbootstrap "builder/server/bootstrap"
	"builder/server/session"
	"builder/shared/config"
	"builder/shared/sessionenv"
)

type Request struct {
	WorkspaceRoot             string
	WorkspaceRootExplicit     bool
	SessionID                 string
	WorkspaceContextSessionID string
	OpenAIBaseURL             string
	OpenAIBaseURLExplicit     bool
	LoadOptions               config.LoadOptions
}

type RunPromptResult struct {
	Config                config.App
	ResolvedWorkspaceRoot string
}

func ResolveSessionConfig(req Request) (config.App, error) {
	workspaceRoot, err := ResolveWorkspaceRoot(req.WorkspaceRoot)
	if err != nil {
		return config.App{}, err
	}
	plan, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{
		WorkspaceRoot:         workspaceRoot,
		WorkspaceRootExplicit: req.WorkspaceRootExplicit,
		SessionID:             req.SessionID,
		OpenAIBaseURL:         req.OpenAIBaseURL,
		OpenAIBaseURLExplicit: req.OpenAIBaseURLExplicit,
		LoadOptions:           req.LoadOptions,
	})
	if err != nil {
		return config.App{}, err
	}
	return plan.Config, nil
}

func ResolveRunPromptConfig(req Request) (RunPromptResult, error) {
	workspaceRoot, err := ResolveWorkspaceRoot(req.WorkspaceRoot)
	if err != nil {
		return RunPromptResult{}, err
	}
	sessionID := strings.TrimSpace(req.SessionID)
	contextSessionID := strings.TrimSpace(req.WorkspaceContextSessionID)
	if sessionID == "" && !req.WorkspaceRootExplicit {
		sessionID = contextSessionID
	}
	plan, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{
		WorkspaceRoot:         workspaceRoot,
		WorkspaceRootExplicit: req.WorkspaceRootExplicit,
		SessionID:             sessionID,
		OpenAIBaseURL:         req.OpenAIBaseURL,
		OpenAIBaseURLExplicit: req.OpenAIBaseURLExplicit,
	})
	if err != nil {
		if sessionID != "" && sessionID == contextSessionID {
			return RunPromptResult{}, workspaceContextSessionError(contextSessionID, err)
		}
		return RunPromptResult{}, err
	}
	resolvedRoot := workspaceRoot
	if strings.TrimSpace(plan.Config.WorkspaceRoot) != "" && plan.Config.WorkspaceRoot != workspaceRoot {
		resolvedRoot = plan.Config.WorkspaceRoot
	}
	return RunPromptResult{Config: plan.Config, ResolvedWorkspaceRoot: resolvedRoot}, nil
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

func workspaceContextSessionError(sessionID string, err error) error {
	if errors.Is(err, session.ErrSessionNotFound) {
		return fmt.Errorf("%s points to missing Builder session %q; unset %s or run from a live Builder shell: %w", sessionenv.BuilderSessionID, strings.TrimSpace(sessionID), sessionenv.BuilderSessionID, err)
	}
	return fmt.Errorf("resolve %s workspace context %q: %w", sessionenv.BuilderSessionID, strings.TrimSpace(sessionID), err)
}
