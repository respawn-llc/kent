package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"core/server/metadata/sqlitegen"
	"core/server/workflow"
)

func (s *Store) ListRunnableRuns(ctx context.Context, limit int64) ([]RunnableRunRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.queries.ListRunnableWorkflowRuns(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]RunnableRunRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, RunnableRunRecord{RunRecord: runRecordFromTaskRun(row), WorkflowRevisionSeen: row.WorkflowRevisionSeen})
	}
	return out, nil
}

func (s *Store) ClaimRun(ctx context.Context, runID workflow.RunID, expectedGeneration int64) (RunnableRunRecord, error) {
	now := s.now().UnixMilli()
	row, err := s.queries.ClaimWorkflowRun(ctx, sqlitegen.ClaimWorkflowRunParams{ID: string(runID), ExpectedGeneration: expectedGeneration, UpdatedAtUnixMs: now, StartedAtUnixMs: now})
	if err != nil {
		return RunnableRunRecord{}, err
	}
	return RunnableRunRecord{RunRecord: runRecordFromClaimedTaskRun(row), WorkflowRevisionSeen: row.WorkflowRevisionSeen}, nil
}

func (s *Store) InterruptRun(ctx context.Context, runID workflow.RunID, reason string, detailJSON string) error {
	if strings.TrimSpace(detailJSON) == "" {
		detailJSON = "{}"
	}
	now := s.now().UnixMilli()
	updated, err := s.queries.InterruptWorkflowRun(ctx, sqlitegen.InterruptWorkflowRunParams{ID: string(runID), UpdatedAtUnixMs: now, InterruptedAtUnixMs: now, InterruptionReason: strings.TrimSpace(reason), InterruptionDetailJson: detailJSON})
	if err != nil {
		return err
	}
	if updated != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) InterruptRunGeneration(ctx context.Context, runID workflow.RunID, generation int64, reason string, detailJSON string) error {
	if strings.TrimSpace(detailJSON) == "" {
		detailJSON = "{}"
	}
	now := s.now().UnixMilli()
	result, err := s.db.ExecContext(ctx, strings.TrimSuffix(interruptRunGenerationQuery, "\n"),
		now,
		now,
		strings.TrimSpace(reason),
		detailJSON,
		string(runID),
		generation,
	)
	if err != nil {
		return err
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

func (s *Store) InterruptTaskRun(ctx context.Context, taskID workflow.TaskID, runID workflow.RunID, reason string) (RunRecord, error) {
	if strings.TrimSpace(string(taskID)) == "" {
		return RunRecord{}, errors.New("task id is required")
	}
	trimmedRunID := strings.TrimSpace(string(runID))
	rows, err := s.db.QueryContext(ctx, strings.TrimSuffix(interruptTaskRunCandidatesQuery, "\n"), string(taskID), trimmedRunID, trimmedRunID)
	if err != nil {
		return RunRecord{}, err
	}
	defer func() { _ = rows.Close() }()
	candidates := []RunRecord{}
	for rows.Next() {
		var row sqlitegen.TaskRunRecord
		if err := rows.Scan(&row.ID, &row.TaskID, &row.PlacementID, &row.NodeID, &row.SessionID, &row.RunGeneration, &row.WorkflowRevisionSeen, &row.AutomationRequestedAtUnixMs, &row.CreatedAtUnixMs, &row.UpdatedAtUnixMs, &row.StartedAtUnixMs, &row.CompletedAtUnixMs, &row.InterruptedAtUnixMs, &row.InterruptionReason, &row.InterruptionDetailJson, &row.WaitingAskID, &row.FinalAnswerViolationCount, &row.InvalidCompletionCount, &row.RunStartSnapshotJson, &row.MetadataJson); err != nil {
			return RunRecord{}, err
		}
		candidates = append(candidates, runRecordFromTaskRun(row))
	}
	if err := rows.Err(); err != nil {
		return RunRecord{}, err
	}
	if len(candidates) == 0 {
		return RunRecord{}, errors.New("task has no active workflow run to interrupt")
	}
	if trimmedRunID == "" && len(candidates) != 1 {
		return RunRecord{}, fmt.Errorf("task has multiple active workflow runs; %w", ErrRunIDRequired)
	}
	selected := candidates[0]
	interruptReason := strings.TrimSpace(reason)
	if interruptReason == "" {
		interruptReason = "user_interrupt"
	}
	if err := s.InterruptRun(ctx, selected.ID, interruptReason, "{}"); err != nil {
		return RunRecord{}, err
	}
	run, err := s.queries.GetTaskRun(ctx, string(selected.ID))
	if err != nil {
		return RunRecord{}, err
	}
	return runRecordFromTaskRun(run), nil
}

func (s *Store) ReconcileStartedRuns(ctx context.Context, reason string) (int64, error) {
	now := s.now().UnixMilli()
	return s.queries.InterruptStartedWorkflowRunsForRecovery(ctx, sqlitegen.InterruptStartedWorkflowRunsForRecoveryParams{UpdatedAtUnixMs: now, InterruptedAtUnixMs: now, InterruptionReason: strings.TrimSpace(reason), InterruptionDetailJson: "{}"})
}

func (s *Store) ListWaitingAskRuns(ctx context.Context) ([]RunRecord, error) {
	rows, err := s.queries.ListWaitingAskWorkflowRuns(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]RunRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, runRecordFromTaskRun(row))
	}
	return out, nil
}

