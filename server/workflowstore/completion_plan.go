package workflowstore

import (
	"context"
	"errors"
	"fmt"

	"core/server/metadata/sqlitegen"
	"core/server/session"
	"core/server/workflow"
	"core/shared/serverapi"
)

type CurrentNodeCompletionPlan struct {
	plan taskExecutionPlan
}

func (p CurrentNodeCompletionPlan) StartContexts() []CurrentNodeStartContext {
	return append([]CurrentNodeStartContext(nil), p.plan.contexts...)
}

type PendingApprovalPlan struct {
	plan taskExecutionPlan
}

func (p PendingApprovalPlan) StartContexts() []CurrentNodeStartContext {
	return append([]CurrentNodeStartContext(nil), p.plan.contexts...)
}

func (s *Store) PlanCurrentNodeCompletion(ctx context.Context, request CurrentNodeCompletionRequest) (plan CurrentNodeCompletionPlan, resultErr error) {
	defer func() {
		reportWorkflowInvariantError(s.invariantPolicy, resultErr)
		if resultErr != nil {
			resultErr = session.DefinitelyUncommittedMutation(resultErr)
		}
	}()
	req, err := prepareCurrentNodeCompletionRequest(request)
	if err != nil {
		return plan, err
	}
	task, err := s.queries.GetTask(ctx, string(req.Source.TaskID))
	if err != nil {
		return plan, err
	}
	definition, record, err := s.GetDefinition(ctx, task.WorkflowID)
	if err != nil {
		return plan, err
	}
	if err := s.preflightInitialExecution(definition); err != nil {
		return plan, err
	}
	source, err := currentNodeDefinitionNode(definition, req.Source.NodeID)
	if err != nil {
		return plan, err
	}
	if !executableNodeKind(source.Kind()) {
		return plan, errors.New("current node is not executable")
	}
	current, err := s.currentNodeForReference(ctx, s.queries, req.Source)
	if err != nil {
		return plan, err
	}
	if _, pending, err := currentNodePendingApprovalID(ctx, s.queries, current.Reference); err != nil {
		return plan, err
	} else if pending {
		return plan, ErrCurrentNodePendingApproval
	}
	group, targets, err := currentNodeCompletionTransition(definition, source, req.TransitionID)
	if err != nil {
		return plan, err
	}
	issues, err := s.currentNodeCompletionOutputIssues(ctx, s.queries, definition, group, source, targets, current, req.OutputValues)
	if err != nil {
		return plan, err
	}
	if len(issues) != 0 {
		return plan, CompletionValidationError{Issues: issues}
	}
	var result CurrentNodeCompletionResult
	var arrival *sqlitegen.UpdateTaskActiveFanoutBranchArrivalParams
	var joinBefore []sqlitegen.TaskActiveFanoutBranch
	switch {
	case len(targets) > 1:
		result, err = planCurrentNodeFanout(ctx, s.queries, s.invariantPolicy, definition, source, current, targets,
			req.Commentary, req.OutputValues, record.Version, group, s.roleResolver, s.resolveRetainedSessionSelection, s.now().UTC())
	case targets[0].Node.Kind() == workflow.NodeKindJoin:
		arrival = &sqlitegen.UpdateTaskActiveFanoutBranchArrivalParams{}
		joinBefore, err = s.queries.ListTaskActiveFanoutBranches(ctx, task.ID)
		if err != nil {
			return plan, err
		}
		result, err = planCurrentNodeJoinArrival(ctx, s.queries, s.invariantPolicy, definition, current, targets[0].Edge,
			req.OutputValues, s.roleResolver, s.resolveRetainedSessionSelection, arrival, joinBefore)
	default:
		result, err = s.planSequentialCompletion(ctx, definition, record.Version, group, source, current, targets[0], req)
	}
	if err != nil {
		return plan, err
	}
	result, err = completeCurrentNodePostTurnFacts(ctx, s.queries, definition, current, completionTargetEdges(targets), source.Kind() == workflow.NodeKindAgent, result)
	if err != nil {
		return plan, err
	}
	execution, err := s.planTaskExecution(ctx, task, record.Version, []workflow.CurrentNode{current}, result.Mutation.Created, nil)
	if err != nil {
		return plan, err
	}
	execution.completion = &result
	execution.joinArrival = arrival
	execution.joinBefore = joinBefore
	execution.legacyFallbacks = result.legacyFallbacks
	return CurrentNodeCompletionPlan{plan: execution}, nil
}

func (s *Store) planSequentialCompletion(
	ctx context.Context,
	definition workflow.Definition,
	version int64,
	group workflow.TransitionGroup,
	source workflow.Node,
	current workflow.CurrentNode,
	target currentNodeCompletionTarget,
	req CurrentNodeCompletionRequest,
) (CurrentNodeCompletionResult, error) {
	materialized, err := materializeCompletionTargetCurrentNode(ctx, s.queries, s.invariantPolicy, definition, target.Edge, source, target.Node,
		s.roleResolver, s.resolveRetainedSessionSelection, current, req.OutputValues, req.Commentary, currentNodeReferenceBranchKey(current.Reference))
	if err != nil {
		return CurrentNodeCompletionResult{}, err
	}
	result := CurrentNodeCompletionResult{}
	if materialized.LegacyFallback != nil {
		checkLegacyContinuationSourceBeforeMutation(s.invariantPolicy, *materialized.LegacyFallback)
		result.legacyFallbacks = append(result.legacyFallbacks, *materialized.LegacyFallback)
	}
	if target.Edge.RequiresApproval {
		approval, err := newPendingApproval(current, version, group, workflow.NodeDisplayName(source), target.Edge, target.Node,
			materialized.CurrentNode, req.Commentary, req.OutputValues, s.now().UTC())
		if err != nil {
			return CurrentNodeCompletionResult{}, err
		}
		result.PendingApproval = &approval
		return result, nil
	}
	handoff, err := currentNodeCompletionHandoff(source, target.Node)
	if err != nil {
		return CurrentNodeCompletionResult{}, err
	}
	result.Mutation = workflow.CurrentNodeMutationResult{
		Removed: []workflow.CurrentNodeReference{current.Reference},
		Created: []workflow.CurrentNode{materialized.CurrentNode},
	}
	result.Handoff = handoff
	if executableNodeKind(target.Node.Kind()) {
		intent, err := newCurrentNodeAutomaticIntent(materialized.CurrentNode.Reference, target.Node)
		if err != nil {
			return CurrentNodeCompletionResult{}, err
		}
		result.AutomaticIntents = []CurrentNodeAutomaticIntent{intent}
	}
	return result, nil
}

