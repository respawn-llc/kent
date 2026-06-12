package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"builder/server/metadata/sqlitegen"
	"builder/server/workflow"
	"builder/server/workflowjson"
)

func (s *Store) ListTransitions(ctx context.Context, taskID workflow.TaskID) ([]TransitionRecord, error) {
	rows, err := s.queries.ListTaskTransitions(ctx, string(taskID))
	if err != nil {
		return nil, err
	}
	out := make([]TransitionRecord, 0, len(rows))
	for _, row := range rows {
		outputs := map[string]string{}
		if err := workflowjson.UnmarshalString(row.OutputValuesJson, &outputs); err != nil {
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
	err = s.db.QueryRowContext(ctx, strings.TrimSuffix(taskIdentityForTransitionQuery, "\n"), id).Scan(&taskID, &projectID, &workflowID)
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
	var transitionCreatedAt int64
	err := s.db.QueryRowContext(ctx, `
SELECT task_id, source_run_id, state, workflow_revision_seen, created_at_unix_ms
FROM task_transitions
WHERE id = ?`, id).Scan(&taskID, &sourceRunID, &state, &revision, &transitionCreatedAt)
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
		if err := workflowjson.UnmarshalString(sourceRun.RunStartSnapshotJson, &sourceSnapshot); err != nil {
			return CompleteRunResult{}, err
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
		edgeMetadata, err := transitionEdgeMetadata(edge)
		if err != nil {
			return CompleteRunResult{}, err
		}
		targetSnapshot := runStartSnapshot{}
		foundSnapshot := false
		if hasSourceRun {
			targetSnapshot, foundSnapshot, err = sourceSnapshot.forNode(targetEdge.TargetNode)
			if err != nil {
				return CompleteRunResult{}, err
			}
		}
		if !foundSnapshot {
			if edgeMetadata.TargetRunStartSnapshot == nil {
				return CompleteRunResult{}, fmt.Errorf("pending approval edge %q has no frozen run-start snapshot for target node %q", edge.ID, targetEdge.TargetNode.ID)
			}
			targetSnapshot = *edgeMetadata.TargetRunStartSnapshot
		}
		targetSnapshotJSON, err := workflowjson.MarshalString(targetSnapshot)
		if err != nil {
			return CompleteRunResult{}, err
		}
		source, ok, err := resolvedContextSourceRunFromMetadata(ctx, tx, edgeMetadata)
		if err != nil {
			return CompleteRunResult{}, err
		}
		if !ok {
			source, err = s.resolveContextSourceRun(ctx, tx, taskID, transitionCreatedAt, sourceRun.PlacementID, &sourceRun, sourceSnapshot, targetEdge)
			if err != nil {
				return CompleteRunResult{}, err
			}
		}
		targetMetadataJSON, err := workflowjson.MarshalString(workflowRunMetadata{
			ContextMode:          string(targetEdge.ContextMode),
			ContextSource:        workflow.CanonicalContextSource(targetEdge.ContextSource),
			SourceRunID:          source.runID,
			SourceSessionID:      source.sessionID,
			PromptTemplate:       strings.TrimSpace(targetEdge.PromptTemplate),
			Parameters:           append([]workflow.Parameter(nil), targetEdge.Parameters...),
			PriorParameterValues: clonePriorParameterValues(edgeMetadata.PriorParameterValues),
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
	if err := workflowjson.UnmarshalString(edge.InputBindingsJson, &inputs); err != nil {
		return edgeContractSnapshot{}, err
	}
	requirements := []workflow.OutputRequirement{}
	if err := workflowjson.UnmarshalString(edge.OutputRequirementsJson, &requirements); err != nil {
		return edgeContractSnapshot{}, err
	}
	metadata, err := transitionEdgeMetadata(edge)
	if err != nil {
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
		ContextSource:      workflow.CanonicalContextSource(metadata.ContextSource),
		RequiresApproval:   edge.RequiresApproval != 0,
		PromptTemplate:     strings.TrimSpace(metadata.PromptTemplate),
		Parameters:         append([]workflow.Parameter(nil), metadata.Parameters...),
		InputBindings:      inputs,
		OutputRequirements: requirements,
	}, nil
}

func transitionEdgeMetadata(edge sqlitegen.TaskTransitionEdgeRecord) (workflowRunMetadata, error) {
	metadata := workflowRunMetadata{}
	if strings.TrimSpace(edge.MetadataJson) != "" {
		if err := workflowjson.UnmarshalString(edge.MetadataJson, &metadata); err != nil {
			return workflowRunMetadata{}, err
		}
	}
	return metadata, nil
}

func insertTransitionEdgeSnapshotWithMetadata(ctx context.Context, q *sqlitegen.Queries, transitionID string, edge edgeContractSnapshot, targetPlacementID string, state string, metadata workflowRunMetadata) error {
	metadata.ContextSource = workflow.CanonicalContextSource(edge.ContextSource)
	metadata.PromptTemplate = strings.TrimSpace(edge.PromptTemplate)
	metadata.Parameters = append([]workflow.Parameter(nil), edge.Parameters...)
	metadataJSON, err := workflowjson.MarshalString(metadata)
	if err != nil {
		return err
	}
	requiresApproval := int64(0)
	if edge.RequiresApproval {
		requiresApproval = 1
	}
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
		RequiresApproval:       requiresApproval,
		InputBindingsJson:      mustInputBindingsJSON(edge.InputBindings),
		OutputRequirementsJson: mustOutputRequirementsJSON(edge.OutputRequirements),
		MetadataJson:           metadataJSON,
	}); err != nil {
		return fmt.Errorf("insert transition edge snapshot: %w", err)
	}
	return nil
}
