package serverapi

import (
	"strings"

	"core/shared/runtimeids"
)

type ProjectHomeSummary struct {
	ProjectID            string                  `json:"project_id"`
	ProjectKey           string                  `json:"project_key"`
	DisplayName          string                  `json:"display_name"`
	PrimaryWorkspace     ProjectWorkspaceSummary `json:"primary_workspace"`
	DefaultWorkflowID    *runtimeids.WorkflowID  `json:"default_workflow_id"`
	DefaultWorkflowName  string                  `json:"default_workflow_name,omitempty"`
	DefaultWorkflowValid bool                    `json:"default_workflow_valid"`
	UpdatedAtUnixMs      int64                   `json:"updated_at_unix_ms"`
	TaskCount            int                     `json:"task_count"`
	AttentionCount       int                     `json:"attention_count"`
	WorkflowCount        int                     `json:"workflow_count"`
}

// ProjectWorkspaceSummary is also used by Workflow read models.
type ProjectWorkspaceSummary struct {
	WorkspaceID     string `json:"workspace_id"`
	DisplayName     string `json:"display_name"`
	RootPath        string `json:"root_path"`
	Availability    string `json:"availability"`
	IsPrimary       bool   `json:"is_primary"`
	UpdatedAtUnixMs int64  `json:"updated_at_unix_ms"`
}

// IsFilesystemRootPath recognizes filesystem roots without applying local OS path rules.
func IsFilesystemRootPath(path string) bool {
	trimmed := strings.TrimSpace(path)
	if trimmed == "/" {
		return true
	}
	if len(trimmed) == 3 &&
		((trimmed[0] >= 'a' && trimmed[0] <= 'z') || (trimmed[0] >= 'A' && trimmed[0] <= 'Z')) &&
		trimmed[1] == ':' && isPathSeparator(trimmed[2]) {
		return true
	}
	if len(trimmed) < 5 || !isPathSeparator(trimmed[0]) || !isPathSeparator(trimmed[1]) {
		return false
	}
	index := 2
	serverStart := index
	for index < len(trimmed) && !isPathSeparator(trimmed[index]) {
		index++
	}
	if index == serverStart {
		return false
	}
	for index < len(trimmed) && isPathSeparator(trimmed[index]) {
		index++
	}
	shareStart := index
	for index < len(trimmed) && !isPathSeparator(trimmed[index]) {
		index++
	}
	if index == shareStart {
		return false
	}
	for index < len(trimmed) {
		if !isPathSeparator(trimmed[index]) {
			return false
		}
		index++
	}
	return true
}

func isPathSeparator(value byte) bool {
	return value == '/' || value == '\\'
}
