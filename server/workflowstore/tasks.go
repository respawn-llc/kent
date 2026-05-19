package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"builder/server/metadata/sqlitegen"
	"builder/server/workflow"
)

type CreateTaskRequest struct {
	ProjectID         string
	WorkflowID        workflow.WorkflowID
	Title             string
	Body              string
	SourceURL         string
	SourceWorkspaceID string
}

type UpdateTaskRequest struct {
	TaskID            workflow.TaskID
	Title             string
	Body              *string
	SourceWorkspaceID string
}

type StartTaskResult struct {
	TransitionID string
	PlacementID  workflow.PlacementID
	RunID        workflow.RunID
}

type CompleteRunRequest struct {
	RunID              workflow.RunID
	TransitionID       string
	OutputValues       map[string]string
	Commentary         string
	Actor              string
	ExpectedGeneration int64
	RequireGeneration  bool
}

type CompletionValidationIssue struct {
	Code    string
	Field   string
	Message string
}

type CompletionValidationError struct {
	Issues []CompletionValidationIssue
}

func (e CompletionValidationError) Error() string {
	if len(e.Issues) == 0 {
		return "workflow completion is invalid"
	}
	parts := make([]string, 0, len(e.Issues))
	for _, issue := range e.Issues {
		if strings.TrimSpace(issue.Field) != "" {
			parts = append(parts, issue.Field+": "+issue.Message)
			continue
		}
		parts = append(parts, issue.Message)
	}
	return "workflow completion is invalid: " + strings.Join(parts, "; ")
}

type ProtocolViolationKind string

const (
	ProtocolViolationFinalAnswer       ProtocolViolationKind = "final_answer"
	ProtocolViolationInvalidCompletion ProtocolViolationKind = "invalid_completion"
)

type RecordProtocolViolationRequest struct {
	RunID              workflow.RunID
	Kind               ProtocolViolationKind
	MaxCount           int
	Detail             string
	ExpectedGeneration int64
	RequireGeneration  bool
}

type RecordProtocolViolationResult struct {
	Count       int64
	Interrupted bool
}

type CompleteRunResult struct {
	TransitionID workflow.TransitionID
	State        string
	PlacementIDs []workflow.PlacementID
	RunIDs       []workflow.RunID
}

type ManualMoveRequest struct {
	TaskID       workflow.TaskID
	TargetNodeID workflow.NodeID
	OutputValues map[string]string
	Commentary   string
	Actor        string
}

type ManualMoveResult = CompleteRunResult

