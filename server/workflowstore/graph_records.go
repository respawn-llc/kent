package workflowstore

import (
	"context"

	"core/server/metadata"
	"core/server/metadata/sqlitegen"
	"core/server/workflow"
	"core/shared/runtimeids"
)

func nullableGraphIdentity(value []byte) (*string, error) {
	if value == nil {
		return nil, nil
	}
	text, err := runtimeids.GraphEntityIDText(value)
	if err != nil {
		return nil, err
	}
	return &text, nil
}

func nullableGraphIdentityArgument(value *string) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	return runtimeids.GraphEntityIDBlob(*value)
}

func workflowDefinitionFromPreparedGraph(
	prepared preparedWorkflowGraphSave,
	workflowID runtimeids.WorkflowID,
	displayName string,
	executionTargetPolicy workflow.ExecutionTargetPolicy,
) (workflow.Definition, error) {
	definition := workflow.Definition{
		ID:                    workflowID,
		DisplayName:           displayName,
		ExecutionTargetPolicy: executionTargetPolicy,
	}
	for _, group := range prepared.nodeGroups {
		definition.NodeGroups = append(definition.NodeGroups, workflow.NodeGroup{
			WorkflowID:  group.WorkflowID,
			ID:          group.ID,
			Key:         group.Key,
			DisplayName: group.DisplayName,
			SortOrder:   group.SortOrder,
		})
	}
	for _, node := range prepared.nodes {
		workflowNode, err := workflowNodeFromRecord(node)
		if err != nil {
			return workflow.Definition{}, err
		}
		definition.Nodes = append(definition.Nodes, workflowNode)
	}
	for _, group := range prepared.transitionGroups {
		definition.TransitionGroups = append(definition.TransitionGroups, workflow.TransitionGroup{
			WorkflowID:   group.WorkflowID,
			ID:           group.ID,
			SourceNodeID: group.SourceNodeID,
			TransitionID: group.TransitionID,
			DisplayName:  group.DisplayName,
			Description:  group.Description,
		})
	}
	for _, edge := range prepared.edges {
		definition.Edges = append(definition.Edges, workflowEdgeFromRecord(edge))
	}
	return definition, nil
}

