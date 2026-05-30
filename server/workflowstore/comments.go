package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"builder/server/metadata/sqlitegen"
	"builder/server/workflow"
)

func (s *Store) AddComment(ctx context.Context, taskID workflow.TaskID, body string, authorKind string, authorID string) (CommentRecord, error) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return CommentRecord{}, errors.New("comment body is required")
	}
	now := s.now().UnixMilli()
	id := prefixedID("comment")
	if err := s.queries.InsertTaskComment(ctx, sqlitegen.InsertTaskCommentParams{ID: id, TaskID: string(taskID), Body: trimmed, AuthorKind: strings.TrimSpace(authorKind), AuthorID: strings.TrimSpace(authorID), CreatedAtUnixMs: now, UpdatedAtUnixMs: now}); err != nil {
		return CommentRecord{}, err
	}
	return CommentRecord{ID: id, TaskID: taskID, Body: trimmed, Author: strings.TrimSpace(authorKind), AuthorID: strings.TrimSpace(authorID), CreatedAt: now, UpdatedAt: now}, nil
}

func (s *Store) ReplaceComment(ctx context.Context, commentID string, body string) error {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return errors.New("comment body is required")
	}
	updated, err := s.queries.UpdateTaskCommentBody(ctx, sqlitegen.UpdateTaskCommentBodyParams{ID: strings.TrimSpace(commentID), Body: trimmed, UpdatedAtUnixMs: s.now().UnixMilli()})
	if err != nil {
		return err
	}
	if updated == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) DeleteComment(ctx context.Context, commentID string) error {
	updated, err := s.queries.DeleteTaskComment(ctx, strings.TrimSpace(commentID))
	if err != nil {
		return err
	}
	if updated == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) TaskIdentityForComment(ctx context.Context, commentID string) (taskID string, projectID string, workflowID string, err error) {
	row := s.metadata.DB().QueryRowContext(ctx, workflowStoreQuery(taskIdentityForCommentQuery), strings.TrimSpace(commentID))
	if scanErr := row.Scan(&taskID, &projectID, &workflowID); scanErr != nil {
		return "", "", "", scanErr
	}
	return taskID, projectID, workflowID, nil
}

func (s *Store) ListComments(ctx context.Context, taskID workflow.TaskID) ([]CommentRecord, error) {
	rows, err := s.queries.ListTaskComments(ctx, string(taskID))
	if err != nil {
		return nil, err
	}
	out := make([]CommentRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, CommentRecord{ID: row.ID, TaskID: workflow.TaskID(row.TaskID), Body: row.Body, Author: row.AuthorKind, AuthorID: row.AuthorID, CreatedAt: row.CreatedAtUnixMs, UpdatedAt: row.UpdatedAtUnixMs})
	}
	return out, nil
}
