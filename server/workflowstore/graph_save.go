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

type WorkflowGraphSaveRequest struct {
	WorkflowID            workflow.WorkflowID
	ExpectedGraphRevision int64
	Confirmed             bool
	NodeGroups            []NodeGroupRecord
	Nodes                 []NodeRecord
	TransitionGroups      []TransitionGroupRecord
	Edges                 []EdgeRecord
}

type WorkflowGraphSaveImpact struct {
	RemovedNodeCount            int64
	RemovedTransitionGroupCount int64
	RemovedEdgeCount            int64
	NodeTaskReferenceCount      int64
	EdgeTaskReferenceCount      int64
}

type WorkflowGraphSaveBlocker struct {
	Code    string
	Message string
	Count   int64
}

type WorkflowGraphSaveResult struct {
	Saved            bool
	GraphRevision    int64
	Impact           WorkflowGraphSaveImpact
	Blockers         []WorkflowGraphSaveBlocker
	ValidationErrors []workflow.ValidationError
}

func (s *Store) SaveWorkflowGraph(ctx context.Context, req WorkflowGraphSaveRequest) (WorkflowGraphSaveResult, error) {
	workflowID := workflow.WorkflowID(strings.TrimSpace(string(req.WorkflowID)))
	if workflowID == "" {
		return WorkflowGraphSaveResult{}, errors.New("workflow id is required")
	}
	if req.ExpectedGraphRevision < 0 {
		return WorkflowGraphSaveResult{}, errors.New("expected graph revision must be non-negative")
	}
	current, err := s.queries.GetWorkflow(ctx, string(workflowID))
	if err != nil {
		return WorkflowGraphSaveResult{}, err
	}
	prepared, def, err := prepareWorkflowGraphSave(workflowID, current.Name, req)
	if err != nil {
		return WorkflowGraphSaveResult{}, err
	}
	validation := workflow.ValidateDefinition(def, workflow.ValidationOptions{Context: workflow.ValidationContextDraft, RoleResolver: s.roleResolver})
	validationErrors := validation.BlockingErrors()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return WorkflowGraphSaveResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
		return WorkflowGraphSaveResult{}, err
	}
	q := s.queries.WithTx(tx)
	row, err := q.GetWorkflow(ctx, string(workflowID))
	if err != nil {
		return WorkflowGraphSaveResult{}, err
	}
	if row.GraphRevision != req.ExpectedGraphRevision {
		return WorkflowGraphSaveResult{
			GraphRevision: row.GraphRevision,
			Blockers:      []WorkflowGraphSaveBlocker{{Code: "graph_revision_changed", Message: "Workflow graph changed. Refresh before saving.", Count: row.GraphRevision}},
		}, nil
	}
	impact, removed, err := workflowGraphSaveImpact(ctx, q, workflowID, prepared)
	if err != nil {
		return WorkflowGraphSaveResult{}, err
	}
	blockers := workflowGraphSaveBlockers(req, impact)
	if len(validationErrors) > 0 {
		blockers = append(blockers, WorkflowGraphSaveBlocker{Code: "validation_failed", Message: "Workflow graph has blocking validation errors.", Count: int64(len(validationErrors))})
	}
	if len(blockers) > 0 {
		return WorkflowGraphSaveResult{GraphRevision: row.GraphRevision, Impact: impact, Blockers: blockers, ValidationErrors: validationErrors}, nil
	}
	if err := applyWorkflowGraphSave(ctx, tx, q, workflowID, prepared, removed); err != nil {
		return WorkflowGraphSaveResult{}, err
	}
	revision, err := q.IncrementWorkflowGraphRevision(ctx, sqlitegen.IncrementWorkflowGraphRevisionParams{ID: string(workflowID), UpdatedAtUnixMs: s.now().UnixMilli()})
	if err != nil {
		return WorkflowGraphSaveResult{}, fmt.Errorf("increment graph revision: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return WorkflowGraphSaveResult{}, err
	}
	return WorkflowGraphSaveResult{Saved: true, GraphRevision: revision, Impact: impact}, nil
}

type preparedWorkflowGraphSave struct {
	nodeGroups       []NodeGroupRecord
	nodes            []NodeRecord
	transitionGroups []TransitionGroupRecord
	edges            []EdgeRecord
}

