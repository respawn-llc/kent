package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"core/server/metadata"
	"core/server/metadata/sqlitegen"
	"core/server/workflow"
	"core/shared/runtimeids"
)

// PlannedCurrentNodeSession supplies the exact identity selected during
// preparation. Snapshot is present only for a fresh Session.
type PlannedCurrentNodeSession struct {
	CurrentNode workflow.CurrentNodeReference
	SessionID   runtimeids.SessionID
	Snapshot    *metadata.PreparedSessionSnapshot
}

type taskExecutionPlan struct {
	task            sqlitegen.TaskRecord
	workflowVersion int64
	sources         []workflow.CurrentNode
	targets         []workflow.CurrentNode
	contexts        []CurrentNodeStartContext
	executionTarget *preparedExecutionTargetMutation
	legacyFallbacks []legacyContinuationSourceFallbackDetail
	completion      *CurrentNodeCompletionResult
	approval        *workflow.PendingApproval
	joinArrival     *sqlitegen.UpdateTaskActiveFanoutBranchArrivalParams
	joinBefore      []sqlitegen.TaskActiveFanoutBranch
}

type TaskStartPlan struct{ plan taskExecutionPlan }

type TaskResumePlan struct{ plan taskExecutionPlan }

type TaskResumeCommitResult struct {
	CurrentNodes []workflow.CurrentNode
	TaskAttentionResolution
}

type ManualMovePlan struct {
	plan       taskExecutionPlan
	noOpTarget *workflow.NodeID
}

type taskExecutionAction uint8

const (
	taskExecutionStart taskExecutionAction = iota
	taskExecutionResume
	taskExecutionMove
	taskExecutionComplete
	taskExecutionApprove
)

func (p TaskStartPlan) StartContexts() []CurrentNodeStartContext {
	return append([]CurrentNodeStartContext(nil), p.plan.contexts...)
}

func (p TaskResumePlan) StartContexts() []CurrentNodeStartContext {
	return append([]CurrentNodeStartContext(nil), p.plan.contexts...)
}

func (p ManualMovePlan) StartContexts() []CurrentNodeStartContext {
	return append([]CurrentNodeStartContext(nil), p.plan.contexts...)
}

func (s *Store) PlanTaskResume(ctx context.Context, taskID workflow.TaskID, references []workflow.CurrentNodeReference, candidate *ExecutionTargetCandidate) (TaskResumePlan, error) {
	if len(references) == 0 {
		return TaskResumePlan{}, errors.New("Task Resume requires selected Current Nodes")
	}
	task, err := s.queries.GetTask(ctx, string(taskID))
	if err != nil {
		return TaskResumePlan{}, err
	}
	record, err := s.queries.GetWorkflow(ctx, task.WorkflowID)
	if err != nil {
		return TaskResumePlan{}, err
	}
	classifications, err := s.PreflightTaskResume(ctx, taskID)
	if err != nil {
		return TaskResumePlan{}, err
	}
	selected := make([]workflow.CurrentNode, 0, len(references))
	seen := make(map[workflow.CurrentNodeReferenceKey]struct{}, len(references))
	for _, reference := range references {
		key, err := reference.Key()
		if err != nil {
			return TaskResumePlan{}, err
		}
		if reference.TaskID != taskID {
			return TaskResumePlan{}, errors.New("Resume Current Node belongs to a different Task")
		}
		if _, duplicate := seen[key]; duplicate {
			return TaskResumePlan{}, errors.New("Resume Current Node is duplicated")
		}
		seen[key] = struct{}{}
		var found bool
		for _, classification := range classifications {
			if classification.CurrentNode.Reference.Equal(reference) {
				if err := classification.ValidationError(); err != nil {
					return TaskResumePlan{}, err
				}
				selected = append(selected, classification.CurrentNode)
				found = true
				break
			}
		}
		if !found {
			return TaskResumePlan{}, errors.New("selected Current Node cannot be resumed")
		}
	}
	definition, _, err := s.GetDefinition(ctx, task.WorkflowID)
	if err != nil {
		return TaskResumePlan{}, err
	}
	if err := s.preflightInitialExecution(definition); err != nil {
		return TaskResumePlan{}, err
	}
	plan, err := s.planTaskExecution(ctx, task, record.Version, selected, selected, candidate)
	return TaskResumePlan{plan: plan}, err
}

