package workflowstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"builder/server/metadata/sqlitegen"
	"builder/server/workflow"
)

const projectWorkflowUnlinkTaskPreviewLimit = 10

func (s *Store) LinkWorkflow(ctx context.Context, projectID string, workflowID workflow.WorkflowID, isDefault bool) (ProjectWorkflowLinkRecord, error) {
	policy := WorkflowLinkDefaultNever
	if isDefault {
		policy = WorkflowLinkDefaultAlways
	}
	return s.LinkWorkflowWithDefaultPolicy(ctx, projectID, workflowID, policy)
}

func (s *Store) LinkWorkflowWithDefaultPolicy(ctx context.Context, projectID string, workflowID workflow.WorkflowID, policy WorkflowLinkDefaultPolicy) (ProjectWorkflowLinkRecord, error) {
	now := s.now().UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ProjectWorkflowLinkRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	link, err := s.linkWorkflowInTx(ctx, tx, q, now, strings.TrimSpace(projectID), workflowID, policy)
	if err != nil {
		return ProjectWorkflowLinkRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return ProjectWorkflowLinkRecord{}, err
	}
	return link, nil
}

func (s *Store) linkWorkflowInTx(ctx context.Context, exec sqlitegen.DBTX, q *sqlitegen.Queries, now int64, projectID string, workflowID workflow.WorkflowID, policy WorkflowLinkDefaultPolicy) (ProjectWorkflowLinkRecord, error) {
	existing, err := q.GetActiveProjectWorkflowLinkByWorkflow(ctx, sqlitegen.GetActiveProjectWorkflowLinkByWorkflowParams{ProjectID: projectID, WorkflowID: string(workflowID)})
	if err == nil {
		link := linkRecordFromRow(existing)
		shouldDefault, err := s.shouldSetWorkflowLinkDefault(ctx, q, projectID, policy)
		if err != nil {
			return ProjectWorkflowLinkRecord{}, err
		}
		if shouldDefault && !link.IsDefault {
			if err := setProjectDefaultWorkflowLink(ctx, exec, now, projectID, link.ID); err != nil {
				return ProjectWorkflowLinkRecord{}, err
			}
			link.IsDefault = true
		}
		return link, nil
	}
	if err != sql.ErrNoRows {
		return ProjectWorkflowLinkRecord{}, err
	}
	shouldDefault, err := s.shouldSetWorkflowLinkDefault(ctx, q, projectID, policy)
	if err != nil {
		return ProjectWorkflowLinkRecord{}, err
	}
	linkID := prefixedID("workflow-link")
	if err := q.InsertProjectWorkflowLink(ctx, sqlitegen.InsertProjectWorkflowLinkParams{ID: linkID, ProjectID: projectID, WorkflowID: string(workflowID), CreatedAtUnixMs: now, UpdatedAtUnixMs: now}); err != nil {
		return ProjectWorkflowLinkRecord{}, fmt.Errorf("insert project workflow link: %w", err)
	}
	if shouldDefault {
		if err := setProjectDefaultWorkflowLink(ctx, exec, now, projectID, linkID); err != nil {
			return ProjectWorkflowLinkRecord{}, err
		}
	}
	return ProjectWorkflowLinkRecord{ID: linkID, ProjectID: projectID, WorkflowID: workflowID, IsDefault: shouldDefault}, nil
}

