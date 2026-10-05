package main

import (
	"core/shared/protoapi"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
)

type taskSummaryJSON struct {
	ID                string   `json:"id"`
	ProjectID         string   `json:"project_id"`
	WorkflowID        string   `json:"workflow_id"`
	ShortID           string   `json:"short_id"`
	Title             string   `json:"title"`
	BodyPreview       *string  `json:"body_preview,omitempty"`
	SourceWorkspaceID *string  `json:"source_workspace_id,omitempty"`
	CreatedAtUnixMs   int64    `json:"created_at_unix_ms"`
	UpdatedAtUnixMs   int64    `json:"updated_at_unix_ms"`
	Done              bool     `json:"done"`
	ActiveNodeIDs     []string `json:"active_node_ids,omitempty"`
}

func taskSummaryOutput(value *taskpb.TaskSummary) taskSummaryJSON {
	out := taskSummaryJSON{
		ID: value.Id, ProjectID: value.ProjectId, WorkflowID: value.WorkflowId, ShortID: value.ShortId,
		Title: value.Title, BodyPreview: value.BodyPreview, SourceWorkspaceID: value.SourceWorkspaceId,
		CreatedAtUnixMs: value.CreatedAt.AsTime().UnixMilli(), UpdatedAtUnixMs: value.UpdatedAt.AsTime().UnixMilli(),
		Done: value.Done, ActiveNodeIDs: value.ActiveNodeIds,
	}
	if value.GetBodyPreview() == "" {
		out.BodyPreview = nil
	}
	return out
}

type taskWorkflowJSON struct {
	WorkflowID  string `json:"workflow_id"`
	DisplayName string `json:"display_name"`
	Version     int64  `json:"version"`
}

type taskActionsJSON struct {
	CanStart     bool `json:"can_start"`
	CanInterrupt bool `json:"can_interrupt"`
	CanResume    bool `json:"can_resume"`
	CanDelete    bool `json:"can_delete"`
}

type taskSourceWorkspaceJSON struct {
	WorkspaceID     string `json:"workspace_id"`
	DisplayName     string `json:"display_name"`
	RootPath        string `json:"root_path"`
	Availability    string `json:"availability"`
	IsPrimary       bool   `json:"is_primary"`
	UpdatedAtUnixMs int64  `json:"updated_at_unix_ms"`
}

func taskSourceWorkspaceOutput(value *taskpb.TaskSourceWorkspace) (taskSourceWorkspaceJSON, error) {
	availability, err := protoapi.ProjectAvailabilityFromProto(value.Availability)
	if err != nil {
		return taskSourceWorkspaceJSON{}, err
	}
	out := taskSourceWorkspaceJSON{
		WorkspaceID: value.WorkspaceId, DisplayName: value.DisplayName, RootPath: value.RootPath,
		Availability: string(availability), IsPrimary: value.IsPrimary,
	}
	if value.UpdatedAt != nil {
		out.UpdatedAtUnixMs = value.UpdatedAt.AsTime().UnixMilli()
	}
	return out, nil
}

type taskExecutionTargetJSON struct {
	Mode         string  `json:"mode"`
	RequestedRef *string `json:"requested_ref,omitempty"`
	ResolvedRef  *string `json:"resolved_ref,omitempty"`
	CommitOID    *string `json:"commit_oid,omitempty"`
	Provenance   string  `json:"provenance"`
}

func taskExecutionTargetOutput(value *taskpb.ExecutionTarget) (*taskExecutionTargetJSON, error) {
	if value == nil {
		return nil, nil
	}
	mode, err := protoapi.WorkflowExecutionTargetMode.Decode(value.Mode)
	if err != nil {
		return nil, err
	}
	provenance, err := protoapi.TaskExecutionTargetProvenance.Decode(value.Provenance)
	if err != nil {
		return nil, err
	}
	return &taskExecutionTargetJSON{
		Mode: mode, RequestedRef: value.RequestedRef, ResolvedRef: value.ResolvedRef,
		CommitOID: value.CommitOid, Provenance: provenance,
	}, nil
}
