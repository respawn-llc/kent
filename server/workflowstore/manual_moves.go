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

func (s *Store) ManualMoveTask(ctx context.Context, req ManualMoveRequest) (ManualMoveResult, error) {
	if strings.TrimSpace(string(req.TaskID)) == "" {
		return ManualMoveResult{}, errors.New("task id is required")
	}
	if strings.TrimSpace(string(req.TargetNodeID)) == "" {
		return ManualMoveResult{}, errors.New("target node id is required")
	}
	actor := strings.TrimSpace(req.Actor)
	if actor == "" {
		actor = "user"
	}
	task, err := s.queries.GetTask(ctx, string(req.TaskID))
	if err != nil {
		return ManualMoveResult{}, err
	}
	def, _, err := s.GetDefinition(ctx, workflow.WorkflowID(task.WorkflowID))
	if err != nil {
		return ManualMoveResult{}, err
	}
	sourcePlacement, sourceNodeID, err := s.activeManualMoveSource(ctx, req.TaskID)
	if err != nil {
		return ManualMoveResult{}, err
	}
	sourceNode, ok := definitionNode(def, sourceNodeID)
	if !ok {
		return ManualMoveResult{}, fmt.Errorf("source node %q missing", sourceNodeID)
	}
	targetNode, ok := definitionNode(def, req.TargetNodeID)
	if !ok {
		return ManualMoveResult{}, fmt.Errorf("target node %q missing", req.TargetNodeID)
	}
	group, edge, ok := definitionEdgeBetween(def, sourceNode.ID, targetNode.ID)
	if !ok {
		return ManualMoveResult{}, fmt.Errorf("no workflow edge from %s to %s", sourceNode.Key, targetNode.Key)
	}
	outputValues := req.OutputValues
	if outputValues == nil {
		outputValues = map[string]string{}
	}
	groupSnapshot := transitionContractSnapshot{
		ID:           group.ID,
		SourceNodeID: sourceNode.ID,
		TransitionID: string(group.TransitionID),
		DisplayName:  group.DisplayName,
		Edges: []edgeContractSnapshot{{
			ID:                 edge.ID,
			Key:                edge.Key,
			TargetNode:         nodeSnapshot(targetNode),
			ContextMode:        edge.ContextMode,
			RequiresApproval:   edge.RequiresApproval,
			InputBindings:      edge.InputBindings,
			OutputRequirements: edge.OutputRequirements,
		}},
	}
	if issues := requiredOutputIssues(groupSnapshot, outputValues); len(issues) > 0 {
		return ManualMoveResult{}, CompletionValidationError{Issues: issues}
	}
	transitionState := "applied"
	edgeState := "applied"
	if targetNode.Kind == workflow.NodeKindAgent || edge.RequiresApproval {
		transitionState = "pending_approval"
		edgeState = "pending"
	}
	outputValuesJSON, err := marshalJSON(outputValues)
	if err != nil {
		return ManualMoveResult{}, err
	}
	now := s.now().UnixMilli()
	transitionID := prefixedID("transition")
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ManualMoveResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	if updated, err := q.UpdateTaskNodePlacementState(ctx, sqlitegen.UpdateTaskNodePlacementStateParams{ID: string(sourcePlacement), State: "completed", UpdatedAtUnixMs: now}); err != nil {
		return ManualMoveResult{}, err
	} else if updated != 1 {
		return ManualMoveResult{}, sql.ErrNoRows
	}
	appliedAt := now
	if transitionState == "pending_approval" {
		appliedAt = 0
	}
	if err := q.InsertTaskTransition(ctx, sqlitegen.InsertTaskTransitionParams{ID: transitionID, TaskID: string(req.TaskID), SourcePlacementID: sql.NullString{String: string(sourcePlacement), Valid: true}, SourceNodeID: sql.NullString{String: string(sourceNode.ID), Valid: true}, SourceNodeKey: string(sourceNode.Key), SourceNodeDisplayName: sourceNode.DisplayName, TransitionGroupID: sql.NullString{String: string(group.ID), Valid: true}, TransitionID: string(group.TransitionID), TransitionDisplayName: group.DisplayName, WorkflowRevisionSeen: task.WorkflowRevisionSeen, Actor: actor, State: transitionState, Commentary: strings.TrimSpace(req.Commentary), OutputValuesJson: outputValuesJSON, CreatedAtUnixMs: now, AppliedAtUnixMs: appliedAt}); err != nil {
		return ManualMoveResult{}, err
	}
	result := ManualMoveResult{TransitionID: workflow.TransitionID(transitionID), State: transitionState}
	targetPlacementID := ""
	if transitionState == "applied" {
		targetPlacementID = prefixedID("placement")
		if err := q.InsertTaskNodePlacement(ctx, sqlitegen.InsertTaskNodePlacementParams{ID: targetPlacementID, TaskID: string(req.TaskID), NodeID: string(targetNode.ID), State: "active", CreatedByTransitionID: sql.NullString{String: transitionID, Valid: true}, CreatedAtUnixMs: now, UpdatedAtUnixMs: now}); err != nil {
			return ManualMoveResult{}, err
		}
		result.PlacementIDs = append(result.PlacementIDs, workflow.PlacementID(targetPlacementID))
	}
	if err := insertTransitionEdgeSnapshot(ctx, q, transitionID, task.WorkflowRevisionSeen, groupSnapshot.Edges[0], targetPlacementID, edgeState); err != nil {
		return ManualMoveResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return ManualMoveResult{}, err
	}
	return result, nil
}

func (s *Store) activeManualMoveSource(ctx context.Context, taskID workflow.TaskID) (workflow.PlacementID, workflow.NodeID, error) {
	var placementID string
	var nodeID string
	err := s.db.QueryRowContext(ctx, `
SELECT id, node_id
FROM task_node_placements
WHERE task_id = ? AND state = 'active'
ORDER BY created_at_unix_ms DESC, rowid DESC
LIMIT 1`, string(taskID)).Scan(&placementID, &nodeID)
	if err != nil {
		return "", "", err
	}
	return workflow.PlacementID(placementID), workflow.NodeID(nodeID), nil
}

func definitionNode(def workflow.Definition, nodeID workflow.NodeID) (workflow.Node, bool) {
	for _, node := range def.Nodes {
		if node.ID == nodeID {
			return node, true
		}
	}
	return workflow.Node{}, false
}

func definitionEdgeBetween(def workflow.Definition, sourceNodeID workflow.NodeID, targetNodeID workflow.NodeID) (workflow.TransitionGroup, workflow.Edge, bool) {
	groups := map[workflow.TransitionGroupID]workflow.TransitionGroup{}
	for _, group := range def.TransitionGroups {
		if group.SourceNodeID == sourceNodeID {
			groups[group.ID] = group
		}
	}
	for _, edge := range def.Edges {
		if edge.TargetNodeID != targetNodeID {
			continue
		}
		group, ok := groups[edge.TransitionGroupID]
		if ok {
			return group, edge, true
		}
	}
	return workflow.TransitionGroup{}, workflow.Edge{}, false
}
