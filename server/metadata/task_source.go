package metadata

import (
	"context"
	"core/server/metadata/sqlitegen"
	"database/sql"
	"errors"
)

type TaskSourceIdentity struct {
	TaskID    string
	ShortID   string
	ProjectID string
}

func (s *Store) FindTaskByProjectSourceURL(ctx context.Context, projectID, sourceURL string) (*TaskSourceIdentity, error) {
	row, err := s.queries.FindTaskByProjectSourceURL(ctx, sqlitegen.FindTaskByProjectSourceURLParams{ProjectID: projectID, SourceUrl: sourceURL})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &TaskSourceIdentity{TaskID: row.ID, ShortID: row.ShortID, ProjectID: row.ProjectID}, nil
}