func (s *Store) CommitTaskResume(ctx context.Context, prepared TaskResumePlan, sessions []PlannedCurrentNodeSession) (TaskResumeCommitResult, error) {
	mutation, attention, err := s.commitTaskExecution(ctx, prepared.plan, sessions, taskExecutionResume)
	return TaskResumeCommitResult{CurrentNodes: mutation.Created, TaskAttentionResolution: attention}, err
}

func (s *Store) PlanManualMove(ctx context.Context, prepared ManualMovePreparation, candidate *ExecutionTargetCandidate) (result ManualMovePlan, resultErr error) {
	defer func() {
		reportWorkflowInvariantError(s.invariantPolicy, resultErr)
	}()
	if prepared.request.TaskID == "" || (!prepared.noOp && prepared.target == nil) {
		return ManualMovePlan{}, errors.New("manual move preparation is invalid")
	}
	// Re-resolve the selection after preparation of the Execution Root.
	current, err := s.PrepareManualMove(ctx, prepared.request)
	if err != nil {
		return ManualMovePlan{}, err
	}
	task, err := s.queries.GetTask(ctx, string(prepared.request.TaskID))
	if err != nil {
		return ManualMovePlan{}, err
	}
	definition, record, err := workflowDefinitionFromQueries(ctx, s.queries, task.WorkflowID)
	if err != nil {
		return ManualMovePlan{}, err
	}
	if record.Version != current.workflowVersion {
		return ManualMovePlan{}, errors.New("workflow changed during Manual Move preparation")
	}
	plan := taskExecutionPlan{task: task, workflowVersion: record.Version, sources: current.currentNodes}
	plan.sources, err = s.listTaskCurrentNodes(ctx, s.queries, prepared.request.TaskID)
	if err != nil {
		return ManualMovePlan{}, err
	}
	if current.noOp {
		return ManualMovePlan{plan: plan, noOpTarget: &current.request.TargetNodeID}, nil
	}
	if !executableNodeKind(current.target.Kind()) {
		if candidate != nil {
			return ManualMovePlan{}, errors.New("non-executable Manual Move does not accept an execution target")
		}
		target, err := newNonExecutableCurrentNode(prepared.request.TaskID, workflow.NodeIDOf(current.target))
		if err != nil {
			return ManualMovePlan{}, err
		}
		plan.targets = []workflow.CurrentNode{target}
		applyManualMoveCommentary(plan.targets, current.request.Commentary)
		return ManualMovePlan{plan: plan}, nil
	}
	if current.choice == nil {
		return ManualMovePlan{}, ErrManualMoveTransitionSelectionRequired
	}
	environment, err := s.manualMoveValueEnvironment(ctx, s.queries, definition, current.request.TaskID, current.currentNodes, current.request.TargetNodeID)
	if err != nil {
		return ManualMovePlan{}, err
	}
	if err := validateManualMoveValues(*current.choice, current.request.Values, environment); err != nil {
		return ManualMovePlan{}, err
	}
	targets, fallbacks, err := s.materializeManualMoveTargets(ctx, s.queries, definition, *current.choice, current.currentNodes, environment, current.request.Values)
	if err != nil {
		return ManualMovePlan{}, err
	}
	for _, detail := range fallbacks {
		checkLegacyContinuationSourceBeforeMutation(s.invariantPolicy, detail)
	}
	applyManualMoveCommentary(targets, current.request.Commentary)
	plan, err = s.planTaskExecution(ctx, task, record.Version, current.currentNodes, targets, candidate)
	plan.legacyFallbacks = fallbacks
	return ManualMovePlan{plan: plan}, err
}

func (s *Store) CommitManualMove(ctx context.Context, prepared ManualMovePlan, sessions []PlannedCurrentNodeSession) (ManualMoveResult, error) {
	if prepared.noOpTarget != nil {
		if len(sessions) != 0 {
			return ManualMoveResult{}, errors.New("no-op Manual Move does not accept Sessions")
		}
		nodes, err := s.ListCurrentNodes(ctx, workflow.TaskID(prepared.plan.task.ID))
		if err != nil {
			return ManualMoveResult{}, err
		}
		for _, node := range nodes {
			if node.Reference.NodeID == *prepared.noOpTarget {
				return ManualMoveResult{Outcome: ManualMoveResultOutcomeNoOp, CurrentNodes: nodes}, nil
			}
		}
		return ManualMoveResult{}, errors.New("Manual Move destination is no longer Current")
	}
	mutation, attention, err := s.commitTaskExecution(ctx, prepared.plan, sessions, taskExecutionMove)
	if err != nil {
		return ManualMoveResult{}, err
	}
	for _, detail := range prepared.plan.legacyFallbacks {
		reportLegacyContinuationSourceAfterCommit(s.invariantPolicy, detail)
	}
	return ManualMoveResult{Outcome: ManualMoveResultOutcomeApplied, Mutation: mutation, TaskAttentionResolution: attention}, nil
}

