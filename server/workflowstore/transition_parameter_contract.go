package workflowstore

import (
	"context"
	"database/sql"
	"errors"

	"core/server/metadata/sqlitegen"
	"core/server/workflow"
)

type transitionContractContextResolutionMode uint8

const (
	transitionContractContextResolutionRequired transitionContractContextResolutionMode = iota
	transitionContractContextResolutionDeferred
)

type transitionContractTargetSessionPolicyMode uint8

const (
	transitionContractTargetSessionPolicyNotNeeded transitionContractTargetSessionPolicyMode = iota
	transitionContractTargetSessionPolicyRequired
)

func (s *Store) planTransitionParameterContract(
	ctx context.Context,
	q *sqlitegen.Queries,
	definition workflow.Definition,
	edge workflow.Edge,
	source workflow.Node,
	target workflow.Node,
	currentSource *workflow.CurrentNode,
	transitionBranchKey *workflow.TransitionBranchKey,
	manualMoveContext bool,
	requireExecutionDescriptions bool,
	contextResolutionMode transitionContractContextResolutionMode,
) (workflow.TransitionParameterContract, error) {
	sessionPolicyRequired := transitionContractTargetSessionPolicyModeForEdge(edge) == transitionContractTargetSessionPolicyRequired
	if currentSource == nil && sessionPolicyRequired {
		return workflow.TransitionParameterContract{}, nil
	}
	if !sessionPolicyRequired {
		return workflow.PlanTransitionParameterContract(workflow.TransitionParameterContractRequest{
			Edge:                         edge,
			SourceKind:                   source.Kind(),
			TargetKind:                   target.Kind(),
			TargetRole:                   workflow.NodeSubagentRole(target),
			Catalog:                      s.roleResolver,
			RequireExecutionDescriptions: requireExecutionDescriptions,
		})
	}
	var retainedSelection *workflow.AgentExecutionSelection
	sessionPolicy := workflow.AssigneeSessionPolicyEstablishTarget
	targetSessionResolved := false
	if contextResolutionMode == transitionContractContextResolutionDeferred &&
		currentSource.SessionID == nil && source.Kind() == workflow.NodeKindAgent &&
		workflow.CanonicalContextSource(edge.ContextSource).Kind == workflow.ContextSourceImmediateSource {
		if currentSource.AgentExecutionSelection == nil {
			return workflow.TransitionParameterContract{}, errors.New("planned Agent source has no execution selection")
		}
		// This outgoing contract belongs to the Agent being planned. Its future
		// continuation reuses that Agent's materialized role, not a database row
		// that the enclosing cutover has not published yet.
		retainedSelection = currentSource.AgentExecutionSelection
		sessionPolicy = workflow.AssigneeSessionPolicyPreserve
		targetSessionResolved = true
	} else {
		contextResolution, err := resolveTransitionContext(
			ctx,
			q,
			definition,
			edge,
			currentSource.Reference.TaskID,
			currentSource,
			transitionBranchKey,
			source,
			target,
			manualMoveContext,
		)
		if err != nil {
			if contextResolutionMode != transitionContractContextResolutionDeferred ||
				(!errors.Is(err, sql.ErrNoRows) && !errors.Is(err, ErrManualMoveTransitionNotUsable)) {
				return workflow.TransitionParameterContract{}, err
			}
			contextResolution = transitionContextResolution{
				TargetSession: workflow.CreateTargetSessionIntent(),
				ActiveSource:  incomingTransitionActiveSource(currentSource),
			}
		}
		sessionID := contextResolution.targetSessionID()

		targetSessionResolved = sessionID != nil
		if sessionID != nil {
			policy, err := workflow.ResolveAssigneeSessionPolicy(workflow.AssigneeSessionPolicyRequest{
				ContextMode:           edge.ContextMode,
				ContextSource:         edge.ContextSource,
				TargetSessionResolved: true,
			})
			if err != nil {
				return workflow.TransitionParameterContract{}, err
			}
			sessionPolicy = policy
			if policy == workflow.AssigneeSessionPolicyPreserve {
				selection, err := s.resolveRetainedSessionSelection(ctx, *sessionID)
				if err != nil {
					if contextResolutionMode != transitionContractContextResolutionDeferred || !errors.Is(err, sql.ErrNoRows) {
						return workflow.TransitionParameterContract{}, err
					}
					targetSessionResolved = false
					sessionPolicy = workflow.AssigneeSessionPolicyEstablishTarget
				} else {
					retainedSelection = selection
				}
			}
		}
	}
	var retainedTargetRole *workflow.TargetAgentRole
	if retainedSelection != nil {
		role := workflow.TargetAgentRole{Identity: retainedSelection.Assignee}
		if s.roleResolver != nil {
			if resolved, ok := s.roleResolver.ResolveConfiguredRole(retainedSelection.Assignee); ok {
				role = resolved
			}
		}
		retainedTargetRole = &role
	}

	edgeCount := 0
	for _, candidate := range definition.Edges {
		if candidate.TransitionGroupID == edge.TransitionGroupID {
			edgeCount++
		}
	}
	return workflow.PlanTransitionParameterContract(workflow.TransitionParameterContractRequest{
		Edge:                         edge,
		SourceKind:                   source.Kind(),
		TargetKind:                   target.Kind(),
		TargetRole:                   workflow.NodeSubagentRole(target),
		RetainedTargetRole:           retainedTargetRole,
		FanOut:                       edgeCount > 1,
		TargetSessionResolved:        targetSessionResolved,
		TargetSessionPolicy:          sessionPolicy,
		Catalog:                      s.roleResolver,
		RequireExecutionDescriptions: requireExecutionDescriptions,
	})
}

func transitionContractTargetSessionPolicyModeForEdge(edge workflow.Edge) transitionContractTargetSessionPolicyMode {
	for _, parameter := range edge.Parameters {
		purpose := workflow.CanonicalParameterPurpose(parameter.Purpose)
		if (purpose == workflow.ParameterPurposeTargetAssignee &&
			workflow.CanonicalAssigneeSelection(edge.AssigneeSelection) != workflow.AssigneeSelectionPreviousNode) ||
			(purpose == workflow.ParameterPurposeTargetThinking &&
				workflow.CanonicalThinkingSelection(edge.ThinkingSelection) != workflow.ThinkingSelectionPreviousNode) ||
			(purpose != workflow.ParameterPurposeTargetAssignee &&
				purpose != workflow.ParameterPurposeTargetThinking) {
			continue
		}
		if edge.ContextMode == workflow.ContextModeContinueSession {
			return transitionContractTargetSessionPolicyRequired
		}
	}
	return transitionContractTargetSessionPolicyNotNeeded
}

func transitionBranchKeyForCurrentNode(reference workflow.CurrentNodeReference) *workflow.TransitionBranchKey {
	branchKey, ok := reference.TransitionBranchKey()
	if !ok {
		return nil
	}
	return &branchKey
}
