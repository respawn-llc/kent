package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"strings"
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

func (s *Store) ListPendingApprovals(ctx context.Context, taskID workflow.TaskID) ([]workflow.PendingApproval, error) {
	if strings.TrimSpace(string(taskID)) == "" {
		return nil, errors.New("task id is required")
	}
	rows, err := s.queries.ListTaskPendingApprovals(ctx, string(taskID))
	if err != nil {
		return nil, err
	}
	approvals := make([]workflow.PendingApproval, 0, len(rows))
	if len(rows) == 0 {
		return approvals, nil
	}
	task, err := s.queries.GetTask(ctx, string(taskID))
	if err != nil {
		return nil, err
	}
	definition, _, err := workflowDefinitionFromQueries(ctx, s.queries, task.WorkflowID)
	if err != nil {
		return nil, err
	}
	resolver := newPendingApprovalResolver(definition)
	for _, row := range rows {
		facts, err := pendingApprovalFromRow(ctx, s.queries, row)
		if err != nil {
			return nil, err
		}
		approval, _, err := resolver.resolve(facts)
		if err != nil {
			return nil, err
		}
		approvals = append(approvals, approval)
	}
	return approvals, nil
}

func (s *Store) PendingApproval(ctx context.Context, approvalID workflow.ApprovalID) (workflow.PendingApproval, error) {
	facts, err := pendingApprovalByID(ctx, s.queries, approvalID)
	if err != nil {
		return workflow.PendingApproval{}, err
	}
	task, err := s.queries.GetTask(ctx, string(facts.Source.TaskID))
	if err != nil {
		return workflow.PendingApproval{}, err
	}
	definition, _, err := workflowDefinitionFromQueries(ctx, s.queries, task.WorkflowID)
	if err != nil {
		return workflow.PendingApproval{}, err
	}
	approval, _, err := newPendingApprovalResolver(definition).resolve(facts)
	return approval, err
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
	id, err := workflow.ParseApprovalID(row.ID)
	if err != nil {
		return facts, err
	}
	nodeID, err := runtimeids.GraphEntityIDText(row.SourceNodeID)
	if err != nil {
		return facts, err
	}
	var branchKey *workflow.TransitionBranchKey
	if row.SourceTransitionBranchKey.Valid {
		branchKey = new(workflow.TransitionBranchKey(row.SourceTransitionBranchKey.String))
	}
	source, err := workflow.NewCurrentNodeReference(workflow.TaskID(row.SourceTaskID), workflow.NodeID(nodeID), branchKey)
	if err != nil {
		return facts, err
	}
	var sessionID *runtimeids.SessionID
	if row.SourceSessionID.Valid {
		parsed, err := runtimeids.ParseSessionID(row.SourceSessionID.String)
		if err != nil {
			return facts, err
		}
		sessionID = &parsed
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
	rows, err := q.ListTaskPendingApprovalBranches(ctx, id.String())
	if err != nil {
		return facts, err
	}
	branches := make([]pendingApprovalBranchFacts, 0, len(rows))
	for _, branch := range rows {
		edgeID, err := runtimeids.GraphEntityIDText(branch.EdgeID)
		if err != nil {
			return facts, err
		}
		branches = append(branches, pendingApprovalBranchFacts{TransitionBranchKey: workflow.TransitionBranchKey(branch.TransitionBranchKey), EdgeID: workflow.EdgeID(edgeID)})
	}
	if len(branches) == 0 {
		return facts, errors.New("pending approval has no branch choices")
	}
	commentary := ""
	if value := metadata.OptionalString(row.Commentary); value != nil {
		commentary = *value
	}
	return pendingApprovalFacts{
		ID: id, Source: source, SourceSessionID: sessionID,
		TransitionID: workflow.TransitionGroupID(groupID),
		Commentary:   commentary, OutputValues: values, Branches: branches, CreatedAt: time.UnixMilli(row.CreatedAtUnixMs).UTC(),
	}, nil
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
	approval := workflow.PendingApproval{
		ID: facts.ID, Source: facts.Source, SourceSessionID: facts.SourceSessionID,
		Transition: workflow.PendingApprovalTransition{ID: facts.TransitionID, SourceDisplayName: workflow.NodeDisplayLabel(source)},
		Commentary: facts.Commentary, OutputValues: facts.OutputValues, CreatedAt: facts.CreatedAt,
		Branches: make([]workflow.PendingApprovalBranch, 0, len(facts.Branches)),
	}
	group, groupAvailable := r.groups[facts.TransitionID]
	if groupAvailable {
		approval.Transition.Group = &group
	}
	selected := make([]currentNodeCompletionTarget, 0, len(facts.Branches))
	for _, branch := range facts.Branches {
		choice := workflow.PendingApprovalBranch{EdgeID: branch.EdgeID, TransitionBranchKey: branch.TransitionBranchKey}
		edge, edgeAvailable := r.edges[branch.EdgeID]
		if groupAvailable && edgeAvailable && edge.TransitionGroupID == group.ID {
			target, ok := r.nodes[edge.TargetNodeID]
			if !ok {
				return workflow.PendingApproval{}, nil, errors.New("pending approval target node is missing")
			}
			targetKey := currentNodeReferenceBranchKey(facts.Source)
			if len(facts.Branches) > 1 {
				targetKey = &branch.TransitionBranchKey
			}
			reference, err := workflow.NewCurrentNodeReference(facts.Source.TaskID, edge.TargetNodeID, targetKey)
			if err != nil {
				return workflow.PendingApproval{}, nil, err
			}
			choice.Target = &workflow.PendingApprovalTarget{Reference: reference, DisplayName: workflow.NodeDisplayLabel(target), NodeKind: target.Kind()}
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