func (s *Store) shouldSetWorkflowLinkDefault(ctx context.Context, q *sqlitegen.Queries, projectID string, policy WorkflowLinkDefaultPolicy) (bool, error) {
	switch policy {
	case WorkflowLinkDefaultAlways:
		return true, nil
	case WorkflowLinkDefaultIfProjectHasNone:
		count, err := q.CountActiveProjectWorkflowLinks(ctx, projectID)
		if err != nil {
			return false, err
		}
		if count == 0 {
			return true, nil
		}
		_, err = q.GetDefaultProjectWorkflowLink(ctx, projectID)
		if err == sql.ErrNoRows {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return false, nil
	case "", WorkflowLinkDefaultNever:
		return false, nil
	default:
		return false, fmt.Errorf("invalid workflow link default policy")
	}
}

func setProjectDefaultWorkflowLink(ctx context.Context, exec sqlitegen.DBTX, now int64, projectID string, linkID string) error {
	updated, err := exec.ExecContext(ctx, `UPDATE projects SET default_project_workflow_link_id = ?, updated_at_unix_ms = ? WHERE id = ?`, linkID, now, projectID)
	if err != nil {
		return fmt.Errorf("set default workflow link: %w", err)
	}
	if count, err := updated.RowsAffected(); err != nil {
		return err
	} else if count != 1 {
		return fmt.Errorf("project workflow link is invalid")
	}
	return nil
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

func (s *Store) ListWorkflowProjectLinks(ctx context.Context, workflowID workflow.WorkflowID) ([]ProjectWorkflowLinkRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT
    id,
    project_id,
    workflow_id,
    is_default,
    created_at_unix_ms,
    updated_at_unix_ms
FROM project_workflow_link_records
WHERE workflow_id = ?
ORDER BY project_id ASC, is_default DESC, created_at_unix_ms ASC`, string(workflowID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []ProjectWorkflowLinkRecord{}
	for rows.Next() {
		var row sqlitegen.ProjectWorkflowLinkRecord
		if err := rows.Scan(&row.ID, &row.ProjectID, &row.WorkflowID, &row.IsDefault, &row.CreatedAtUnixMs, &row.UpdatedAtUnixMs); err != nil {
			return nil, err
		}
		out = append(out, linkRecordFromRow(row))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) GetProjectWorkflowLink(ctx context.Context, linkID string) (ProjectWorkflowLinkRecord, error) {
	row, err := s.queries.GetProjectWorkflowLink(ctx, strings.TrimSpace(linkID))
	if err != nil {
		return ProjectWorkflowLinkRecord{}, err
	}
	return linkRecordFromRow(row), nil
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
	updated, err := tx.ExecContext(ctx, `UPDATE projects SET default_project_workflow_link_id = ?, updated_at_unix_ms = ? WHERE id = ?`, link.ID, now, projectID)
	if err != nil {
		return ProjectWorkflowLinkRecord{}, err
	}
	if count, err := updated.RowsAffected(); err != nil {
		return ProjectWorkflowLinkRecord{}, err
	} else if count != 1 {
		return ProjectWorkflowLinkRecord{}, fmt.Errorf("project workflow link is invalid")
	}
	if err := tx.Commit(); err != nil {
		return ProjectWorkflowLinkRecord{}, err
	}
	return s.GetProjectWorkflowLink(ctx, link.ID)
}

func (s *Store) UnlinkProjectWorkflow(ctx context.Context, linkID string, replacementDefaultLinkID string) (ProjectWorkflowUnlinkResult, error) {
	now := s.now().UnixMilli()
	linkID = strings.TrimSpace(linkID)
	replacementDefaultLinkID = strings.TrimSpace(replacementDefaultLinkID)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ProjectWorkflowUnlinkResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	link, err := q.GetProjectWorkflowLink(ctx, linkID)
	if err != nil {
		return ProjectWorkflowUnlinkResult{}, err
	}
	result := ProjectWorkflowUnlinkResult{LinkID: link.ID, ProjectID: link.ProjectID, WorkflowID: workflow.WorkflowID(link.WorkflowID)}
	if replacementDefaultLinkID != "" && replacementDefaultLinkID == link.ID {
		return ProjectWorkflowUnlinkResult{}, fmt.Errorf("replacement default workflow link is invalid")
	}
	taskRefs, err := q.CountTasksByProjectWorkflowLink(ctx, link.ID)
	if err != nil {
		return ProjectWorkflowUnlinkResult{}, err
	}
	if taskRefs > 0 {
		tasks, err := q.ListProjectWorkflowLinkTaskReferences(ctx, sqlitegen.ListProjectWorkflowLinkTaskReferencesParams{ProjectWorkflowLinkID: link.ID, Limit: projectWorkflowUnlinkTaskPreviewLimit})
		if err != nil {
			return ProjectWorkflowUnlinkResult{}, err
		}
		refs := make([]ProjectWorkflowUnlinkTaskReference, 0, len(tasks))
		for _, task := range tasks {
			refs = append(refs, ProjectWorkflowUnlinkTaskReference{TaskID: workflow.TaskID(task.ID), ShortID: task.ShortID, Title: task.Title})
		}
		result.Blockers = append(result.Blockers, ProjectWorkflowUnlinkBlocker{
			Code:    "task_references",
			Message: "Workflow link still has tasks. Move or delete those tasks before unlinking this workflow from the project.",
			Count:   int(taskRefs),
			Tasks:   refs,
		})
		return result, nil
	}
	if replacementDefaultLinkID != "" {
		var replacementCount int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM project_workflow_links WHERE id = ? AND project_id = ?`, replacementDefaultLinkID, link.ProjectID).Scan(&replacementCount); err != nil {
			return ProjectWorkflowUnlinkResult{}, err
		}
		if replacementCount != 1 {
			return ProjectWorkflowUnlinkResult{}, fmt.Errorf("replacement default workflow link is invalid")
		}
		if _, err := q.DeleteProjectWorkflowLink(ctx, link.ID); err != nil {
			return ProjectWorkflowUnlinkResult{}, err
		}
		updated, err := tx.ExecContext(ctx, `UPDATE projects SET default_project_workflow_link_id = ?, updated_at_unix_ms = ? WHERE id = ?`, replacementDefaultLinkID, now, link.ProjectID)
		if err != nil {
			return ProjectWorkflowUnlinkResult{}, err
		}
		if count, err := updated.RowsAffected(); err != nil {
			return ProjectWorkflowUnlinkResult{}, err
		} else if count != 1 {
			return ProjectWorkflowUnlinkResult{}, fmt.Errorf("replacement default workflow link is invalid")
		}
	} else {
		deleted, err := tx.ExecContext(ctx, `
DELETE FROM project_workflow_links
WHERE id = ?
  AND NOT (
      EXISTS (
          SELECT 1
          FROM projects p
          WHERE p.id = project_workflow_links.project_id
            AND p.default_project_workflow_link_id = project_workflow_links.id
      )
      AND (
          SELECT COUNT(*)
          FROM project_workflow_links active
          WHERE active.project_id = project_workflow_links.project_id
      ) > 1
  )`, link.ID)
		if err != nil {
			return ProjectWorkflowUnlinkResult{}, err
		}
		deletedCount, err := deleted.RowsAffected()
		if err != nil {
			return ProjectWorkflowUnlinkResult{}, err
		}
		if deletedCount != 1 {
			var defaultLinkID string
			var activeLinkCount int
			if err := tx.QueryRowContext(ctx, `
SELECT
    COALESCE(p.default_project_workflow_link_id, ''),
    (SELECT COUNT(*) FROM project_workflow_links active WHERE active.project_id = p.id)
FROM projects p
WHERE p.id = ?`, link.ProjectID).Scan(&defaultLinkID, &activeLinkCount); err != nil {
				return ProjectWorkflowUnlinkResult{}, err
			}
			if defaultLinkID == link.ID && activeLinkCount > 1 {
				result.Blockers = append(result.Blockers, ProjectWorkflowUnlinkBlocker{
					Code:    "default_replacement_required",
					Message: "Default workflow link requires a replacement before unlinking because this project has other linked workflows.",
					Count:   activeLinkCount - 1,
				})
				return result, nil
			}
			return ProjectWorkflowUnlinkResult{}, fmt.Errorf("project workflow link is invalid")
		}
	}
	if err := tx.Commit(); err != nil {
		return ProjectWorkflowUnlinkResult{}, err
	}
	result.Unlinked = true
	return result, nil
}

func (s *Store) resolveTaskWorkflowLink(ctx context.Context, projectID string, workflowID workflow.WorkflowID) (sqlitegen.ProjectWorkflowLinkRecord, error) {
	if strings.TrimSpace(string(workflowID)) == "" {
		return s.queries.GetDefaultProjectWorkflowLink(ctx, projectID)
	}
	return s.queries.GetActiveProjectWorkflowLinkByWorkflow(ctx, sqlitegen.GetActiveProjectWorkflowLinkByWorkflowParams{ProjectID: projectID, WorkflowID: string(workflowID)})
}

func linkRecordFromRow(row sqlitegen.ProjectWorkflowLinkRecord) ProjectWorkflowLinkRecord {
	return ProjectWorkflowLinkRecord{ID: row.ID, ProjectID: row.ProjectID, WorkflowID: workflow.WorkflowID(row.WorkflowID), IsDefault: row.IsDefault != 0}
}
