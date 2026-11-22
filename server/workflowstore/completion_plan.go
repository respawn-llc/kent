package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"core/server/metadata/sqlitegen"
	"core/server/sessionstore"
	"core/server/workflow"
	"core/shared/workflowcontract"
)

type CurrentNodeCompletionPlan struct {
	plan taskExecutionPlan
}

func (p CurrentNodeCompletionPlan) StartContexts() []CurrentNodeStartContext {
	return append([]CurrentNodeStartContext(nil), p.plan.contexts...)
}

type PendingApprovalPlan struct {
	plan    taskExecutionPlan
	handoff CompletionHandoff
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
	if pending, err := currentNodeHasPendingApproval(ctx, s.queries, current.Reference); err != nil {
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
			req.Commentary, req.OutputValues, group, s.roleResolver, s.resolveRetainedSessionSelection, s.now().UTC())
	case targets[0].Node.Kind() == workflow.NodeKindJoin:
		arrival = &sqlitegen.UpdateTaskActiveFanoutBranchArrivalParams{}
		result, joinBefore, err = s.planCompletionJoinArrival(ctx, definition, current, targets[0].Edge, req.OutputValues, arrival)
	default:
		result, err = s.planSequentialCompletion(ctx, definition, group, source, current, targets[0], req)
	}
	if err != nil {
		return plan, err
	}
	result.Handoff, err = completionHandoff(definition, group, source, targets, result.Mutation.Created)
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

func (s *Store) planCompletionJoinArrival(ctx context.Context, definition workflow.Definition, current workflow.CurrentNode,
	edge workflow.Edge, outputValues map[string]string, arrival *sqlitegen.UpdateTaskActiveFanoutBranchArrivalParams,
) (CurrentNodeCompletionResult, []sqlitegen.TaskActiveFanoutBranch, error) {
	before, err := s.queries.ListTaskActiveFanoutBranches(ctx, string(current.Reference.TaskID))
	if err != nil {
		return CurrentNodeCompletionResult{}, nil, err
	}
	result, err := planCurrentNodeJoinArrival(ctx, s.queries, s.invariantPolicy, definition, current, edge,
		outputValues, s.roleResolver, s.resolveRetainedSessionSelection, arrival, before)
	return result, before, err
}

func completionHandoff(definition workflow.Definition, group workflow.TransitionGroup, source workflow.Node,
	targets []currentNodeCompletionTarget, created []workflow.CurrentNode,
) (CompletionHandoff, error) {
	destination := workflow.DisplayLabel(group.DisplayName, string(group.TransitionID))
	if len(targets) == 1 {
		target := targets[0].Node
		if target.Kind() == workflow.NodeKindJoin && len(created) == 1 {
			source = target
			var err error
			target, err = currentNodeDefinitionNode(definition, created[0].Reference.NodeID)
			if err != nil {
				return CompletionHandoff{}, err
			}
		}
		destination = workflow.NodeDisplayLabel(target)
	}
	return CompletionHandoff{SourceNodeDisplayName: workflow.NodeDisplayLabel(source), DestinationDisplayName: destination}, nil
}

func (s *Store) planSequentialCompletion(
	ctx context.Context,
	definition workflow.Definition,
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
		approval, err := newPendingApproval(current, group, workflow.NodeDisplayLabel(source), target.Edge, target.Node,
			materialized.CurrentNode, req.Commentary, req.OutputValues, s.now().UTC())
		if err != nil {
			return CurrentNodeCompletionResult{}, err
		}
		result.PendingApproval = &approval
		return result, nil
	}
	result.Mutation = workflow.CurrentNodeMutationResult{
		Removed: []workflow.CurrentNodeReference{current.Reference},
		Created: []workflow.CurrentNode{materialized.CurrentNode},
	}
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
		event := workflowcontract.Event{
			ProjectID: &prepared.plan.task.ProjectID, WorkflowID: &prepared.plan.task.WorkflowID,
			Resource: workflowcontract.EventResourceTask, Action: workflowcontract.EventActionCompleted,
			PrimaryEntityID: prepared.plan.task.ID,
		}
		if err := s.PublishWorkflowEvent(ctx, event); err != nil {
			outcome.PostCommitDiagnostic = fmt.Errorf("publish current node task event: %w", err)
		}
	}
	return outcome, nil
}

