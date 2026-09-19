package protoapi

import (
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

// WorkflowValidationErrorsForJSON projects the diagnostic contract still used
// inside Task JSON responses and CLI reports, not migrated Workflow API calls.
func WorkflowValidationErrorsForJSON(in []*pb.WorkflowValidationError) ([]serverapi.WorkflowValidationError, error) {
	out := make([]serverapi.WorkflowValidationError, 0, len(in))
	for _, value := range in {
		code, err := WorkflowValidationErrorCode.Decode(value.Code)
		if err != nil {
			return nil, err
		}
		projected := serverapi.WorkflowValidationError{
			Code: code, Message: value.Message, NodeID: value.NodeId, TransitionGroupID: value.TransitionGroupId,
			EdgeID: value.EdgeId, RelatedIDs: value.RelatedIds, BlocksContext: value.BlocksContext,
		}
		if value.WorkflowId != nil {
			id, err := runtimeids.ParseWorkflowID(*value.WorkflowId)
			if err != nil {
				return nil, err
			}
			projected.WorkflowID = &id
		}
		if details := value.Details; details != nil {
			projected.Details = &serverapi.WorkflowValidationErrorDetails{
				FieldName: details.FieldName, InputName: details.InputName, Placeholder: details.Placeholder,
				ProviderEdgeID: details.ProviderEdgeId, Role: details.Role, RequiredTool: details.RequiredTool,
			}
		}
		out = append(out, projected)
	}
	return out, nil
}

func WorkflowOutputFieldsForJSON(in []*pb.OutputField) []serverapi.WorkflowOutputField {
	out := make([]serverapi.WorkflowOutputField, 0, len(in))
	for _, value := range in {
		out = append(out, serverapi.WorkflowOutputField{Name: value.Name, Description: value.Description})
	}
	return out
}