func (s *Store) CreateTask(ctx context.Context, req CreateTaskRequest) (TaskRecord, error) {
	title := strings.TrimSpace(req.Title)
	body := strings.TrimSpace(req.Body)
	if title == "" {
		return TaskRecord{}, errors.New("task title is required")
	}
	link, err := s.resolveTaskWorkflowLink(ctx, req.ProjectID, req.WorkflowID)
	if err != nil {
		return TaskRecord{}, err
	}
	sourceWorkspaceID, err := s.resolveTaskSourceWorkspace(ctx, req.ProjectID, req.SourceWorkspaceID)
	if err != nil {
		return TaskRecord{}, err
	}
	def, wf, err := s.GetDefinition(ctx, workflow.WorkflowID(link.WorkflowID))
	if err != nil {
		return TaskRecord{}, err
	}
	startNode, err := startNode(def)
	if err != nil {
		return TaskRecord{}, err
	}
	now := s.now().UnixMilli()
	taskID := prefixedID("task")
	placementID := prefixedID("placement")
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TaskRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	allocated, err := q.AllocateProjectTaskSequence(ctx, sqlitegen.AllocateProjectTaskSequenceParams{ProjectID: req.ProjectID, UpdatedAtUnixMs: now})
	if err != nil {
		return TaskRecord{}, fmt.Errorf("allocate task sequence: %w", err)
	}
	seq := allocated.NextTaskSeq - 1
	shortID := fmt.Sprintf("%s-%d", strings.TrimSpace(allocated.ProjectKey), seq)
	metadataJSON, err := taskMetadataWithSourceWorkspaceSnapshot(ctx, q, "{}", sourceWorkspaceID)
	if err != nil {
		return TaskRecord{}, err
	}
	if err := q.InsertTask(ctx, sqlitegen.InsertTaskParams{ID: taskID, ProjectID: req.ProjectID, ProjectWorkflowLinkID: link.ID, WorkflowID: link.WorkflowID, WorkflowRevisionSeen: wf.GraphRevision, TaskSeq: seq, ShortID: shortID, Title: title, Body: body, SourceUrl: strings.TrimSpace(req.SourceURL), SourceWorkspaceID: sql.NullString{String: sourceWorkspaceID, Valid: sourceWorkspaceID != ""}, ManagedWorktreeID: sql.NullString{}, CreatedAtUnixMs: now, UpdatedAtUnixMs: now, MetadataJson: metadataJSON}); err != nil {
		return TaskRecord{}, fmt.Errorf("insert task: %w", err)
	}
	if err := q.InsertTaskNodePlacement(ctx, sqlitegen.InsertTaskNodePlacementParams{ID: placementID, TaskID: taskID, NodeID: string(startNode.ID), State: "active", CreatedAtUnixMs: now, UpdatedAtUnixMs: now}); err != nil {
		return TaskRecord{}, fmt.Errorf("insert start placement: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return TaskRecord{}, err
	}
	return TaskRecord{ID: workflow.TaskID(taskID), ProjectID: req.ProjectID, WorkflowID: workflow.WorkflowID(link.WorkflowID), LinkID: link.ID, ShortID: shortID, Title: title, Body: body, SourceURL: strings.TrimSpace(req.SourceURL), SourceWorkspaceID: sourceWorkspaceID, GraphRevision: wf.GraphRevision}, nil
}

func (s *Store) UpdateTask(ctx context.Context, req UpdateTaskRequest) (TaskRecord, error) {
	title := strings.TrimSpace(req.Title)
	if strings.TrimSpace(string(req.TaskID)) == "" {
		return TaskRecord{}, errors.New("task id is required")
	}
	if title == "" {
		return TaskRecord{}, errors.New("task title is required")
	}
	now := s.now().UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TaskRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	task, err := q.GetTask(ctx, string(req.TaskID))
	if err != nil {
		return TaskRecord{}, err
	}
	if task.CanceledAtUnixMs != 0 {
		return TaskRecord{}, fmt.Errorf("cannot edit canceled task")
	}
	body := task.Body
	if req.Body != nil {
		body = strings.TrimSpace(*req.Body)
	}
	if task.ManagedWorktreeID.Valid && strings.TrimSpace(task.ManagedWorktreeID.String) != "" {
		return TaskRecord{}, fmt.Errorf("cannot edit task after automation starts")
	}
	runCount, err := q.CountTaskRunsByTask(ctx, task.ID)
	if err != nil {
		return TaskRecord{}, err
	}
	if runCount != 0 {
		return TaskRecord{}, fmt.Errorf("cannot edit task after automation starts")
	}
	if _, err := q.GetActiveStartPlacementForTask(ctx, task.ID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TaskRecord{}, fmt.Errorf("cannot edit task after automation starts")
		}
		return TaskRecord{}, err
	}
	sourceWorkspaceID := strings.TrimSpace(req.SourceWorkspaceID)
	if sourceWorkspaceID == "" {
		sourceWorkspaceID = strings.TrimSpace(task.SourceWorkspaceID.String)
	}
	sourceWorkspaceID, err = resolveTaskSourceWorkspaceWithQueries(ctx, q, task.ProjectID, sourceWorkspaceID)
	if err != nil {
		return TaskRecord{}, err
	}
	metadataJSON, err := taskMetadataWithSourceWorkspaceSnapshot(ctx, q, task.MetadataJson, sourceWorkspaceID)
	if err != nil {
		return TaskRecord{}, err
	}
	updated, err := q.UpdateTaskEditableFields(ctx, sqlitegen.UpdateTaskEditableFieldsParams{ID: task.ID, Title: title, Body: body, SourceWorkspaceID: sql.NullString{String: sourceWorkspaceID, Valid: sourceWorkspaceID != ""}, MetadataJson: metadataJSON, UpdatedAtUnixMs: now})
	if err != nil {
		return TaskRecord{}, fmt.Errorf("update task: %w", err)
	}
	if updated != 1 {
		return TaskRecord{}, sql.ErrNoRows
	}
	row, err := q.GetTask(ctx, task.ID)
	if err != nil {
		return TaskRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return TaskRecord{}, err
	}
	return taskRecordFromTask(row), nil
}

