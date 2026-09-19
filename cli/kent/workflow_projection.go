package main

import (
	"errors"

	"core/server/workflow"
	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

func workflowRecordForCLI(record *pb.WorkflowRecord) (workflowRecordJSON, error) {
	id, err := runtimeids.ParseWorkflowID(record.GetId())
	if err != nil {
		return workflowRecordJSON{}, err
	}
	mode, err := protoapi.WorkflowExecutionTargetMode.Decode(record.ExecutionTargetPolicy.GetMode())
	if err != nil {
		return workflowRecordJSON{}, err
	}
	result := workflowRecordJSON{
		ID: id, Name: record.Name, Description: record.Description, Version: record.Version,
		ExecutionTargetPolicy: workflowExecutionTargetPolicyJSON{Mode: serverapi.WorkflowExecutionTargetMode(mode), CustomRef: record.ExecutionTargetPolicy.CustomRef},
	}
	if record.ProjectLink != nil {
		result.ProjectLink = &workflowListProjectLinkJSON{Default: record.ProjectLink.Default}
	}
	return result, nil
}

func workflowRecordsForCLI(records []*pb.WorkflowRecord) ([]workflowRecordJSON, error) {
	projected := make([]workflowRecordJSON, len(records))
	for i := range records {
		record, err := workflowRecordForCLI(records[i])
		if err != nil {
			return nil, err
		}
		projected[i] = record
	}
	return projected, nil
}

func workflowDeleteResponseForCLI(response *pb.DeleteSuccess) (workflowDeleteJSON, error) {
	impact, err := workflowDeleteImpactForCLI(response.Impact)
	if err != nil {
		return workflowDeleteJSON{}, err
	}
	result := workflowDeleteJSON{Deleted: response.Deleted, Impact: impact}
	for _, blocker := range response.Blockers {
		result.Blockers = append(result.Blockers, workflowDeleteBlockerJSON{Code: blocker.Code, Message: blocker.Message, Count: blocker.Count})
	}
	return result, nil
}

func workflowDeleteImpactForCLI(impact *pb.DeleteImpact) (workflowDeleteImpactJSON, error) {
	id, err := runtimeids.ParseWorkflowID(impact.GetWorkflowId())
	if err != nil {
		return workflowDeleteImpactJSON{}, err
	}
	return workflowDeleteImpactJSON{
		WorkflowID: id, Version: impact.Version, ProjectCount: impact.ProjectCount, LinkCount: impact.LinkCount,
		DefaultReplacementProjectCount: impact.DefaultReplacementProjectCount, TaskCount: impact.TaskCount,
		CurrentNodeCount: impact.CurrentNodeCount, PendingApprovalCount: impact.PendingApprovalCount, BlockedTaskCount: impact.BlockedTaskCount,
	}, nil
}

func workflowTaskSummaryForCLI(summary serverapi.WorkflowTaskSummary) (serverapi.WorkflowTaskSummary, error) {
	if summary.WorkflowID.IsZero() {
		return serverapi.WorkflowTaskSummary{}, errors.New("workflow_id is required")
	}
	return summary, nil
}

func workflowDefinitionForCLI(definition *pb.WorkflowDefinition) (workflowDefinitionJSON, error) {
	workflow, err := workflowRecordForCLI(definition.Workflow)
	if err != nil {
		return workflowDefinitionJSON{}, err
	}
	document, err := workflowGraphDocumentFromDefinition(definition)
	if err != nil {
		return workflowDefinitionJSON{}, err
	}
	projected := workflowDefinitionJSON{
		Workflow: workflow, Nodes: make([]workflowNodeJSON, 0, len(definition.Nodes)),
		TransitionGroups: make([]workflowTransitionGroupJSON, 0, len(definition.TransitionGroups)),
		Edges:            make([]workflowEdgeJSON, 0, len(definition.Edges)),
	}
	for _, group := range definition.NodeGroups {
		id, err := runtimeids.ParseWorkflowID(group.WorkflowId)
		if err != nil {
			return workflowDefinitionJSON{}, err
		}
		projected.NodeGroups = append(projected.NodeGroups, workflowNodeGroupJSON{
			GroupID: group.GroupId, WorkflowID: id, GroupKey: group.GroupKey, DisplayName: group.DisplayName, SortOrder: int(group.SortOrder),
		})
	}
	for index, node := range definition.Nodes {
		id, err := runtimeids.ParseWorkflowID(node.WorkflowId)
		if err != nil {
			return workflowDefinitionJSON{}, err
		}
		projected.Nodes = append(projected.Nodes, workflowNodeJSON{workflowGraphDocumentNode: document.Graph.Nodes[index], WorkflowID: id, GroupKey: node.GroupKey})
	}
	for index, group := range definition.TransitionGroups {
		id, err := runtimeids.ParseWorkflowID(group.WorkflowId)
		if err != nil {
			return workflowDefinitionJSON{}, err
		}
		projected.TransitionGroups = append(projected.TransitionGroups, workflowTransitionGroupJSON{
			workflowGraphDocumentTransition: document.Graph.TransitionGroups[index], WorkflowID: id,
		})
	}
	for index, edge := range definition.Edges {
		id, err := runtimeids.ParseWorkflowID(edge.WorkflowId)
		if err != nil {
			return workflowDefinitionJSON{}, err
		}
		value := workflowEdgeJSON{workflowGraphDocumentEdge: document.Graph.Edges[index], WorkflowID: id, InputBindings: workflowInputBindingsForCLI(edge.InputBindings)}
		for _, requirement := range edge.OutputRequirements {
			value.OutputRequirements = append(value.OutputRequirements, workflowOutputRequirementJSON{FieldName: requirement.FieldName})
		}
		projected.Edges = append(projected.Edges, value)
	}
	projected.DerivedWiring, err = workflowWiringForCLI(definition.DerivedWiring)
	return projected, err
}

func projectWorkflowLinkForCLI(link *pb.ProjectWorkflowLink) (projectWorkflowLinkJSON, error) {
	id, err := runtimeids.ParseWorkflowID(link.GetWorkflowId())
	if err != nil {
		return projectWorkflowLinkJSON{}, err
	}
	return projectWorkflowLinkJSON{ID: link.Id, ProjectID: link.ProjectId, WorkflowID: id, Default: link.Default}, nil
}

func workflowUnlinkForCLI(response *pb.UnlinkProjectSuccess) workflowUnlinkJSON {
	result := workflowUnlinkJSON{LinkID: response.LinkId, Unlinked: response.Unlinked}
	for _, blocker := range response.Blockers {
		value := workflowUnlinkBlockerJSON{Code: blocker.Code, Message: blocker.Message, Count: blocker.Count}
		for _, task := range blocker.Tasks {
			value.Tasks = append(value.Tasks, workflowUnlinkTaskJSON{TaskID: task.TaskId, ShortID: task.ShortId, Title: task.Title})
		}
		result.Blockers = append(result.Blockers, value)
	}
	return result
}

func workflowValidationForCLI(response *pb.ValidateResponse) (workflowValidationJSON, error) {
	diagnostics, err := protoapi.WorkflowValidationErrorsForJSON(response.Errors)
	if err != nil {
		return workflowValidationJSON{}, err
	}
	return workflowValidationJSON{Valid: response.Valid, Errors: workflowValidationErrorsForCLI(diagnostics)}, nil
}

func workflowValidationErrorsForCLI(errors []serverapi.WorkflowValidationError) []serverapi.WorkflowValidationError {
	projected := append([]serverapi.WorkflowValidationError(nil), errors...)
	for i := range projected {
		projected[i].Message, _ = workflowValidationErrorMessageForCLI(projected[i])
	}
	return projected
}

func workflowValidationErrorMessageForCLI(err serverapi.WorkflowValidationError) (string, bool) {
	switch err.Code {
	case string(workflow.CodeSessionSourceCannotOwnSession):
		return "This prompt references a source node that cannot own a Session. Use an agent source node for this placeholder.", true
	case string(workflow.CodeSessionTransitionMissing):
		return "This prompt references an unknown transition. Correct the transition key or define the transition before using this placeholder.", true
	case string(workflow.CodeSessionTransitionNotGuaranteed):
		return "This prompt references a transition that is not guaranteed to run before the prompt. Reference a transition that runs on every incoming path.", true
	case string(workflow.CodeSessionTransitionAmbiguous):
		return "This prompt references more than one matching transition. Make the Session-producing transition unambiguous before using this placeholder.", true
	default:
		return err.Message, false
	}
}

func workflowTaskDetailForCLI(detail serverapi.WorkflowTaskDetail) (serverapi.WorkflowTaskDetail, error) {
	projected := detail
	summary, err := workflowTaskSummaryForCLI(detail.Summary)
	if err != nil {
		return serverapi.WorkflowTaskDetail{}, err
	}
	projected.Summary = summary
	return projected, nil
}
