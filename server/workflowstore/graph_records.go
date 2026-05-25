package workflowstore

import (
	"context"

	"builder/server/metadata/sqlitegen"
	"builder/server/workflow"
)

func currentWorkflowGraphSavePrepared(ctx context.Context, q *sqlitegen.Queries, workflowID workflow.WorkflowID) (preparedWorkflowGraphSave, error) {
	nodeGroups, err := q.ListWorkflowNodeGroups(ctx, string(workflowID))
	if err != nil {
		return preparedWorkflowGraphSave{}, err
	}
	nodes, err := q.ListWorkflowNodes(ctx, string(workflowID))
	if err != nil {
		return preparedWorkflowGraphSave{}, err
	}
	transitionGroups, err := q.ListWorkflowTransitionGroups(ctx, string(workflowID))
	if err != nil {
		return preparedWorkflowGraphSave{}, err
	}
	edges, err := q.ListWorkflowEdges(ctx, string(workflowID))
	if err != nil {
		return preparedWorkflowGraphSave{}, err
	}
	prepared := preparedWorkflowGraphSave{
		nodeGroups:       make([]NodeGroupRecord, 0, len(nodeGroups)),
		nodes:            make([]NodeRecord, 0, len(nodes)),
		transitionGroups: make([]TransitionGroupRecord, 0, len(transitionGroups)),
		edges:            make([]EdgeRecord, 0, len(edges)),
	}
	for _, group := range nodeGroups {
		prepared.nodeGroups = append(prepared.nodeGroups, NodeGroupRecord{ID: group.ID, WorkflowID: workflow.WorkflowID(group.WorkflowID), Key: workflow.ModelKey(group.GroupKey), DisplayName: group.DisplayName, SortOrder: group.SortOrder})
	}
	for _, node := range nodes {
		outputFields := []workflow.OutputField{}
		if err := unmarshalJSON(node.OutputFieldsJson, &outputFields); err != nil {
			return preparedWorkflowGraphSave{}, err
		}
		groupID := ""
		if node.GroupID.Valid {
			groupID = node.GroupID.String
		}
		prepared.nodes = append(prepared.nodes, NodeRecord{ID: workflow.NodeID(node.ID), WorkflowID: workflow.WorkflowID(node.WorkflowID), Key: workflow.ModelKey(node.NodeKey), Kind: workflow.NodeKind(node.Kind), DisplayName: node.DisplayName, GroupID: groupID, SubagentRole: node.SubagentRole, PromptTemplate: node.PromptTemplate, OutputFields: outputFields})
	}
	for _, group := range transitionGroups {
		prepared.transitionGroups = append(prepared.transitionGroups, TransitionGroupRecord{ID: workflow.TransitionGroupID(group.ID), WorkflowID: workflow.WorkflowID(group.WorkflowID), SourceNodeID: workflow.NodeID(group.SourceNodeID), TransitionID: workflow.TransitionID(group.TransitionID), DisplayName: group.DisplayName})
	}
	for _, edge := range edges {
		inputs := []workflow.InputBinding{}
		if err := unmarshalJSON(edge.InputBindingsJson, &inputs); err != nil {
			return preparedWorkflowGraphSave{}, err
		}
		requirements := []workflow.OutputRequirement{}
		if err := unmarshalJSON(edge.OutputRequirementsJson, &requirements); err != nil {
			return preparedWorkflowGraphSave{}, err
		}
		prepared.edges = append(prepared.edges, EdgeRecord{
			ID:                 workflow.EdgeID(edge.ID),
			WorkflowID:         workflow.WorkflowID(edge.WorkflowID),
			TransitionGroupID:  workflow.TransitionGroupID(edge.TransitionGroupID),
			Key:                workflow.ModelKey(edge.EdgeKey),
			TargetNodeID:       workflow.NodeID(edge.TargetNodeID),
			RequiresApproval:   edge.RequiresApproval != 0,
			ContextMode:        workflow.ContextMode(edge.ContextMode),
			ContextSource:      workflow.CanonicalContextSource(workflow.ContextSource{Kind: workflow.ContextSourceKind(edge.ContextSourceKind), NodeKey: workflow.ModelKey(edge.ContextSourceNodeKey)}),
			InputBindings:      inputs,
			OutputRequirements: requirements,
		})
	}
	return prepared, nil
}

