package startupconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"builder/server/bootstrap"
	"builder/shared/config"
	"builder/shared/sessioncontract"
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
	ContextAgentRole      string
}

func ResolveSessionConfig(req Request) (config.App, error) {
	workspaceRoot, err := ResolveWorkspaceRoot(req.WorkspaceRoot)
	if err != nil {
		return config.App{}, err
	}
	plan, err := bootstrap.ResolveConfig(bootstrap.Request{
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
	plan, err := bootstrap.ResolveConfig(bootstrap.Request{
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
	contextAgentRole := ""
	if contextSessionID != "" {
		if agentRole, err := bootstrap.ResolveSessionAgentRole(plan.Config.PersistenceRoot, contextSessionID); err == nil {
			contextAgentRole = agentRole
		}
	}
	resolvedRoot := workspaceRoot
	if strings.TrimSpace(plan.Config.WorkspaceRoot) != "" && plan.Config.WorkspaceRoot != workspaceRoot {
		resolvedRoot = plan.Config.WorkspaceRoot
	}
	return RunPromptResult{Config: plan.Config, ResolvedWorkspaceRoot: resolvedRoot, ContextAgentRole: contextAgentRole}, nil
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
	if errors.Is(err, sessioncontract.ErrSessionNotFound) {
		return fmt.Errorf("%s points to missing Kent session %q; unset %s or run from a live Kent shell: %w", sessionenv.BuilderSessionID, strings.TrimSpace(sessionID), sessionenv.BuilderSessionID, err)
	}
	return fmt.Errorf("resolve %s workspace context %q: %w", sessionenv.BuilderSessionID, strings.TrimSpace(sessionID), err)
}
