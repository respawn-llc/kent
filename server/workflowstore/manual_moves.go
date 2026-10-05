package workflowstore

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

	"core/server/metadata/sqlitegen"
	"core/server/workflow"
	"core/shared/runtimeids"
)

type ManualMovePreparation struct {
	request                 ManualMoveRequest
	target                  workflow.Node
	choice                  *ManualMoveTransitionChoice
	currentNodes            []workflow.CurrentNode
	noOp                    bool
	requiresExecutionTarget bool
	completedOrigin         bool
	workflowVersion         int64
}

func (p ManualMovePreparation) RequiresExecutionTarget() bool {
	return p.requiresExecutionTarget
}

func (p ManualMovePreparation) ReopensCompletedTask() bool {
	return p.requiresExecutionTarget && p.completedOrigin
}

func (p ManualMovePreparation) IsNoOp() bool {
	return p.noOp
}

func (p ManualMovePreparation) CurrentNodes() []workflow.CurrentNode {
	return append([]workflow.CurrentNode(nil), p.currentNodes...)
}

func (p ManualMovePreparation) TaskID() workflow.TaskID {
	return p.request.TaskID
}

func (s *Store) PrepareManualMove(ctx context.Context, req ManualMoveRequest) (preparation ManualMovePreparation, resultErr error) {
	defer func() {
		reportWorkflowInvariantError(s.invariantPolicy, resultErr)
	}()
	if err := validateCommentarySize(req.Commentary); err != nil {
		return ManualMovePreparation{}, err
	}
	req.Commentary = strings.TrimSpace(req.Commentary)
	task, err := s.queries.GetTask(ctx, string(req.TaskID))
	if err != nil {
		return ManualMovePreparation{}, err
	}
	record, err := s.queries.GetWorkflow(ctx, task.WorkflowID)
	if err != nil {
		return ManualMovePreparation{}, err
	}
	defer func() {
		preparation.workflowVersion = record.Version
	}()
	preview, err := s.resolveManualMove(ctx, s.queries, req)
	if err != nil {
		return ManualMovePreparation{}, err
	}
	switch preview.Outcome {
	case ManualMovePreviewOutcomeNoOp:
		return ManualMovePreparation{request: req, noOp: true, currentNodes: preview.CurrentNodes}, nil
	case ManualMovePreviewOutcomeDirect:
		target, err := currentNodeDefinitionNodeFromTask(ctx, s.queries, req.TaskID, req.TargetNodeID)
		if err != nil {
			return ManualMovePreparation{}, err
		}
		return ManualMovePreparation{request: req, target: target, currentNodes: preview.CurrentNodes}, nil
	case ManualMovePreviewOutcomeTransition:
		if len(preview.Choices) != 1 {
			return ManualMovePreparation{}, ErrManualMoveTransitionSelectionRequired
		}
		target, err := currentNodeDefinitionNodeFromTask(ctx, s.queries, req.TaskID, req.TargetNodeID)
		if err != nil {
			return ManualMovePreparation{}, err
		}
		choice := preview.Choices[0]
		currentNodes, err := s.listTaskCurrentNodes(ctx, s.queries, req.TaskID)
		if err != nil {
			return ManualMovePreparation{}, err
		}
		completedOrigin, err := taskCurrentNodesAreCompleted(ctx, s.queries, req.TaskID, currentNodes)
		if err != nil {
			return ManualMovePreparation{}, err
		}
		return ManualMovePreparation{
			request:                 req,
			target:                  target,
			choice:                  &choice,
			requiresExecutionTarget: executableNodeKind(target.Kind()),
			currentNodes:            currentNodes,
			completedOrigin:         completedOrigin,
		}, nil
	case ManualMovePreviewOutcomeBlocked:
		return ManualMovePreparation{}, manualMovePreviewBlockerError(preview.Blocker)
	default:
		return ManualMovePreparation{}, fmt.Errorf("manual move preview cannot be prepared from outcome %q", preview.Outcome)
	}
}

func taskCurrentNodesAreCompleted(ctx context.Context, q *sqlitegen.Queries, taskID workflow.TaskID, nodes []workflow.CurrentNode) (bool, error) {
	for _, current := range nodes {
		node, err := currentNodeDefinitionNodeFromTask(ctx, q, taskID, current.Reference.NodeID)
		if err != nil {
			return false, err
		}
		if node.Kind() == workflow.NodeKindTerminal {
			return true, nil
		}
	}
	return false, nil
}

func currentNodeDefinitionNodeFromTask(ctx context.Context, q *sqlitegen.Queries, taskID workflow.TaskID, nodeID workflow.NodeID) (workflow.Node, error) {
	task, err := q.GetTask(ctx, string(taskID))
	if err != nil {
		return nil, err
	}
	definition, _, err := workflowDefinitionFromQueries(ctx, q, runtimeids.WorkflowID(task.WorkflowID))
	if err != nil {
		return nil, err
	}
	return currentNodeDefinitionNode(definition, nodeID)
}

func applyManualMoveCommentary(targets []workflow.CurrentNode, commentary string) {
	if commentary == "" {
		return
	}
	for index := range targets {
		if targets[index].CurrentInputValues == nil {
			targets[index].CurrentInputValues = make(map[string]string)
		}
		targets[index].CurrentInputValues[workflow.RuntimePromptParameterCommentary] = commentary
	}
}

