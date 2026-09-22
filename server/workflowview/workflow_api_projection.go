package workflowview

import (
	"core/server/workflow"
	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	"core/shared/runtimeids"
)

func DerivedWiring(def workflow.Definition, catalogs ...workflow.TargetAgentCatalog) (*pb.DerivedWiring, error) {
	var catalog workflow.TargetAgentCatalog
	if len(catalogs) > 0 {
		catalog = catalogs[0]
	}
	derived := workflow.DeriveWiringWithCatalog(def, catalog)
	diagnostics, err := ValidationErrors(workflow.WorkflowIDPointer(def.ID), derived.Diagnostics)
	if err != nil {
		return nil, err
	}
	resp := &pb.DerivedWiring{Diagnostics: diagnostics}
	for _, node := range def.Nodes {
		nodeID := workflow.NodeIDOf(node)
		resp.Nodes = append(resp.Nodes, &pb.DerivedNodeWiring{
			NodeId:                  string(nodeID),
			PossibleProvisionFields: OutputFields(derived.PossibleProvisionFieldsForNode(nodeID)),
			JoinOutputFields:        OutputFields(derived.JoinOutputFieldsForNode(nodeID)),
		})
	}
	for _, group := range def.TransitionGroups {
		resp.TransitionGroups = append(resp.TransitionGroups, &pb.DerivedTransitionGroupWiring{
			TransitionGroupId:       string(group.ID),
			RequiredProvisionFields: OutputFields(derived.RequiredProvisionFieldsForTransitionGroup(group.ID)),
		})
	}
	for _, edge := range def.Edges {
		applicability := derived.SelectorApplicabilityForEdge(edge.ID)
		assignee, err := selectorApplicability(applicability.Assignee)
		if err != nil {
			return nil, err
		}
		thinking, err := selectorApplicability(applicability.Thinking)
		if err != nil {
			return nil, err
		}
		resp.Edges = append(resp.Edges, &pb.DerivedEdgeWiring{
			EdgeId:                         string(edge.ID),
			InputBindings:                  InputBindings(derived.InputBindingsForEdge(edge.ID)),
			RequiredProvisionFields:        OutputFields(derived.RequiredProvisionFieldsForEdge(edge.ID)),
			RequiredProviderFields:         OutputFields(derived.RequiredProviderFieldsForJoinEdge(edge.ID)),
			AssigneeSelectionApplicability: assignee,
			ThinkingSelectionApplicability: thinking,
		})
	}
	return resp, nil
}

func selectorApplicability(fact workflow.SelectorApplicability) (*pb.SelectorApplicability, error) {
	reason, err := protoapi.WorkflowSelectorApplicabilityReason.Encode(string(fact.Reason))
	if err != nil {
		return nil, err
	}
	return &pb.SelectorApplicability{
		Available:        fact.Available,
		ParameterVisible: fact.ParameterVisible,
		Reason:           reason,
	}, nil
}

func ValidationErrors(inheritedWorkflowID *runtimeids.WorkflowID, errs []workflow.ValidationError) ([]*pb.WorkflowValidationError, error) {
	out := make([]*pb.WorkflowValidationError, 0, len(errs))
	for _, err := range errs {
		relatedIDs := append([]string(nil), err.RelatedIDs...)
		for _, entity := range err.RelatedEntities {
			relatedIDs = append(relatedIDs, entity.EntityID)
		}
		code, encodeErr := protoapi.WorkflowValidationErrorCode.Encode(string(err.Code))
		if encodeErr != nil {
			return nil, encodeErr
		}
		workflowID := err.WorkflowID
		if workflowID == nil {
			workflowID = inheritedWorkflowID
		}
		projected := &pb.WorkflowValidationError{
			Code:              code,
			Message:           err.Message,
			NodeId:            graphIDPointer(err.NodeID),
			TransitionGroupId: graphIDPointer(err.TransitionGroupID),
			EdgeId:            graphIDPointer(err.EdgeID),
			Details:           validationErrorDetails(err),
			RelatedIds:        relatedIDs,
			BlocksContext:     err.BlocksContext,
		}
		if workflowID != nil {
			id := workflowID.String()
			projected.WorkflowId = &id
		}
		out = append(out, projected)
	}
	return out, nil
}

func graphIDPointer[T ~string](value *T) *string {
	if value == nil {
		return nil
	}
	copy := string(*value)
	return &copy
}

func validationErrorDetails(err workflow.ValidationError) *pb.WorkflowValidationErrorDetails {
	var requiredTool *string
	if err.RequiredTool != nil {
		value := string(*err.RequiredTool)
		requiredTool = &value
	}
	details := pb.WorkflowValidationErrorDetails{
		FieldName:      err.FieldName,
		InputName:      err.InputName,
		Placeholder:    err.Placeholder,
		ProviderEdgeId: graphIDPointer(err.ProviderEdgeID),
		Role:           err.AgentRole,
		RequiredTool:   requiredTool,
	}
	if details.FieldName == "" && details.InputName == "" && details.Placeholder == "" &&
		details.ProviderEdgeId == nil && details.Role == nil && details.RequiredTool == nil {
		return nil
	}
	return &details
}

func OutputFields(in []workflow.OutputField) []*pb.OutputField {
	out := make([]*pb.OutputField, 0, len(in))
	for _, field := range in {
		out = append(out, &pb.OutputField{Name: field.Name, Description: field.Description})
	}
	return out
}

func InputBindings(in []workflow.InputBinding) []*pb.InputBinding {
	out := make([]*pb.InputBinding, 0, len(in))
	for _, binding := range in {
		out = append(out, &pb.InputBinding{Name: binding.Name, Source: string(binding.Source), Field: binding.Field})
	}
	return out
}