type removedWorkflowGraphRows struct {
	nodeGroups       []string
	nodes            []workflow.NodeID
	transitionGroups []workflow.TransitionGroupID
	edges            []workflow.EdgeID
}

func prepareWorkflowGraphSave(workflowID workflow.WorkflowID, displayName string, req WorkflowGraphSaveRequest) (preparedWorkflowGraphSave, workflow.Definition, error) {
	prepared := preparedWorkflowGraphSave{
		nodeGroups:       append([]NodeGroupRecord(nil), req.NodeGroups...),
		nodes:            append([]NodeRecord(nil), req.Nodes...),
		transitionGroups: append([]TransitionGroupRecord(nil), req.TransitionGroups...),
		edges:            append([]EdgeRecord(nil), req.Edges...),
	}
	groupsByKey := map[workflow.ModelKey]string{}
	groupsByID := map[string]bool{}
	for i, group := range prepared.nodeGroups {
		group.WorkflowID = defaultWorkflowID(group.WorkflowID, workflowID)
		group.ID = strings.TrimSpace(group.ID)
		if group.ID == "" {
			return preparedWorkflowGraphSave{}, workflow.Definition{}, errors.New("workflow node group id is required")
		}
		group.Key = workflow.ModelKey(strings.TrimSpace(string(group.Key)))
		if group.Key == "" {
			return preparedWorkflowGraphSave{}, workflow.Definition{}, errors.New("workflow node group key is required")
		}
		if group.WorkflowID != workflowID {
			return preparedWorkflowGraphSave{}, workflow.Definition{}, fmt.Errorf("workflow node group %q belongs to workflow %q", group.ID, group.WorkflowID)
		}
		group.DisplayName = strings.TrimSpace(group.DisplayName)
		if group.SortOrder == 0 {
			group.SortOrder = int64(i * 100)
		}
		if groupsByID[group.ID] {
			return preparedWorkflowGraphSave{}, workflow.Definition{}, fmt.Errorf("duplicate workflow node group id %q", group.ID)
		}
		if existingID, exists := groupsByKey[group.Key]; exists {
			return preparedWorkflowGraphSave{}, workflow.Definition{}, fmt.Errorf("duplicate workflow node group key %q between %q and %q", group.Key, existingID, group.ID)
		}
		groupsByID[group.ID] = true
		groupsByKey[group.Key] = group.ID
		prepared.nodeGroups[i] = group
	}

	def := workflow.Definition{ID: workflowID, DisplayName: displayName}
	for i, node := range prepared.nodes {
		node.WorkflowID = defaultWorkflowID(node.WorkflowID, workflowID)
		node.GroupID = strings.TrimSpace(node.GroupID)
		node.GroupKey = strings.TrimSpace(node.GroupKey)
		if node.GroupID == "" && node.GroupKey != "" {
			groupID, ok := groupsByKey[workflow.ModelKey(node.GroupKey)]
			if !ok {
				return preparedWorkflowGraphSave{}, workflow.Definition{}, fmt.Errorf("workflow node group key %q is not in the saved graph", node.GroupKey)
			}
			node.GroupID = groupID
		}
		if node.GroupID != "" && !groupsByID[node.GroupID] {
			return preparedWorkflowGraphSave{}, workflow.Definition{}, fmt.Errorf("workflow node group %q is not in the saved graph", node.GroupID)
		}
		prepared.nodes[i] = node
		def.Nodes = append(def.Nodes, workflow.Node{WorkflowID: node.WorkflowID, ID: node.ID, Key: node.Key, Kind: node.Kind, DisplayName: node.DisplayName, SubagentRole: node.SubagentRole, PromptTemplate: node.PromptTemplate, OutputFields: node.OutputFields})
	}
	for i, group := range prepared.transitionGroups {
		group.WorkflowID = defaultWorkflowID(group.WorkflowID, workflowID)
		prepared.transitionGroups[i] = group
		def.TransitionGroups = append(def.TransitionGroups, workflow.TransitionGroup{WorkflowID: group.WorkflowID, ID: group.ID, SourceNodeID: group.SourceNodeID, TransitionID: group.TransitionID, DisplayName: group.DisplayName})
	}
	for i, edge := range prepared.edges {
		edge.WorkflowID = defaultWorkflowID(edge.WorkflowID, workflowID)
		edge.ContextSource = workflow.CanonicalContextSource(edge.ContextSource)
		prepared.edges[i] = edge
		def.Edges = append(def.Edges, workflow.Edge{WorkflowID: edge.WorkflowID, ID: edge.ID, Key: edge.Key, TransitionGroupID: edge.TransitionGroupID, TargetNodeID: edge.TargetNodeID, ContextMode: edge.ContextMode, ContextSource: edge.ContextSource, RequiresApproval: edge.RequiresApproval, InputBindings: edge.InputBindings, OutputRequirements: edge.OutputRequirements})
	}
	return prepared, def, nil
}

