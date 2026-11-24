package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"

	"core/shared/serverapi"
)

const ProjectWorkspaceContextLimit = 5

type SessionProjectWorkspaceContext struct {
	ProjectName    string
	WorkspaceCount int64
	Workspaces     []ProjectWorkspaceCatalogRow
}

func (s *Store) ReadSessionProjectWorkspaceContext(
	ctx context.Context,
	sessionID string,
) (SessionProjectWorkspaceContext, error) {
	if s == nil || s.queries == nil {
		return SessionProjectWorkspaceContext{}, errors.New("metadata store is required")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return SessionProjectWorkspaceContext{}, errors.New("session id is required")
	}

	projectID, err := s.ResolveSessionProjectID(ctx, sessionID)
	if err != nil {
		return SessionProjectWorkspaceContext{}, fmt.Errorf("resolve Session Project for Workspace context: %w", err)
	}
	projectName, err := s.queries.GetProjectDisplayName(ctx, projectID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SessionProjectWorkspaceContext{}, fmt.Errorf("%w: %q", serverapi.ErrProjectNotFound, projectID)
		}
		return SessionProjectWorkspaceContext{}, fmt.Errorf("read Project name for Workspace context: %w", err)
	}
	workspaceCount, err := s.queries.CountProjectWorkspaces(ctx, projectID)
	if err != nil {
		return SessionProjectWorkspaceContext{}, fmt.Errorf("count Project Workspaces for context: %w", err)
	}

	workspaces := []ProjectWorkspaceCatalogRow{}
	if workspaceCount > 1 {
		for offset := 0; ; {
			page, err := s.ListProjectWorkspaceCatalogPage(ctx, projectID, offset, ProjectWorkspaceContextLimit)
			if err != nil {
				return SessionProjectWorkspaceContext{}, fmt.Errorf("read Project Workspace context page: %w", err)
			}
			for _, workspace := range page.Workspaces {
				info, err := os.Stat(workspace.CanonicalRoot)
				if err != nil || !info.IsDir() {
					continue
				}
				workspaces = append(workspaces, workspace)
				if len(workspaces) == ProjectWorkspaceContextLimit {
					break
				}
			}
			if len(workspaces) == ProjectWorkspaceContextLimit || page.NextOffset == nil {
				break
			}
			offset = *page.NextOffset
		}
	}
	return SessionProjectWorkspaceContext{
		ProjectName:    projectName,
		WorkspaceCount: workspaceCount,
		Workspaces:     workspaces,
	}, nil
}
