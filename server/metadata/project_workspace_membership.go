package metadata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"core/server/metadata/sqlitegen"
	"core/shared/serverapi"
)

// FindContainingProjectWorkspace returns the root-most attached ancestor of a
// resolved absolute path. A nil Workspace means the Project has no attachment
// containing that path; missing Projects and failed reads are errors.
func (s *Store) FindContainingProjectWorkspace(ctx context.Context, projectID, resolvedPath string) (*sqlitegen.Workspace, error) {
	if !filepath.IsAbs(resolvedPath) {
		return nil, errors.New("Workspace membership requires an absolute resolved path")
	}
	ancestors := []string{}
	for path := filepath.Clean(resolvedPath); ; path = filepath.Dir(path) {
		ancestors = append(ancestors, path)
		if filepath.Dir(path) == path {
			break
		}
	}
	encoded, err := json.Marshal(ancestors)
	if err != nil {
		return nil, err
	}
	row, err := s.queries.FindContainingProjectWorkspace(ctx, sqlitegen.FindContainingProjectWorkspaceParams{
		ProjectID: projectID, AncestorsJson: string(encoded),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %q", serverapi.ErrProjectNotFound, projectID)
	}
	if err != nil {
		return nil, err
	}
	if !row.WorkspaceID.Valid {
		return nil, nil
	}
	return &sqlitegen.Workspace{
		ID: row.WorkspaceID.String, ProjectID: row.ProjectID,
		CanonicalRootPath: row.CanonicalRootPath.String, GitMetadataJson: row.GitMetadataJson.String,
		CreatedAtUnixMs: row.CreatedAtUnixMs.Int64, UpdatedAtUnixMs: row.UpdatedAtUnixMs.Int64,
	}, nil
}