func defaultWorkflowID(actual workflow.WorkflowID, fallback workflow.WorkflowID) workflow.WorkflowID {
	if strings.TrimSpace(string(actual)) == "" {
		return fallback
	}
	return actual
}

func workflowGraphSaveImpact(ctx context.Context, q *sqlitegen.Queries, workflowID workflow.WorkflowID, prepared preparedWorkflowGraphSave) (WorkflowGraphSaveImpact, removedWorkflowGraphRows, error) {
	currentGroups, err := q.ListWorkflowNodeGroups(ctx, string(workflowID))
	if err != nil {
		return WorkflowGraphSaveImpact{}, removedWorkflowGraphRows{}, err
	}
	currentNodes, err := q.ListWorkflowNodes(ctx, string(workflowID))
	if err != nil {
		return WorkflowGraphSaveImpact{}, removedWorkflowGraphRows{}, err
	}
	currentTransitionGroups, err := q.ListWorkflowTransitionGroups(ctx, string(workflowID))
	if err != nil {
		return WorkflowGraphSaveImpact{}, removedWorkflowGraphRows{}, err
	}
	currentEdges, err := q.ListWorkflowEdges(ctx, string(workflowID))
	if err != nil {
		return WorkflowGraphSaveImpact{}, removedWorkflowGraphRows{}, err
	}
	removed := removedWorkflowGraphRows{}
	nextGroups := workflowGraphNodeGroupIDs(prepared.nodeGroups)
	for _, group := range currentGroups {
		if !nextGroups[group.ID] {
			removed.nodeGroups = append(removed.nodeGroups, group.ID)
		}
	}
	nextNodes := workflowGraphNodeIDs(prepared.nodes)
	for _, node := range currentNodes {
		id := workflow.NodeID(node.ID)
		if !nextNodes[id] {
			removed.nodes = append(removed.nodes, id)
		}
	}
	nextTransitionGroups := workflowGraphTransitionGroupIDs(prepared.transitionGroups)
	for _, group := range currentTransitionGroups {
		id := workflow.TransitionGroupID(group.ID)
		if !nextTransitionGroups[id] {
			removed.transitionGroups = append(removed.transitionGroups, id)
		}
	}
	nextEdges := workflowGraphEdgeIDs(prepared.edges)
	for _, edge := range currentEdges {
		id := workflow.EdgeID(edge.ID)
		if !nextEdges[id] {
			removed.edges = append(removed.edges, id)
		}
	}

	impact := WorkflowGraphSaveImpact{
		RemovedNodeCount:            int64(len(removed.nodes)),
		RemovedTransitionGroupCount: int64(len(removed.transitionGroups)),
		RemovedEdgeCount:            int64(len(removed.edges)),
	}
	for _, nodeID := range removed.nodes {
		count, err := q.CountTaskNodeReferences(ctx, string(nodeID))
		if err != nil {
			return WorkflowGraphSaveImpact{}, removedWorkflowGraphRows{}, err
		}
		impact.NodeTaskReferenceCount += count
	}
	for _, edgeID := range removed.edges {
		count, err := q.CountTaskEdgeReferences(ctx, sql.NullString{String: string(edgeID), Valid: true})
		if err != nil {
			return WorkflowGraphSaveImpact{}, removedWorkflowGraphRows{}, err
		}
		impact.EdgeTaskReferenceCount += count
	}
	return impact, removed, nil
}

