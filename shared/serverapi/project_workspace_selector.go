package serverapi

import (
	"strings"
)

type ProjectWorkspaceSelector struct {
	WorkspaceID   *string `json:"workspace_id,omitempty"`
	WorkspaceRoot *string `json:"workspace_root,omitempty"`
}

func NewProjectWorkspaceSelectorForID(workspaceID string) ProjectWorkspaceSelector {
	return ProjectWorkspaceSelector{WorkspaceID: normalizeWorkspaceSelectorArm(&workspaceID)}
}

func NewProjectWorkspaceSelectorForRoot(workspaceRoot string) ProjectWorkspaceSelector {
	return ProjectWorkspaceSelector{WorkspaceRoot: normalizeWorkspaceSelectorArm(&workspaceRoot)}
}

func (s ProjectWorkspaceSelector) WorkspaceIDValue() *string {
	return normalizeWorkspaceSelectorArm(s.WorkspaceID)
}

func (s ProjectWorkspaceSelector) WorkspaceRootValue() *string {
	return normalizeWorkspaceSelectorArm(s.WorkspaceRoot)
}

func normalizeWorkspaceSelectorArm(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}