func (s *Store) resolveTaskSourceWorkspace(ctx context.Context, projectID string, workspaceID string) (string, error) {
	trimmedProjectID := strings.TrimSpace(projectID)
	trimmedWorkspaceID := strings.TrimSpace(workspaceID)
	if trimmedProjectID == "" {
		return "", errors.New("project id is required")
	}
	if trimmedWorkspaceID != "" {
		workspace, err := s.metadata.GetWorkspaceByID(ctx, trimmedWorkspaceID)
		if err != nil {
			return "", fmt.Errorf("source workspace %q: %w", trimmedWorkspaceID, err)
		}
		if strings.TrimSpace(workspace.ProjectID) != trimmedProjectID {
			return "", fmt.Errorf("source workspace %q does not belong to project %q", trimmedWorkspaceID, trimmedProjectID)
		}
		return trimmedWorkspaceID, nil
	}
	workspaces, err := s.metadata.ListProjectWorkspaces(ctx, trimmedProjectID)
	if err != nil {
		return "", err
	}
	for _, workspace := range workspaces {
		if workspace.IsPrimary && strings.TrimSpace(workspace.WorkspaceID) != "" {
			return strings.TrimSpace(workspace.WorkspaceID), nil
		}
	}
	for _, workspace := range workspaces {
		if strings.TrimSpace(workspace.WorkspaceID) != "" {
			return strings.TrimSpace(workspace.WorkspaceID), nil
		}
	}
	return "", fmt.Errorf("project %q has no source workspace", trimmedProjectID)
}

func resolveTaskSourceWorkspaceWithQueries(ctx context.Context, q *sqlitegen.Queries, projectID string, workspaceID string) (string, error) {
	trimmedProjectID := strings.TrimSpace(projectID)
	trimmedWorkspaceID := strings.TrimSpace(workspaceID)
	if trimmedProjectID == "" {
		return "", errors.New("project id is required")
	}
	if trimmedWorkspaceID != "" {
		workspace, err := q.GetWorkspaceByID(ctx, trimmedWorkspaceID)
		if err != nil {
			return "", fmt.Errorf("source workspace %q: %w", trimmedWorkspaceID, err)
		}
		if strings.TrimSpace(workspace.ProjectID) != trimmedProjectID {
			return "", fmt.Errorf("source workspace %q does not belong to project %q", trimmedWorkspaceID, trimmedProjectID)
		}
		return trimmedWorkspaceID, nil
	}
	workspaces, err := q.ListProjectWorkspaces(ctx, trimmedProjectID)
	if err != nil {
		return "", err
	}
	for _, workspace := range workspaces {
		if workspace.IsPrimary != 0 && strings.TrimSpace(workspace.ID) != "" {
			return strings.TrimSpace(workspace.ID), nil
		}
	}
	for _, workspace := range workspaces {
		if strings.TrimSpace(workspace.ID) != "" {
			return strings.TrimSpace(workspace.ID), nil
		}
	}
	return "", fmt.Errorf("project %q has no source workspace", trimmedProjectID)
}

func taskMetadataWithSourceWorkspaceSnapshot(ctx context.Context, q *sqlitegen.Queries, currentMetadata string, sourceWorkspaceID string) (string, error) {
	payload := map[string]any{}
	if strings.TrimSpace(currentMetadata) != "" {
		if err := unmarshalJSON(currentMetadata, &payload); err != nil {
			return "", fmt.Errorf("decode task metadata json: %w", err)
		}
	}
	trimmedWorkspaceID := strings.TrimSpace(sourceWorkspaceID)
	if trimmedWorkspaceID == "" {
		delete(payload, "source_workspace_snapshot")
		return marshalJSON(payload)
	}
	workspace, err := q.GetWorkspaceByID(ctx, trimmedWorkspaceID)
	if err != nil {
		return "", fmt.Errorf("source workspace snapshot %q: %w", trimmedWorkspaceID, err)
	}
	payload["source_workspace_snapshot"] = map[string]string{
		"workspace_id": workspace.ID,
		"display_name": workspace.DisplayName,
		"root_path":    workspace.CanonicalRootPath,
	}
	return marshalJSON(payload)
}