func (s *Store) ResumeTaskRun(ctx context.Context, taskID workflow.TaskID) (RunRecord, error) {
	return s.ResumeTaskRunByID(ctx, taskID, "")
}

func (s *Store) ResumeTaskRunByID(ctx context.Context, taskID workflow.TaskID, runID workflow.RunID) (RunRecord, error) {
	if strings.TrimSpace(string(taskID)) == "" {
		return RunRecord{}, errors.New("task id is required")
	}
	task, err := s.queries.GetTask(ctx, string(taskID))
	if err != nil {
		return RunRecord{}, err
	}
	if task.CanceledAtUnixMs != 0 {
		return RunRecord{}, ErrTaskCanceled
	}
	trimmedRunID := strings.TrimSpace(string(runID))
	rows, err := s.db.QueryContext(ctx, strings.TrimSuffix(resumeTaskRunCandidatesQuery, "\n"), string(taskID), trimmedRunID, trimmedRunID)
	if err != nil {
		return RunRecord{}, err
	}
	defer func() { _ = rows.Close() }()
	type candidate struct {
		id           string
		snapshotJSON string
	}
	candidates := []candidate{}
	for rows.Next() {
		var next candidate
		if err := rows.Scan(&next.id, &next.snapshotJSON); err != nil {
			return RunRecord{}, err
		}
		candidates = append(candidates, next)
	}
	if err := rows.Err(); err != nil {
		return RunRecord{}, err
	}
	if len(candidates) == 0 {
		return RunRecord{}, errors.New("task has no interrupted workflow run to resume")
	}
	if trimmedRunID == "" && len(candidates) != 1 {
		return RunRecord{}, fmt.Errorf("task has multiple interrupted workflow runs; %w", ErrRunIDRequired)
	}
	snapshot := runStartSnapshot{}
	if err := workflow.UnmarshalString(candidates[0].snapshotJSON, &snapshot); err != nil {
		return RunRecord{}, err
	}
	if err := s.validateRunnableRole(snapshot.Node.SubagentRole); err != nil {
		return RunRecord{}, err
	}
	now := s.now().UnixMilli()
	result, err := s.db.ExecContext(ctx, strings.TrimSuffix(resumeTaskRunQuery, "\n"), now, candidates[0].id)
	if err != nil {
		return RunRecord{}, err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return RunRecord{}, err
	}
	if updated != 1 {
		return RunRecord{}, sql.ErrNoRows
	}
	run, err := s.queries.GetTaskRun(ctx, candidates[0].id)
	if err != nil {
		return RunRecord{}, err
	}
	return runRecordFromTaskRun(run), nil
}

