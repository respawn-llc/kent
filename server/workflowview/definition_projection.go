package workflowview

import (
	"context"
	"errors"

	"core/server/workflow"
	"core/server/workflowstore"
	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	"core/shared/runtimeids"
	"core/shared/textutil"
)

type DefinitionProjection struct {
	store   *workflowstore.Store
	catalog workflow.TargetAgentCatalog
}

type definitionSnapshot struct {
	domain    workflow.Definition
	api       *pb.WorkflowDefinition
	nodeKinds map[string]workflow.NodeKind
}

func NewDefinitionProjection(store *workflowstore.Store) (*DefinitionProjection, error) {
	if store == nil {
		return nil, errors.New("workflow store is required")
	}
	return &DefinitionProjection{store: store, catalog: store.TargetAgentCatalog()}, nil
}

func (p *DefinitionProjection) GetDefinition(ctx context.Context, workflowID runtimeids.WorkflowID) (*pb.WorkflowDefinition, map[string]workflow.NodeKind, error) {
	snapshot, err := p.snapshot(ctx, workflowID)
	if err != nil {
		return nil, nil, err
	}
	return snapshot.api, snapshot.nodeKinds, nil
}

func (p *DefinitionProjection) CurrentNodesByTask(ctx context.Context, taskIDs []workflow.TaskID) (map[workflow.TaskID][]workflow.CurrentNode, error) {
	if p == nil || p.store == nil {
		return nil, errors.New("definition projection is required")
	}
	return p.store.ListCurrentNodesByTask(ctx, taskIDs)
}

func workflowNodesByID(def *pb.WorkflowDefinition) map[string]*pb.WorkflowNode {
	nodes := make(map[string]*pb.WorkflowNode, len(def.Nodes))
	for _, node := range def.Nodes {
		nodes[node.Id] = node
	}
	return nodes
}

func (p *DefinitionProjection) snapshot(ctx context.Context, workflowID runtimeids.WorkflowID) (definitionSnapshot, error) {
	if p == nil {
		return definitionSnapshot{}, errors.New("definition projection is required")
	}
	if workflowID.IsZero() {
		return definitionSnapshot{}, errors.New("workflow_id is required")
	}
	domain, record, err := p.store.GetDefinition(ctx, workflowID)
	if err != nil {
		return definitionSnapshot{}, err
	}
	api, nodeKinds, err := ProjectDefinition(domain, record, p.catalog)
	return definitionSnapshot{domain: domain, api: api, nodeKinds: nodeKinds}, err
}