func (s *Store) PlanTaskStart(ctx context.Context, taskID workflow.TaskID, candidate *ExecutionTargetCandidate) (TaskStartPlan, error) {
	prepared, err := s.prepareTaskStart(ctx, taskID)
	if err != nil {
		return TaskStartPlan{}, err
	}
	target, err := s.materializeTaskStart(prepared)
	if err != nil {
		return TaskStartPlan{}, err
	}
	plan, err := s.planTaskExecution(ctx, prepared.task, prepared.workflowVersion, []workflow.CurrentNode{prepared.startCurrentNode}, []workflow.CurrentNode{target}, candidate)
	return TaskStartPlan{plan: plan}, err
}

func (s *Store) planTaskExecution(
	ctx context.Context,
	task sqlitegen.TaskRecord,
	workflowVersion int64,
	sources, targets []workflow.CurrentNode,
	candidate *ExecutionTargetCandidate,
) (taskExecutionPlan, error) {
	definition, record, err := workflowDefinitionFromQueries(ctx, s.queries, task.WorkflowID)
	if err != nil {
		return taskExecutionPlan{}, err
	}
	if record.Version != workflowVersion {
		return taskExecutionPlan{}, errors.New("workflow changed during execution preparation")
	}
	return s.materializeTaskExecutionPlan(ctx, task, record, definition, sources, targets, candidate)
}

func (s *Store) materializeTaskExecutionPlan(
	ctx context.Context,
	task sqlitegen.TaskRecord,
	record WorkflowRecord,
	definition workflow.Definition,
	sources, targets []workflow.CurrentNode,
	candidate *ExecutionTargetCandidate,
) (taskExecutionPlan, error) {
	taskRecord, err := taskRecordFromTask(task)
	if err != nil {
		return taskExecutionPlan{}, err
	}
	plan := taskExecutionPlan{
		task: task, workflowVersion: record.Version,
	}
	plan.sources, err = copyPlannedCurrentNodes(sources)
	if err != nil {
		return taskExecutionPlan{}, err
	}
	plan.targets, err = copyPlannedCurrentNodes(targets)
	if err != nil {
		return taskExecutionPlan{}, err
	}
	for _, target := range targets {
		node, err := currentNodeDefinitionNode(definition, target.Reference.NodeID)
		if err != nil {
			return taskExecutionPlan{}, err
		}
		if !executableNodeKind(node.Kind()) {
			continue
		}
		if plan.executionTarget == nil {
			mutation, err := s.prepareExecutionTargetMutation(ctx, task, copyExecutionTargetCandidate(candidate))
			if err != nil {
				return taskExecutionPlan{}, err
			}
			plan.executionTarget = &mutation
		}
		target.Scheduling = &workflow.CurrentNodeScheduling{State: workflow.CurrentNodeSchedulingAdmitted}
		root := copyExecutionRoot(plan.executionTarget.executionRoot)
		input, err := s.resolveMaterializedCurrentNodeStartContext(ctx, s.queries, taskRecord, record, definition, target, &root)
		if err != nil {
			return taskExecutionPlan{}, err
		}
		plan.contexts = append(plan.contexts, input)
	}
	return plan, nil
}

// Contexts are caller-owned preparation inputs. Their mutable maps and pointers
// must not let launch preparation rewrite the validated cutover materialization.
func copyPlannedCurrentNodes(nodes []workflow.CurrentNode) ([]workflow.CurrentNode, error) {
	result := make([]workflow.CurrentNode, 0, len(nodes))
	for _, node := range nodes {
		copy, err := workflow.NewCurrentNodeWithMaterializedSource(
			node.Reference, node.CurrentInputValues, node.PriorValues, node.SessionID,
			node.ContinuationSource, node.Scheduling, node.AgentExecutionSelection,
		)
		if err != nil {
			return nil, err
		}
		if node.EnteredByEdgeID != nil {
			id := *node.EnteredByEdgeID
			copy.EnteredByEdgeID = &id
		}
		result = append(result, copy)
	}
	return result, nil
}

func copyExecutionRoot(root ExecutionRoot) ExecutionRoot {
	if root.Managed != nil {
		managed := *root.Managed
		root.Managed = &managed
	}
	return root
}

