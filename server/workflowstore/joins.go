package workflowstore

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"builder/server/metadata/sqlitegen"
	"builder/server/workflow"
)

type joinArrival struct {
	PlacementID   string
	BranchEdgeID  string
	SourceNodeKey string
	OutputValues  map[string]string
}

func (s *Store) applyJoinIfReady(ctx context.Context, tx *sql.Tx, q *sqlitegen.Queries, now int64, taskID string, sourcePlacementID string, sourceSnapshot runStartSnapshot, joinEdge edgeContractSnapshot) (CompleteRunResult, error) {
	var batchID sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT parallel_batch_transition_id FROM task_node_placements WHERE id = ?`, sourcePlacementID).Scan(&batchID); err != nil {
		return CompleteRunResult{}, err
	}
	if !batchID.Valid || strings.TrimSpace(batchID.String) == "" {
		return CompleteRunResult{}, nil
	}
	var existingJoinPlacement string
	err := tx.QueryRowContext(ctx, `
SELECT id
FROM task_node_placements
WHERE task_id = ? AND node_id = ? AND parallel_batch_transition_id = ?
LIMIT 1`, taskID, string(joinEdge.TargetNode.ID), batchID.String).Scan(&existingJoinPlacement)
	if err == nil {
		return CompleteRunResult{}, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return CompleteRunResult{}, err
	}
	expected, err := joinExpectedBranches(ctx, tx, batchID.String)
	if err != nil {
		return CompleteRunResult{}, err
	}
	if len(expected) == 0 {
		return CompleteRunResult{}, nil
	}
	arrivals, err := joinArrivals(ctx, tx, batchID.String, joinEdge.TargetNode.ID)
	if err != nil {
		return CompleteRunResult{}, err
	}
	arrivedExpected := map[string]bool{}
	for _, arrival := range arrivals {
		if expected[arrival.PlacementID] {
			arrivedExpected[arrival.PlacementID] = true
		}
	}
	if len(arrivedExpected) < len(expected) {
		return CompleteRunResult{}, nil
	}
	joinSnapshot, found, err := sourceSnapshot.forNode(joinEdge.TargetNode)
	if err != nil {
		return CompleteRunResult{}, err
	}
	if !found {
		return CompleteRunResult{}, fmt.Errorf("join node %q missing from run snapshot", joinEdge.TargetNode.ID)
	}
	groups := joinSnapshot.transitionGroupsForNode(joinEdge.TargetNode.ID)
	if len(groups) != 1 || len(groups[0].Edges) != 1 {
		return CompleteRunResult{}, fmt.Errorf("join node %q must have exactly one outgoing edge", joinEdge.TargetNode.ID)
	}
	aggregate := deterministicJoinAggregate(arrivals)
	aggregateJSON, err := marshalJSON(map[string]string{"aggregate": aggregate})
	if err != nil {
		return CompleteRunResult{}, err
	}
	joinPlacementID := prefixedID("placement")
	if err := q.InsertTaskNodePlacement(ctx, sqlitegen.InsertTaskNodePlacementParams{ID: joinPlacementID, TaskID: taskID, NodeID: string(joinEdge.TargetNode.ID), State: "completed", ParallelBatchTransitionID: sql.NullString{String: batchID.String, Valid: true}, CreatedAtUnixMs: now, UpdatedAtUnixMs: now}); err != nil {
		return CompleteRunResult{}, err
	}
	joinTransitionID := prefixedID("transition")
	group := groups[0]
	if err := q.InsertTaskTransition(ctx, sqlitegen.InsertTaskTransitionParams{ID: joinTransitionID, TaskID: taskID, SourcePlacementID: sql.NullString{String: joinPlacementID, Valid: true}, SourceNodeKey: string(joinEdge.TargetNode.Key), SourceNodeDisplayName: joinEdge.TargetNode.DisplayName, TransitionID: group.TransitionID, TransitionDisplayName: group.DisplayName, WorkflowRevisionSeen: joinSnapshot.WorkflowRevisionSeen, Actor: "system", State: "applied", OutputValuesJson: aggregateJSON, CreatedAtUnixMs: now, AppliedAtUnixMs: now}); err != nil {
		return CompleteRunResult{}, err
	}
	result := CompleteRunResult{TransitionID: workflow.TransitionID(joinTransitionID), State: "applied"}
	outEdge := group.Edges[0]
	targetPlacementID := prefixedID("placement")
	if err := q.InsertTaskNodePlacement(ctx, sqlitegen.InsertTaskNodePlacementParams{ID: targetPlacementID, TaskID: taskID, NodeID: string(outEdge.TargetNode.ID), State: "active", CreatedAtUnixMs: now, UpdatedAtUnixMs: now}); err != nil {
		return CompleteRunResult{}, err
	}
	result.PlacementIDs = append(result.PlacementIDs, workflow.PlacementID(targetPlacementID))
	if err := insertTransitionEdgeSnapshot(ctx, q, joinTransitionID, outEdge, targetPlacementID, "applied", resolvedContextSourceRun{}); err != nil {
		return CompleteRunResult{}, err
	}
	if outEdge.TargetNode.Kind == workflow.NodeKindAgent {
		targetRunID := prefixedID("run")
		targetSnapshot, foundSnapshot, err := joinSnapshot.forNode(outEdge.TargetNode)
		if err != nil {
			return CompleteRunResult{}, err
		}
		if !foundSnapshot {
			return CompleteRunResult{}, fmt.Errorf("join target node %q missing from run snapshot", outEdge.TargetNode.ID)
		}
		targetSnapshotJSON, err := marshalJSON(targetSnapshot)
		if err != nil {
			return CompleteRunResult{}, err
		}
		source, err := s.resolveContextSourceRun(ctx, tx, taskID, now, nil, joinSnapshot, outEdge)
		if err != nil {
			return CompleteRunResult{}, err
		}
		targetMetadataJSON, err := targetRunMetadata(outEdge, source)
		if err != nil {
			return CompleteRunResult{}, err
		}
		if err := q.InsertTaskRun(ctx, sqlitegen.InsertTaskRunParams{ID: targetRunID, PlacementID: targetPlacementID, WorkflowRevisionSeen: targetSnapshot.WorkflowRevisionSeen, AutomationRequestedAtUnixMs: now, CreatedAtUnixMs: now, UpdatedAtUnixMs: now, InterruptionDetailJson: "{}", RunStartSnapshotJson: targetSnapshotJSON, MetadataJson: targetMetadataJSON}); err != nil {
			return CompleteRunResult{}, err
		}
		result.RunIDs = append(result.RunIDs, workflow.RunID(targetRunID))
	}
	return result, nil
}

func joinExpectedBranches(ctx context.Context, tx *sql.Tx, batchID string) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT target_placement_id
FROM task_transition_edges
WHERE task_transition_id = ? AND target_placement_id IS NOT NULL
ORDER BY rowid ASC`, batchID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	expected := map[string]bool{}
	for rows.Next() {
		var placementID sql.NullString
		if err := rows.Scan(&placementID); err != nil {
			return nil, err
		}
		if placementID.Valid && strings.TrimSpace(placementID.String) != "" {
			expected[placementID.String] = true
		}
	}
	return expected, rows.Err()
}

