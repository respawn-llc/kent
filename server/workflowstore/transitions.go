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

func (s *Store) ListTransitions(ctx context.Context, taskID workflow.TaskID) ([]TransitionRecord, error) {
	rows, err := s.queries.ListTaskTransitions(ctx, string(taskID))
	if err != nil {
		return nil, err
	}
	out := make([]TransitionRecord, 0, len(rows))
	for _, row := range rows {
		outputs := map[string]string{}
		if err := unmarshalJSON(row.OutputValuesJson, &outputs); err != nil {
			return nil, err
		}
		out = append(out, TransitionRecord{ID: workflow.TransitionID(row.ID), TaskID: workflow.TaskID(row.TaskID), TransitionID: row.TransitionID, State: row.State, Commentary: row.Commentary, OutputValues: outputs, CreatedAt: row.CreatedAtUnixMs})
	}
	return out, nil
}

func (s *Store) ListTransitionEdges(ctx context.Context, transitionID workflow.TransitionID) ([]TransitionEdgeRecord, error) {
	rows, err := s.queries.ListTaskTransitionEdges(ctx, string(transitionID))
	if err != nil {
		return nil, err
	}
	out := make([]TransitionEdgeRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, TransitionEdgeRecord{
			ID:                   row.ID,
			TaskTransitionID:     workflow.TransitionID(row.TaskTransitionID),
			WorkflowEdgeID:       workflow.EdgeID(row.WorkflowEdgeID.String),
			EdgeKey:              row.EdgeKey,
			TargetNodeID:         workflow.NodeID(row.TargetNodeID.String),
			TargetPlacementID:    workflow.PlacementID(row.TargetPlacementID.String),
			State:                row.State,
			WorkflowRevisionSeen: row.WorkflowRevisionSeen,
		})
	}
	return out, nil
}

func (s *Store) TaskIdentityForTransition(ctx context.Context, transitionID workflow.TransitionID) (taskID string, projectID string, workflowID string, err error) {
	id := strings.TrimSpace(string(transitionID))
	if id == "" {
		return "", "", "", errors.New("transition id is required")
	}
	err = s.db.QueryRowContext(ctx, `
SELECT t.id, t.project_id, t.workflow_id
FROM task_transitions tt
JOIN task_records t ON t.id = tt.task_id
WHERE tt.id = ?
LIMIT 1`, id).Scan(&taskID, &projectID, &workflowID)
	return taskID, projectID, workflowID, err
}

