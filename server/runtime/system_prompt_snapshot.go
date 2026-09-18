package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"core/prompts"
	"core/server/session"
	"core/shared/config"
	"core/shared/toolspec"
)

type systemPromptSnapshotOptions struct {
	WorkspaceRoot    string
	GlobalConfigDir  string
	SystemPromptFile *config.SystemPromptFile
}

func (e *Engine) buildSystemPromptSnapshotForRoot(locked session.LockedContract, workspaceRoot string) (string, error) {
	enabledTools := toolIDsFromNames(locked.EnabledTools)
	if len(enabledTools) == 0 && !locked.HasEnabledTools {
		enabledTools = e.cfg.EnabledTools
	}
	return e.buildSystemPromptSnapshotFromConfig(locked, workspaceRoot, systemPromptSnapshotOptions{
		WorkspaceRoot:    workspaceRoot,
		GlobalConfigDir:  e.cfg.GlobalConfigDir,
		SystemPromptFile: e.cfg.SystemPromptFile,
	}, enabledTools)
}

func (e *Engine) buildSystemPromptSnapshotFromConfig(locked session.LockedContract, workspaceRoot string, opts systemPromptSnapshotOptions, enabledTools []toolspec.ID) (string, error) {
	includeToolPreambles := true
	if locked.ToolPreambles != nil {
		includeToolPreambles = *locked.ToolPreambles
	}
	args := prompts.SystemPromptTemplateArgs{
		EstimatedToolCallsForContext: config.EstimatedToolCallsForContextWindow(e.cfg.ContextWindowTokens, e.cfg.EffectiveContextWindowPercent),
		EditingToolName:              editingToolName(enabledTools),
	}
	opts.WorkspaceRoot = workspaceRoot
	template, sourcePath, hasCustom, err := readSystemPromptTemplate(opts)
	if err != nil {
		return "", err
	}
	if hasCustom {
		rendered, err := prompts.RenderCustomSystemPrompt(template, includeToolPreambles, args)
		if err != nil {
			return "", fmt.Errorf("render system prompt file %q: %w", sourcePath, err)
		}
		return rendered, nil
	}
	return prompts.WithToolPreambles(prompts.BaseSystemPrompt(args), includeToolPreambles), nil
}

func editingToolName(enabled []toolspec.ID) string {
	for _, id := range enabled {
		if id == toolspec.ToolPatch {
			return "patch"
		}
		if id == toolspec.ToolEdit {
			return "edit"
		}
	}
	return "shell"
}

func (e *Engine) systemPromptWorkspaceRoot() string {
	if e == nil {
		return ""
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.systemPromptWorkspaceRootLocked()
}

func (e *Engine) systemPromptWorkspaceRootLocked() string {
	activeRoot := ""
	if e.transcriptState != nil {
		activeRoot = e.transcriptState.WorkingDir()
	}
	if activeRoot == "" {
		activeRoot = strings.TrimSpace(e.cfg.TranscriptWorkingDir)
	}
	persistedRoot := strings.TrimSpace(e.store.Meta().WorkspaceRoot)
	if activeRoot == "" {
		return persistedRoot
	}
	if persistedRoot == "" {
		return activeRoot
	}
	if pathWithinRoot(activeRoot, persistedRoot) {
		return persistedRoot
	}
	return activeRoot
}

func readSystemPromptTemplate(opts systemPromptSnapshotOptions) (string, string, bool, error) {
	paths, err := systemPromptPathsWithConfig(opts)
	if err != nil {
		return "", "", false, err
	}
	for _, path := range paths {
		data, readErr := os.ReadFile(path)
		if readErr == nil {
			template := strings.TrimSpace(string(data))
			if template == "" {
				continue
			}
			return template, path, true, nil
		}
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		return "", "", false, fmt.Errorf("read system prompt file %q: %w", path, readErr)
	}
	return "", "", false, nil
}

func systemPromptPathsWithConfig(opts systemPromptSnapshotOptions) ([]string, error) {
	paths := make([]string, 0, 3)
	addPath := func(path string) {
		trimmed := strings.TrimSpace(path)
		if trimmed != "" {
			paths = append(paths, trimmed)
		}
	}
	absWorkspace := ""
	if trimmed := strings.TrimSpace(opts.WorkspaceRoot); trimmed != "" {
		var err error
		absWorkspace, err = filepath.Abs(trimmed)
		if err != nil {
			return nil, fmt.Errorf("resolve workspace root: %w", err)
		}
	}
	addConfigPath := func(scope config.SystemPromptFileScope) {
		if opts.SystemPromptFile != nil && opts.SystemPromptFile.Scope == scope {
			addPath(opts.SystemPromptFile.Path)
		}
	}
	addConfigPath(config.SystemPromptFileScopeSubagent)
	addConfigPath(config.SystemPromptFileScopeWorkspaceConfig)
	if absWorkspace != "" {
		addPath(filepath.Join(absWorkspace, agentsGlobalDirName, systemPromptFileName))
	}
	addConfigPath(config.SystemPromptFileScopeHomeConfig)
	if globalDir, err := resolveGlobalConfigDir(opts.GlobalConfigDir); err == nil {
		addPath(filepath.Join(globalDir, systemPromptFileName))
	}
	return paths, nil
}

func pathWithinRoot(path string, root string) bool {
	absPath, pathErr := filepath.Abs(strings.TrimSpace(path))
	absRoot, rootErr := filepath.Abs(strings.TrimSpace(root))
	if pathErr != nil || rootErr != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func (e *Engine) reviewerSystemPrompt(ctx context.Context) (string, error) {
	if prompt, ok, err := e.ensureReviewerPromptFresh(ctx); err != nil {
		return "", err
	} else if ok {
		return prompt, nil
	}
	if prompt, ok := e.lockedReviewerPromptSnapshot(); ok {
		return prompt, nil
	}
	prompt, err := e.buildReviewerPromptSnapshot()
	if err != nil {
		return "", err
	}
	if e.cfg.Reviewer.SystemPromptFile == nil {
		return prompt, nil
	}
	if err := e.store.BackfillLockedReviewerPrompt(prompt); err != nil {
		return "", err
	}
	if prompt, ok := e.lockedReviewerPromptSnapshot(); ok {
		return prompt, nil
	}
	e.lockedContractState().FillReviewerPrompt(prompt)
	return prompt, nil
}

func (e *Engine) lockedReviewerPromptSnapshot() (string, bool) {
	if e == nil {
		return "", false
	}
	return e.lockedContractState().ReviewerPromptSnapshot()
}

// errReadReviewerSystemPromptFile wraps failures to read the configured reviewer.system_prompt_file.
var errReadReviewerSystemPromptFile = errors.New("read reviewer.system_prompt_file")

func (e *Engine) buildReviewerPromptSnapshot() (string, error) {
	path := e.cfg.Reviewer.SystemPromptFile
	if path == nil {
		return prompts.ReviewerSystemPrompt, nil
	}
	return buildReviewerPromptSnapshotFromFile(*path)
}

func buildReviewerPromptSnapshotFromFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("%w %q: %w", errReadReviewerSystemPromptFile, path, err)
	}
	return strings.TrimSpace(string(data)), nil
}