func copyExecutionTargetCandidate(candidate *ExecutionTargetCandidate) *ExecutionTargetCandidate {
	if candidate == nil {
		return nil
	}
	copy := *candidate
	copy.Root = copyExecutionRoot(candidate.Root)
	if copy.Snapshot.RequestedRef != nil {
		value := *copy.Snapshot.RequestedRef
		copy.Snapshot.RequestedRef = &value
	}
	if copy.Snapshot.ResolvedRef != nil {
		value := *copy.Snapshot.ResolvedRef
		copy.Snapshot.ResolvedRef = &value
	}
	if copy.Snapshot.CommitOID != nil {
		value := *copy.Snapshot.CommitOID
		copy.Snapshot.CommitOID = &value
	}
	return &copy
}

func (s *Store) CommitTaskStart(ctx context.Context, prepared TaskStartPlan, sessions []PlannedCurrentNodeSession) (StartTaskResult, error) {
	mutation, _, err := s.commitTaskExecution(ctx, prepared.plan, sessions, taskExecutionStart)
	return StartTaskResult{Mutation: mutation}, err
}

func (s *Store) commitTaskExecution(
	ctx context.Context,
	plan taskExecutionPlan,
	sessions []PlannedCurrentNodeSession,
	action taskExecutionAction,
) (workflow.CurrentNodeMutationResult, TaskAttentionResolution, error) {
	var empty workflow.CurrentNodeMutationResult
	var attention TaskAttentionResolution
	if plan.task.ID == "" || len(plan.sources) == 0 ||
		(len(plan.targets) == 0 && plan.completion == nil) {
		return empty, attention, errors.New("task execution plan is invalid")
	}
	lease, err := s.graphSaves.AcquireShared(ctx, plan.task.WorkflowID)
	if err != nil {
		return empty, attention, err
	}
	defer lease.Release()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, attention, err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	locked, err := q.AcquireManualMoveTaskWriteLock(ctx, plan.task.ID)
	if err != nil {
		return empty, attention, err
	}
	if locked != 1 {
		return empty, attention, sql.ErrNoRows
	}
	task, err := s.validateTaskExecutionPlan(ctx, q, plan, action)
	if err != nil {
		return empty, attention, err
	}
	now := s.now().UTC()
	if plan.executionTarget != nil {
		if err := applyPreparedExecutionTargetMutation(ctx, q, task, *plan.executionTarget, now.UnixMilli()); err != nil {
			return empty, attention, err
		}
	}
	targets, err := s.preparePlannedCurrentNodeSessions(ctx, tx, plan, sessions, action)
	if err != nil {
		return empty, attention, err
	}
	if plan.approval != nil {
		projection, found, err := pendingApprovalAttentionProjection(ctx, q, plan.approval.ID)
		if err != nil || !found {
			return empty, attention, errors.Join(err, errors.New("pending approval disappeared before cutover"))
		}
		removed, err := q.DeleteTaskPendingApproval(ctx, plan.approval.ID.String())
		if err != nil || removed != 1 {
			return empty, attention, errors.Join(err, errors.New("pending approval changed before cutover"))
		}
		attention.Approvals = append(attention.Approvals, projection)
	}
	if plan.joinArrival != nil {
		updated, err := q.UpdateTaskActiveFanoutBranchArrival(ctx, *plan.joinArrival)
		if err != nil || updated != 1 {
			return empty, attention, errors.Join(err, errors.New("join arrival is no longer pending"))
		}
	}
	if plan.completion != nil && plan.completion.PendingApproval != nil {
		if err := insertPendingApproval(ctx, q, *plan.completion.PendingApproval); err != nil {
			return empty, attention, err
		}
	}
	if action == taskExecutionMove {
		attention, err = s.taskAttentionResolution(ctx, q, workflow.TaskID(task.ID))
		if err != nil {
			return empty, attention, err
		}
		if _, err := q.DeleteTaskPendingApprovalsByTask(ctx, task.ID); err != nil {
			return empty, attention, err
		}
	}
	removed := make([]workflow.CurrentNodeReference, 0, len(plan.sources))
	for _, source := range plan.sources {
		if plan.completion != nil && plan.completion.PendingApproval != nil {
			break
		}
		if action == taskExecutionResume {
			projection, found, err := s.pendingInterruptedCurrentNodeAttentionProjection(ctx, q, source.Reference)
			if err != nil {
				return empty, attention, err
			}
			if found {
				attention.InterruptedCurrentNodes = append(attention.InterruptedCurrentNodes, projection)
			}
		}
		count, err := deleteTaskCurrentNode(ctx, q, source.Reference)
		if err != nil {
			return empty, attention, err
		}
		if count != 1 {
			return empty, attention, sql.ErrNoRows
		}
		removed = append(removed, source.Reference)
	}
	if action == taskExecutionMove || (plan.joinArrival != nil && len(targets) != 0) {
		if _, err := q.DeleteTaskActiveFanout(ctx, task.ID); err != nil {
			return empty, attention, err
		}
	}
	for index := range targets {
		if targets[index].Scheduling != nil && action != taskExecutionComplete {
			targets[index].Scheduling = &workflow.CurrentNodeScheduling{State: workflow.CurrentNodeSchedulingAdmitted}
		}
	}
	if len(targets) > 1 && action != taskExecutionResume {
		err = insertTaskFanoutTargets(ctx, q, workflow.TaskID(task.ID), targets, now)
	} else {
		for _, target := range targets {
			if err = insertTaskCurrentNode(ctx, q, target, now); err != nil {
				break
			}
		}
	}
	if err != nil {
		return empty, attention, err
	}
	if err := touchTaskUpdatedAt(ctx, q, task.ID, now.UnixMilli()); err != nil {
		return empty, attention, err
	}
	if err := tx.Commit(); err != nil {
		return empty, attention, err
	}
	return workflow.CurrentNodeMutationResult{Removed: removed, Created: targets}, attention, nil
}