func (s *Store) validateRunnableRole(role string) error {
	trimmed := strings.TrimSpace(role)
	if trimmed == "" {
		return WorkflowValidationError{Codes: []workflow.ValidationErrorCode{workflow.CodeAgentRoleRequired}}
	}
	if workflow.IsDefaultAgentRole(trimmed) {
		return nil
	}
	if s.roleResolver != nil && !s.roleResolver.RoleExists(trimmed) {
		return WorkflowValidationError{Codes: []workflow.ValidationErrorCode{workflow.CodeAgentRoleMissing}}
	}
	return nil
}

func (s *Store) GetRunStartContext(ctx context.Context, runID workflow.RunID) (RunStartContext, error) {
	run, err := s.queries.GetTaskRun(ctx, string(runID))
	if err != nil {
		return RunStartContext{}, err
	}
	task, err := s.queries.GetTask(ctx, run.TaskID)
	if err != nil {
		return RunStartContext{}, err
	}
	workflowRow, err := s.queries.GetWorkflow(ctx, task.WorkflowID)
	if err != nil {
		return RunStartContext{}, err
	}
	workflowRecord := WorkflowRecord{ID: workflow.WorkflowID(workflowRow.ID), Name: workflowRow.Name, Description: workflowRow.Description, Version: workflowRow.Version}
	snapshot := runStartSnapshot{}
	if err := workflow.UnmarshalString(run.RunStartSnapshotJson, &snapshot); err != nil {
		return RunStartContext{}, err
	}
	inputValues, err := s.resolveRunInputValues(ctx, run.PlacementID, taskRecordFromTask(task))
	if err != nil {
		return RunStartContext{}, err
	}
	transitionContext, err := s.resolveRunTransitionContext(ctx, run.PlacementID, run.MetadataJson)
	if err != nil {
		return RunStartContext{}, err
	}
	isFanoutBranch, err := s.placementIsFanoutBranch(ctx, run.TaskID, run.PlacementID)
	if err != nil {
		return RunStartContext{}, err
	}
	runMetadata := workflowRunMetadata{}
	if strings.TrimSpace(run.MetadataJson) != "" {
		if err := workflow.UnmarshalString(run.MetadataJson, &runMetadata); err != nil {
			return RunStartContext{}, fmt.Errorf("resolve workflow run metadata: %w", err)
		}
	}
	parameterValues := map[string]string{}
	for key, value := range inputValues {
		parameterValues[key] = value
	}
	priorParameterValues := clonePriorParameterValues(runMetadata.PriorParameterValues)
	parameters := append([]workflow.Parameter(nil), runMetadata.Parameters...)
	worktreeID := strings.TrimSpace(task.ManagedWorktreeID.String)
	if worktreeID == "" {
		return RunStartContext{
			Run:                  runRecordFromTaskRun(run),
			Task:                 taskRecordFromTask(task),
			Workflow:             workflowRecord,
			Node:                 nodeRecordFromSnapshot(snapshot.Node, snapshot.WorkflowID),
			ContextMode:          transitionContext.ContextMode,
			SourceRunID:          transitionContext.SourceRunID,
			SourceSessionID:      transitionContext.SourceSessionID,
			SourceNode:           transitionContext.SourceNode,
			IsFanoutBranch:       isFanoutBranch,
			TransitionIDs:        transitionIDsFromSnapshot(snapshot),
			TransitionOptions:    transitionOptionsFromSnapshot(snapshot),
			PromptTemplate:       strings.TrimSpace(runMetadata.PromptTemplate),
			Parameters:           parameters,
			ParameterValues:      parameterValues,
			PriorParameterValues: priorParameterValues,
			InputValues:          inputValues,
			NodeOutputValues:     runMetadata.NodeOutputValues,
		}, nil
	}
	worktree, err := s.metadata.GetWorktreeRecordByID(ctx, worktreeID)
	if err != nil {
		return RunStartContext{}, err
	}
	workspace, err := s.metadata.GetWorkspaceByID(ctx, worktree.WorkspaceID)
	if err != nil {
		return RunStartContext{}, err
	}
	return RunStartContext{
		Run:                  runRecordFromTaskRun(run),
		Task:                 taskRecordFromTask(task),
		Workflow:             workflowRecord,
		Node:                 nodeRecordFromSnapshot(snapshot.Node, snapshot.WorkflowID),
		ContextMode:          transitionContext.ContextMode,
		SourceRunID:          transitionContext.SourceRunID,
		SourceSessionID:      transitionContext.SourceSessionID,
		SourceNode:           transitionContext.SourceNode,
		TransitionIDs:        transitionIDsFromSnapshot(snapshot),
		TransitionOptions:    transitionOptionsFromSnapshot(snapshot),
		PromptTemplate:       strings.TrimSpace(runMetadata.PromptTemplate),
		Parameters:           parameters,
		ParameterValues:      parameterValues,
		PriorParameterValues: priorParameterValues,
		InputValues:          inputValues,
		NodeOutputValues:     runMetadata.NodeOutputValues,
		WorkspaceID:          workspace.ID,
		WorkspaceRoot:        workspace.CanonicalRootPath,
		WorktreeID:           worktree.ID,
		WorktreeRoot:         worktree.CanonicalRoot,
	}, nil
}

