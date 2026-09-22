package main

import (
	"errors"
	"strings"

	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	"core/shared/textutil"
)

func workflowDocumentNode(node *pb.GraphDraftNode) (workflowGraphDocumentNode, error) {
	kind, err := protoapi.WorkflowNodeKind.Decode(node.Kind)
	if err != nil {
		return workflowGraphDocumentNode{}, err
	}
	var completion string
	if node.CompletionMode != nil {
		completion, err = protoapi.WorkflowCompletionMode.Decode(*node.CompletionMode)
		if err != nil {
			return workflowGraphDocumentNode{}, err
		}
	}
	providers := make([]workflowJoinInputProviderJSON, 0, len(node.JoinInputProviders))
	for _, provider := range node.JoinInputProviders {
		providers = append(providers, workflowJoinInputProviderJSON{InputName: provider.InputName, ProviderEdgeID: provider.ProviderEdgeId})
	}
	return workflowGraphDocumentNode{
		ID: node.Id, Key: node.Key, Kind: kind, DisplayName: node.DisplayName,
		GroupID: node.GroupId, SubagentRole: node.GetSubagentRole(), CompletionMode: completion,
		ScriptPath: node.ScriptPath, JoinInputProviders: providers,
	}, nil
}

func workflowDocumentEdge(edge *pb.GraphDraftEdge) (workflowGraphDocumentEdge, error) {
	assignee, assigneeErr := protoapi.WorkflowAssigneeSelection.Decode(edge.AssigneeSelection)
	thinking, thinkingErr := protoapi.WorkflowThinkingSelection.Decode(edge.ThinkingSelection)
	mode, modeErr := protoapi.WorkflowContextMode.Decode(edge.ContextMode)
	source, sourceErr := protoapi.WorkflowContextSourceKind.Decode(edge.ContextSource.GetKind())
	if err := errors.Join(assigneeErr, thinkingErr, modeErr, sourceErr); err != nil {
		return workflowGraphDocumentEdge{}, err
	}
	parameters := make([]workflowParameterJSON, 0, len(edge.Parameters))
	for _, parameter := range edge.Parameters {
		purpose, err := protoapi.WorkflowParameterPurpose.Decode(parameter.Purpose)
		if err != nil {
			return workflowGraphDocumentEdge{}, err
		}
		parameters = append(parameters, workflowParameterJSON{Key: parameter.Key, Description: parameter.Description, Purpose: purpose})
	}
	return workflowGraphDocumentEdge{
		ID: edge.Id, TransitionGroupID: edge.TransitionGroupId, Key: edge.Key, TargetNodeID: edge.TargetNodeId,
		AssigneeSelection: assignee, ThinkingSelection: thinking, RequiresApproval: edge.RequiresApproval,
		ContextMode: mode, ContextSource: workflowContextSourceJSON{Kind: source, NodeKey: edge.ContextSource.GetNodeKey()},
		PromptTemplate: edge.PromptTemplate, Parameters: parameters,
	}, nil
}

func workflowDocumentEdgeToProto(edge workflowGraphDocumentEdge) (*pb.GraphDraftEdge, error) {
	assigneeValue := strings.TrimSpace(edge.AssigneeSelection)
	if assigneeValue == "" {
		assigneeValue = "configured"
	}
	thinkingValue := strings.TrimSpace(edge.ThinkingSelection)
	if thinkingValue == "" {
		thinkingValue = "configured"
	}
	sourceValue := strings.TrimSpace(edge.ContextSource.Kind)
	if sourceValue == "" {
		sourceValue = "immediate_source"
	}
	assignee, assigneeErr := protoapi.WorkflowAssigneeSelection.Encode(assigneeValue)
	thinking, thinkingErr := protoapi.WorkflowThinkingSelection.Encode(thinkingValue)
	mode, modeErr := protoapi.WorkflowContextMode.Encode(strings.TrimSpace(edge.ContextMode))
	source, sourceErr := protoapi.WorkflowContextSourceKind.Encode(sourceValue)
	if err := errors.Join(assigneeErr, thinkingErr, modeErr, sourceErr); err != nil {
		return nil, err
	}
	parameters := make([]*pb.Parameter, 0, len(edge.Parameters))
	for _, parameter := range edge.Parameters {
		value := strings.TrimSpace(parameter.Purpose)
		if value == "" {
			value = "ordinary"
		}
		purpose, err := protoapi.WorkflowParameterPurpose.Encode(value)
		if err != nil {
			return nil, err
		}
		parameters = append(parameters, &pb.Parameter{Key: parameter.Key, Description: parameter.Description, Purpose: purpose})
	}
	return &pb.GraphDraftEdge{
		Id: edge.ID, TransitionGroupId: edge.TransitionGroupID, Key: edge.Key, TargetNodeId: edge.TargetNodeID,
		AssigneeSelection: assignee, ThinkingSelection: thinking, RequiresApproval: edge.RequiresApproval,
		ContextMode: mode, ContextSource: &pb.ContextSource{Kind: source, NodeKey: textutil.OptionalExactString(edge.ContextSource.NodeKey)},
		PromptTemplate: edge.PromptTemplate, Parameters: parameters,
	}, nil
}
