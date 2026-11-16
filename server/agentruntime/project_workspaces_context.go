package runtime

import (
	"context"
	"errors"
	"fmt"

	"core/server/metadata"
)

type projectWorkspacesContext = metadata.SessionProjectWorkspaceContext

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
	return facts, nil
}