func (s *Store) ApproveTransition(ctx context.Context, transitionID workflow.TransitionID) (CompleteRunResult, error) {
	id := strings.TrimSpace(string(transitionID))
	if id == "" {
		return CompleteRunResult{}, errors.New("transition id is required")
	}
	var taskID string
	var sourceRunID sql.NullString
	var state string
	var revision int64
	err := s.db.QueryRowContext(ctx, `
SELECT task_id, source_run_id, state, workflow_revision_seen
FROM task_transitions
WHERE id = ?`, id).Scan(&taskID, &sourceRunID, &state, &revision)
	if err != nil {
		return CompleteRunResult{}, err
	}
	if state == "approved" || state == "applied" {
		return s.approvedTransitionResult(ctx, id, state)
	}
	if state != "pending_approval" {
		return CompleteRunResult{}, fmt.Errorf("transition %s is not pending approval", id)
	}
	edges, err := s.queries.ListTaskTransitionEdges(ctx, id)
	if err != nil {
		return CompleteRunResult{}, err
	}
	if len(edges) == 0 {
		return CompleteRunResult{}, errors.New("pending approval has no edge snapshots")
	}
	hasSourceRun := sourceRunID.Valid && strings.TrimSpace(sourceRunID.String) != ""
	sourceRun := sqlitegen.TaskRunRecord{}
	sourceSnapshot := runStartSnapshot{}
	if hasSourceRun {
		sourceRun, err = s.queries.GetTaskRun(ctx, sourceRunID.String)
		if err != nil {
			return CompleteRunResult{}, err
		}
		if err := unmarshalJSON(sourceRun.RunStartSnapshotJson, &sourceSnapshot); err != nil {
			return CompleteRunResult{}, err
		}
	}
	var fallbackDef workflow.Definition
	var fallbackWorkflow WorkflowRecord
	if !hasSourceRun || !sourceSnapshot.hasFullGraphContract() {
		for _, edge := range edges {
			if workflow.NodeKind(edge.TargetNodeKind) != workflow.NodeKindAgent {
				continue
			}
			workflowID := sourceSnapshot.WorkflowID
			if !hasSourceRun {
				task, taskErr := s.queries.GetTask(ctx, taskID)
				if taskErr != nil {
					return CompleteRunResult{}, taskErr
				}
				workflowID = workflow.WorkflowID(task.WorkflowID)
			}
			fallbackDef, fallbackWorkflow, err = s.GetDefinition(ctx, workflowID)
			if err != nil {
				return CompleteRunResult{}, err
			}
			break
		}
	}
	now := s.now().UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CompleteRunResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	updatedTransition, err := tx.ExecContext(ctx, `
UPDATE task_transitions
SET state = 'approved', applied_at_unix_ms = ?
WHERE id = ? AND state = 'pending_approval'`, now, id)
	if err != nil {
		return CompleteRunResult{}, err
	}
	updatedCount, err := updatedTransition.RowsAffected()
	if err != nil {
		return CompleteRunResult{}, err
	}
	if updatedCount != 1 {
		_ = tx.Rollback()
		var currentState string
		if scanErr := s.db.QueryRowContext(ctx, `SELECT state FROM task_transitions WHERE id = ?`, id).Scan(&currentState); scanErr != nil {
			return CompleteRunResult{}, scanErr
		}
		if currentState == "approved" || currentState == "applied" {
			return s.approvedTransitionResult(ctx, id, currentState)
		}
		return CompleteRunResult{}, sql.ErrNoRows
	}
	if err := touchTaskUpdatedAt(ctx, tx, taskID, now); err != nil {
		return CompleteRunResult{}, err
	}
	result := CompleteRunResult{TransitionID: workflow.TransitionID(id), State: "approved"}
	for _, edge := range edges {
		if edge.State != "pending" {
			continue
		}
		targetEdge, err := edgeContractSnapshotFromTransitionEdge(edge)
		if err != nil {
			return CompleteRunResult{}, err
		}
		if targetEdge.TargetNode.Kind == workflow.NodeKindJoin {
			if !hasSourceRun {
				return CompleteRunResult{}, errors.New("pending approval to join has no source run")
			}
			if _, err := tx.ExecContext(ctx, `
UPDATE task_transition_edges
SET state = 'applied'
WHERE id = ? AND state = 'pending'`, edge.ID); err != nil {
				return CompleteRunResult{}, fmt.Errorf("update approved join edge snapshot: %w", err)
			}
			joined, err := s.applyJoinIfReady(ctx, tx, q, now, taskID, sourceRun.PlacementID, sourceSnapshot, targetEdge)
			if err != nil {
				return CompleteRunResult{}, err
			}
			result.PlacementIDs = append(result.PlacementIDs, joined.PlacementIDs...)
			result.RunIDs = append(result.RunIDs, joined.RunIDs...)
			continue
		}
		targetPlacementID := prefixedID("placement")
		if err := q.InsertTaskNodePlacement(ctx, sqlitegen.InsertTaskNodePlacementParams{ID: targetPlacementID, TaskID: taskID, NodeID: edge.TargetNodeID.String, State: "active", ParallelBatchTransitionID: sql.NullString{String: id, Valid: len(edges) > 1}, ParallelBranchEdgeID: sql.NullString{String: edge.WorkflowEdgeID.String, Valid: len(edges) > 1 && edge.WorkflowEdgeID.Valid}, CreatedAtUnixMs: now, UpdatedAtUnixMs: now}); err != nil {
			return CompleteRunResult{}, fmt.Errorf("insert approved target placement: %w", err)
		}
		result.PlacementIDs = append(result.PlacementIDs, workflow.PlacementID(targetPlacementID))
		if _, err := tx.ExecContext(ctx, `
UPDATE task_transition_edges
SET state = 'applied', target_placement_id = ?
WHERE id = ? AND state = 'pending'`, targetPlacementID, edge.ID); err != nil {
			return CompleteRunResult{}, fmt.Errorf("update approved edge snapshot: %w", err)
		}
		if workflow.NodeKind(edge.TargetNodeKind) != workflow.NodeKindAgent {
			continue
		}
		targetRunID := prefixedID("run")
		targetSnapshot := runStartSnapshot{}
		foundSnapshot := false
		if hasSourceRun {
			targetSnapshot, foundSnapshot, err = sourceSnapshot.forNode(targetEdge.TargetNode)
			if err != nil {
				return CompleteRunResult{}, err
			}
		}
		if !foundSnapshot {
			targetSnapshot, err = newRunStartSnapshot(fallbackDef, fallbackWorkflow, targetEdge.TargetNode.ID)
			if err != nil {
				return CompleteRunResult{}, err
			}
		}
		targetSnapshotJSON, err := marshalJSON(targetSnapshot)
		if err != nil {
			return CompleteRunResult{}, err
		}
		targetMetadataJSON, err := marshalJSON(map[string]string{
			"context_mode":      string(targetEdge.ContextMode),
			"source_run_id":     sourceRun.ID,
			"source_session_id": strings.TrimSpace(sourceRun.SessionID.String),
		})
		if err != nil {
			return CompleteRunResult{}, err
		}
		if err := q.InsertTaskRun(ctx, sqlitegen.InsertTaskRunParams{ID: targetRunID, PlacementID: targetPlacementID, WorkflowRevisionSeen: targetSnapshot.WorkflowRevisionSeen, AutomationRequestedAtUnixMs: now, CreatedAtUnixMs: now, UpdatedAtUnixMs: now, InterruptionDetailJson: "{}", RunStartSnapshotJson: targetSnapshotJSON, MetadataJson: targetMetadataJSON}); err != nil {
			return CompleteRunResult{}, fmt.Errorf("insert approved target run: %w", err)
		}
		result.RunIDs = append(result.RunIDs, workflow.RunID(targetRunID))
	}
	if err := tx.Commit(); err != nil {
		return CompleteRunResult{}, err
	}
	return result, nil
}