func withWorkflowGraphNode(prepared preparedWorkflowGraphSave, node NodeRecord) preparedWorkflowGraphSave {
	out := clonePreparedWorkflowGraphSave(prepared)
	for i, current := range out.nodes {
		if current.ID == node.ID {
			out.nodes[i] = node
			return out
		}
	}
	out.nodes = append(out.nodes, node)
	return out
}

func withoutWorkflowGraphNode(prepared preparedWorkflowGraphSave, nodeID workflow.NodeID) preparedWorkflowGraphSave {
	out := clonePreparedWorkflowGraphSave(prepared)
	filtered := make([]NodeRecord, 0, len(out.nodes))
	for _, node := range out.nodes {
		if node.ID != nodeID {
			filtered = append(filtered, node)
		}
	}
	out.nodes = filtered
	return out
}

func withWorkflowGraphNodeGroup(prepared preparedWorkflowGraphSave, group NodeGroupRecord) preparedWorkflowGraphSave {
	out := clonePreparedWorkflowGraphSave(prepared)
	for i, current := range out.nodeGroups {
		if current.ID == group.ID {
			out.nodeGroups[i] = group
			return out
		}
	}
	out.nodeGroups = append(out.nodeGroups, group)
	return out
}

func withoutWorkflowGraphNodeGroup(prepared preparedWorkflowGraphSave, groupID string) preparedWorkflowGraphSave {
	out := clonePreparedWorkflowGraphSave(prepared)
	filtered := make([]NodeGroupRecord, 0, len(out.nodeGroups))
	for _, group := range out.nodeGroups {
		if group.ID != groupID {
			filtered = append(filtered, group)
		}
	}
	out.nodeGroups = filtered
	return out
}

func withWorkflowGraphTransitionGroup(prepared preparedWorkflowGraphSave, group TransitionGroupRecord) preparedWorkflowGraphSave {
	out := clonePreparedWorkflowGraphSave(prepared)
	for i, current := range out.transitionGroups {
		if current.ID == group.ID {
			out.transitionGroups[i] = group
			return out
		}
	}
	out.transitionGroups = append(out.transitionGroups, group)
	return out
}

func withWorkflowGraphEdge(prepared preparedWorkflowGraphSave, edge EdgeRecord) preparedWorkflowGraphSave {
	out := clonePreparedWorkflowGraphSave(prepared)
	for i, current := range out.edges {
		if current.ID == edge.ID {
			out.edges[i] = edge
			return out
		}
	}
	out.edges = append(out.edges, edge)
	return out
}

func withoutWorkflowGraphEdge(prepared preparedWorkflowGraphSave, edgeID workflow.EdgeID) preparedWorkflowGraphSave {
	out := clonePreparedWorkflowGraphSave(prepared)
	filtered := make([]EdgeRecord, 0, len(out.edges))
	for _, edge := range out.edges {
		if edge.ID != edgeID {
			filtered = append(filtered, edge)
		}
	}
	out.edges = filtered
	return out
}

func clonePreparedWorkflowGraphSave(prepared preparedWorkflowGraphSave) preparedWorkflowGraphSave {
	return preparedWorkflowGraphSave{
		nodeGroups:       append([]NodeGroupRecord(nil), prepared.nodeGroups...),
		nodes:            append([]NodeRecord(nil), prepared.nodes...),
		transitionGroups: append([]TransitionGroupRecord(nil), prepared.transitionGroups...),
		edges:            append([]EdgeRecord(nil), prepared.edges...),
	}
}
