package workflowview

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"core/server/metadata"
	"core/server/metadata/sqlitegen"
	"core/server/workflow"
	"core/shared/invariant"
	"core/shared/protoapi"
	projectpb "core/shared/protoapi/gen/kent/api/project"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/serverapi"

	"google.golang.org/protobuf/types/known/timestamppb"
)

type projectFactsReader interface {
	GetProjectEditMetadata(context.Context, string) (sqlitegen.GetProjectEditMetadataRow, error)
	GetProjectPrimaryWorkspaceID(context.Context, string) (string, error)
	CountProjectWorkspaces(context.Context, string) (int64, error)
	GetWorkspaceByID(context.Context, string) (sqlitegen.Workspace, error)
}

func projectWorkspaceFacts(ctx context.Context, q projectFactsReader, projectID string) (*taskpb.BoardProject, error) {
	project, err := q.GetProjectEditMetadata(ctx, projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, serverapi.ErrProjectNotFound
	}
	if err != nil {
		return nil, err
	}
	defaultID, err := metadata.ResolveProjectSourceWorkspaceID(ctx, q, projectID)
	if err != nil {
		return nil, err
	}
	count, err := q.CountProjectWorkspaces(ctx, projectID)
	if err != nil {
		return nil, err
	}
	attachedCount, err := protoapi.Int32(int(count), "attached_workspace_count")
	if err != nil {
		return nil, err
	}
	return &taskpb.BoardProject{
		ProjectKey: project.ProjectKey, DisplayName: project.DisplayName,
		DefaultWorkspaceId: defaultID, AttachedWorkspaceCount: attachedCount,
	}, nil
}

type sourceWorkspaceReader interface {
	GetWorkspaceByID(context.Context, string) (sqlitegen.Workspace, error)
}

func taskSourceWorkspace(ctx context.Context, q sourceWorkspaceReader, task sqlitegen.TaskRecord, primaryWorkspaceID string) (*taskpb.TaskSourceWorkspace, error) {
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
		return nil, invalidTaskSource(err)
	}
	if snapshot.SourceWorkspaceSnapshot != nil {
		var saved struct {
			WorkspaceID string `json:"workspace_id"`
			DisplayName string `json:"display_name"`
			RootPath    string `json:"root_path"`
		}
		if err := json.Unmarshal(snapshot.SourceWorkspaceSnapshot, &saved); err != nil {
			return nil, invalidTaskSource(err)
		}
		if strings.TrimSpace(saved.WorkspaceID) == "" || strings.TrimSpace(saved.RootPath) == "" {
			return nil, invalidTaskSource(fmt.Errorf("task %q source Workspace snapshot has invalid durable identity", task.ID))
		}
		if strings.TrimSpace(saved.DisplayName) == "" {
			saved.DisplayName = displayNameForPath(saved.RootPath)
		}
		return &taskpb.TaskSourceWorkspace{
			WorkspaceId: saved.WorkspaceID, DisplayName: saved.DisplayName, RootPath: saved.RootPath,
			Availability: projectpb.ProjectAvailability_PROJECT_AVAILABILITY_UNLINKED,
		}, nil
	}
	if task.SourceWorkspaceID.Valid {
		return nil, invalidTaskSource(fmt.Errorf("task %q has no retained source Workspace facts", task.ID))
	}
	return attachedTaskSourceWorkspace(ctx, q, task, primaryWorkspaceID, primaryWorkspaceID)
}

func invalidTaskSource(err error) error {
	invariant.NewPolicy().Check(false, invariant.FailureDiagnostic(invariant.ScopeReadModelPublication, "resolve Task source Workspace", err))
	return err
}

func attachedTaskSourceWorkspace(ctx context.Context, q sourceWorkspaceReader, task sqlitegen.TaskRecord, sourceWorkspaceID, primaryWorkspaceID string) (*taskpb.TaskSourceWorkspace, error) {
	row, err := q.GetWorkspaceByID(ctx, sourceWorkspaceID)
	if err == nil {
		if row.ProjectID != task.ProjectID {
			return nil, invalidTaskSource(fmt.Errorf("task %q source workspace %q belongs to project %q", task.ID, row.ID, row.ProjectID))
		}
		displayName := displayNameForPath(row.CanonicalRootPath)
		if strings.TrimSpace(row.ID) == "" || strings.TrimSpace(row.CanonicalRootPath) == "" || displayName == "" {
			return nil, invalidTaskSource(fmt.Errorf("task %q source workspace %q has invalid durable identity", task.ID, row.ID))
		}
		return &taskpb.TaskSourceWorkspace{
			WorkspaceId: row.ID, DisplayName: displayName, RootPath: row.CanonicalRootPath,
			Availability: projectpb.ProjectAvailability_PROJECT_AVAILABILITY_AVAILABLE,
			IsPrimary:    row.ID == primaryWorkspaceID, UpdatedAt: timestamppb.New(time.UnixMilli(row.UpdatedAtUnixMs)),
		}, nil
	}
	return nil, err
}
