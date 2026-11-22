package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"core/server/metadata"
	"core/server/metadata/sqlitegen"
	"core/server/workflow"
	"core/shared/runtimeids"
)

type PendingApprovalApplyResult struct {
	Mutation         workflow.CurrentNodeMutationResult
	ResolvedApproval workflow.PendingApproval
	Handoff          CompletionHandoff
	AutomaticIntents []workflow.CurrentNodeReference
	TaskAttentionResolution
}

type pendingApprovalFacts struct {
	ID              workflow.ApprovalID
	Source          workflow.CurrentNodeReference
	SourceSessionID *runtimeids.SessionID
	TransitionID    workflow.TransitionGroupID
	Commentary      string
	OutputValues    map[string]string
	Branches        []pendingApprovalBranchFacts
	CreatedAt       time.Time
}

type pendingApprovalBranchFacts struct {
	TransitionBranchKey workflow.TransitionBranchKey
	EdgeID              workflow.EdgeID
}

func (r PendingApprovalApplyResult) Committed() bool {
	return len(r.Mutation.Removed) != 0
}

func (s *Store) IsCurrentNodeExecutionEligible(ctx context.Context, reference workflow.CurrentNodeReference) (bool, error) {
	if _, err := s.currentNodeForReference(ctx, s.queries, reference); err != nil {
		return false, err
	}
	pending, err := s.CurrentNodeAwaitingApproval(ctx, reference)
	return !pending, err
}

func pendingApprovalByID(ctx context.Context, q *sqlitegen.Queries, id workflow.ApprovalID) (pendingApprovalFacts, error) {
	row, err := q.GetTaskPendingApproval(ctx, id.String())
	if err != nil {
		return pendingApprovalFacts{}, err
	}
	return pendingApprovalFromRow(ctx, q, row)
}

func pendingApprovalHandoff(approval workflow.PendingApproval) CompletionHandoff {
	group := approval.Transition.Group
	destination := workflow.DisplayLabel(group.DisplayName, string(group.TransitionID))
	if len(approval.Branches) == 1 {
		destination = approval.Branches[0].Target.DisplayName
	}
	return CompletionHandoff{SourceNodeDisplayName: approval.Transition.SourceDisplayName, DestinationDisplayName: destination}
}

func validatePendingApprovalSequentialTarget(source, target workflow.CurrentNodeReference) error {
	sourceKey, sourceScoped := source.TransitionBranchKey()
	targetKey, targetScoped := target.TransitionBranchKey()
	if sourceScoped != targetScoped || sourceScoped && sourceKey != targetKey {
		return errors.New("pending approval target scope must match its source")
	}
	return nil
}

func validatePendingApprovalFanoutTargets(source workflow.CurrentNodeReference, branches []workflow.PendingApprovalBranch) error {
	if source.IsBranchScoped() || len(branches) < 2 {
		return errors.New("fanout approval requires a serial source and multiple branches")
	}
	for _, branch := range branches {
		key, scoped := branch.Target.Reference.TransitionBranchKey()
		if !scoped || key != branch.TransitionBranchKey || branch.Target.Reference.TaskID != source.TaskID {
			return errors.New("pending approval target scope does not match its branch")
		}
	}
	return nil
}

func newPendingApproval(
	source workflow.CurrentNode,
	group workflow.TransitionGroup,
	sourceDisplayName string,
	edge workflow.Edge,
	target workflow.Node,
	targetCurrentNode workflow.CurrentNode,
	commentary string,
	outputValues map[string]string,
	createdAt time.Time,
) (workflow.PendingApproval, error) {
	return newPendingApprovalWithBranches(source, group, sourceDisplayName, commentary, outputValues,
		[]workflow.PendingApprovalBranch{{
			TransitionBranchKey: workflow.TransitionBranchKey(edge.Key),
			EdgeID:              edge.ID,
			Target: &workflow.PendingApprovalTarget{
				Reference: targetCurrentNode.Reference, DisplayName: workflow.NodeDisplayLabel(target), NodeKind: target.Kind(),
			},
		}}, createdAt)
}