func manualMovePreviewBlockerError(blocker ManualMoveBlocker) error {
	switch blocker {
	case ManualMoveBlockerNoSourcePosition:
		return ErrManualMoveNoSourcePosition
	case ManualMoveBlockerContextSessionUnavailable:
		return ErrManualMoveTransitionNotUsable
	case ManualMoveBlockerParallelBranchRequiresFanOut, ManualMoveBlockerNoUsableTransition:
		return ErrManualMoveTransitionNotUsable
	default:
		return fmt.Errorf("manual move is blocked: %s", blocker)
	}
}

func (s *Store) materializeManualMoveTargets(
	ctx context.Context,
	q *sqlitegen.Queries,
	definition workflow.Definition,
	choice ManualMoveTransitionChoice,
	currentNodes []workflow.CurrentNode,
	environment manualMoveValueEnvironment,
	submitted map[workflow.ModelKey]map[string]string,
) (
	[]workflow.CurrentNode,
	[]legacyContinuationSourceFallbackDetail,
	error,
) {
	if len(choice.Edges) == 0 {
		return nil, nil, ErrManualMoveTransitionNotUsable
	}
	targets := make([]workflow.CurrentNode, 0, len(choice.Edges))
	legacyFallbacks := make([]legacyContinuationSourceFallbackDetail, 0, len(choice.Edges))
	contextSource := manualMoveContextCurrentNode(currentNodes)
	priorValues := manualMoveBasePriorValues(currentNodes)
	for _, edge := range choice.Edges {
		target, err := currentNodeDefinitionNode(definition, edge.TargetNodeID)
		if err != nil {
			return nil, nil, err
		}
		var branchKey *workflow.TransitionBranchKey
		if len(choice.Edges) > 1 {
			value := workflow.TransitionBranchKey(strings.TrimSpace(string(edge.Key)))
			if value == "" {
				return nil, nil, errors.New("manual move fan-out branch key is required")
			}
			branchKey = &value
		}
		materializedTarget, err := materializeTransitionTargetCurrentNode(ctx, q, transitionTargetMaterializationRequest{
			Definition:                      definition,
			Edge:                            edge,
			Source:                          choice.SourceNode,
			Target:                          target,
			Catalog:                         s.roleResolver,
			ResolveRetainedSessionSelection: s.resolveRetainedSessionSelection,
			ContextTaskID:                   currentNodes[0].Reference.TaskID,
			ContextCurrentSource:            contextSource,
			ManualMoveContext:               true,
			PriorValues:                     priorValues,
			Value: func(providerNode, _ workflow.ModelKey, outputName string) (string, bool) {
				value := manualMoveSubmittedOrResolved(providerNode, outputName, environment, submitted)
				if value == nil {
					return "", false
				}
				return *value, true
			},
			TransitionBranchKey: branchKey,
		})
		if err != nil {
			return nil, nil, err
		}
		if materializedTarget.LegacyFallback != nil {
			legacyFallbacks = append(legacyFallbacks, *materializedTarget.LegacyFallback)
		}
		targets = append(targets, materializedTarget.CurrentNode)
	}
	if len(targets) > 1 {
		if err := validateFanoutTargets(currentNodes[0].Reference.TaskID, targets); err != nil {
			return nil, nil, err
		}
	}
	return targets, legacyFallbacks, nil
}

func sameCurrentNodeMaterialization(left, right workflow.CurrentNode) bool {
	return left.Reference.Equal(right.Reference) &&
		sameOptionalEdgeID(left.EnteredByEdgeID, right.EnteredByEdgeID) &&
		maps.Equal(left.CurrentInputValues, right.CurrentInputValues) &&
		sameMaterializedPriorValues(left.PriorValues, right.PriorValues) &&
		sameOptionalSessionID(left.SessionID, right.SessionID) &&
		sameMaterializedContinuationSource(left.ContinuationSource, right.ContinuationSource) &&
		sameAgentExecutionSelection(left.AgentExecutionSelection, right.AgentExecutionSelection)
}

func sameOptionalEdgeID(left, right *workflow.EdgeID) bool {
	return (left == nil && right == nil) ||
		(left != nil && right != nil && *left == *right)
}

func sameMaterializedPriorValues(left, right workflow.MaterializedPriorValues) bool {
	return maps.EqualFunc(
		left.TransitionParameters,
		right.TransitionParameters,
		func(left, right map[string]string) bool {
			return maps.Equal(left, right)
		},
	)
}

func sameAgentExecutionSelection(left, right *workflow.AgentExecutionSelection) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Assignee == right.Assignee &&
		left.Origin == right.Origin &&
		((left.Thinking == nil && right.Thinking == nil) ||
			(left.Thinking != nil && right.Thinking != nil && *left.Thinking == *right.Thinking))
}

func manualMoveBasePriorValues(currentNodes []workflow.CurrentNode) workflow.MaterializedPriorValues {
	priorValues := workflow.MaterializedPriorValues{}
	for _, currentNode := range currentNodes {
		for transitionKey, values := range currentNode.PriorValues.TransitionParameters {
			for parameterName, value := range values {
				if _, exists := priorValues.TransitionParameters[transitionKey][parameterName]; !exists {
					priorValues.SetTransitionParameter(transitionKey, parameterName, value)
				}
			}
		}
	}
	return priorValues
}

func manualMoveContextCurrentNode(currentNodes []workflow.CurrentNode) *workflow.CurrentNode {
	if len(currentNodes) != 1 || currentNodes[0].Reference.IsBranchScoped() {
		return nil
	}
	current := currentNodes[0]
	return &current
}