func (s *Store) StartTask(ctx context.Context, taskID workflow.TaskID) (StartTaskResult, error) {
	prepared, err := s.prepareTaskStart(ctx, taskID)
	if err != nil {
		return StartTaskResult{}, err
	}
	now := s.now().UnixMilli()
	transitionID := prefixedID("transition")
	targetPlacementID := prefixedID("placement")
	runID := prefixedID("run")
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return StartTaskResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	updatedStart, err := tx.ExecContext(ctx, `
UPDATE task_node_placements
SET state = ?, updated_at_unix_ms = ?
WHERE id = ?
  AND state = 'active'
  AND task_id IN (
      SELECT id
      FROM tasks
      WHERE id = ?
        AND canceled_at_unix_ms = 0
  )`, "completed", now, prepared.startPlacement.ID, string(taskID))
	if err != nil {
		return StartTaskResult{}, err
	}
	if updated, err := updatedStart.RowsAffected(); err != nil {
		return StartTaskResult{}, err
	} else if updated != 1 {
		return StartTaskResult{}, sql.ErrNoRows
	}
	if err := touchTaskUpdatedAt(ctx, tx, string(taskID), now); err != nil {
		return StartTaskResult{}, err
	}
	if err := q.InsertTaskTransition(ctx, sqlitegen.InsertTaskTransitionParams{ID: transitionID, TaskID: string(taskID), SourcePlacementID: sql.NullString{String: prepared.startPlacement.ID, Valid: true}, SourceNodeID: sql.NullString{String: string(prepared.start.ID), Valid: true}, SourceNodeKey: string(prepared.start.Key), SourceNodeDisplayName: prepared.start.DisplayName, TransitionGroupID: sql.NullString{String: string(prepared.group.ID), Valid: true}, TransitionID: string(prepared.group.TransitionID), TransitionDisplayName: prepared.group.DisplayName, WorkflowRevisionSeen: prepared.workflow.GraphRevision, Actor: "system", State: "applied", OutputValuesJson: "{}", CreatedAtUnixMs: now, AppliedAtUnixMs: now}); err != nil {
		return StartTaskResult{}, err
	}
	if err := q.InsertTaskNodePlacement(ctx, sqlitegen.InsertTaskNodePlacementParams{ID: targetPlacementID, TaskID: string(taskID), NodeID: string(prepared.target.ID), State: "active", CreatedByTransitionID: sql.NullString{String: transitionID, Valid: true}, CreatedAtUnixMs: now, UpdatedAtUnixMs: now}); err != nil {
		return StartTaskResult{}, err
	}
	if err := q.InsertTaskTransitionEdge(ctx, sqlitegen.InsertTaskTransitionEdgeParams{ID: prefixedID("transition-edge"), TaskTransitionID: transitionID, WorkflowEdgeID: sql.NullString{String: string(prepared.edge.ID), Valid: true}, EdgeKey: string(prepared.edge.Key), WorkflowRevisionSeen: prepared.workflow.GraphRevision, TargetNodeID: sql.NullString{String: string(prepared.target.ID), Valid: true}, TargetNodeKey: string(prepared.target.Key), TargetNodeDisplayName: prepared.target.DisplayName, TargetNodeKind: string(prepared.target.Kind), TargetPlacementID: sql.NullString{String: targetPlacementID, Valid: true}, State: "applied", ContextMode: string(prepared.edge.ContextMode), RequiresApproval: boolToInt64(prepared.edge.RequiresApproval), InputBindingsJson: mustJSON(prepared.edge.InputBindings), OutputRequirementsJson: mustJSON(prepared.edge.OutputRequirements), MetadataJson: "{}"}); err != nil {
		return StartTaskResult{}, err
	}
	runSnapshot, err := newRunStartSnapshot(prepared.definition, prepared.workflow, prepared.target.ID)
	if err != nil {
		return StartTaskResult{}, err
	}
	runSnapshotJSON, err := marshalJSON(runSnapshot)
	if err != nil {
		return StartTaskResult{}, err
	}
	if err := q.InsertTaskRun(ctx, sqlitegen.InsertTaskRunParams{ID: runID, TaskID: string(taskID), PlacementID: targetPlacementID, NodeID: string(prepared.target.ID), WorkflowRevisionSeen: prepared.workflow.GraphRevision, AutomationRequestedAtUnixMs: now, CreatedAtUnixMs: now, UpdatedAtUnixMs: now, InterruptionDetailJson: "{}", RunStartSnapshotJson: runSnapshotJSON, MetadataJson: "{}"}); err != nil {
		return StartTaskResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return StartTaskResult{}, err
	}
	return StartTaskResult{TransitionID: transitionID, PlacementID: workflow.PlacementID(targetPlacementID), RunID: workflow.RunID(runID)}, nil
}

func (s *Store) ValidateTaskStart(ctx context.Context, taskID workflow.TaskID) error {
	_, err := s.prepareTaskStart(ctx, taskID)
	return err
}

type preparedTaskStart struct {
	task           sqlitegen.Task
	definition     workflow.Definition
	workflow       WorkflowRecord
	start          workflow.Node
	group          workflow.TransitionGroup
	edge           workflow.Edge
	target         workflow.Node
	startPlacement sqlitegen.TaskNodePlacement
}