func newPendingApprovalWithBranches(
	source workflow.CurrentNode,
	group workflow.TransitionGroup,
	sourceDisplayName, commentary string,
	outputValues map[string]string,
	branches []workflow.PendingApprovalBranch,
	createdAt time.Time,
) (workflow.PendingApproval, error) {
	if err := source.Reference.Validate(); err != nil {
		return workflow.PendingApproval{}, err
	}
	if group.ID == "" || len(branches) == 0 || createdAt.IsZero() {
		return workflow.PendingApproval{}, errors.New("pending approval choice and creation time are required")
	}
	seen := make(map[workflow.EdgeID]bool, len(branches))
	for _, branch := range branches {
		if branch.EdgeID == "" || seen[branch.EdgeID] {
			return workflow.PendingApproval{}, errors.New("pending approval branch choice must be present and unique")
		}
		seen[branch.EdgeID] = true
		if err := branch.Target.Reference.Validate(); err != nil {
			return workflow.PendingApproval{}, err
		}
	}
	return workflow.PendingApproval{
		ID: workflow.NewApprovalID(), Source: source.Reference, SourceSessionID: source.SessionID,
		Transition: workflow.PendingApprovalTransition{ID: group.ID, Group: &group, SourceDisplayName: sourceDisplayName},
		Commentary: commentary, OutputValues: cloneCurrentNodeOutputValues(outputValues),
		Branches:  append([]workflow.PendingApprovalBranch(nil), branches...),
		CreatedAt: createdAt.UTC().Truncate(time.Millisecond),
	}, nil
}

func insertPendingApproval(ctx context.Context, q *sqlitegen.Queries, approval workflow.PendingApproval) error {
	sourceID, err := runtimeids.GraphEntityIDBlob(string(approval.Source.NodeID))
	if err != nil {
		return err
	}
	groupID, err := runtimeids.GraphEntityIDBlob(string(approval.Transition.ID))
	if err != nil {
		return err
	}
	values, err := workflow.MarshalString(approval.OutputValues)
	if err != nil {
		return err
	}
	branchKey := sql.NullString{}
	if key, scoped := approval.Source.TransitionBranchKey(); scoped {
		branchKey = sql.NullString{String: string(key), Valid: true}
	}
	sessionID := sql.NullString{}
	if approval.SourceSessionID != nil {
		sessionID = sql.NullString{String: approval.SourceSessionID.String(), Valid: true}
	}
	if err := q.InsertTaskPendingApproval(ctx, sqlitegen.InsertTaskPendingApprovalParams{
		ID: approval.ID.String(), SourceTaskID: string(approval.Source.TaskID), SourceNodeID: sourceID,
		SourceTransitionBranchKey: branchKey, SourceSessionID: sessionID,
		TransitionGroupID: groupID, Commentary: sql.NullString{String: approval.Commentary, Valid: true},
		MaterializedValuesJson: values, CreatedAtUnixMs: approval.CreatedAt.UnixMilli(),
	}); err != nil {
		return err
	}
	for _, branch := range approval.Branches {
		edgeID, err := runtimeids.GraphEntityIDBlob(string(branch.EdgeID))
		if err != nil {
			return err
		}
		if err := q.InsertTaskPendingApprovalBranch(ctx, sqlitegen.InsertTaskPendingApprovalBranchParams{
			ApprovalID: approval.ID.String(), TransitionBranchKey: string(branch.TransitionBranchKey), EdgeID: edgeID,
		}); err != nil {
			return err
		}
	}
	return nil
}

func pendingApprovalFromRow(ctx context.Context, q *sqlitegen.Queries, row sqlitegen.TaskPendingApproval) (facts pendingApprovalFacts, err error) {
	facts, err = pendingApprovalHeaderFromRow(row)
	if err != nil {
		return facts, err
	}
	rows, err := q.ListTaskPendingApprovalBranches(ctx, facts.ID.String())
	if err != nil {
		return facts, err
	}
	for _, row := range rows {
		branch, err := pendingApprovalBranchFromRow(row.TransitionBranchKey, row.EdgeID)
		if err != nil {
			return facts, err
		}
		facts.Branches = append(facts.Branches, branch)
	}
	if len(facts.Branches) == 0 {
		return facts, errors.New("pending approval has no branch choices")
	}
	return facts, nil
}