// placementIsFanoutBranch reports whether the run's placement was created as a
// branch of a parallel fan-out transition group. The scheduler records this by
// setting ParallelBranchEdgeID only on fan-out branch placements.
func (s *Store) placementIsFanoutBranch(ctx context.Context, taskID, placementID string) (bool, error) {
	placementID = strings.TrimSpace(placementID)
	if placementID == "" {
		return false, nil
	}
	placements, err := s.queries.ListTaskNodePlacements(ctx, taskID)
	if err != nil {
		return false, fmt.Errorf("list task node placements: %w", err)
	}
	for _, placement := range placements {
		if placement.ID != placementID {
			continue
		}
		return placement.ParallelBranchEdgeID.Valid && strings.TrimSpace(placement.ParallelBranchEdgeID.String) != "", nil
	}
	return false, nil
}

type runTransitionContext struct {
	ContextMode     workflow.ContextMode
	SourceRunID     workflow.RunID
	SourceSessionID string
	SourceNode      NodeRecord
}

func (s *Store) resolveRunTransitionContext(ctx context.Context, placementID string, runMetadataJSON string) (runTransitionContext, error) {
	var contextMode string
	var sourceRunID sql.NullString
	err := s.db.QueryRowContext(ctx, strings.TrimSuffix(resolveRunTransitionContextQuery, "\n"), placementID).Scan(&contextMode, &sourceRunID)
	if errors.Is(err, sql.ErrNoRows) {
		return runTransitionContext{ContextMode: workflow.ContextModeNewSession}, nil
	}
	if err != nil {
		return runTransitionContext{}, fmt.Errorf("resolve workflow run transition context: %w", err)
	}
	resolved := runTransitionContext{ContextMode: workflow.ContextMode(strings.TrimSpace(contextMode))}
	if resolved.ContextMode == "" {
		resolved.ContextMode = workflow.ContextModeNewSession
	}
	runMetadata := workflowRunMetadata{}
	if strings.TrimSpace(runMetadataJSON) != "" {
		if err := workflow.UnmarshalString(runMetadataJSON, &runMetadata); err != nil {
			return runTransitionContext{}, fmt.Errorf("resolve workflow run metadata: %w", err)
		}
		if strings.TrimSpace(runMetadata.ContextMode) != "" {
			resolved.ContextMode = workflow.ContextMode(strings.TrimSpace(runMetadata.ContextMode))
		}
		if strings.TrimSpace(runMetadata.SourceRunID) != "" {
			sourceRunID = sql.NullString{String: strings.TrimSpace(runMetadata.SourceRunID), Valid: true}
		}
	}
	if !sourceRunID.Valid || strings.TrimSpace(sourceRunID.String) == "" {
		return resolved, nil
	}
	sourceRun, err := s.queries.GetTaskRun(ctx, sourceRunID.String)
	if err != nil {
		return runTransitionContext{}, err
	}
	sourceSnapshot := runStartSnapshot{}
	if err := workflow.UnmarshalString(sourceRun.RunStartSnapshotJson, &sourceSnapshot); err != nil {
		return runTransitionContext{}, err
	}
	resolved.SourceRunID = workflow.RunID(sourceRun.ID)
	resolved.SourceSessionID = strings.TrimSpace(sourceRun.SessionID.String)
	resolved.SourceNode = nodeRecordFromSnapshot(sourceSnapshot.Node, sourceSnapshot.WorkflowID)
	return resolved, nil
}

