package workflowview

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"core/server/metadata"
	"core/server/metadata/sqlitegen"
	"core/server/sessionruntime"
	"core/server/workflow"
	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
)

type TaskDetail struct {
	queries      *sqlitegen.Queries
	projection   *TaskStatusProjection
	dependencies *TaskDependencies
}

func NewTaskDetail(metadataStore *metadata.Store, projection *TaskStatusProjection, dependencies *TaskDependencies) (*TaskDetail, error) {
	if metadataStore == nil || metadataStore.Queries() == nil {
		return nil, errors.New("metadata store is required")
	}
	if projection == nil {
		return nil, errors.New("task status projection is required")
	}
	if dependencies == nil {
		return nil, errors.New("task dependencies read model is required")
	}
	return &TaskDetail{
		queries:      metadataStore.Queries(),
		projection:   projection,
		dependencies: dependencies,
	}, nil
}

func (d *TaskDetail) GetTask(ctx context.Context, taskID string) (*taskpb.TaskDetail, error) {
	if d == nil {
		return nil, errors.New("task detail is required")
	}
	if strings.TrimSpace(taskID) == "" {
		return nil, ErrTaskIDRequired
	}
	task, err := d.queries.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return d.task(ctx, task)
}

func (d *TaskDetail) ListCurrentNodes(ctx context.Context, taskID string) ([]workflow.CurrentNode, error) {
	if d == nil || d.queries == nil {
		return nil, errors.New("task detail is required")
	}
	if strings.TrimSpace(taskID) == "" {
		return nil, ErrTaskIDRequired
	}
	nodesByTask, err := d.projection.workflowStore.ListCurrentNodesByTaskWithQueries(ctx, d.queries, []workflow.TaskID{workflow.TaskID(taskID)})
	if err != nil {
		return nil, err
	}
	return nodesByTask[workflow.TaskID(taskID)], nil
}

func (d *TaskDetail) GetTaskByProjectShortID(ctx context.Context, projectID string, shortID string) (*taskpb.TaskDetail, error) {
	if d == nil {
		return nil, errors.New("task detail is required")
	}
	trimmedProjectID := strings.TrimSpace(projectID)
	if trimmedProjectID == "" {
		return nil, errors.New("project_id is required")
	}
	trimmedShortID := strings.TrimSpace(shortID)
	if trimmedShortID == "" {
		return nil, errors.New("short_id is required")
	}
	task, err := d.queries.GetTaskByProjectShortID(ctx, sqlitegen.GetTaskByProjectShortIDParams{ProjectID: trimmedProjectID, ShortID: trimmedShortID})
	if err != nil {
		return nil, err
	}
	return d.task(ctx, task)
}

func (d *TaskDetail) GetTaskByShortID(ctx context.Context, shortID string) (*taskpb.TaskDetail, error) {
	if d == nil {
		return nil, errors.New("task detail is required")
	}
	trimmedShortID := strings.TrimSpace(shortID)
	if trimmedShortID == "" {
		return nil, errors.New("short_id is required")
	}
	tasks, err := d.queries.ListTasksByShortID(ctx, trimmedShortID)
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, sql.ErrNoRows
	}
	if len(tasks) > 1 {
		return nil, fmt.Errorf("task short_id %q is ambiguous; use task id", trimmedShortID)
	}
	return d.GetTask(ctx, tasks[0].ID)
}