func (s *Store) validateTaskExecutionPlan(ctx context.Context, q *sqlitegen.Queries, plan taskExecutionPlan, action taskExecutionAction) (sqlitegen.TaskRecord, error) {
	task, err := q.GetTask(ctx, plan.task.ID)
	if err != nil {
		return task, err
	}
	if task.WorkflowID != plan.task.WorkflowID || task.ProjectID != plan.task.ProjectID ||
		task.SourceWorkspaceID != plan.task.SourceWorkspaceID || task.ManagedWorktreeID != plan.task.ManagedWorktreeID ||
		task.ExecutionTargetMode != plan.task.ExecutionTargetMode ||
		task.ExecutionTargetRequestedRef != plan.task.ExecutionTargetRequestedRef ||
		task.ExecutionTargetResolvedRef != plan.task.ExecutionTargetResolvedRef ||
		task.ExecutionTargetCommitOid != plan.task.ExecutionTargetCommitOid ||
		task.ExecutionTargetProvenance != plan.task.ExecutionTargetProvenance {
		return task, errors.New("task execution target changed after preparation")
	}
	record, err := q.GetWorkflow(ctx, task.WorkflowID)
	if err != nil {
		return task, err
	}
	if record.Version != plan.workflowVersion {
		return task, errors.New("workflow changed after execution preparation")
	}
	current, err := s.listTaskCurrentNodes(ctx, q, workflow.TaskID(task.ID))
	if err != nil {
		return task, err
	}
	if action == taskExecutionResume || action == taskExecutionApprove {
		if err := validateTaskContextSelection(workflow.TaskID(task.ID), current); err != nil {
			return task, err
		}
	}
	if (action == taskExecutionStart || action == taskExecutionMove) && len(current) != len(plan.sources) {
		return task, errors.New("task position changed after execution preparation")
	}
	for _, expected := range plan.sources {
		var found bool
		for _, actual := range current {
			if !actual.Reference.Equal(expected.Reference) {
				continue
			}
			found = true
			if !sameCurrentNodeMaterialization(expected, actual) {
				return task, errors.New("current node changed after execution preparation")
			}
			if action == taskExecutionStart && actual.Scheduling != nil {
				return task, errors.New("Task Start source is executable")
			}
			if action == taskExecutionResume && (actual.Scheduling == nil || actual.Scheduling.State != workflow.CurrentNodeSchedulingInterrupted) {
				return task, errors.New("current node is no longer interrupted")
			}
			if action == taskExecutionResume {
				_, pending, err := currentNodePendingApprovalID(ctx, q, actual.Reference)
				if err != nil {
					return task, err
				}
				if pending {
					return task, errors.New("current node is waiting for approval")
				}
			}
			if action == taskExecutionComplete {
				if _, pending, err := currentNodePendingApprovalID(ctx, q, actual.Reference); err != nil {
					return task, err
				} else if pending {
					return task, ErrCurrentNodePendingApproval
				}
			}
			break
		}
		if !found {
			return task, sql.ErrNoRows
		}
	}
	if target := plan.executionTarget; target != nil {
		if target.candidate != nil {
			if err := validateExecutionTargetCandidateForTask(ctx, q, task, *target.candidate, target.mode); err != nil {
				return task, err
			}
		} else {
			root, err := executionRootForTask(ctx, q, task)
			if err != nil {
				return task, err
			}
			if root.SourceWorkspaceID != target.executionRoot.SourceWorkspaceID || root.EffectiveRoot() != target.executionRoot.EffectiveRoot() {
				return task, errors.New("execution root changed after preparation")
			}
		}
	}
	if plan.joinArrival != nil {
		arrivals, err := q.ListTaskActiveFanoutBranches(ctx, task.ID)
		if err != nil {
			return task, err
		}
		if !slices.Equal(arrivals, plan.joinBefore) {
			return task, errors.New("fan-out arrivals changed after completion preparation")
		}
	}
	return task, nil
}