func (s *Store) resolveRunInputValues(ctx context.Context, placementID string, task TaskRecord) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, strings.TrimSuffix(resolveRunInputValuesQuery, "\n"), placementID)
	if err != nil {
		return nil, fmt.Errorf("resolve workflow run input values: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return map[string]string{}, nil
	}
	var commentary, outputValuesJSON, inputBindingsJSON string
	if err := rows.Scan(&commentary, &outputValuesJSON, &inputBindingsJSON); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	outputValues := map[string]string{}
	if err := workflow.UnmarshalString(outputValuesJSON, &outputValues); err != nil {
		return nil, err
	}
	bindings := []workflow.InputBinding{}
	if err := workflow.UnmarshalString(inputBindingsJSON, &bindings); err != nil {
		return nil, err
	}
	return resolveInputBindingValues(task, commentary, outputValues, bindings)
}

func resolveInputBindingValues(task TaskRecord, commentary string, outputValues map[string]string, bindings []workflow.InputBinding) (map[string]string, error) {
	if len(bindings) == 0 {
		return map[string]string{}, nil
	}
	values := make(map[string]string, len(bindings))
	for _, binding := range bindings {
		name := strings.TrimSpace(binding.Name)
		if name == "" {
			continue
		}
		switch binding.Source {
		case workflow.BindingSourceTask:
			values[name] = taskInputBindingValue(task, binding.Field)
		case workflow.BindingSourceTransitionOutput:
			field := strings.TrimSpace(binding.Field)
			if field == "commentary" {
				values[name] = commentary
			} else {
				values[name] = outputValues[field]
			}
		case workflow.BindingSourceJoin:
			values[name] = outputValues[strings.TrimSpace(binding.Field)]
		default:
			return nil, fmt.Errorf("unsupported input binding source %q", binding.Source)
		}
	}
	return values, nil
}

func taskInputBindingValue(task TaskRecord, field string) string {
	switch strings.TrimSpace(field) {
	case "short_id":
		return task.ShortID
	case "title":
		return task.Title
	case "body":
		return task.Body
	case "source_url":
		return task.SourceURL
	default:
		return ""
	}
}