func (s *Store) prepareTaskStart(ctx context.Context, taskID workflow.TaskID) (preparedTaskStart, error) {
	task, err := s.queries.GetTask(ctx, string(taskID))
	if err != nil {
		return preparedTaskStart{}, err
	}
	if task.CanceledAtUnixMs != 0 {
		return preparedTaskStart{}, fmt.Errorf("task is canceled")
	}
	def, wf, err := s.GetDefinition(ctx, workflow.WorkflowID(task.WorkflowID))
	if err != nil {
		return preparedTaskStart{}, err
	}
	validation := workflow.ValidateDefinition(def, workflow.ValidationOptions{Context: workflow.ValidationContextExecution, RoleResolver: s.roleResolver})
	if validation.HasBlockingErrors() {
		return preparedTaskStart{}, fmt.Errorf("workflow validation failed: %v", validation.Codes())
	}
	start, err := startNode(def)
	if err != nil {
		return preparedTaskStart{}, err
	}
	group, edge, target, err := startTransition(def, start.ID)
	if err != nil {
		return preparedTaskStart{}, err
	}
	startPlacement, err := s.queries.GetActiveStartPlacementForTask(ctx, string(taskID))
	if err != nil {
		return preparedTaskStart{}, err
	}
	return preparedTaskStart{task: task, definition: def, workflow: wf, start: start, group: group, edge: edge, target: target, startPlacement: startPlacement}, nil
}

