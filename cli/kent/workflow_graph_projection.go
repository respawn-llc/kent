package main

import (
	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	"core/shared/workflowcontract"
)

func workflowWiringForCLI(wiring *pb.DerivedWiring) (workflowDerivedWiringJSON, error) {
	diagnostics, err := protoapi.WorkflowValidationErrorsForJSON(wiring.Diagnostics)
	if err != nil {
		return workflowDerivedWiringJSON{}, err
	}
	result := workflowDerivedWiringJSON{Diagnostics: workflowValidationErrorsForCLI(diagnostics)}
	for _, node := range wiring.Nodes {
		result.Nodes = append(result.Nodes, workflowDerivedNodeJSON{
			NodeID: node.NodeId, PossibleProvisionFields: protoapi.WorkflowOutputFieldsForJSON(node.PossibleProvisionFields),
			JoinOutputFields: protoapi.WorkflowOutputFieldsForJSON(node.JoinOutputFields),
		})
	}
	for _, group := range wiring.TransitionGroups {
		result.TransitionGroups = append(result.TransitionGroups, workflowDerivedTransitionJSON{
			TransitionGroupID: group.TransitionGroupId, RequiredProvisionFields: protoapi.WorkflowOutputFieldsForJSON(group.RequiredProvisionFields),
		})
	}
	for _, edge := range wiring.Edges {
		assignee := workflowApplicabilityForCLI(edge.AssigneeSelectionApplicability)
		thinking := workflowApplicabilityForCLI(edge.ThinkingSelectionApplicability)
		result.Edges = append(result.Edges, workflowDerivedEdgeJSON{
			EdgeID: edge.EdgeId, InputBindings: workflowInputBindingsForCLI(edge.InputBindings),
			RequiredProvisionFields:        protoapi.WorkflowOutputFieldsForJSON(edge.RequiredProvisionFields),
			RequiredProviderFields:         protoapi.WorkflowOutputFieldsForJSON(edge.RequiredProviderFields),
			AssigneeSelectionApplicability: assignee, ThinkingSelectionApplicability: thinking,
		})
	}
	return result, nil
}

func workflowApplicabilityForCLI(value *pb.SelectorApplicability) workflowSelectorApplicabilityJSON {
	reason := protoapi.WorkflowSelectorApplicabilityReason.FromProto(value.Reason)
	return workflowSelectorApplicabilityJSON{Available: value.Available, ParameterVisible: value.ParameterVisible, Reason: reason}
}

func workflowInputBindingsForCLI(values []*pb.InputBinding) []workflowInputBindingJSON {
	result := make([]workflowInputBindingJSON, 0, len(values))
	for _, value := range values {
		result = append(result, workflowInputBindingJSON{Name: value.Name, Source: value.Source, Field: value.Field})
	}
	return result
}

func workflowGraphImpactForCLI(impact *pb.GraphSaveImpact) *workflowGraphImpactJSON {
	entities := workflowGraphEntitiesForCLI(impact.RemovedEntities)
	return &workflowGraphImpactJSON{
		RemovedNodeGroupCount: impact.RemovedNodeGroupCount, RemovedNodeCount: impact.RemovedNodeCount,
		RemovedTransitionGroupCount: impact.RemovedTransitionGroupCount, RemovedEdgeCount: impact.RemovedEdgeCount,
		RemovedEntities: entities, NodeTaskReferenceCount: impact.NodeTaskReferenceCount, EdgeTaskReferenceCount: impact.EdgeTaskReferenceCount,
		ActiveCurrentNodeCount: impact.ActiveCurrentNodeCount, PendingApprovalCount: impact.PendingApprovalCount,
		StartNodeChangeCount: impact.StartNodeChangeCount, LastTerminalChangeCount: impact.LastTerminalChangeCount,
		TaskReferencedNodeKindChangeCount: impact.TaskReferencedNodeKindChangeCount,
	}
}

func workflowGraphEntitiesForCLI(entities []*pb.GraphEntityReference) []workflowcontract.WorkflowGraphEntityReference {
	result := make([]workflowcontract.WorkflowGraphEntityReference, 0, len(entities))
	for _, entity := range entities {
		kind := protoapi.WorkflowGraphEntityType.FromProto(entity.EntityType)
		result = append(result, workflowcontract.WorkflowGraphEntityReference{
			EntityType: workflowcontract.WorkflowGraphEntityType(kind), EntityID: entity.EntityId,
		})
	}
	return result
}

func workflowGraphBlockersForCLI(blockers []*pb.GraphSaveBlocker) []workflowGraphBlockerJSON {
	result := make([]workflowGraphBlockerJSON, 0, len(blockers))
	for _, blocker := range blockers {
		entities := workflowGraphEntitiesForCLI(blocker.AffectedEntities)
		result = append(result, workflowGraphBlockerJSON{Code: blocker.Code, Message: blocker.Message, Count: blocker.Count, AffectedEntities: entities})
	}
	return result
}

func workflowValidationResultsForCLI(results []*pb.ModeValidationResult) (map[string]workflowValidationJSON, error) {
	out := make(map[string]workflowValidationJSON, len(results))
	for _, result := range results {
		mode := protoapi.WorkflowValidationMode.FromProto(result.Mode)
		value, err := workflowValidationForCLI(result.Result)
		if err != nil {
			return nil, err
		}
		out[mode] = value
	}
	return out, nil
}