func (s *Store) RejectTransition(ctx context.Context, transitionID workflow.TransitionID) (CompleteRunResult, error) {
	id := strings.TrimSpace(string(transitionID))
	if id == "" {
		return CompleteRunResult{}, errors.New("transition id is required")
	}
	now := s.now().UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CompleteRunResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
UPDATE task_transitions
SET state = 'rejected', applied_at_unix_ms = ?
WHERE id = ? AND state = 'pending_approval'`, now, id)
	if err != nil {
		return CompleteRunResult{}, err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return CompleteRunResult{}, err
	}
	if updated == 0 {
		var state string
		if scanErr := tx.QueryRowContext(ctx, `SELECT state FROM task_transitions WHERE id = ?`, id).Scan(&state); scanErr != nil {
			return CompleteRunResult{}, scanErr
		}
		if state != "rejected" {
			return CompleteRunResult{}, fmt.Errorf("transition %s is not pending approval", id)
		}
		return CompleteRunResult{TransitionID: workflow.TransitionID(id), State: "rejected"}, tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE task_transition_edges
SET state = 'blocked'
WHERE task_transition_id = ? AND state = 'pending'`, id); err != nil {
		return CompleteRunResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return CompleteRunResult{}, err
	}
	return CompleteRunResult{TransitionID: workflow.TransitionID(id), State: "rejected"}, nil
}

