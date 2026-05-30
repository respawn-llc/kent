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
	def, workflowRecord, err := s.GetDefinition(ctx, workflow.WorkflowID(task.WorkflowID))
	if err != nil {
		return ManualMoveResult{}, err
	}
	derived := workflow.DeriveWiring(def)
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
	sourceRunID, sourceSessionID, err := s.latestRunForPlacement(ctx, sourcePlacement)
	if err != nil {
		return ManualMoveResult{}, err
	}
	reusedOutputValues := map[string]string(nil)
	if targetNode.Kind == workflow.NodeKindTerminal && sourceNode.Kind != workflow.NodeKindTerminal {
		group, edge, ok = terminalArchiveManualMoveContract(sourceNode, targetNode)
	} else if !ok {
		group, edge, reusedOutputValues, sourceRunID, sourceSessionID, ok, err = s.backwardManualMoveEdge(ctx, sourcePlacement, targetNode)
		if err != nil {
			return ManualMoveResult{}, err
		}
		if !ok && targetNode.Kind == workflow.NodeKindStart {
			group, edge, ok = startResetManualMoveContract(sourceNode, targetNode)
		}
		if !ok && req.AllowMissingEdge {
			group, edge, ok = missingEdgeManualMoveContract(sourceNode, targetNode)
		}
		if !ok {
			return ManualMoveResult{}, fmt.Errorf("no workflow edge from %s to %s", sourceNode.Key, targetNode.Key)
		}
	}
	outputValues := req.OutputValues
	if outputValues == nil {
		outputValues = map[string]string{}
	}
	if len(outputValues) == 0 && len(reusedOutputValues) > 0 {
		outputValues = reusedOutputValues
	}
	if workflow.CanonicalContextSource(edge.ContextSource).Kind == workflow.ContextSourceSelectedNode {
		return ManualMoveResult{}, errors.New("manual move with selected context source is not supported")
	}
	if edge.ContextMode == workflow.ContextModeContinueSession && strings.TrimSpace(sourceSessionID) == "" {
		return ManualMoveResult{}, errors.New("continue_session requires source session for manual move")
	}
	groupSnapshot := transitionContractSnapshot{
		ID:           group.ID,
		SourceNodeID: sourceNode.ID,
		TransitionID: string(group.TransitionID),
		DisplayName:  group.DisplayName,
		Edges:        []edgeContractSnapshot{manualMoveEdgeSnapshot(edge, targetNode, derived)},
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
	if transitionState == "pending_approval" && sourceRunID == "" && !req.AllowMissingEdge {
		return ManualMoveResult{}, errors.New("manual move requiring approval needs a source run")
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
	updatedResult, err := tx.ExecContext(ctx, `
UPDATE task_node_placements
SET state = 'completed', updated_at_unix_ms = ?
WHERE id = ? AND state = 'active'`, now, string(sourcePlacement))
	if err != nil {
		return ManualMoveResult{}, err
	}
	updated, err := updatedResult.RowsAffected()
	if err != nil {
		return ManualMoveResult{}, err
	}
	if updated != 1 {
		return ManualMoveResult{}, sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET updated_at_unix_ms = ? WHERE id = ?`, now, string(req.TaskID)); err != nil {
		return ManualMoveResult{}, fmt.Errorf("update task timestamp: %w", err)
	}
	appliedAt := now
	if transitionState == "pending_approval" {
		appliedAt = 0
	}
	if err := q.InsertTaskTransition(ctx, sqlitegen.InsertTaskTransitionParams{ID: transitionID, TaskID: string(req.TaskID), SourceRunID: sql.NullString{String: string(sourceRunID), Valid: sourceRunID != ""}, SourcePlacementID: sql.NullString{String: string(sourcePlacement), Valid: true}, SourceNodeKey: string(sourceNode.Key), SourceNodeDisplayName: sourceNode.DisplayName, TransitionID: string(group.TransitionID), TransitionDisplayName: group.DisplayName, WorkflowRevisionSeen: task.WorkflowRevisionSeen, Actor: actor, State: transitionState, Commentary: strings.TrimSpace(req.Commentary), OutputValuesJson: outputValuesJSON, CreatedAtUnixMs: now, AppliedAtUnixMs: appliedAt}); err != nil {
		return ManualMoveResult{}, err
	}
	result := ManualMoveResult{TransitionID: workflow.TransitionID(transitionID), State: transitionState, RequiresApproval: edge.RequiresApproval}
	targetPlacementID := ""
	if transitionState == "applied" {
		targetPlacementID = prefixedID("placement")
		if err := q.InsertTaskNodePlacement(ctx, sqlitegen.InsertTaskNodePlacementParams{ID: targetPlacementID, TaskID: string(req.TaskID), NodeID: string(targetNode.ID), State: "active", CreatedAtUnixMs: now, UpdatedAtUnixMs: now}); err != nil {
			return ManualMoveResult{}, err
		}
		result.PlacementIDs = append(result.PlacementIDs, workflow.PlacementID(targetPlacementID))
	}
	edgeMetadata := workflowRunMetadata{ContextSource: workflow.CanonicalContextSource(groupSnapshot.Edges[0].ContextSource)}
	if transitionState == "pending_approval" && targetNode.Kind == workflow.NodeKindAgent {
		targetSnapshot, err := newRunStartSnapshot(def, workflowRecord, targetNode.ID)
		if err != nil {
			return ManualMoveResult{}, err
		}
		nodeOutputValues, err := s.resolvePromptNodeOutputValues(ctx, tx, string(req.TaskID), now, targetSnapshot)
		if err != nil {
			return ManualMoveResult{}, err
		}
		edgeMetadata.NodeOutputValues = nodeOutputValues
		edgeMetadata.TargetRunStartSnapshot = &targetSnapshot
	}
	if err := insertTransitionEdgeSnapshotWithMetadata(ctx, q, transitionID, groupSnapshot.Edges[0], targetPlacementID, edgeState, edgeMetadata); err != nil {
		return ManualMoveResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return ManualMoveResult{}, err
	}
	return result, nil
}

func terminalArchiveManualMoveContract(sourceNode workflow.Node, targetNode workflow.Node) (workflow.TransitionGroup, workflow.Edge, bool) {
	group := workflow.TransitionGroup{
		ID:           "",
		SourceNodeID: sourceNode.ID,
		TransitionID: "manual_done",
		DisplayName:  "Move to Done",
	}
	edge := workflow.Edge{
		ID:                 "",
		Key:                "manual_done",
		TargetNodeID:       targetNode.ID,
		ContextMode:        workflow.ContextModeNewSession,
		ContextSource:      workflow.ContextSource{Kind: workflow.ContextSourceImmediateSource},
		RequiresApproval:   false,
		InputBindings:      nil,
		OutputRequirements: nil,
	}
	return group, edge, true
}

func startResetManualMoveContract(sourceNode workflow.Node, targetNode workflow.Node) (workflow.TransitionGroup, workflow.Edge, bool) {
	group := workflow.TransitionGroup{
		ID:           "",
		SourceNodeID: sourceNode.ID,
		TransitionID: "manual_start",
		DisplayName:  "Move to Backlog",
	}
	edge := workflow.Edge{
		ID:                 "",
		Key:                "manual_start",
		TargetNodeID:       targetNode.ID,
		ContextMode:        workflow.ContextModeNewSession,
		ContextSource:      workflow.ContextSource{Kind: workflow.ContextSourceImmediateSource},
		RequiresApproval:   false,
		InputBindings:      nil,
		OutputRequirements: nil,
	}
	return group, edge, true
}

func missingEdgeManualMoveContract(sourceNode workflow.Node, targetNode workflow.Node) (workflow.TransitionGroup, workflow.Edge, bool) {
	inputBindings := targetInputBindings(targetNode)
	group := workflow.TransitionGroup{
		ID:           "",
		SourceNodeID: sourceNode.ID,
		TransitionID: "manual_override",
		DisplayName:  "Manual Override",
	}
	edge := workflow.Edge{
		ID:                 "",
		Key:                "manual_override",
		TargetNodeID:       targetNode.ID,
		ContextMode:        workflow.ContextModeNewSession,
		ContextSource:      workflow.ContextSource{Kind: workflow.ContextSourceImmediateSource},
		RequiresApproval:   false,
		InputBindings:      inputBindings,
		OutputRequirements: outputRequirementsFromTransitionInputBindings(inputBindings),
	}
	return group, edge, true
}

func targetInputBindings(targetNode workflow.Node) []workflow.InputBinding {
	seen := map[string]bool{}
	bindings := []workflow.InputBinding{}
	for _, input := range targetNode.InputFields {
		name := strings.TrimSpace(input.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		bindings = append(bindings, workflow.InputBinding{Name: name, Source: workflow.BindingSourceTransitionOutput, Field: name})
	}
	return bindings
}

func manualMoveEdgeSnapshot(edge workflow.Edge, targetNode workflow.Node, derived workflow.DerivedWiring) edgeContractSnapshot {
	inputBindings := edge.InputBindings
	outputRequirements := edge.OutputRequirements
	if strings.TrimSpace(string(edge.ID)) != "" {
		inputBindings = edgeInputBindingsSnapshot(edge, derived)
		outputRequirements = edgeOutputRequirementsSnapshot(edge, targetNode, derived)
	}
	return edgeContractSnapshot{
		ID:                 edge.ID,
		Key:                edge.Key,
		TargetNode:         nodeSnapshotWithDerivedWiring(targetNode, derived),
		ContextMode:        edge.ContextMode,
		ContextSource:      workflow.CanonicalContextSource(edge.ContextSource),
		RequiresApproval:   edge.RequiresApproval,
		InputBindings:      inputBindings,
		OutputRequirements: outputRequirements,
	}
}

func outputRequirementsFromTransitionInputBindings(bindings []workflow.InputBinding) []workflow.OutputRequirement {
	seen := map[string]bool{}
	requirements := []workflow.OutputRequirement{}
	for _, binding := range bindings {
		field := strings.TrimSpace(binding.Field)
		if field == "" || seen[field] {
			continue
		}
		seen[field] = true
		requirements = append(requirements, workflow.OutputRequirement{FieldName: field})
	}
	return requirements
}

func (s *Store) latestRunForPlacement(ctx context.Context, placementID workflow.PlacementID) (workflow.RunID, string, error) {
	var runID string
	var sessionID sql.NullString
	err := s.db.QueryRowContext(ctx, workflowStoreQuery(latestRunForPlacementQuery), string(placementID)).Scan(&runID, &sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	return workflow.RunID(runID), strings.TrimSpace(sessionID.String), nil
}

func (s *Store) backwardManualMoveEdge(ctx context.Context, sourcePlacement workflow.PlacementID, targetNode workflow.Node) (workflow.TransitionGroup, workflow.Edge, map[string]string, workflow.RunID, string, bool, error) {
	var groupID sql.NullString
	var transitionID string
	var transitionDisplayName string
	var outputValuesJSON string
	var sourceRunID sql.NullString
	var workflowEdgeID sql.NullString
	var edgeKey string
	var contextMode string
	var requiresApproval int64
	var inputBindingsJSON string
	var outputRequirementsJSON string
	var metadataJSON string
	err := s.db.QueryRowContext(ctx, workflowStoreQuery(manualMovePreviousTransitionQuery), string(sourcePlacement), string(targetNode.ID)).Scan(&groupID, &transitionID, &transitionDisplayName, &outputValuesJSON, &sourceRunID, &workflowEdgeID, &edgeKey, &contextMode, &requiresApproval, &inputBindingsJSON, &outputRequirementsJSON, &metadataJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return workflow.TransitionGroup{}, workflow.Edge{}, nil, "", "", false, nil
	}
	if err != nil {
		return workflow.TransitionGroup{}, workflow.Edge{}, nil, "", "", false, err
	}
	outputValues := map[string]string{}
	if err := unmarshalJSON(outputValuesJSON, &outputValues); err != nil {
		return workflow.TransitionGroup{}, workflow.Edge{}, nil, "", "", false, err
	}
	inputs := []workflow.InputBinding{}
	if err := unmarshalJSON(inputBindingsJSON, &inputs); err != nil {
		return workflow.TransitionGroup{}, workflow.Edge{}, nil, "", "", false, err
	}
	requirements := []workflow.OutputRequirement{}
	if err := unmarshalJSON(outputRequirementsJSON, &requirements); err != nil {
		return workflow.TransitionGroup{}, workflow.Edge{}, nil, "", "", false, err
	}
	metadata := workflowRunMetadata{}
	if strings.TrimSpace(metadataJSON) != "" {
		if err := unmarshalJSON(metadataJSON, &metadata); err != nil {
			return workflow.TransitionGroup{}, workflow.Edge{}, nil, "", "", false, err
		}
	}
	sessionID := ""
	if sourceRunID.Valid && strings.TrimSpace(sourceRunID.String) != "" {
		sourceRun, err := s.queries.GetTaskRun(ctx, sourceRunID.String)
		if err != nil {
			return workflow.TransitionGroup{}, workflow.Edge{}, nil, "", "", false, err
		}
		sessionID = strings.TrimSpace(sourceRun.SessionID.String)
	}
	group := workflow.TransitionGroup{ID: workflow.TransitionGroupID(groupID.String), TransitionID: workflow.TransitionID(transitionID), DisplayName: transitionDisplayName}
	edge := workflow.Edge{ID: workflow.EdgeID(workflowEdgeID.String), Key: workflow.ModelKey(edgeKey), TargetNodeID: targetNode.ID, ContextMode: workflow.ContextMode(contextMode), ContextSource: workflow.CanonicalContextSource(metadata.ContextSource), RequiresApproval: requiresApproval != 0, InputBindings: inputs, OutputRequirements: requirements}
	return group, edge, outputValues, workflow.RunID(sourceRunID.String), sessionID, true, nil
}

func (s *Store) activeManualMoveSource(ctx context.Context, taskID workflow.TaskID) (workflow.PlacementID, workflow.NodeID, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, node_id, parallel_batch_transition_id
FROM task_node_placements
WHERE task_id = ? AND state = 'active'
ORDER BY created_at_unix_ms DESC, rowid DESC`, string(taskID))
	if err != nil {
		return "", "", err
	}
	defer func() { _ = rows.Close() }()
	var placements []struct {
		id      string
		nodeID  string
		batchID sql.NullString
	}
	for rows.Next() {
		var placement struct {
			id      string
			nodeID  string
			batchID sql.NullString
		}
		if err := rows.Scan(&placement.id, &placement.nodeID, &placement.batchID); err != nil {
			return "", "", err
		}
		if placement.batchID.Valid && strings.TrimSpace(placement.batchID.String) != "" {
			return "", "", errors.New("manual move during active parallel batch is not supported")
		}
		placements = append(placements, placement)
	}
	if err := rows.Err(); err != nil {
		return "", "", err
	}
	if len(placements) == 0 {
		return "", "", sql.ErrNoRows
	}
	if len(placements) != 1 {
		return "", "", errors.New("manual move with multiple active placements is not supported")
	}
	return workflow.PlacementID(placements[0].id), workflow.NodeID(placements[0].nodeID), nil
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