func (s *Store) AttachRunSession(ctx context.Context, runID workflow.RunID, expectedGeneration int64, sessionID string) error {
	result, err := s.db.ExecContext(ctx, strings.TrimSuffix(attachRunSessionQuery, "\n"),
		s.now().UnixMilli(),
		strings.TrimSpace(sessionID),
		string(runID),
		expectedGeneration,
		strings.TrimSpace(sessionID),
	)
	if err != nil {
		return fmt.Errorf("attach workflow run session: %w", err)
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

func (s *Store) SetRunWaitingAsk(ctx context.Context, runID workflow.RunID, expectedGeneration int64, askID string) error {
	trimmedAskID := strings.TrimSpace(askID)
	if trimmedAskID == "" {
		return fmt.Errorf("ask id is required")
	}
	now := s.now().UnixMilli()
	result, err := s.db.ExecContext(ctx, strings.TrimSuffix(setRunWaitingAskQuery, "\n"),
		now,
		trimmedAskID,
		string(runID),
		expectedGeneration,
	)
	if err != nil {
		return fmt.Errorf("set workflow run waiting ask: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated != 1 {
		return sql.ErrNoRows
	}
	event, err := runWaitingAskWorkflowEvent(ctx, s.db, string(runID), "question_waiting", trimmedAskID, now)
	if err != nil {
		return err
	}
	return s.PublishWorkflowEvent(ctx, event)
}

func (s *Store) ClearRunWaitingAsk(ctx context.Context, runID workflow.RunID, expectedGeneration int64, askID string) error {
	trimmedAskID := strings.TrimSpace(askID)
	if trimmedAskID == "" {
		return fmt.Errorf("ask id is required")
	}
	now := s.now().UnixMilli()
	result, err := s.db.ExecContext(ctx, strings.TrimSuffix(clearRunWaitingAskQuery, "\n"),
		now,
		string(runID),
		expectedGeneration,
		trimmedAskID,
	)
	if err != nil {
		return fmt.Errorf("clear workflow run waiting ask: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated != 1 {
		return sql.ErrNoRows
	}
	event, err := runWaitingAskWorkflowEvent(ctx, s.db, string(runID), "question_cleared", trimmedAskID, now)
	if err != nil {
		return err
	}
	return s.PublishWorkflowEvent(ctx, event)
}

func runWaitingAskWorkflowEvent(
	ctx context.Context,
	db *sql.DB,
	runID string,
	action string,
	askID string,
	occurredAtUnixMs int64,
) (WorkflowEventRecord, error) {
	var projectID string
	var workflowID string
	var taskID string
	if err := db.QueryRowContext(ctx, `
SELECT t.project_id, t.workflow_id, t.id
FROM task_runs r
JOIN task_node_placements p ON p.id = r.placement_id
JOIN task_records t ON t.id = p.task_id
WHERE r.id = ?
`, strings.TrimSpace(runID)).Scan(&projectID, &workflowID, &taskID); err != nil {
		return WorkflowEventRecord{}, fmt.Errorf("load waiting ask event run identity: %w", err)
	}
	return WorkflowEventRecord{
		ProjectID:        projectID,
		WorkflowID:       workflowID,
		Resource:         "task",
		Action:           action,
		ChangedIDs:       []string{taskID, strings.TrimSpace(runID), strings.TrimSpace(askID)},
		OccurredAtUnixMs: occurredAtUnixMs,
	}, nil
}

func (s *Store) ResolveTaskWaitingAsk(ctx context.Context, taskID workflow.TaskID, runID workflow.RunID, askID string) (RunRecord, error) {
	trimmedTaskID := strings.TrimSpace(string(taskID))
	trimmedRunID := strings.TrimSpace(string(runID))
	trimmedAskID := strings.TrimSpace(askID)
	if trimmedTaskID == "" {
		return RunRecord{}, errors.New("task id is required")
	}
	if trimmedAskID == "" {
		return RunRecord{}, errors.New("ask id is required")
	}
	rows, err := s.db.QueryContext(ctx, strings.TrimSuffix(resolveTaskWaitingAskQuery, "\n"), trimmedTaskID, trimmedAskID, trimmedRunID, trimmedRunID)
	if err != nil {
		return RunRecord{}, err
	}
	defer func() { _ = rows.Close() }()
	matches := []RunRecord{}
	for rows.Next() {
		var row sqlitegen.TaskRunRecord
		if err := rows.Scan(&row.ID, &row.TaskID, &row.PlacementID, &row.NodeID, &row.SessionID, &row.RunGeneration, &row.WorkflowRevisionSeen, &row.AutomationRequestedAtUnixMs, &row.CreatedAtUnixMs, &row.UpdatedAtUnixMs, &row.StartedAtUnixMs, &row.CompletedAtUnixMs, &row.InterruptedAtUnixMs, &row.InterruptionReason, &row.InterruptionDetailJson, &row.WaitingAskID, &row.FinalAnswerViolationCount, &row.InvalidCompletionCount, &row.RunStartSnapshotJson, &row.MetadataJson); err != nil {
			return RunRecord{}, err
		}
		matches = append(matches, runRecordFromTaskRun(row))
	}
	if err := rows.Err(); err != nil {
		return RunRecord{}, err
	}
	if len(matches) == 0 {
		return RunRecord{}, ErrTaskAskNotPending
	}
	if trimmedRunID == "" && len(matches) != 1 {
		return RunRecord{}, fmt.Errorf("task has multiple matching pending asks; %w", ErrRunIDRequired)
	}
	return matches[0], nil
}
