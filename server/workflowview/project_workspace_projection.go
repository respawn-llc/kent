package workflowview

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"core/server/metadata"
	"core/server/metadata/sqlitegen"
	"core/server/workflow"
	"core/shared/clientui"
	"core/shared/serverapi"
)

type projectFactsReader interface {
	GetProjectEditMetadata(context.Context, string) (sqlitegen.GetProjectEditMetadataRow, error)
	GetProjectPrimaryWorkspaceID(context.Context, string) (string, error)
	CountProjectWorkspaces(context.Context, string) (int64, error)
	GetWorkspaceByID(context.Context, string) (sqlitegen.Workspace, error)
}

func projectWorkspaceFacts(ctx context.Context, q projectFactsReader, projectID string) (serverapi.ProjectBoardProject, error) {
	project, err := q.GetProjectEditMetadata(ctx, projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return serverapi.ProjectBoardProject{}, serverapi.ErrProjectNotFound
	}
	if err != nil {
		return serverapi.ProjectBoardProject{}, err
	}
	defaultID, err := metadata.ResolveProjectSourceWorkspaceID(ctx, q, projectID)
	if err != nil {
		return serverapi.ProjectBoardProject{}, err
	}
	count, err := q.CountProjectWorkspaces(ctx, projectID)
	if err != nil {
		return serverapi.ProjectBoardProject{}, err
	}
	return serverapi.ProjectBoardProject{
		ProjectKey: project.ProjectKey, DisplayName: project.DisplayName,
		DefaultWorkspaceID: defaultID, AttachedWorkspaceCount: int(count),
	}, nil
}

type sourceWorkspaceReader interface {
	GetWorkspaceByID(context.Context, string) (sqlitegen.Workspace, error)
}

func taskSourceWorkspace(ctx context.Context, q sourceWorkspaceReader, task sqlitegen.TaskRecord, primaryWorkspaceID string) (serverapi.ProjectWorkspaceSummary, error) {
	if task.SourceWorkspaceID.Valid {
		source, err := attachedTaskSourceWorkspace(ctx, q, task, task.SourceWorkspaceID.String, primaryWorkspaceID)
		if !errors.Is(err, sql.ErrNoRows) {
			return source, err
		}
	}
	snapshot := struct {
		SourceWorkspaceSnapshot json.RawMessage `json:"source_workspace_snapshot"`
	}{}
	if err := workflow.UnmarshalString(task.MetadataJson, &snapshot); err != nil {
		return serverapi.ProjectWorkspaceSummary{}, err
	}
	if snapshot.SourceWorkspaceSnapshot != nil {
		var saved struct {
			WorkspaceID string `json:"workspace_id"`
			DisplayName string `json:"display_name"`
			RootPath    string `json:"root_path"`
		}
		if err := json.Unmarshal(snapshot.SourceWorkspaceSnapshot, &saved); err != nil {
			return serverapi.ProjectWorkspaceSummary{}, err
		}
		if strings.TrimSpace(saved.WorkspaceID) == "" || strings.TrimSpace(saved.RootPath) == "" {
			return serverapi.ProjectWorkspaceSummary{}, fmt.Errorf("task %q source Workspace snapshot has invalid durable identity", task.ID)
		}
		if strings.TrimSpace(saved.DisplayName) == "" {
			saved.DisplayName = displayNameForPath(saved.RootPath)
		}
		return serverapi.ProjectWorkspaceSummary{
			WorkspaceID: saved.WorkspaceID, DisplayName: saved.DisplayName, RootPath: saved.RootPath,
			Availability: string(clientui.ProjectAvailabilityUnlinked),
		}, nil
	}
	if task.SourceWorkspaceID.Valid {
		return serverapi.ProjectWorkspaceSummary{}, fmt.Errorf("task %q has no retained source Workspace facts", task.ID)
	}
	return attachedTaskSourceWorkspace(ctx, q, task, primaryWorkspaceID, primaryWorkspaceID)
}

func attachedTaskSourceWorkspace(ctx context.Context, q sourceWorkspaceReader, task sqlitegen.TaskRecord, sourceWorkspaceID, primaryWorkspaceID string) (serverapi.ProjectWorkspaceSummary, error) {
	row, err := q.GetWorkspaceByID(ctx, sourceWorkspaceID)
	if err == nil {
		if row.ProjectID != task.ProjectID {
			return serverapi.ProjectWorkspaceSummary{}, fmt.Errorf("task %q source workspace %q belongs to project %q", task.ID, row.ID, row.ProjectID)
		}
		displayName := displayNameForPath(row.CanonicalRootPath)
		if strings.TrimSpace(row.ID) == "" || strings.TrimSpace(row.CanonicalRootPath) == "" || displayName == "" {
			return serverapi.ProjectWorkspaceSummary{}, fmt.Errorf("task %q source workspace %q has invalid durable identity", task.ID, row.ID)
		}
		return serverapi.ProjectWorkspaceSummary{
			WorkspaceID: row.ID, DisplayName: displayName, RootPath: row.CanonicalRootPath,
			Availability: string(clientui.ProjectAvailabilityAvailable),
			IsPrimary:    row.ID == primaryWorkspaceID, UpdatedAtUnixMs: row.UpdatedAtUnixMs,
		}, nil
	}
	return serverapi.ProjectWorkspaceSummary{}, err
}