func pendingApprovalSourceFromRow(taskID string, node []byte, sourceBranchKey, sourceSessionID sql.NullString) (workflow.CurrentNodeReference, *runtimeids.SessionID, error) {
	nodeID, err := runtimeids.GraphEntityIDText(node)
	if err != nil {
		return workflow.CurrentNodeReference{}, nil, err
	}
	var branchKey *workflow.TransitionBranchKey
	if sourceBranchKey.Valid {
		branchKey = new(workflow.TransitionBranchKey(sourceBranchKey.String))
	}
	source, err := workflow.NewCurrentNodeReference(workflow.TaskID(taskID), workflow.NodeID(nodeID), branchKey)
	if err != nil {
		return workflow.CurrentNodeReference{}, nil, err
	}
	var sessionID *runtimeids.SessionID
	if sourceSessionID.Valid {
		parsed, err := runtimeids.ParseSessionID(sourceSessionID.String)
		if err != nil {
			return workflow.CurrentNodeReference{}, nil, err
		}
		sessionID = &parsed
	}
	return source, sessionID, nil
}

func pendingApprovalHeaderFromRow(row sqlitegen.TaskPendingApproval) (facts pendingApprovalFacts, err error) {
	id, err := workflow.ParseApprovalID(row.ID)
	if err != nil {
		return facts, err
	}
	source, sessionID, err := pendingApprovalSourceFromRow(row.SourceTaskID, row.SourceNodeID, row.SourceTransitionBranchKey, row.SourceSessionID)
	if err != nil {
		return facts, err
	}
	groupID, err := runtimeids.GraphEntityIDText(row.TransitionGroupID)
	if err != nil {
		return facts, err
	}
	values := map[string]string{}
	if err := workflow.UnmarshalString(row.MaterializedValuesJson, &values); err != nil {
		return facts, err
	}
	if values == nil {
		return facts, errors.New("pending approval submitted values must be an object")
	}
	commentary := ""
	if value := metadata.OptionalString(row.Commentary); value != nil {
		commentary = *value
	}
	return pendingApprovalFacts{
		ID: id, Source: source, SourceSessionID: sessionID,
		TransitionID: workflow.TransitionGroupID(groupID),
		Commentary:   commentary, OutputValues: values, CreatedAt: time.UnixMilli(row.CreatedAtUnixMs).UTC(),
	}, nil
}

func pendingApprovalBranchFromRow(branchKey string, edge []byte) (pendingApprovalBranchFacts, error) {
	edgeID, err := runtimeids.GraphEntityIDText(edge)
	return pendingApprovalBranchFacts{TransitionBranchKey: workflow.TransitionBranchKey(branchKey), EdgeID: workflow.EdgeID(edgeID)}, err
}

func (facts pendingApprovalFacts) presentation(sourceDisplayName string, group *workflow.TransitionGroup) workflow.PendingApproval {
	return workflow.PendingApproval{
		ID: facts.ID, Source: facts.Source, SourceSessionID: facts.SourceSessionID,
		Transition: workflow.PendingApprovalTransition{ID: facts.TransitionID, Group: group, SourceDisplayName: sourceDisplayName},
		Commentary: facts.Commentary, OutputValues: facts.OutputValues, CreatedAt: facts.CreatedAt,
	}
}

func (branch pendingApprovalBranchFacts) choice() workflow.PendingApprovalBranch {
	return workflow.PendingApprovalBranch{EdgeID: branch.EdgeID, TransitionBranchKey: branch.TransitionBranchKey}
}

func (branch pendingApprovalBranchFacts) target(
	source workflow.CurrentNodeReference, branchCount int, targetNodeID workflow.NodeID, targetKind workflow.NodeKind, targetLabel string,
) (*workflow.PendingApprovalTarget, error) {
	targetKey := currentNodeReferenceBranchKey(source)
	if branchCount > 1 {
		targetKey = &branch.TransitionBranchKey
	}
	reference, err := workflow.NewCurrentNodeReference(source.TaskID, targetNodeID, targetKey)
	if err != nil {
		return nil, err
	}
	return &workflow.PendingApprovalTarget{Reference: reference, DisplayName: targetLabel, NodeKind: targetKind}, nil
}