// ProjectDefinition is the canonical pure domain-to-API workflow projection.
func ProjectDefinition(def workflow.Definition, record workflowstore.WorkflowRecord, catalogs ...workflow.TargetAgentCatalog) (*pb.WorkflowDefinition, map[string]workflow.NodeKind, error) {
	projectedRecord, err := ProjectRecord(record)
	if err != nil {
		return nil, nil, err
	}
	api := &pb.WorkflowDefinition{Workflow: projectedRecord}
	groupKeyByID := make(map[string]string, len(def.NodeGroups))
	for _, group := range def.NodeGroups {
		groupKeyByID[group.ID] = string(group.Key)
		api.NodeGroups = append(api.NodeGroups, &pb.WorkflowNodeGroup{
			GroupId:     group.ID,
			WorkflowId:  group.WorkflowID.String(),
			GroupKey:    string(group.Key),
			DisplayName: group.DisplayName,
			SortOrder:   int32(group.SortOrder),
		})
	}
	nodeKinds := make(map[string]workflow.NodeKind, len(def.Nodes))
	for _, node := range def.Nodes {
		identity := node.Identity()
		var scriptPath *string
		if value, present := workflow.NodeScriptPath(node).Value(); present {
			scriptPath = &value
		}
		joinProviders := workflow.NodeJoinInputProviders(node)
		projectedJoinProviders := make([]*pb.JoinInputProvider, 0, len(joinProviders))
		for _, provider := range joinProviders {
			projectedJoinProviders = append(projectedJoinProviders, &pb.JoinInputProvider{
				InputName:      provider.InputName,
				ProviderEdgeId: string(provider.ProviderEdgeID),
			})
		}
		nodeID := string(identity.ID)
		groupKey := ""
		if identity.GroupID != nil {
			groupKey = groupKeyByID[*identity.GroupID]
		}
		kind, err := protoapi.WorkflowNodeKind.Encode(string(node.Kind()))
		if err != nil {
			return nil, nil, err
		}
		var completion *pb.CompletionMode
		if value := workflow.NodeCompletionMode(node); value != "" {
			mode, err := protoapi.WorkflowCompletionMode.Encode(value)
			if err != nil {
				return nil, nil, err
			}
			completion = &mode
		}
		api.Nodes = append(api.Nodes, &pb.WorkflowNode{
			Id:                 nodeID,
			WorkflowId:         identity.WorkflowID.String(),
			Key:                string(identity.Key),
			Kind:               kind,
			DisplayName:        identity.DisplayName,
			GroupId:            textutil.Pointer(identity.GroupID),
			GroupKey:           groupKey,
			SubagentRole:       textutil.OptionalExactString(workflow.NodeSubagentRole(node)),
			CompletionMode:     completion,
			ScriptPath:         scriptPath,
			JoinInputProviders: projectedJoinProviders,
		})
		nodeKinds[nodeID] = node.Kind()
	}
	for _, group := range def.TransitionGroups {
		api.TransitionGroups = append(api.TransitionGroups, &pb.WorkflowTransitionGroup{
			Id:           string(group.ID),
			WorkflowId:   group.WorkflowID.String(),
			SourceNodeId: string(group.SourceNodeID),
			TransitionId: string(group.TransitionID),
			DisplayName:  group.DisplayName,
			Description:  group.Description,
		})
	}
	for _, edge := range def.Edges {
		parameters := make([]*pb.Parameter, 0, len(edge.Parameters))
		for _, parameter := range edge.Parameters {
			purpose, err := protoapi.WorkflowParameterPurpose.Encode(string(workflow.CanonicalParameterPurpose(parameter.Purpose)))
			if err != nil {
				return nil, nil, err
			}
			parameters = append(parameters, &pb.Parameter{Key: parameter.Key, Description: parameter.Description, Purpose: purpose})
		}
		requirements := make([]*pb.OutputRequirement, 0, len(edge.OutputRequirements))
		for _, requirement := range edge.OutputRequirements {
			requirements = append(requirements, &pb.OutputRequirement{FieldName: requirement.FieldName})
		}
		assignee, assigneeErr := protoapi.WorkflowAssigneeSelection.Encode(string(workflow.CanonicalAssigneeSelection(edge.AssigneeSelection)))
		thinking, thinkingErr := protoapi.WorkflowThinkingSelection.Encode(string(workflow.CanonicalThinkingSelection(edge.ThinkingSelection)))
		contextMode, contextErr := protoapi.WorkflowContextMode.Encode(string(edge.ContextMode))
		source, sourceErr := apiContextSource(edge.ContextSource)
		if err := errors.Join(assigneeErr, thinkingErr, contextErr, sourceErr); err != nil {
			return nil, nil, err
		}
		api.Edges = append(api.Edges, &pb.WorkflowEdge{
			Id:                 string(edge.ID),
			WorkflowId:         edge.WorkflowID.String(),
			TransitionGroupId:  string(edge.TransitionGroupID),
			Key:                string(edge.Key),
			TargetNodeId:       string(edge.TargetNodeID),
			AssigneeSelection:  assignee,
			ThinkingSelection:  thinking,
			RequiresApproval:   edge.RequiresApproval,
			ContextMode:        contextMode,
			ContextSource:      source,
			PromptTemplate:     edge.PromptTemplate,
			Parameters:         parameters,
			InputBindings:      InputBindings(edge.InputBindings),
			OutputRequirements: requirements,
		})
	}
	api.DerivedWiring, err = DerivedWiring(def, catalogs...)
	return api, nodeKinds, err
}

func ProjectRecord(record workflowstore.WorkflowRecord) (*pb.WorkflowRecord, error) {
	policy, err := projectExecutionTargetPolicy(record.ExecutionTargetPolicy)
	if err != nil {
		return nil, err
	}
	result := &pb.WorkflowRecord{
		Id: record.ID.String(), Name: record.Name, Description: record.Description,
		Version: record.Version, ExecutionTargetPolicy: policy,
	}
	if record.ProjectLink != nil {
		result.ProjectLink = &pb.WorkflowListProjectLink{Default: record.ProjectLink.Default}
	}
	return result, nil
}

func projectExecutionTargetPolicy(policy workflow.ExecutionTargetPolicy) (*pb.ExecutionTargetConfiguration, error) {
	canonical := policy.Canonical()
	var customRef *string
	if canonical.CustomRef != nil {
		value := *canonical.CustomRef
		customRef = &value
	}
	mode, err := protoapi.WorkflowExecutionTargetMode.Encode(string(canonical.Mode))
	if err != nil {
		return nil, err
	}
	return &pb.ExecutionTargetConfiguration{
		Mode:      mode,
		CustomRef: customRef,
	}, nil
}