func (s *Store) PlanPendingApproval(ctx context.Context, id workflow.ApprovalID) (prepared PendingApprovalPlan, resultErr error) {
	facts, err := pendingApprovalByID(ctx, s.queries, id)
	if err != nil {
		return PendingApprovalPlan{}, err
	}
	defer func() {
		var validation CompletionValidationError
		var unavailable workflow.RetainedTargetUnavailableError
		if errors.As(resultErr, &validation) || errors.As(resultErr, &unavailable) || errors.Is(resultErr, sql.ErrNoRows) {
			resultErr = fmt.Errorf("%w: %w", &TaskContextSelectionRequiredError{TaskID: facts.Source.TaskID}, resultErr)
		}
	}()
	if err := s.requireTaskContextSelectionResolved(ctx, s.queries, facts.Source.TaskID); err != nil {
		return PendingApprovalPlan{}, err
	}
	task, err := s.queries.GetTask(ctx, string(facts.Source.TaskID))
	if err != nil {
		return PendingApprovalPlan{}, err
	}
	definition, record, err := workflowDefinitionFromQueries(ctx, s.queries, task.WorkflowID)
	if err != nil {
		return PendingApprovalPlan{}, err
	}
	approval, selected, err := newPendingApprovalResolver(definition).resolve(facts)
	if err != nil {
		return PendingApprovalPlan{}, err
	}
	if !approval.ChoiceAvailable() {
		return PendingApprovalPlan{}, &TaskContextSelectionRequiredError{TaskID: approval.Source.TaskID}
	}
	current, err := s.currentNodeForReference(ctx, s.queries, approval.Source)
	if err != nil {
		return PendingApprovalPlan{}, err
	}
	source, err := currentNodeDefinitionNode(definition, current.Reference.NodeID)
	if err != nil {
		return PendingApprovalPlan{}, err
	}
	if len(selected) == 0 {
		return PendingApprovalPlan{}, errors.New("pending approval has no target branches")
	}
	if len(selected) == 1 {
		if err := validatePendingApprovalSequentialTarget(approval.Source, approval.Branches[0].Target.Reference); err != nil {
			return PendingApprovalPlan{}, err
		}
	} else if err := validatePendingApprovalFanoutTargets(approval.Source, approval.Branches); err != nil {
		return PendingApprovalPlan{}, err
	}
	issues, err := s.currentNodeCompletionOutputIssues(ctx, s.queries, definition, *approval.Transition.Group, source, selected, current, approval.OutputValues)
	if err != nil {
		return PendingApprovalPlan{}, err
	}
	if len(issues) != 0 {
		return PendingApprovalPlan{}, CompletionValidationError{Issues: issues}
	}
	var targets []workflow.CurrentNode
	var arrival *sqlitegen.UpdateTaskActiveFanoutBranchArrivalParams
	var joinBefore []sqlitegen.TaskActiveFanoutBranch
	var fallbacks []legacyContinuationSourceFallbackDetail
	if len(selected) == 1 && selected[0].Node.Kind() == workflow.NodeKindJoin {
		arrival = &sqlitegen.UpdateTaskActiveFanoutBranchArrivalParams{}
		result, before, err := s.planCompletionJoinArrival(ctx, definition, current, selected[0].Edge, approval.OutputValues, arrival)
		if err != nil {
			return PendingApprovalPlan{}, err
		}
		joinBefore = before
		targets = result.Mutation.Created
		fallbacks = result.legacyFallbacks
	} else {
		targets = make([]workflow.CurrentNode, 0, len(selected))
		for index, target := range selected {
			materialized, err := materializeCompletionTargetCurrentNode(ctx, s.queries, s.invariantPolicy, definition,
				target.Edge, source, target.Node, s.roleResolver, s.resolveRetainedSessionSelection, current,
				approval.OutputValues, approval.Commentary, currentNodeReferenceBranchKey(approval.Branches[index].Target.Reference))
			if err != nil {
				return PendingApprovalPlan{}, err
			}
			targets = append(targets, materialized.CurrentNode)
		}
	}
	handoff, err := completionHandoff(definition, *approval.Transition.Group, source, selected, targets)
	if err != nil {
		return PendingApprovalPlan{}, err
	}
	plan, err := s.materializeTaskExecutionPlan(ctx, task, record, definition, []workflow.CurrentNode{current}, targets, nil)
	if err != nil {
		return PendingApprovalPlan{}, err
	}
	plan.approval = &approval
	plan.joinArrival = arrival
	plan.joinBefore = joinBefore
	plan.legacyFallbacks = fallbacks
	return PendingApprovalPlan{plan: plan, handoff: handoff}, nil
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
		Handoff: prepared.handoff, TaskAttentionResolution: attention,
	}
	for _, detail := range prepared.plan.legacyFallbacks {
		reportLegacyContinuationSourceAfterCommit(s.invariantPolicy, detail)
	}
	for _, target := range mutation.Created {
		if target.Scheduling != nil {
			result.AutomaticIntents = append(result.AutomaticIntents, target.Reference)
		}
	}
	return result, nil
}