func (s *Store) CompleteRun(ctx context.Context, req CompleteRunRequest) (CompleteRunResult, error) {
	if strings.TrimSpace(string(req.RunID)) == "" {
		return CompleteRunResult{}, errors.New("run id is required")
	}
	if len(req.Commentary) > workflow.MaxCommentaryBytes {
		return CompleteRunResult{}, CompletionValidationError{Issues: []CompletionValidationIssue{{Code: "commentary_too_large", Field: "commentary", Message: "commentary is too large"}}}
	}
	issues := []CompletionValidationIssue{}
	for _, name := range sortedStringKeys(req.OutputValues) {
		value := req.OutputValues[name]
		if strings.TrimSpace(name) == "" {
			issues = append(issues, CompletionValidationIssue{Code: "output_field_required", Message: "output field name is required"})
		}
		if len(value) > workflow.MaxOutputValueBytes {
			issues = append(issues, CompletionValidationIssue{Code: "output_too_large", Field: strings.TrimSpace(name), Message: "output field is too large"})
		}
	}
	if len(issues) > 0 {
		return CompleteRunResult{}, CompletionValidationError{Issues: issues}
	}
	actor := strings.TrimSpace(req.Actor)
	if actor == "" {
		actor = "agent"
	}
	if actor != "agent" && actor != "user" && actor != "system" {
		return CompleteRunResult{}, fmt.Errorf("unsupported transition actor %q", actor)
	}
	if req.OutputValues == nil {
		req.OutputValues = map[string]string{}
	}
	run, err := s.queries.GetTaskRun(ctx, string(req.RunID))
	if err != nil {
		return CompleteRunResult{}, err
	}
	if run.CompletedAtUnixMs != 0 {
		return CompleteRunResult{}, errors.New("run already completed")
	}
	if run.InterruptedAtUnixMs != 0 {
		return CompleteRunResult{}, errors.New("run already interrupted")
	}
	if req.RequireGeneration && run.RunGeneration != req.ExpectedGeneration {
		return CompleteRunResult{}, fmt.Errorf("stale workflow run generation: got %d want %d", req.ExpectedGeneration, run.RunGeneration)
	}
	snapshot := runStartSnapshot{}
	if err := unmarshalJSON(run.RunStartSnapshotJson, &snapshot); err != nil {
		return CompleteRunResult{}, err
	}
	selectedTransitionID := strings.TrimSpace(req.TransitionID)
	availableGroups := snapshot.transitionGroupsForNode(snapshot.Node.ID)
	if selectedTransitionID == "" {
		if len(availableGroups) == 0 {
			return CompleteRunResult{}, CompletionValidationError{Issues: []CompletionValidationIssue{{Code: "no_outgoing_transition", Field: "transition_id", Message: "no outgoing transition is available in run-start snapshot"}}}
		}
		if len(availableGroups) != 1 {
			return CompleteRunResult{}, CompletionValidationError{Issues: []CompletionValidationIssue{{Code: "transition_id_required", Field: "transition_id", Message: "transition id is required when multiple transitions are available"}}}
		}
		selectedTransitionID = availableGroups[0].TransitionID
	}
	group, ok := snapshot.transitionByID(selectedTransitionID)
	if !ok {
		return CompleteRunResult{}, CompletionValidationError{Issues: []CompletionValidationIssue{{Code: "invalid_transition_id", Field: "transition_id", Message: fmt.Sprintf("transition %q is not available in run-start snapshot", selectedTransitionID)}}}
	}
	issues = append(issues, knownOutputIssues(snapshot.Node, req.OutputValues)...)
	issues = append(issues, requiredOutputIssues(group, req.OutputValues)...)
	if len(issues) > 0 {
		return CompleteRunResult{}, CompletionValidationError{Issues: issues}
	}
	if supportIssues := group.unsupportedRuntimeIssues(); len(supportIssues) > 0 {
		issues := make([]CompletionValidationIssue, 0, len(supportIssues))
		for _, issue := range supportIssues {
			issues = append(issues, CompletionValidationIssue{Code: string(issue.Code), Field: "transition_id", Message: issue.Message})
		}
		return CompleteRunResult{}, CompletionValidationError{Issues: issues}
	}
	outputValuesJSON, err := marshalJSON(req.OutputValues)
	if err != nil {
		return CompleteRunResult{}, err
	}
	now := s.now().UnixMilli()
	transitionState := "applied"
	appliedAt := now
	requiresApproval := transitionGroupRequiresApproval(group)
	if requiresApproval {
		transitionState = "pending_approval"
		appliedAt = 0
	}
	var fallbackDef workflow.Definition
	var fallbackWorkflow WorkflowRecord
	if !snapshot.hasFullGraphContract() && transitionGroupHasAgentTarget(group) {
		fallbackDef, fallbackWorkflow, err = s.GetDefinition(ctx, snapshot.WorkflowID)
		if err != nil {
			return CompleteRunResult{}, err
		}
	}
	transitionID := prefixedID("transition")
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CompleteRunResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	updatedRun, err := tx.ExecContext(ctx, `
UPDATE task_runs
SET
    updated_at_unix_ms = ?,
    completed_at_unix_ms = ?,
    waiting_ask_id = ''
WHERE id = ?
  AND run_generation = ?
  AND completed_at_unix_ms = 0
  AND interrupted_at_unix_ms = 0`,
		now,
		now,
		run.ID,
		run.RunGeneration,
	)
	if err != nil {
		return CompleteRunResult{}, fmt.Errorf("complete run: %w", err)
	}
	updatedCount, err := updatedRun.RowsAffected()
	if err != nil {
		return CompleteRunResult{}, err
	}
	if updatedCount != 1 {
		return CompleteRunResult{}, sql.ErrNoRows
	}
	if updated, err := q.UpdateTaskNodePlacementState(ctx, sqlitegen.UpdateTaskNodePlacementStateParams{ID: run.PlacementID, State: "completed", UpdatedAtUnixMs: now}); err != nil {
		return CompleteRunResult{}, fmt.Errorf("complete source placement: %w", err)
	} else if updated != 1 {
		return CompleteRunResult{}, sql.ErrNoRows
	}
	if err := touchTaskUpdatedAt(ctx, tx, run.TaskID, now); err != nil {
		return CompleteRunResult{}, err
	}
	if err := q.InsertTaskTransition(ctx, sqlitegen.InsertTaskTransitionParams{ID: transitionID, TaskID: run.TaskID, SourceRunID: sql.NullString{String: run.ID, Valid: true}, SourcePlacementID: sql.NullString{String: run.PlacementID, Valid: true}, SourceNodeID: sql.NullString{String: string(snapshot.Node.ID), Valid: true}, SourceNodeKey: string(snapshot.Node.Key), SourceNodeDisplayName: snapshot.Node.DisplayName, TransitionGroupID: sql.NullString{String: string(group.ID), Valid: true}, TransitionID: group.TransitionID, TransitionDisplayName: group.DisplayName, WorkflowRevisionSeen: snapshot.WorkflowRevisionSeen, Actor: actor, State: transitionState, Commentary: strings.TrimSpace(req.Commentary), OutputValuesJson: outputValuesJSON, CreatedAtUnixMs: now, AppliedAtUnixMs: appliedAt}); err != nil {
		return CompleteRunResult{}, fmt.Errorf("insert completion transition: %w", err)
	}
	result := CompleteRunResult{TransitionID: workflow.TransitionID(transitionID), State: transitionState}
	for _, edge := range group.Edges {
		if requiresApproval {
			if err := insertTransitionEdgeSnapshot(ctx, q, transitionID, snapshot.WorkflowRevisionSeen, edge, "", "pending"); err != nil {
				return CompleteRunResult{}, err
			}
			continue
		}
		if edge.TargetNode.Kind == workflow.NodeKindJoin {
			if err := insertTransitionEdgeSnapshot(ctx, q, transitionID, snapshot.WorkflowRevisionSeen, edge, "", "applied"); err != nil {
				return CompleteRunResult{}, err
			}
			joined, err := s.applyJoinIfReady(ctx, tx, q, now, run.TaskID, run.PlacementID, snapshot, edge)
			if err != nil {
				return CompleteRunResult{}, err
			}
			result.PlacementIDs = append(result.PlacementIDs, joined.PlacementIDs...)
			result.RunIDs = append(result.RunIDs, joined.RunIDs...)
			continue
		}
		targetPlacementID := prefixedID("placement")
		isFanoutBranch := len(group.Edges) > 1
		if err := q.InsertTaskNodePlacement(ctx, sqlitegen.InsertTaskNodePlacementParams{ID: targetPlacementID, TaskID: run.TaskID, NodeID: string(edge.TargetNode.ID), State: "active", CreatedByTransitionID: sql.NullString{String: transitionID, Valid: true}, ParallelBatchTransitionID: sql.NullString{String: transitionID, Valid: isFanoutBranch}, ParallelBranchEdgeID: sql.NullString{String: string(edge.ID), Valid: isFanoutBranch}, CreatedAtUnixMs: now, UpdatedAtUnixMs: now}); err != nil {
			return CompleteRunResult{}, fmt.Errorf("insert target placement: %w", err)
		}
		result.PlacementIDs = append(result.PlacementIDs, workflow.PlacementID(targetPlacementID))
		if err := insertTransitionEdgeSnapshot(ctx, q, transitionID, snapshot.WorkflowRevisionSeen, edge, targetPlacementID, "applied"); err != nil {
			return CompleteRunResult{}, err
		}
		if edge.TargetNode.Kind != workflow.NodeKindAgent {
			continue
		}
		targetRunID := prefixedID("run")
		targetSnapshot, foundSnapshot, err := snapshot.forNode(edge.TargetNode)
		if err != nil {
			return CompleteRunResult{}, err
		}
		if !foundSnapshot {
			targetSnapshot, err = newRunStartSnapshot(fallbackDef, fallbackWorkflow, edge.TargetNode.ID)
			if err != nil {
				return CompleteRunResult{}, err
			}
		}
		targetSnapshotJSON, err := marshalJSON(targetSnapshot)
		if err != nil {
			return CompleteRunResult{}, err
		}
		targetMetadataJSON, err := marshalJSON(map[string]string{
			"context_mode":      string(edge.ContextMode),
			"source_run_id":     run.ID,
			"source_session_id": strings.TrimSpace(run.SessionID.String),
		})
		if err != nil {
			return CompleteRunResult{}, err
		}
		if err := q.InsertTaskRun(ctx, sqlitegen.InsertTaskRunParams{ID: targetRunID, TaskID: run.TaskID, PlacementID: targetPlacementID, NodeID: string(edge.TargetNode.ID), WorkflowRevisionSeen: targetSnapshot.WorkflowRevisionSeen, AutomationRequestedAtUnixMs: now, CreatedAtUnixMs: now, UpdatedAtUnixMs: now, InterruptionDetailJson: "{}", RunStartSnapshotJson: targetSnapshotJSON, MetadataJson: targetMetadataJSON}); err != nil {
			return CompleteRunResult{}, fmt.Errorf("insert target run: %w", err)
		}
		result.RunIDs = append(result.RunIDs, workflow.RunID(targetRunID))
	}
	if err := recordRunCompletedWorkflowEvent(ctx, tx, q, run.TaskID, transitionID, run.ID, now); err != nil {
		return CompleteRunResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return CompleteRunResult{}, err
	}
	return result, nil
}

