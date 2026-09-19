package metadata

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"core/server/metadata/sqlitegen"
)

func (s *Store) ResolveSessionProjectID(ctx context.Context, sessionID string) (string, error) {
	row, err := s.resolveSessionExecutionTargetRow(ctx, sessionID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(row.ProjectID) == "" {
		return "", errors.New("Session Project identity is required")
	}
	return row.ProjectID, nil
}

// ResolveProjectSourceWorkspaceID resolves the Project's authoritative source
// Workspace through its exact default attachment.
func ResolveProjectSourceWorkspaceID(ctx context.Context, q projectSourceWorkspaceQueries, projectID string) (string, error) {
	if q == nil {
		return "", errors.New("metadata queries are required")
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return "", errors.New("project id is required")
	}

	primaryWorkspaceID, err := q.GetProjectPrimaryWorkspaceID(ctx, projectID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(primaryWorkspaceID) == "" {
		return "", fmt.Errorf("project %q has no default Workspace", projectID)
	}
	primaryWorkspace, err := q.GetWorkspaceByID(ctx, primaryWorkspaceID)
	if err != nil {
		return "", err
	}
	if primaryWorkspace.ProjectID != projectID {
		return "", fmt.Errorf("project %q default Workspace belongs to another Project", projectID)
	}
	return primaryWorkspace.ID, nil
}

type projectSourceWorkspaceQueries interface {
	GetProjectPrimaryWorkspaceID(context.Context, string) (string, error)
	GetWorkspaceByID(context.Context, string) (sqlitegen.Workspace, error)
}