func joinArrivals(ctx context.Context, tx *sql.Tx, batchID string, joinNodeID workflow.NodeID) ([]joinArrival, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT
    p.id,
    p.parallel_branch_edge_id,
    tr.source_node_key,
    tr.output_values_json
FROM task_node_placements p
JOIN task_transitions tr ON tr.source_placement_id = p.id
JOIN task_transition_edges te ON te.task_transition_id = tr.id
WHERE p.parallel_batch_transition_id = ?
  AND p.state = 'completed'
  AND te.target_node_id = ?
  AND te.state = 'applied'
ORDER BY p.parallel_branch_edge_id ASC, tr.created_at_unix_ms ASC`, batchID, string(joinNodeID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	arrivals := []joinArrival{}
	seenPlacements := map[string]bool{}
	for rows.Next() {
		var placementID string
		var branchEdgeID sql.NullString
		var sourceNodeKey string
		var outputValuesJSON string
		if err := rows.Scan(&placementID, &branchEdgeID, &sourceNodeKey, &outputValuesJSON); err != nil {
			return nil, err
		}
		if strings.TrimSpace(placementID) == "" || seenPlacements[placementID] {
			continue
		}
		seenPlacements[placementID] = true
		key := strings.TrimSpace(branchEdgeID.String)
		if key == "" {
			continue
		}
		outputs := map[string]string{}
		if err := unmarshalJSON(outputValuesJSON, &outputs); err != nil {
			return nil, err
		}
		arrivals = append(arrivals, joinArrival{PlacementID: placementID, BranchEdgeID: key, SourceNodeKey: sourceNodeKey, OutputValues: outputs})
	}
	return arrivals, rows.Err()
}

func deterministicJoinAggregate(arrivals []joinArrival) string {
	sort.SliceStable(arrivals, func(i, j int) bool {
		if arrivals[i].SourceNodeKey == arrivals[j].SourceNodeKey {
			return arrivals[i].BranchEdgeID < arrivals[j].BranchEdgeID
		}
		return arrivals[i].SourceNodeKey < arrivals[j].SourceNodeKey
	})
	lines := make([]string, 0, len(arrivals))
	for _, arrival := range arrivals {
		lines = append(lines, fmt.Sprintf("%s (%s): %s", arrival.SourceNodeKey, arrival.BranchEdgeID, mustJSON(arrival.OutputValues)))
	}
	return strings.Join(lines, "\n")
}