func (d *TaskDetail) task(ctx context.Context, task sqlitegen.TaskRecord) (*taskpb.TaskDetail, error) {
	taskID := workflow.TaskID(task.ID)
	observation, err := d.projection.Observe([]workflow.TaskID{taskID})
	if err != nil {
		return nil, err
	}
	results, err := d.projection.Project(ctx, observation, d.queries, []workflow.TaskID{taskID})
	if err != nil {
		return nil, err
	}
	projected, ok := results[taskID]
	if !ok {
		return nil, fmt.Errorf("task status projection omitted Task %q", task.ID)
	}
	projectedDependencies, err := d.dependencies.projectTaskDependencies(ctx, task.ID, observation, d.queries)
	if err != nil {
		return nil, err
	}
	task = projected.Task
	definition := projected.Definition
	liveSessions, currentScripts, err := taskDetailLiveTargets(
		ctx,
		d.queries.ListSessionNamesByIDs,
		task.ID,
		projected.LiveExecutions,
		definition,
	)
	if err != nil {
		return nil, err
	}
	labelsByTask, err := loadTaskLabelsByTask(ctx, d.queries, []string{task.ID})
	if err != nil {
		return nil, err
	}
	labelIDsByTask := taskLabelIDsByTask(labelsByTask)
	project, err := projectWorkspaceFacts(ctx, d.queries, task.ProjectID)
	if err != nil {
		return nil, err
	}
	sourceWorkspace, err := taskSourceWorkspace(ctx, d.queries, task, project.DefaultWorkspaceId)
	if err != nil {
		return nil, err
	}
	executionTarget, worktreePath, err := d.executionTargetForTask(ctx, task, sourceWorkspace.WorkspaceId)
	if err != nil {
		return nil, err
	}
	retainedSessionCount, err := d.queries.CountTaskSessions(ctx, sql.NullString{String: task.ID, Valid: true})
	if err != nil {
		return nil, err
	}
	retainedCount, err := protoapi.Int32(int(retainedSessionCount), "retained_session_count")
	if err != nil {
		return nil, err
	}
	attentionCount, err := protoapi.Int32(projected.AttentionCount, "attention_count")
	if err != nil {
		return nil, err
	}
	var sourceURL *string
	if task.SourceUrl != "" {
		sourceURL = &task.SourceUrl
	}
	detail := &taskpb.TaskDetail{
		Summary: taskSummary(task, projected.Status, projected.Done),
		Project: project,
		Workflow: &taskpb.TaskWorkflowSummary{
			WorkflowId:  definition.domain.ID.String(),
			DisplayName: definition.api.Workflow.Name,
			Version:     definition.api.Workflow.Version,
		},
		Body:                 task.Body,
		SourceUrl:            sourceURL,
		SourceWorkspace:      sourceWorkspace,
		ExecutionTarget:      executionTarget,
		WorktreePath:         worktreePath,
		CurrentNodes:         ProjectCurrentNodes(projected.CurrentNodes),
		LiveSessions:         liveSessions,
		CurrentScripts:       currentScripts,
		RetainedSessionCount: retainedCount,
		Status:               projected.Status,
		Actions:              projected.Actions,
		LabelIds:             labelIDsByTask[task.ID],
		AttentionCount:       attentionCount,
	}
	detail.Dependencies = projectedDependencies
	return detail, nil
}

func taskDetailLiveTargets(
	ctx context.Context,
	nameLookup sessionNameLookup,
	taskID string,
	executions []sessionruntime.TaskExecution,
	definition definitionSnapshot,
) ([]*taskpb.LiveSession, []*taskpb.CurrentScript, error) {
	type liveAgent struct {
		sessionID       string
		nodeDisplayName string
	}
	agents := make([]liveAgent, 0, len(executions))
	sessionIDs := make([]string, 0, len(executions))
	scripts := make([]*taskpb.CurrentScript, 0, len(executions))
	nodesByID := workflowNodesByID(definition.api)
	seenSessionIDs := make(map[string]struct{}, len(executions))
	for _, execution := range executions {
		switch {
		case execution.Agent != nil:
			sessionID := execution.Agent.SessionID.String()
			if _, exists := seenSessionIDs[sessionID]; exists {
				return nil, nil, fmt.Errorf("task %q has duplicate live Session %q", taskID, sessionID)
			}
			seenSessionIDs[sessionID] = struct{}{}
			nodeID := string(execution.Ref.CurrentNode.NodeID)
			node, exists := nodesByID[nodeID]
			if !exists {
				return nil, nil, fmt.Errorf("task %q live Agent execution references unknown Node %q", taskID, nodeID)
			}
			if node.Kind != pb.NodeKind_WORKFLOW_NODE_KIND_AGENT {
				return nil, nil, fmt.Errorf("task %q live Agent execution references %s Node %q", taskID, node.Kind, nodeID)
			}
			if strings.TrimSpace(node.DisplayName) == "" {
				return nil, nil, fmt.Errorf("task %q live Agent execution Node %q has a blank display name", taskID, nodeID)
			}
			sessionIDs = append(sessionIDs, sessionID)
			agents = append(agents, liveAgent{sessionID: sessionID, nodeDisplayName: node.DisplayName})
		case execution.Script != nil:
			if strings.TrimSpace(execution.Script.Path) == "" {
				return nil, nil, fmt.Errorf("task %q live Script execution has a blank target path", taskID)
			}
			scripts = append(scripts, &taskpb.CurrentScript{
				CurrentNode: workflowCurrentNodeReference(execution.Ref.CurrentNode),
				Path:        execution.Script.Path,
			})
		default:
			return nil, nil, fmt.Errorf("task %q live workflow execution has no target", taskID)
		}
	}
	names, err := resolveSessionNames(ctx, nameLookup, sessionIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("task %q live Sessions: %w", taskID, err)
	}
	liveSessions := make([]*taskpb.LiveSession, 0, len(agents))
	for _, agent := range agents {
		liveSessions = append(liveSessions, &taskpb.LiveSession{
			SessionId:       agent.sessionID,
			SessionName:     names[agent.sessionID],
			NodeDisplayName: agent.nodeDisplayName,
		})
	}
	sort.Slice(liveSessions, func(i, j int) bool { return liveSessions[i].SessionId < liveSessions[j].SessionId })
	sortTaskDetailCurrentScripts(scripts)
	return liveSessions, scripts, nil
}