func workflowGraphSaveBlockers(req WorkflowGraphSaveRequest, impact WorkflowGraphSaveImpact) []WorkflowGraphSaveBlocker {
	blockers := []WorkflowGraphSaveBlocker{}
	if impact.NodeTaskReferenceCount > 0 {
		blockers = append(blockers, WorkflowGraphSaveBlocker{Code: "node_task_references", Message: "Removed workflow nodes are referenced by existing tasks.", Count: impact.NodeTaskReferenceCount})
	}
	if impact.EdgeTaskReferenceCount > 0 {
		blockers = append(blockers, WorkflowGraphSaveBlocker{Code: "edge_task_references", Message: "Removed workflow edges are referenced by existing tasks.", Count: impact.EdgeTaskReferenceCount})
	}
	removedCount := impact.RemovedNodeCount + impact.RemovedTransitionGroupCount + impact.RemovedEdgeCount
	if removedCount > 0 && !req.Confirmed {
		blockers = append(blockers, WorkflowGraphSaveBlocker{Code: "confirmation_required", Message: "Workflow graph save removes graph rows. Confirm with the current impact before saving.", Count: removedCount})
	}
	return blockers
}

func applyWorkflowGraphSave(ctx context.Context, tx *sql.Tx, q *sqlitegen.Queries, workflowID workflow.WorkflowID, prepared preparedWorkflowGraphSave, removed removedWorkflowGraphRows) error {
	for _, edgeID := range removed.edges {
		if deleted, err := q.DeleteWorkflowEdge(ctx, string(edgeID)); err != nil {
			return fmt.Errorf("delete removed workflow edge: %w", err)
		} else if deleted != 1 {
			return sql.ErrNoRows
		}
	}
	for _, groupID := range removed.transitionGroups {
		if _, err := tx.ExecContext(ctx, `DELETE FROM workflow_transition_groups WHERE id = ?`, string(groupID)); err != nil {
			return fmt.Errorf("delete removed workflow transition group: %w", err)
		}
	}
	for _, nodeID := range removed.nodes {
		if deleted, err := q.DeleteWorkflowNode(ctx, string(nodeID)); err != nil {
			return fmt.Errorf("delete removed workflow node: %w", err)
		} else if deleted != 1 {
			return sql.ErrNoRows
		}
	}
	for _, groupID := range removed.nodeGroups {
		if deleted, err := q.DeleteWorkflowNodeGroup(ctx, sqlitegen.DeleteWorkflowNodeGroupParams{ID: groupID, WorkflowID: string(workflowID)}); err != nil {
			return fmt.Errorf("delete removed workflow node group: %w", err)
		} else if deleted != 1 {
			return sql.ErrNoRows
		}
	}
	for _, group := range prepared.nodeGroups {
		if err := upsertWorkflowNodeGroup(ctx, tx, group); err != nil {
			return err
		}
	}
	for index, node := range prepared.nodes {
		if err := upsertWorkflowNode(ctx, tx, node, int64(index*100)); err != nil {
			return err
		}
	}
	for index, group := range prepared.transitionGroups {
		if err := upsertWorkflowTransitionGroup(ctx, tx, group, int64(index*100)); err != nil {
			return err
		}
	}
	for index, edge := range prepared.edges {
		if err := upsertWorkflowEdge(ctx, tx, edge, int64(index*100)); err != nil {
			return err
		}
	}
	return nil
}

func upsertWorkflowNodeGroup(ctx context.Context, tx *sql.Tx, group NodeGroupRecord) error {
	result, err := tx.ExecContext(ctx, `
INSERT INTO workflow_node_groups (id, workflow_id, group_key, display_name, sort_order)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    group_key = excluded.group_key,
    display_name = excluded.display_name,
    sort_order = excluded.sort_order
WHERE workflow_node_groups.workflow_id = excluded.workflow_id`,
		group.ID,
		string(group.WorkflowID),
		string(group.Key),
		strings.TrimSpace(group.DisplayName),
		group.SortOrder,
	)
	return expectAffectedRow(result, err, "save workflow node group")
}

