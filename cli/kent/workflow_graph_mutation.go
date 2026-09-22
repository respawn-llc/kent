package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"core/shared/apicontract"
	protoapi "core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	"core/shared/runtimeids"
)

type workflowGraphDraftMutation[T any] func(*pb.GraphDraft) (*pb.GraphDraft, T, error)

type workflowGraphMutationResult struct{ Version int64 }

func workflowGraphMutationBlocked(workflowID runtimeids.WorkflowID, blockers []*pb.GraphSaveBlocker) error {
	if len(blockers) == 0 {
		return fmt.Errorf(
			"Workflow %s graph mutation cannot be saved by this high-level command; use `kent workflow graph apply`",
			workflowID,
		)
	}
	codes := make([]string, 0, len(blockers))
	for _, blocker := range blockers {
		codes = append(codes, blocker.Code)
	}
	return fmt.Errorf(
		"Workflow %s graph mutation was blocked (%s); use `kent workflow graph apply`",
		workflowID,
		strings.Join(codes, ", "),
	)
}

func runWorkflowGraphMutation[T any](
	ctx context.Context,
	remote apicontract.WorkflowService,
	workflowID runtimeids.WorkflowID,
	mutate workflowGraphDraftMutation[T],
) (T, workflowGraphMutationResult, error) {
	var zero T
	current, err := resolveWorkflowDefinition(ctx, remote, workflowID)
	if err != nil {
		return zero, workflowGraphMutationResult{}, err
	}
	graph, value, err := mutate(protoapi.WorkflowGraphDraftFromDefinition(current))
	if err != nil {
		return zero, workflowGraphMutationResult{}, err
	}
	preview, err := previewWorkflowGraphDraft(ctx, remote, current, graph)
	if err != nil {
		return zero, workflowGraphMutationResult{}, err
	}
	if err := protoapi.Validate(preview.Response); err != nil {
		return zero, workflowGraphMutationResult{}, fmt.Errorf("validate Workflow graph save preview: %w", err)
	}
	if preview.Response.ConfirmationRequired || len(preview.Response.Blockers) != 0 || !preview.Response.CanSave {
		return zero, workflowGraphMutationResult{}, workflowGraphMutationBlocked(workflowID, preview.Response.Blockers)
	}
	response, err := remote.SaveWorkflowGraph(ctx, &pb.GraphSaveRequest{
		WorkflowId:      current.Workflow.Id,
		ExpectedVersion: current.Workflow.Version,
		Graph:           preview.Graph,
	})
	if err != nil {
		return zero, workflowGraphMutationResult{}, err
	}
	if err := protoapi.Validate(response); err != nil {
		return zero, workflowGraphMutationResult{}, fmt.Errorf("validate Workflow graph save response: %w", err)
	}
	if !response.Saved || response.ConfirmationRequired || len(response.Blockers) != 0 || !response.CanSave {
		return zero, workflowGraphMutationResult{}, workflowGraphMutationBlocked(workflowID, response.Blockers)
	}
	return value, workflowGraphMutationResult{Version: response.CurrentVersion}, nil
}

type workflowNodeUpdateDraftMutation struct {
	NodeKey        string
	Key            *string
	Kind           *string
	DisplayName    *string
	SubagentRole   workflowStringMutation
	CompletionMode workflowStringMutation
	ScriptPath     workflowOptionalStringMutation
}

type workflowStringMutation struct {
	Set   bool
	Value string
}

type workflowOptionalStringMutation struct {
	Set   bool
	Value *string
}

type workflowGraphMutationUsageError struct {
	err error
}

func (e workflowGraphMutationUsageError) Error() string {
	return e.err.Error()
}

func (e workflowGraphMutationUsageError) Unwrap() error {
	return e.err
}

func applyWorkflowGraphMutationValue[T any](target *T, value *T) {
	if value != nil {
		*target = *value
	}
}

type workflowEdgeDraftMutationResult struct {
	Edge          *pb.GraphDraftEdge
	Group         *pb.GraphDraftTransitionGroup
	TargetNodeKey string
}

type workflowEdgeAddDraftMutation struct {
	SourceNodeKey         string
	TransitionID          string
	TransitionDescription workflowStringMutation
	NewTransitionGroupID  string
	Edge                  *pb.GraphDraftEdge
	TargetNodeKey         string
}

type workflowEdgeUpdateDraftMutation struct {
	EdgeID                  string
	TransitionID            *string
	TransitionDisplayName   *string
	TransitionDescription   workflowStringMutation
	EdgeKey                 *string
	TargetNodeKey           *string
	ContextMode             *string
	ContextSource           *pb.ContextSource
	RequiresApproval        *bool
	PromptTemplate          workflowStringMutation
	AssigneeSelection       *string
	ThinkingSelection       *string
	TargetAssigneeParameter *pb.Parameter
	TargetThinkingParameter *pb.Parameter
	OrdinaryParameters      *[]*pb.Parameter
}

func addWorkflowNodeDraftMutation(node *pb.GraphDraftNode) workflowGraphDraftMutation[*pb.GraphDraftNode] {
	return func(graph *pb.GraphDraft) (*pb.GraphDraft, *pb.GraphDraftNode, error) {
		graph.Nodes = append(graph.Nodes, node)
		return graph, node, nil
	}
}