func sortTaskDetailCurrentScripts(scripts []*taskpb.CurrentScript) {
	sort.Slice(scripts, func(i, j int) bool {
		if scripts[i].CurrentNode.NodeId != scripts[j].CurrentNode.NodeId {
			return scripts[i].CurrentNode.NodeId < scripts[j].CurrentNode.NodeId
		}
		leftBranch := scripts[i].CurrentNode.TransitionBranchKey
		rightBranch := scripts[j].CurrentNode.TransitionBranchKey
		if leftBranch == nil {
			return rightBranch != nil || scripts[i].Path < scripts[j].Path
		}
		if rightBranch == nil {
			return false
		}
		if *leftBranch != *rightBranch {
			return *leftBranch < *rightBranch
		}
		return scripts[i].Path < scripts[j].Path
	})
}

func (d *TaskDetail) executionTargetForTask(ctx context.Context, task sqlitegen.TaskRecord, sourceWorkspaceID string) (*taskpb.ExecutionTarget, *string, error) {
	if !task.ExecutionTargetMode.Valid {
		if task.ExecutionTargetRequestedRef.Valid ||
			task.ExecutionTargetResolvedRef.Valid ||
			task.ExecutionTargetCommitOid.Valid ||
			task.ExecutionTargetProvenance.Valid {
			return nil, nil, errors.New("unlocked task has execution target facts")
		}
		if task.ManagedWorktreeID.Valid && strings.TrimSpace(task.ManagedWorktreeID.String) == "" {
			return nil, nil, errors.New("task managed worktree id is blank")
		}
		return nil, nil, nil
	}
	mode, err := protoapi.WorkflowExecutionTargetMode.Encode(task.ExecutionTargetMode.String)
	if err != nil {
		return nil, nil, err
	}
	provenance, err := protoapi.TaskExecutionTargetProvenance.Encode(task.ExecutionTargetProvenance.String)
	if err != nil {
		return nil, nil, err
	}
	target := &taskpb.ExecutionTarget{
		Mode:         mode,
		RequestedRef: metadata.OptionalString(task.ExecutionTargetRequestedRef),
		ResolvedRef:  metadata.OptionalString(task.ExecutionTargetResolvedRef),
		CommitOid:    metadata.OptionalString(task.ExecutionTargetCommitOid),
		Provenance:   provenance,
	}
	if err := protoapi.Validate(target); err != nil {
		return nil, nil, fmt.Errorf("project task execution target: %w", err)
	}
	if !task.ManagedWorktreeID.Valid {
		return target, nil, nil
	}
	worktreeID := strings.TrimSpace(task.ManagedWorktreeID.String)
	if worktreeID == "" {
		return nil, nil, errors.New("task managed worktree id is blank")
	}
	row, err := d.queries.GetWorktreeByID(ctx, worktreeID)
	if err != nil {
		return nil, nil, err
	}
	if row.WorkspaceID != sourceWorkspaceID {
		return nil, nil, fmt.Errorf("task %q managed worktree %q belongs to workspace %q", task.ID, row.ID, row.WorkspaceID)
	}
	path := strings.TrimSpace(row.CanonicalRootPath)
	if path == "" {
		return nil, nil, errors.New("task managed worktree path is blank")
	}
	return target, &path, nil
}

func displayNameForPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	return filepath.Base(filepath.Clean(trimmed))
}