func recordRunCompletedWorkflowEvent(ctx context.Context, tx *sql.Tx, q *sqlitegen.Queries, taskID string, transitionID string, runID string, now int64) error {
	var projectID string
	var workflowID string
	if err := tx.QueryRowContext(ctx, `SELECT project_id, workflow_id FROM tasks WHERE id = ?`, taskID).Scan(&projectID, &workflowID); err != nil {
		return fmt.Errorf("load completion event task identity: %w", err)
	}
	changedIDs, err := marshalJSON([]string{taskID, transitionID, runID})
	if err != nil {
		return err
	}
	if _, err := q.InsertWorkflowEvent(ctx, sqlitegen.InsertWorkflowEventParams{
		ProjectID:        projectID,
		WorkflowID:       workflowID,
		Resource:         "task",
		Action:           "completed",
		ChangedIdsJson:   changedIDs,
		OccurredAtUnixMs: now,
	}); err != nil {
		return fmt.Errorf("record completion workflow event: %w", err)
	}
	return nil
}

func touchTaskUpdatedAt(ctx context.Context, tx *sql.Tx, taskID string, now int64) error {
	result, err := tx.ExecContext(ctx, `UPDATE tasks SET updated_at_unix_ms = ? WHERE id = ?`, now, taskID)
	if err != nil {
		return fmt.Errorf("update task timestamp: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func transitionGroupRequiresApproval(group transitionContractSnapshot) bool {
	for _, edge := range group.Edges {
		if edge.RequiresApproval {
			return true
		}
	}
	return false
}

func (s *Store) CancelTask(ctx context.Context, taskID workflow.TaskID, reason string) error {
	task, err := s.queries.GetTask(ctx, string(taskID))
	if err != nil {
		return err
	}
	def, _, err := s.GetDefinition(ctx, workflow.WorkflowID(task.WorkflowID))
	if err != nil {
		return err
	}
	terminal, err := terminalNode(def)
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
	if updated, err := q.CancelTask(ctx, sqlitegen.CancelTaskParams{ID: string(taskID), CanceledAtUnixMs: now, CancellationReason: strings.TrimSpace(reason), UpdatedAtUnixMs: now}); err != nil {
		return err
	} else if updated != 1 {
		return sql.ErrNoRows
	}
	if _, err := q.InterruptActiveTaskRuns(ctx, sqlitegen.InterruptActiveTaskRunsParams{TaskID: string(taskID), UpdatedAtUnixMs: now, InterruptedAtUnixMs: now, InterruptionReason: "task_canceled", InterruptionDetailJson: "{}"}); err != nil {
		return err
	}
	placements, err := q.ListTaskNodePlacements(ctx, string(taskID))
	if err != nil {
		return err
	}
	hasActiveTerminal := false
	for _, placement := range placements {
		if placement.State != "active" && placement.State != "waiting_approval" {
			continue
		}
		if placement.NodeID == string(terminal.ID) && placement.State == "active" {
			hasActiveTerminal = true
			continue
		}
		if _, err := q.UpdateTaskNodePlacementState(ctx, sqlitegen.UpdateTaskNodePlacementStateParams{ID: placement.ID, State: "completed", UpdatedAtUnixMs: now}); err != nil {
			return err
		}
	}
	if !hasActiveTerminal {
		if err := q.InsertTaskNodePlacement(ctx, sqlitegen.InsertTaskNodePlacementParams{ID: prefixedID("placement"), TaskID: string(taskID), NodeID: string(terminal.ID), State: "active", CreatedAtUnixMs: now, UpdatedAtUnixMs: now}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListPlacements(ctx context.Context, taskID workflow.TaskID) ([]PlacementRecord, error) {
	rows, err := s.queries.ListTaskNodePlacements(ctx, string(taskID))
	if err != nil {
		return nil, err
	}
	out := make([]PlacementRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, PlacementRecord{ID: workflow.PlacementID(row.ID), TaskID: workflow.TaskID(row.TaskID), NodeID: workflow.NodeID(row.NodeID), State: row.State})
	}
	return out, nil
}

func (s *Store) ListRuns(ctx context.Context, taskID workflow.TaskID) ([]RunRecord, error) {
	rows, err := s.queries.ListTaskRuns(ctx, string(taskID))
	if err != nil {
		return nil, err
	}
	out := make([]RunRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, runRecordFromTaskRun(row))
	}
	return out, nil
}

func startNode(def workflow.Definition) (workflow.Node, error) {
	for _, node := range def.Nodes {
		if node.Kind == workflow.NodeKindStart {
			return node, nil
		}
	}
	return workflow.Node{}, errors.New("workflow has no start node")
}

func terminalNode(def workflow.Definition) (workflow.Node, error) {
	var fallback workflow.Node
	for _, node := range def.Nodes {
		if node.Kind != workflow.NodeKindTerminal {
			continue
		}
		if string(node.Key) == "done" {
			return node, nil
		}
		if fallback.ID == "" {
			fallback = node
		}
	}
	if fallback.ID != "" {
		return fallback, nil
	}
	return workflow.Node{}, errors.New("workflow has no terminal node")
}

func startTransition(def workflow.Definition, startNodeID workflow.NodeID) (workflow.TransitionGroup, workflow.Edge, workflow.Node, error) {
	var groups []workflow.TransitionGroup
	for _, group := range def.TransitionGroups {
		if group.SourceNodeID == startNodeID {
			groups = append(groups, group)
		}
	}
	if len(groups) != 1 {
		return workflow.TransitionGroup{}, workflow.Edge{}, workflow.Node{}, errors.New("start node must have exactly one transition group")
	}
	var edges []workflow.Edge
	for _, edge := range def.Edges {
		if edge.TransitionGroupID == groups[0].ID {
			edges = append(edges, edge)
		}
	}
	if len(edges) != 1 {
		return workflow.TransitionGroup{}, workflow.Edge{}, workflow.Node{}, errors.New("start transition group must have exactly one edge")
	}
	for _, node := range def.Nodes {
		if node.ID == edges[0].TargetNodeID {
			return groups[0], edges[0], node, nil
		}
	}
	return workflow.TransitionGroup{}, workflow.Edge{}, workflow.Node{}, errors.New("start transition target missing")
}