func upsertWorkflowNode(ctx context.Context, tx *sql.Tx, node NodeRecord, sortOrder int64) error {
	outputFields, err := marshalJSON(node.OutputFields)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
INSERT INTO workflow_nodes (id, workflow_id, node_key, kind, display_name, subagent_role, prompt_template, output_fields_json, group_id, sort_order)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    node_key = excluded.node_key,
    kind = excluded.kind,
    display_name = excluded.display_name,
    subagent_role = excluded.subagent_role,
    prompt_template = excluded.prompt_template,
    output_fields_json = excluded.output_fields_json,
    group_id = excluded.group_id,
    sort_order = excluded.sort_order
WHERE workflow_nodes.workflow_id = excluded.workflow_id`,
		string(node.ID),
		string(node.WorkflowID),
		string(node.Key),
		string(node.Kind),
		strings.TrimSpace(node.DisplayName),
		strings.TrimSpace(node.SubagentRole),
		strings.TrimSpace(node.PromptTemplate),
		outputFields,
		nullableString(node.GroupID),
		sortOrder,
	)
	return expectAffectedRow(result, err, "save workflow node")
}

func upsertWorkflowTransitionGroup(ctx context.Context, tx *sql.Tx, group TransitionGroupRecord, sortOrder int64) error {
	result, err := tx.ExecContext(ctx, `
INSERT INTO workflow_transition_groups (id, source_node_id, transition_id, display_name, sort_order)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    source_node_id = excluded.source_node_id,
    transition_id = excluded.transition_id,
    display_name = excluded.display_name,
    sort_order = excluded.sort_order
WHERE EXISTS (
    SELECT 1
    FROM workflow_nodes source
    WHERE source.id = workflow_transition_groups.source_node_id
      AND source.workflow_id = ?
)`,
		string(group.ID),
		string(group.SourceNodeID),
		strings.TrimSpace(string(group.TransitionID)),
		strings.TrimSpace(group.DisplayName),
		sortOrder,
		string(group.WorkflowID),
	)
	return expectAffectedRow(result, err, "save workflow transition group")
}

func upsertWorkflowEdge(ctx context.Context, tx *sql.Tx, edge EdgeRecord, sortOrder int64) error {
	contextSource := workflow.CanonicalContextSource(edge.ContextSource)
	inputs, err := marshalJSON(edge.InputBindings)
	if err != nil {
		return err
	}
	requirements, err := marshalJSON(edge.OutputRequirements)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
INSERT INTO workflow_edges (id, transition_group_id, edge_key, target_node_id, requires_approval, context_mode, context_source_kind, context_source_node_key, input_bindings_json, output_requirements_json, sort_order)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    transition_group_id = excluded.transition_group_id,
    edge_key = excluded.edge_key,
    target_node_id = excluded.target_node_id,
    requires_approval = excluded.requires_approval,
    context_mode = excluded.context_mode,
    context_source_kind = excluded.context_source_kind,
    context_source_node_key = excluded.context_source_node_key,
    input_bindings_json = excluded.input_bindings_json,
    output_requirements_json = excluded.output_requirements_json,
    sort_order = excluded.sort_order
WHERE EXISTS (
    SELECT 1
    FROM workflow_transition_groups tg
    JOIN workflow_nodes source ON source.id = tg.source_node_id
    WHERE tg.id = workflow_edges.transition_group_id
      AND source.workflow_id = ?
)`,
		string(edge.ID),
		string(edge.TransitionGroupID),
		string(edge.Key),
		string(edge.TargetNodeID),
		boolToInt64(edge.RequiresApproval),
		string(edge.ContextMode),
		string(contextSource.Kind),
		string(contextSource.NodeKey),
		inputs,
		requirements,
		sortOrder,
		string(edge.WorkflowID),
	)
	return expectAffectedRow(result, err, "save workflow edge")
}

func expectAffectedRow(result sql.Result, err error, op string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("%s: %w", op, sql.ErrNoRows)
	}
	return nil
}

func workflowGraphNodeGroupIDs(groups []NodeGroupRecord) map[string]bool {
	out := make(map[string]bool, len(groups))
	for _, group := range groups {
		out[group.ID] = true
	}
	return out
}

func workflowGraphNodeIDs(nodes []NodeRecord) map[workflow.NodeID]bool {
	out := make(map[workflow.NodeID]bool, len(nodes))
	for _, node := range nodes {
		out[node.ID] = true
	}
	return out
}

func workflowGraphTransitionGroupIDs(groups []TransitionGroupRecord) map[workflow.TransitionGroupID]bool {
	out := make(map[workflow.TransitionGroupID]bool, len(groups))
	for _, group := range groups {
		out[group.ID] = true
	}
	return out
}

func workflowGraphEdgeIDs(edges []EdgeRecord) map[workflow.EdgeID]bool {
	out := make(map[workflow.EdgeID]bool, len(edges))
	for _, edge := range edges {
		out[edge.ID] = true
	}
	return out
}