func updateWorkflowNodeDraftMutation(update workflowNodeUpdateDraftMutation) workflowGraphDraftMutation[*pb.GraphDraftNode] {
	return func(graph *pb.GraphDraft) (*pb.GraphDraft, *pb.GraphDraftNode, error) {
		nodeKey := strings.TrimSpace(update.NodeKey)
		for index := range graph.Nodes {
			if graph.Nodes[index].Key != nodeKey {
				continue
			}
			applyWorkflowGraphMutationValue(&graph.Nodes[index].Key, update.Key)
			if update.Kind != nil {
				kind, err := protoapi.WorkflowNodeKind.Encode(*update.Kind)
				if err != nil {
					return nil, nil, err
				}
				graph.Nodes[index].Kind = kind
			}
			applyWorkflowGraphMutationValue(&graph.Nodes[index].DisplayName, update.DisplayName)
			if update.SubagentRole.Set {
				graph.Nodes[index].SubagentRole = nil
				if update.SubagentRole.Value != "" {
					graph.Nodes[index].SubagentRole = &update.SubagentRole.Value
				}
			}
			if update.CompletionMode.Set {
				graph.Nodes[index].CompletionMode = nil
				if update.CompletionMode.Value != "" {
					mode, err := protoapi.WorkflowCompletionMode.Encode(update.CompletionMode.Value)
					if err != nil {
						return nil, nil, err
					}
					graph.Nodes[index].CompletionMode = &mode
				}
			}
			if update.ScriptPath.Set {
				graph.Nodes[index].ScriptPath = update.ScriptPath.Value
			}
			return graph, graph.Nodes[index], nil
		}
		return &pb.GraphDraft{}, &pb.GraphDraftNode{}, fmt.Errorf("workflow node key %q not found", nodeKey)
	}
}

func addWorkflowEdgeDraftMutation(add workflowEdgeAddDraftMutation) workflowGraphDraftMutation[workflowEdgeDraftMutationResult] {
	return func(graph *pb.GraphDraft) (*pb.GraphDraft, workflowEdgeDraftMutationResult, error) {
		source, err := findWorkflowGraphDraftNodeByKey(graph, add.SourceNodeKey)
		if err != nil {
			return &pb.GraphDraft{}, workflowEdgeDraftMutationResult{}, err
		}
		target, err := findWorkflowGraphDraftNodeByKey(graph, add.TargetNodeKey)
		if err != nil {
			return &pb.GraphDraft{}, workflowEdgeDraftMutationResult{}, err
		}
		transitionID := strings.TrimSpace(add.TransitionID)
		groupIndex := -1
		for index := range graph.TransitionGroups {
			group := graph.TransitionGroups[index]
			if group.SourceNodeId == source.Id && group.TransitionId == transitionID {
				groupIndex = index
				break
			}
		}
		if groupIndex < 0 {
			graph.TransitionGroups = append(graph.TransitionGroups, &pb.GraphDraftTransitionGroup{
				Id:           add.NewTransitionGroupID,
				SourceNodeId: source.Id,
				TransitionId: transitionID,
				DisplayName:  workflowDisplayNameFromKey(transitionID),
				Description:  add.TransitionDescription.Value,
			})
			groupIndex = len(graph.TransitionGroups) - 1
		} else if add.TransitionDescription.Set {
			graph.TransitionGroups[groupIndex].Description = add.TransitionDescription.Value
		}
		add.Edge.TransitionGroupId = graph.TransitionGroups[groupIndex].Id
		add.Edge.TargetNodeId = target.Id
		add.Edge.Parameters = append([]*pb.Parameter(nil), add.Edge.Parameters...)
		graph.Edges = append(graph.Edges, add.Edge)
		return graph, workflowEdgeDraftMutationResult{
			Edge:          add.Edge,
			Group:         graph.TransitionGroups[groupIndex],
			TargetNodeKey: target.Key,
		}, nil
	}
}