func (s *Store) CommitCurrentNodeCompletion(ctx context.Context, prepared CurrentNodeCompletionPlan, sessions []PlannedCurrentNodeSession) (CurrentNodeCompletionOutcome, error) {
	if prepared.plan.completion == nil {
		return CurrentNodeCompletionOutcome{}, session.DefinitelyUncommittedMutation(errors.New("current node completion plan is required"))
	}
	mutation, _, err := s.commitTaskExecution(ctx, prepared.plan, sessions, taskExecutionComplete)
	if err != nil {
		return CurrentNodeCompletionOutcome{}, err
	}
	result := *prepared.plan.completion
	result.Mutation = mutation
	outcome := CurrentNodeCompletionOutcome{CommitReceipt: session.CommitReceipt{Committed: true}, CurrentNodeCompletionResult: result}
	for _, detail := range result.legacyFallbacks {
		reportLegacyContinuationSourceAfterCommit(s.invariantPolicy, detail)
	}
	if len(mutation.Removed) != 0 {
		event := WorkflowEventRecord{
			ProjectID: &prepared.plan.task.ProjectID, WorkflowID: &prepared.plan.task.WorkflowID,
			Resource: serverapi.WorkflowProjectEventResourceTask, Action: serverapi.WorkflowProjectEventActionCompleted,
			PrimaryEntityID: prepared.plan.task.ID,
		}
		if err := s.PublishWorkflowEvent(ctx, event); err != nil {
			outcome.PostCommitDiagnostic = fmt.Errorf("publish current node task event: %w", err)
		}
	}
	return outcome, nil
}

func (s *Store) PlanPendingApproval(ctx context.Context, id workflow.ApprovalID) (PendingApprovalPlan, error) {
	approval, err := s.PendingApproval(ctx, id)
	if err != nil {
		return PendingApprovalPlan{}, err
	}
	if err := s.requireTaskContextSelectionResolved(ctx, s.queries, approval.Source.TaskID); err != nil {
		return PendingApprovalPlan{}, err
	}
	task, err := s.queries.GetTask(ctx, string(approval.Source.TaskID))
	if err != nil {
		return PendingApprovalPlan{}, err
	}
	definition, record, err := workflowDefinitionFromQueries(ctx, s.queries, task.WorkflowID)
	if err != nil {
		return PendingApprovalPlan{}, err
	}
	current, err := s.currentNodeForReference(ctx, s.queries, approval.Source)
	if err != nil {
		return PendingApprovalPlan{}, err
	}
	targets := make([]workflow.CurrentNode, 0, len(approval.Branches))
	for _, branch := range approval.Branches {
		targets = append(targets, branch.Target.CurrentNode)
	}
	if len(targets) == 0 {
		return PendingApprovalPlan{}, errors.New("pending approval has no target branches")
	}
	if len(targets) == 1 {
		if err := validatePendingApprovalSequentialTarget(approval.Source, targets[0].Reference); err != nil {
			return PendingApprovalPlan{}, err
		}
	} else if err := validatePendingApprovalFanoutTargets(approval.Source, approval.Branches); err != nil {
		return PendingApprovalPlan{}, err
	}
	// The approved incoming Transition is frozen; subsequent outgoing choices
	// and other Workflow facts are resolved from the current definition.
	for index := range definition.Edges {
		for _, branch := range approval.Branches {
			if definition.Edges[index].ID == branch.EffectiveEdge.ID {
				definition.Edges[index] = branch.EffectiveEdge
				break
			}
		}
	}
	plan, err := s.materializeTaskExecutionPlan(ctx, task, record, definition, []workflow.CurrentNode{current}, targets, nil)
	if err != nil {
		return PendingApprovalPlan{}, err
	}
	plan.approval = &approval
	return PendingApprovalPlan{plan: plan}, nil
}

func (s *Store) CommitPendingApproval(ctx context.Context, prepared PendingApprovalPlan, sessions []PlannedCurrentNodeSession) (PendingApprovalApplyResult, error) {
	if prepared.plan.approval == nil {
		return PendingApprovalApplyResult{}, errors.New("pending approval plan is required")
	}
	mutation, attention, err := s.commitTaskExecution(ctx, prepared.plan, sessions, taskExecutionApprove)
	if err != nil {
		return PendingApprovalApplyResult{}, err
	}
	result := PendingApprovalApplyResult{
		Mutation: mutation, ResolvedApproval: *prepared.plan.approval,
		Handoff: pendingApprovalHandoff(*prepared.plan.approval), TaskAttentionResolution: attention,
	}
	for _, target := range mutation.Created {
		if target.Scheduling != nil {
			result.AutomaticIntents = append(result.AutomaticIntents, target.Reference)
		}
	}
	return result, nil
}