type pendingApprovalResolver struct {
	groups map[workflow.TransitionGroupID]workflow.TransitionGroup
	edges  map[workflow.EdgeID]workflow.Edge
	nodes  map[workflow.NodeID]workflow.Node
}

func newPendingApprovalResolver(definition workflow.Definition) pendingApprovalResolver {
	r := pendingApprovalResolver{
		groups: make(map[workflow.TransitionGroupID]workflow.TransitionGroup, len(definition.TransitionGroups)),
		edges:  make(map[workflow.EdgeID]workflow.Edge, len(definition.Edges)),
		nodes:  make(map[workflow.NodeID]workflow.Node, len(definition.Nodes)),
	}
	for _, group := range definition.TransitionGroups {
		r.groups[group.ID] = group
	}
	for _, edge := range definition.Edges {
		r.edges[edge.ID] = edge
	}
	for _, node := range definition.Nodes {
		r.nodes[workflow.NodeIDOf(node)] = node
	}
	return r
}

func (r pendingApprovalResolver) resolve(facts pendingApprovalFacts) (workflow.PendingApproval, []currentNodeCompletionTarget, error) {
	source, ok := r.nodes[facts.Source.NodeID]
	if !ok {
		return workflow.PendingApproval{}, nil, errors.New("pending approval source node is missing")
	}
	var currentGroup *workflow.TransitionGroup
	group, groupAvailable := r.groups[facts.TransitionID]
	groupAvailable = groupAvailable && group.SourceNodeID == facts.Source.NodeID
	if groupAvailable {
		currentGroup = &group
	}
	approval := facts.presentation(workflow.NodeDisplayLabel(source), currentGroup)
	approval.Branches = make([]workflow.PendingApprovalBranch, 0, len(facts.Branches))
	selected := make([]currentNodeCompletionTarget, 0, len(facts.Branches))
	for _, branch := range facts.Branches {
		choice := branch.choice()
		edge, edgeAvailable := r.edges[branch.EdgeID]
		if groupAvailable && edgeAvailable && edge.TransitionGroupID == group.ID {
			target, ok := r.nodes[edge.TargetNodeID]
			if !ok {
				return workflow.PendingApproval{}, nil, errors.New("pending approval target node is missing")
			}
			var err error
			choice.Target, err = branch.target(facts.Source, len(facts.Branches), edge.TargetNodeID, target.Kind(), workflow.NodeDisplayLabel(target))
			if err != nil {
				return workflow.PendingApproval{}, nil, err
			}
			selected = append(selected, currentNodeCompletionTarget{Edge: edge, Node: target})
		}
		approval.Branches = append(approval.Branches, choice)
	}
	return approval, selected, nil
}

func sameMaterializedContinuationSource(left, right workflow.MaterializedContinuationSource) bool {
	if left.Kind() != right.Kind() {
		return false
	}
	leftID, leftExact := left.ExactSessionID()
	rightID, rightExact := right.ExactSessionID()
	return leftExact == rightExact && (!leftExact || leftID == rightID)
}

func (s *Store) CurrentNodeAwaitingApproval(ctx context.Context, reference workflow.CurrentNodeReference) (bool, error) {
	return currentNodeHasPendingApproval(ctx, s.queries, reference)
}

func currentNodeHasPendingApproval(ctx context.Context, q *sqlitegen.Queries, reference workflow.CurrentNodeReference) (bool, error) {
	if err := reference.Validate(); err != nil {
		return false, err
	}
	nodeID, err := runtimeids.GraphEntityIDBlob(string(reference.NodeID))
	if err != nil {
		return false, err
	}
	key := sql.NullString{}
	if value, scoped := reference.TransitionBranchKey(); scoped {
		key = sql.NullString{String: string(value), Valid: true}
	}
	pending, err := q.HasTaskPendingApprovalForCurrentNode(ctx, sqlitegen.HasTaskPendingApprovalForCurrentNodeParams{
		TaskID: string(reference.TaskID), NodeID: nodeID, TransitionBranchKey: key,
	})
	return pending, err
}

func sameOptionalSessionID(left, right *runtimeids.SessionID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