func (s *Store) preparePlannedCurrentNodeSessions(ctx context.Context, tx *sql.Tx, plan taskExecutionPlan, sessions []PlannedCurrentNodeSession, action taskExecutionAction) ([]workflow.CurrentNode, error) {
	assignments := make(map[workflow.CurrentNodeReferenceKey]PlannedCurrentNodeSession, len(sessions))
	identities := make(map[runtimeids.SessionID]struct{}, len(sessions))
	for _, assignment := range sessions {
		key, err := assignment.CurrentNode.Key()
		if err != nil {
			return nil, err
		}
		if assignment.SessionID.IsZero() {
			return nil, errors.New("planned Session identity is required")
		}
		if _, exists := assignments[key]; exists {
			return nil, errors.New("duplicate planned Current Node")
		}
		if _, exists := identities[assignment.SessionID]; exists {
			return nil, errors.New("Session cannot be assigned to multiple Current Nodes")
		}
		assignments[key] = assignment
		identities[assignment.SessionID] = struct{}{}
	}
	targets := append([]workflow.CurrentNode(nil), plan.targets...)
	for index := range targets {
		target := &targets[index]
		if target.AgentExecutionSelection == nil {
			continue
		}
		key, err := target.Reference.Key()
		if err != nil {
			return nil, err
		}
		assignment, found := assignments[key]
		if !found {
			return nil, fmt.Errorf("Agent target %v has no planned Session", target.Reference)
		}
		delete(assignments, key)
		if assignment.Snapshot != nil {
			if target.SessionID != nil && (action == taskExecutionResume || !target.Reference.IsBranchScoped()) {
				return nil, errors.New("execution requires the selected retained Session")
			}
			if assignment.Snapshot.SessionID() != assignment.SessionID {
				return nil, errors.New("planned Session snapshot identity does not match assignment")
			}
			executionTarget := assignment.Snapshot.ExecutionTarget()
			root := plan.executionTarget.executionRoot
			if executionTarget.Workspace == nil || executionTarget.Workspace.ID != root.SourceWorkspaceID ||
				(root.Managed == nil && executionTarget.Worktree != nil) ||
				(root.Managed != nil && (executionTarget.Worktree == nil || executionTarget.Worktree.ID != root.Managed.WorktreeID)) {
				return nil, errors.New("planned Session execution target does not match Task")
			}
			if err := metadata.InsertPreparedSession(ctx, tx, *assignment.Snapshot); err != nil {
				return nil, err
			}
		} else if target.SessionID == nil || *target.SessionID != assignment.SessionID {
			return nil, errors.New("retained Session does not match planned target")
		} else {
			root := plan.executionTarget.executionRoot
			update := metadata.SessionExecutionTargetUpdate{
				SessionID:  assignment.SessionID.String(),
				Workspace:  &metadata.SessionExecutionTargetUpdateWorkspace{ID: root.SourceWorkspaceID},
				CwdRelpath: ".",
			}
			if root.Managed != nil {
				update.Worktree = &metadata.SessionExecutionTargetUpdateWorktree{ID: root.Managed.WorktreeID}
			}
			if err := metadata.UpdateSessionExecutionTargetInTransaction(ctx, tx, update); err != nil {
				return nil, err
			}
		}
		target.SessionID = &assignment.SessionID
		source, err := workflow.NewExactMaterializedContinuationSource(assignment.SessionID)
		if err != nil {
			return nil, err
		}
		target.ContinuationSource = source
	}
	if len(assignments) != 0 {
		return nil, errors.New("Session assignment has no Agent target")
	}
	return targets, nil
}
