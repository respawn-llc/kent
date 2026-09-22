package protoapi

import (
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"

	"google.golang.org/protobuf/proto"
)

// WorkflowGraphDraftFromDefinition retains authored graph values and order;
// derived wiring belongs to the server and is not part of the submitted draft.
func WorkflowGraphDraftFromDefinition(definition *pb.WorkflowDefinition) *pb.GraphDraft {
	graph := &pb.GraphDraft{
		NodeGroups:       make([]*pb.GraphDraftNodeGroup, 0, len(definition.NodeGroups)),
		Nodes:            make([]*pb.GraphDraftNode, 0, len(definition.Nodes)),
		TransitionGroups: make([]*pb.GraphDraftTransitionGroup, 0, len(definition.TransitionGroups)),
		Edges:            make([]*pb.GraphDraftEdge, 0, len(definition.Edges)),
	}
	for _, group := range definition.NodeGroups {
		graph.NodeGroups = append(graph.NodeGroups, &pb.GraphDraftNodeGroup{
			Id: group.GroupId, Key: group.GroupKey, DisplayName: group.DisplayName,
		})
	}
	for _, node := range definition.Nodes {
		providers := make([]*pb.DraftJoinInputProvider, 0, len(node.JoinInputProviders))
		for _, provider := range node.JoinInputProviders {
			providers = append(providers, &pb.DraftJoinInputProvider{InputName: provider.InputName, ProviderEdgeId: provider.ProviderEdgeId})
		}
		graph.Nodes = append(graph.Nodes, &pb.GraphDraftNode{
			Id: node.Id, Key: node.Key, Kind: node.Kind, DisplayName: node.DisplayName,
			GroupId: clonePointer(node.GroupId), GroupKey: node.GroupKey,
			SubagentRole: clonePointer(node.SubagentRole), CompletionMode: clonePointer(node.CompletionMode),
			ScriptPath: clonePointer(node.ScriptPath), JoinInputProviders: providers,
		})
	}
	for _, group := range definition.TransitionGroups {
		graph.TransitionGroups = append(graph.TransitionGroups, &pb.GraphDraftTransitionGroup{
			Id: group.Id, SourceNodeId: group.SourceNodeId, TransitionId: group.TransitionId,
			DisplayName: group.DisplayName, Description: group.Description,
		})
	}
	for _, edge := range definition.Edges {
		parameters := make([]*pb.Parameter, 0, len(edge.Parameters))
		for _, parameter := range edge.Parameters {
			parameters = append(parameters, proto.CloneOf(parameter))
		}
		graph.Edges = append(graph.Edges, &pb.GraphDraftEdge{
			Id: edge.Id, TransitionGroupId: edge.TransitionGroupId, Key: edge.Key, TargetNodeId: edge.TargetNodeId,
			AssigneeSelection: edge.AssigneeSelection, ThinkingSelection: edge.ThinkingSelection,
			RequiresApproval: edge.RequiresApproval, ContextMode: edge.ContextMode,
			ContextSource: proto.CloneOf(edge.ContextSource), PromptTemplate: edge.PromptTemplate, Parameters: parameters,
		})
	}
	return graph
}