func currentWorkflowGraphSavePrepared(ctx context.Context, q *sqlitegen.Queries, workflowID runtimeids.WorkflowID) (preparedWorkflowGraphSave, error) {
	nodeGroups, err := q.ListWorkflowNodeGroups(ctx, workflowID)
	if err != nil {
		return preparedWorkflowGraphSave{}, err
	}
	nodes, err := q.ListWorkflowNodes(ctx, workflowID)
	if err != nil {
		return preparedWorkflowGraphSave{}, err
	}
	transitionGroups, err := q.ListWorkflowTransitionGroups(ctx, workflowID)
	if err != nil {
		return preparedWorkflowGraphSave{}, err
	}
	edges, err := q.ListWorkflowEdges(ctx, workflowID)
	if err != nil {
		return preparedWorkflowGraphSave{}, err
	}
	prepared := preparedWorkflowGraphSave{
		nodeGroups:       make([]NodeGroupRecord, 0, len(nodeGroups)),
		nodes:            make([]NodeRecord, 0, len(nodes)),
		transitionGroups: make([]TransitionGroupRecord, 0, len(transitionGroups)),
		edges:            make([]EdgeRecord, 0, len(edges)),
	}
	providerRows, err := q.ListWorkflowJoinProviders(ctx, workflowID)
	if err != nil {
		return preparedWorkflowGraphSave{}, err
	}
	providers := make(map[string][]workflow.JoinInputProvider)
	for _, row := range providerRows {
		edgeID, err := runtimeids.GraphEntityIDText(row.ProviderEdgeID)
		if err != nil {
			return preparedWorkflowGraphSave{}, err
		}
		providers[string(row.NodeID)] = append(providers[string(row.NodeID)], workflow.JoinInputProvider{
			InputName: row.InputName, ProviderEdgeID: workflow.EdgeID(edgeID),
		})
	}
	parameterRows, err := q.ListWorkflowParameters(ctx, workflowID)
	if err != nil {
		return preparedWorkflowGraphSave{}, err
	}
	parametersByEdge := make(map[string][]workflow.Parameter)
	for _, row := range parameterRows {
		parametersByEdge[string(row.EdgeID)] = append(parametersByEdge[string(row.EdgeID)], workflow.Parameter{
			Key: row.ParameterKey, Description: row.Description, Purpose: workflow.ParameterPurpose(row.Purpose),
		})
	}
	bindingRows, err := q.ListWorkflowInputBindings(ctx, workflowID)
	if err != nil {
		return preparedWorkflowGraphSave{}, err
	}
	bindingsByEdge := make(map[string][]workflow.InputBinding)
	for _, row := range bindingRows {
		bindingsByEdge[string(row.EdgeID)] = append(bindingsByEdge[string(row.EdgeID)], workflow.InputBinding{
			Name: row.Name, Source: workflow.BindingSource(row.Source), Field: row.Field,
		})
	}
	requirementRows, err := q.ListWorkflowOutputRequirements(ctx, workflowID)
	if err != nil {
		return preparedWorkflowGraphSave{}, err
	}
	requirementsByEdge := make(map[string][]workflow.OutputRequirement)
	for _, row := range requirementRows {
		requirementsByEdge[string(row.EdgeID)] = append(requirementsByEdge[string(row.EdgeID)], workflow.OutputRequirement{
			FieldName: row.FieldName,
		})
	}
	for _, group := range nodeGroups {
		id, err := runtimeids.GraphEntityIDText(group.ID)
		if err != nil {
			return preparedWorkflowGraphSave{}, err
		}
		prepared.nodeGroups = append(prepared.nodeGroups, NodeGroupRecord{ID: id, WorkflowID: group.WorkflowID, Key: workflow.ModelKey(group.GroupKey), DisplayName: metadata.OptionalString(group.DisplayName), SortOrder: group.SortOrder})
	}
	for _, node := range nodes {
		id, err := runtimeids.GraphEntityIDText(node.ID)
		if err != nil {
			return preparedWorkflowGraphSave{}, err
		}
		joinProviders := providers[string(node.ID)]
		groupID, err := nullableGraphIdentity(node.GroupID)
		if err != nil {
			return preparedWorkflowGraphSave{}, err
		}
		scriptPath := ""
		if node.ScriptPath.Valid {
			scriptPath = node.ScriptPath.String
		}
		prepared.nodes = append(prepared.nodes, NodeRecord{ID: workflow.NodeID(id), WorkflowID: node.WorkflowID, Key: workflow.ModelKey(node.NodeKey), Kind: workflow.NodeKind(node.Kind), DisplayName: node.DisplayName, GroupID: groupID, SubagentRole: node.SubagentRole, CompletionMode: node.CompletionMode, ScriptPath: scriptPath, JoinInputProviders: joinProviders, SortOrder: node.SortOrder})
	}
	for _, group := range transitionGroups {
		id, err := runtimeids.GraphEntityIDText(group.ID)
		if err != nil {
			return preparedWorkflowGraphSave{}, err
		}
		sourceNodeID, err := runtimeids.GraphEntityIDText(group.SourceNodeID)
		if err != nil {
			return preparedWorkflowGraphSave{}, err
		}
		prepared.transitionGroups = append(prepared.transitionGroups, TransitionGroupRecord{ID: workflow.TransitionGroupID(id), WorkflowID: workflowID, SourceNodeID: workflow.NodeID(sourceNodeID), TransitionID: workflow.TransitionID(group.TransitionID), DisplayName: group.DisplayName, Description: group.Description, SortOrder: group.SortOrder})
	}
	for _, edge := range edges {
		id, err := runtimeids.GraphEntityIDText(edge.ID)
		if err != nil {
			return preparedWorkflowGraphSave{}, err
		}
		groupID, err := runtimeids.GraphEntityIDText(edge.TransitionGroupID)
		if err != nil {
			return preparedWorkflowGraphSave{}, err
		}
		targetNodeID, err := runtimeids.GraphEntityIDText(edge.TargetNodeID)
		if err != nil {
			return preparedWorkflowGraphSave{}, err
		}
		parameters := parametersByEdge[string(edge.ID)]
		inputs := bindingsByEdge[string(edge.ID)]
		requirements := requirementsByEdge[string(edge.ID)]
		prepared.edges = append(prepared.edges, EdgeRecord{
			ID:                 workflow.EdgeID(id),
			WorkflowID:         edge.WorkflowID,
			TransitionGroupID:  workflow.TransitionGroupID(groupID),
			Key:                workflow.ModelKey(edge.EdgeKey),
			TargetNodeID:       workflow.NodeID(targetNodeID),
			AssigneeSelection:  workflow.AssigneeSelection(edge.AssigneeSelection),
			ThinkingSelection:  workflow.ThinkingSelection(edge.ThinkingSelection),
			RequiresApproval:   edge.RequiresApproval != 0,
			ContextMode:        workflow.ContextMode(edge.ContextMode),
			ContextSource:      workflow.CanonicalContextSource(workflow.ContextSource{Kind: workflow.ContextSourceKind(edge.ContextSourceKind), NodeKey: workflow.ModelKey(edge.ContextSourceNodeKey)}),
			PromptTemplate:     edge.PromptTemplate,
			Parameters:         parameters,
			InputBindings:      inputs,
			OutputRequirements: requirements,
			SortOrder:          edge.SortOrder,
		})
	}
	return prepared, nil
}

func workflowEdgeFromRecord(edge EdgeRecord) workflow.Edge {
	return workflow.Edge{
		WorkflowID:         edge.WorkflowID,
		ID:                 edge.ID,
		Key:                edge.Key,
		TransitionGroupID:  edge.TransitionGroupID,
		TargetNodeID:       edge.TargetNodeID,
		AssigneeSelection:  edge.AssigneeSelection,
		ThinkingSelection:  edge.ThinkingSelection,
		ContextMode:        edge.ContextMode,
		ContextSource:      workflow.CanonicalContextSource(edge.ContextSource),
		RequiresApproval:   edge.RequiresApproval,
		PromptTemplate:     edge.PromptTemplate,
		Parameters:         edge.Parameters,
		InputBindings:      edge.InputBindings,
		OutputRequirements: edge.OutputRequirements,
	}
}