func updateWorkflowEdgeDraftMutation(update workflowEdgeUpdateDraftMutation) workflowGraphDraftMutation[workflowEdgeDraftMutationResult] {
	return func(graph *pb.GraphDraft) (*pb.GraphDraft, workflowEdgeDraftMutationResult, error) {
		edgeIndex := -1
		edgeID := strings.TrimSpace(update.EdgeID)
		for index := range graph.Edges {
			if graph.Edges[index].Id == edgeID {
				edgeIndex = index
				break
			}
		}
		if edgeIndex < 0 {
			return &pb.GraphDraft{}, workflowEdgeDraftMutationResult{}, fmt.Errorf("workflow edge %q not found", edgeID)
		}
		groupIndex := -1
		for index := range graph.TransitionGroups {
			if graph.TransitionGroups[index].Id == graph.Edges[edgeIndex].TransitionGroupId {
				groupIndex = index
				break
			}
		}
		if groupIndex < 0 {
			return &pb.GraphDraft{}, workflowEdgeDraftMutationResult{}, fmt.Errorf(
				"workflow transition group %q not found",
				graph.Edges[edgeIndex].TransitionGroupId,
			)
		}

		group := graph.TransitionGroups[groupIndex]
		if update.TransitionID != nil {
			group.TransitionId = *update.TransitionID
		}
		if update.TransitionDisplayName != nil {
			group.DisplayName = *update.TransitionDisplayName
		} else if update.TransitionID != nil {
			group.DisplayName = workflowDisplayNameFromKey(*update.TransitionID)
		}
		if update.TransitionDescription.Set {
			group.Description = update.TransitionDescription.Value
		}

		edge := graph.Edges[edgeIndex]
		if update.AssigneeSelection != nil {
			value, err := protoapi.WorkflowAssigneeSelection.Encode(*update.AssigneeSelection)
			if err != nil {
				return nil, workflowEdgeDraftMutationResult{}, err
			}
			edge.AssigneeSelection = value
		}
		if update.ThinkingSelection != nil {
			value, err := protoapi.WorkflowThinkingSelection.Encode(*update.ThinkingSelection)
			if err != nil {
				return nil, workflowEdgeDraftMutationResult{}, err
			}
			edge.ThinkingSelection = value
		}
		if update.TargetAssigneeParameter != nil && edge.AssigneeSelection != pb.AssigneeSelection_WORKFLOW_ASSIGNEE_SELECTION_PREVIOUS_NODE {
			return &pb.GraphDraft{}, workflowEdgeDraftMutationResult{}, workflowGraphMutationUsageError{
				err: errors.New("target-assignee-param requires assignee selection previous_node"),
			}
		}
		if update.TargetThinkingParameter != nil && edge.ThinkingSelection != pb.ThinkingSelection_WORKFLOW_THINKING_SELECTION_PREVIOUS_NODE {
			return &pb.GraphDraft{}, workflowEdgeDraftMutationResult{}, workflowGraphMutationUsageError{
				err: errors.New("target-thinking-param requires thinking selection previous_node"),
			}
		}
		parameters, err := workflowEdgeParametersForUpdate(
			edge.Parameters,
			edge.AssigneeSelection,
			edge.ThinkingSelection,
			update.TargetAssigneeParameter,
			update.TargetThinkingParameter,
			nil,
			false,
		)
		if err != nil {
			return &pb.GraphDraft{}, workflowEdgeDraftMutationResult{}, workflowGraphMutationUsageError{err: err}
		}
		edge.Parameters = parameters
		applyWorkflowGraphMutationValue(&edge.Key, update.EdgeKey)
		targetNodeKey := ""
		if update.TargetNodeKey != nil {
			target, err := findWorkflowGraphDraftNodeByKey(graph, *update.TargetNodeKey)
			if err != nil {
				return &pb.GraphDraft{}, workflowEdgeDraftMutationResult{}, err
			}
			edge.TargetNodeId = target.Id
			targetNodeKey = target.Key
		} else {
			target, err := findWorkflowGraphDraftNodeByID(graph, edge.TargetNodeId)
			if err != nil {
				return &pb.GraphDraft{}, workflowEdgeDraftMutationResult{}, err
			}
			targetNodeKey = target.Key
		}
		if update.ContextMode != nil {
			mode, err := protoapi.WorkflowContextMode.Encode(*update.ContextMode)
			if err != nil {
				return nil, workflowEdgeDraftMutationResult{}, err
			}
			edge.ContextMode = mode
		}
		if update.ContextSource != nil {
			edge.ContextSource = update.ContextSource
		}
		applyWorkflowGraphMutationValue(&edge.RequiresApproval, update.RequiresApproval)
		if update.PromptTemplate.Set {
			edge.PromptTemplate = update.PromptTemplate.Value
		}
		if update.OrdinaryParameters != nil {
			parameters, err := workflowEdgeParametersForUpdate(
				edge.Parameters,
				edge.AssigneeSelection,
				edge.ThinkingSelection,
				nil,
				nil,
				*update.OrdinaryParameters,
				len(*update.OrdinaryParameters) == 0,
			)
			if err != nil {
				return &pb.GraphDraft{}, workflowEdgeDraftMutationResult{}, workflowGraphMutationUsageError{err: err}
			}
			edge.Parameters = parameters
		}
		return graph, workflowEdgeDraftMutationResult{
			Edge:          edge,
			Group:         group,
			TargetNodeKey: targetNodeKey,
		}, nil
	}
}

func findWorkflowGraphDraftNodeByKey(graph *pb.GraphDraft, key string) (*pb.GraphDraftNode, error) {
	nodeKey := strings.TrimSpace(key)
	for _, node := range graph.Nodes {
		if node.Key == nodeKey {
			return node, nil
		}
	}
	return &pb.GraphDraftNode{}, fmt.Errorf("workflow node key %q not found", nodeKey)
}

func findWorkflowGraphDraftNodeByID(graph *pb.GraphDraft, id string) (*pb.GraphDraftNode, error) {
	for _, node := range graph.Nodes {
		if node.Id == id {
			return node, nil
		}
	}
	return &pb.GraphDraftNode{}, fmt.Errorf("workflow node %q not found", id)
}