func (s *Store) approvedTransitionResult(ctx context.Context, transitionID string, state string) (CompleteRunResult, error) {
	edges, err := s.queries.ListTaskTransitionEdges(ctx, transitionID)
	if err != nil {
		return CompleteRunResult{}, err
	}
	result := CompleteRunResult{TransitionID: workflow.TransitionID(transitionID), State: state}
	placementIDs := map[string]bool{}
	runIDs := map[string]bool{}
	for _, edge := range edges {
		if edge.TargetPlacementID.Valid && strings.TrimSpace(edge.TargetPlacementID.String) != "" && !placementIDs[edge.TargetPlacementID.String] {
			placementIDs[edge.TargetPlacementID.String] = true
			result.PlacementIDs = append(result.PlacementIDs, workflow.PlacementID(edge.TargetPlacementID.String))
		}
		if !edge.TargetPlacementID.Valid || strings.TrimSpace(edge.TargetPlacementID.String) == "" {
			continue
		}
		rows, err := s.db.QueryContext(ctx, `
SELECT id
FROM task_runs
WHERE placement_id = ?
ORDER BY created_at_unix_ms, id`, strings.TrimSpace(edge.TargetPlacementID.String))
		if err != nil {
			return CompleteRunResult{}, err
		}
		for rows.Next() {
			var runID string
			if err := rows.Scan(&runID); err != nil {
				_ = rows.Close()
				return CompleteRunResult{}, err
			}
			if trimmed := strings.TrimSpace(runID); trimmed != "" && !runIDs[trimmed] {
				runIDs[trimmed] = true
				result.RunIDs = append(result.RunIDs, workflow.RunID(trimmed))
			}
		}
		if err := rows.Close(); err != nil {
			return CompleteRunResult{}, err
		}
		if err := rows.Err(); err != nil {
			return CompleteRunResult{}, err
		}
	}
	return result, nil
}

func edgeContractSnapshotFromTransitionEdge(edge sqlitegen.TaskTransitionEdgeRecord) (edgeContractSnapshot, error) {
	inputs := []workflow.InputBinding{}
	if err := unmarshalJSON(edge.InputBindingsJson, &inputs); err != nil {
		return edgeContractSnapshot{}, err
	}
	requirements := []workflow.OutputRequirement{}
	if err := unmarshalJSON(edge.OutputRequirementsJson, &requirements); err != nil {
		return edgeContractSnapshot{}, err
	}
	return edgeContractSnapshot{
		ID:  workflow.EdgeID(edge.WorkflowEdgeID.String),
		Key: workflow.ModelKey(edge.EdgeKey),
		TargetNode: nodeContractSnapshot{
			ID:          workflow.NodeID(edge.TargetNodeID.String),
			Key:         workflow.ModelKey(edge.TargetNodeKey),
			DisplayName: edge.TargetNodeDisplayName,
			Kind:        workflow.NodeKind(edge.TargetNodeKind),
		},
		ContextMode:        workflow.ContextMode(edge.ContextMode),
		RequiresApproval:   edge.RequiresApproval != 0,
		InputBindings:      inputs,
		OutputRequirements: requirements,
	}, nil
}

func insertTransitionEdgeSnapshot(ctx context.Context, q *sqlitegen.Queries, transitionID string, edge edgeContractSnapshot, targetPlacementID string, state string) error {
	if err := q.InsertTaskTransitionEdge(ctx, sqlitegen.InsertTaskTransitionEdgeParams{
		ID:                     prefixedID("transition-edge"),
		TaskTransitionID:       transitionID,
		WorkflowEdgeID:         sql.NullString{String: string(edge.ID), Valid: edge.ID != ""},
		EdgeKey:                string(edge.Key),
		TargetNodeID:           sql.NullString{String: string(edge.TargetNode.ID), Valid: edge.TargetNode.ID != ""},
		TargetNodeKey:          string(edge.TargetNode.Key),
		TargetNodeDisplayName:  edge.TargetNode.DisplayName,
		TargetNodeKind:         string(edge.TargetNode.Kind),
		TargetPlacementID:      sql.NullString{String: targetPlacementID, Valid: targetPlacementID != ""},
		State:                  state,
		ContextMode:            string(edge.ContextMode),
		RequiresApproval:       boolToInt64(edge.RequiresApproval),
		InputBindingsJson:      mustInputBindingsJSON(edge.InputBindings),
		OutputRequirementsJson: mustOutputRequirementsJSON(edge.OutputRequirements),
		MetadataJson:           "{}",
	}); err != nil {
		return fmt.Errorf("insert transition edge snapshot: %w", err)
	}
	return nil
}
