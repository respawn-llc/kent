package workflowstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"builder/server/metadata/sqlitegen"
	"builder/server/workflow"
)

func (s *Store) LinkWorkflow(ctx context.Context, projectID string, workflowID workflow.WorkflowID, isDefault bool) (ProjectWorkflowLinkRecord, error) {
	now := s.now().UnixMilli()
	linkID := prefixedID("workflow-link")
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ProjectWorkflowLinkRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	if isDefault {
		if err := q.ClearProjectDefaultWorkflowLinks(ctx, sqlitegen.ClearProjectDefaultWorkflowLinksParams{ProjectID: projectID, UpdatedAtUnixMs: now}); err != nil {
			return ProjectWorkflowLinkRecord{}, err
		}
	}
	if err := q.InsertProjectWorkflowLink(ctx, sqlitegen.InsertProjectWorkflowLinkParams{ID: linkID, ProjectID: projectID, WorkflowID: string(workflowID), IsDefault: boolToInt64(isDefault), CreatedAtUnixMs: now, UpdatedAtUnixMs: now}); err != nil {
		return ProjectWorkflowLinkRecord{}, fmt.Errorf("insert project workflow link: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ProjectWorkflowLinkRecord{}, err
	}
	return ProjectWorkflowLinkRecord{ID: linkID, ProjectID: projectID, WorkflowID: workflowID, IsDefault: isDefault}, nil
}

func (s *Store) ListProjectWorkflowLinks(ctx context.Context, projectID string) ([]ProjectWorkflowLinkRecord, error) {
	rows, err := s.queries.ListProjectWorkflowLinks(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]ProjectWorkflowLinkRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, linkRecordFromRow(row))
	}
	return out, nil
}

func (s *Store) SetDefaultProjectWorkflowLink(ctx context.Context, projectID string, workflowID workflow.WorkflowID) (ProjectWorkflowLinkRecord, error) {
	now := s.now().UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ProjectWorkflowLinkRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	link, err := q.GetActiveProjectWorkflowLinkByWorkflow(ctx, sqlitegen.GetActiveProjectWorkflowLinkByWorkflowParams{ProjectID: projectID, WorkflowID: string(workflowID)})
	if err != nil {
		return ProjectWorkflowLinkRecord{}, err
	}
	if err := q.ClearProjectDefaultWorkflowLinks(ctx, sqlitegen.ClearProjectDefaultWorkflowLinksParams{ProjectID: projectID, UpdatedAtUnixMs: now}); err != nil {
		return ProjectWorkflowLinkRecord{}, err
	}
	updated, err := tx.ExecContext(ctx, `UPDATE project_workflow_links SET is_default = 1, updated_at_unix_ms = ? WHERE id = ? AND project_id = ? AND unlinked_at_unix_ms = 0`, now, link.ID, projectID)
	if err != nil {
		return ProjectWorkflowLinkRecord{}, err
	}
	if count, err := updated.RowsAffected(); err != nil {
		return ProjectWorkflowLinkRecord{}, err
	} else if count != 1 {
		return ProjectWorkflowLinkRecord{}, sql.ErrNoRows
	}
	if err := tx.Commit(); err != nil {
		return ProjectWorkflowLinkRecord{}, err
	}
	link.IsDefault = 1
	link.UpdatedAtUnixMs = now
	return linkRecordFromRow(link), nil
}

func (s *Store) UnlinkProjectWorkflow(ctx context.Context, linkID string, replacementDefaultLinkID string) error {
	link, err := s.queries.GetProjectWorkflowLink(ctx, linkID)
	if err != nil {
		return err
	}
	nonTerminal, err := s.queries.CountNonTerminalTasksByProjectWorkflowLink(ctx, linkID)
	if err != nil {
		return err
	}
	if nonTerminal > 0 {
		return fmt.Errorf("project workflow link has non-terminal task references")
	}
	activeLinks, err := s.queries.CountActiveProjectWorkflowLinks(ctx, link.ProjectID)
	if err != nil {
		return err
	}
	if link.IsDefault != 0 && activeLinks > 1 && strings.TrimSpace(replacementDefaultLinkID) == "" {
		return fmt.Errorf("replacement default workflow link is required")
	}
	taskRefs, err := s.queries.CountTasksByProjectWorkflowLink(ctx, linkID)
	if err != nil {
		return err
	}
	now := s.now().UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	if taskRefs == 0 {
		if _, err := q.DeleteProjectWorkflowLink(ctx, linkID); err != nil {
			return err
		}
	} else {
		if _, err := q.SoftUnlinkProjectWorkflowLink(ctx, sqlitegen.SoftUnlinkProjectWorkflowLinkParams{ID: linkID, UnlinkedAtUnixMs: now, UpdatedAtUnixMs: now}); err != nil {
			return err
		}
	}
	replacementDefaultLinkID = strings.TrimSpace(replacementDefaultLinkID)
	if replacementDefaultLinkID != "" {
		if err := q.ClearProjectDefaultWorkflowLinks(ctx, sqlitegen.ClearProjectDefaultWorkflowLinksParams{ProjectID: link.ProjectID, UpdatedAtUnixMs: now}); err != nil {
			return err
		}
		updated, err := tx.ExecContext(ctx, `UPDATE project_workflow_links SET is_default = 1, updated_at_unix_ms = ? WHERE id = ? AND project_id = ? AND unlinked_at_unix_ms = 0`, now, replacementDefaultLinkID, link.ProjectID)
		if err != nil {
			return err
		}
		if count, err := updated.RowsAffected(); err != nil {
			return err
		} else if count != 1 {
			return fmt.Errorf("replacement default workflow link is invalid")
		}
	}
	return tx.Commit()
}

func (s *Store) resolveTaskWorkflowLink(ctx context.Context, projectID string, workflowID workflow.WorkflowID) (sqlitegen.ProjectWorkflowLink, error) {
	if strings.TrimSpace(string(workflowID)) == "" {
		return s.queries.GetDefaultProjectWorkflowLink(ctx, projectID)
	}
	return s.queries.GetActiveProjectWorkflowLinkByWorkflow(ctx, sqlitegen.GetActiveProjectWorkflowLinkByWorkflowParams{ProjectID: projectID, WorkflowID: string(workflowID)})
}

func linkRecordFromRow(row sqlitegen.ProjectWorkflowLink) ProjectWorkflowLinkRecord {
	return ProjectWorkflowLinkRecord{ID: row.ID, ProjectID: row.ProjectID, WorkflowID: workflow.WorkflowID(row.WorkflowID), IsDefault: row.IsDefault != 0, UnlinkedAtUnixMs: row.UnlinkedAtUnixMs}
}
