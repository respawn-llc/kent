package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"

	"core/server/metadata"
)

type projectWorkspacesContext struct {
	ProjectName    string
	WorkspaceCount int64
	Workspaces     []metadata.ProjectWorkspaceCatalogRow
}

func loadProjectWorkspacesContext(
	ctx context.Context,
	reader ProjectWorkspaceContextReader,
	sessionID string,
) (projectWorkspacesContext, error) {
	if reader == nil {
		return projectWorkspacesContext{}, errors.New("Project Workspace context reader is required")
	}
	facts, err := reader.ReadSessionProjectWorkspaceContext(ctx, sessionID)
	if err != nil {
		return projectWorkspacesContext{}, fmt.Errorf("read Project Workspace context: %w", err)
	}
	result := projectWorkspacesContext{
		ProjectName:    facts.ProjectName,
		WorkspaceCount: facts.WorkspaceCount,
	}
	if facts.WorkspaceCount <= 1 {
		return result, nil
	}
	result.Workspaces = make([]metadata.ProjectWorkspaceCatalogRow, 0, len(facts.Workspaces))
	for _, workspace := range facts.Workspaces {
		info, err := os.Stat(workspace.CanonicalRoot)
		if err != nil || !info.IsDir() {
			continue
		}
		result.Workspaces = append(result.Workspaces, workspace)
	}
	return result, nil
}
