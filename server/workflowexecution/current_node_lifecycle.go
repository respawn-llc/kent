package workflowexecution

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"core/server/sessionruntime"
	"core/server/workflow"
	"core/server/workflowruntime"
	"core/server/workflowstore"
	"core/shared/runtimeids"
)

var ErrManualMoveLifecycleConflict = errors.New("workflow task has a non-interruptible lifecycle conflict")

func (c *CurrentNodeController) StartTask(
	ctx context.Context,
	taskID workflow.TaskID,
	candidate *workflowstore.ExecutionTargetCandidate,
) (workflowstore.StartTaskResult, error) {
	if c == nil {
		return workflowstore.StartTaskResult{}, errors.New("current node workflow controller is required")
	}
	return runCurrentNodeTaskMutation(ctx, c, taskID, func(ctx context.Context) (workflowstore.StartTaskResult, error) {
		if err := c.EnsureTaskQuiescent(taskID); err != nil {
			return workflowstore.StartTaskResult{}, err
		}
		plan, err := c.store.PlanTaskStart(ctx, taskID, candidate)
		if err != nil {
			return workflowstore.StartTaskResult{}, err
		}
		starts, sessions, err := c.prepareStarts(ctx, plan.StartContexts(), workflowruntime.TaskPromptDeliveryAssignment)
		if err != nil {
			return workflowstore.StartTaskResult{}, err
		}
		started, err := c.store.CommitTaskStart(ctx, plan, sessions)
		if err != nil {
			return workflowstore.StartTaskResult{}, err
		}
		return started, c.queuePreparedStarts(starts)
	})
}

func (c *CurrentNodeController) prepareStarts(
	ctx context.Context,
	inputs []workflowstore.CurrentNodeStartContext,
	delivery workflowruntime.TaskPromptDelivery,
) ([]currentNodeQueuedStart, []workflowstore.PlannedCurrentNodeSession, error) {
	starts := make([]currentNodeQueuedStart, 0, len(inputs))
	sessions := make([]workflowstore.PlannedCurrentNodeSession, 0, len(inputs))
	for _, input := range inputs {
		nodeDelivery := delivery
		if input.CurrentNode.SessionID == nil {
			nodeDelivery = workflowruntime.TaskPromptDeliveryAssignment
		}
		prepared, err := c.runner.PrepareCurrentNode(ctx, input, nodeDelivery)
		if err != nil {
			return nil, nil, err
		}
		if prepared.Session != nil {
			sessions = append(sessions, *prepared.Session)
		}
		starts = append(starts, currentNodeQueuedStart{
			reference:          input.CurrentNode.Reference,
			nodeKind:           input.Node.Kind,
			taskPromptDelivery: nodeDelivery,
			assignmentSteer:    prepared.Assignment,
			scheduling:         workflow.CurrentNodeSchedulingAdmitted,
			completion:         newCurrentNodeAdmissionCompletion(),
		})
	}
	return starts, sessions, nil
}

func (c *CurrentNodeController) queuePreparedStarts(starts []currentNodeQueuedStart) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, start := range starts {
		if err := c.queueExplicitStartLocked(start); err != nil {
			return err
		}
	}
	c.wakeAdmissionWorker()
	return nil
}

type TaskResumeOutcome string

const (
	TaskResumeApplied TaskResumeOutcome = "applied"
	TaskResumeNoOp    TaskResumeOutcome = "no_op"
)

type TaskResumeResult struct {
	Outcome      TaskResumeOutcome
	CurrentNodes []workflow.CurrentNode
}

type TaskResumePreflightOutcome string

const (
	TaskResumePreflightResumable TaskResumePreflightOutcome = "resumable"
	TaskResumePreflightNoOp      TaskResumePreflightOutcome = "no_op"
)

type TaskResumePreflight struct {
	Outcome      TaskResumePreflightOutcome
	CurrentNodes []workflow.CurrentNode
}

func (c *CurrentNodeController) ResumeTask(ctx context.Context, taskID workflow.TaskID, candidate *workflowstore.ExecutionTargetCandidate) (TaskResumeResult, error) {
	result, _, err := c.resumeTask(ctx, taskID, candidate, nil)
	return result, err
}

func (c *CurrentNodeController) ReactivateWorkflowSession(
	ctx context.Context,
	sessionID runtimeids.SessionID,
) (sessionruntime.ExecutionHandle, error) {
	if c == nil {
		return nil, errors.New("current node workflow controller is required")
	}
	if sessionID.IsZero() {
		return nil, errors.New("session id is required")
	}
	input, err := c.store.ResolveCurrentSessionStartContext(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if handle, live := c.authority.SessionExecution(sessionID); live {
		return c.validateReactivatedWorkflowExecution(handle, sessionID, input.CurrentNode.Reference)
	}
	result, completion, err := c.resumeTask(
		ctx,
		input.Task.ID,
		nil,
		&input.CurrentNode.Reference,
	)
	if err != nil {
		return nil, err
	}
	if result.Outcome != TaskResumeApplied && result.Outcome != TaskResumeNoOp {
		return nil, fmt.Errorf("workflow Session reactivation returned invalid Resume outcome %q", result.Outcome)
	}
	if completion != nil {
		handle, waitErr := completion.wait(ctx)
		if waitErr != nil {
			return nil, fmt.Errorf("reactivate workflow Session %s: %w", sessionID, waitErr)
		}
		return c.validateReactivatedWorkflowExecution(handle, sessionID, input.CurrentNode.Reference)
	}
	handle, live := c.authority.SessionExecution(sessionID)
	if !live {
		return nil, &TaskResumeConflictError{TaskID: input.Task.ID}
	}
	return c.validateReactivatedWorkflowExecution(handle, sessionID, input.CurrentNode.Reference)
}

func (c *CurrentNodeController) validateReactivatedWorkflowExecution(
	handle sessionruntime.ExecutionHandle,
	sessionID runtimeids.SessionID,
	currentNode workflow.CurrentNodeReference,
) (sessionruntime.ExecutionHandle, error) {
	return c.authority.ValidateLiveWorkflowAgentExecution(handle, sessionID, currentNode)
}

func (c *CurrentNodeController) currentNodePreparingLocked(
	key workflow.CurrentNodeReferenceKey,
) bool {
	for _, start := range c.explicitQueue {
		startKey, err := start.reference.Key()
		if err != nil {
			panic(fmt.Sprintf("inspect explicit admission queue: %v", err))
		}
		if startKey == key {
			return true
		}
	}
	for entry := c.automaticQueue.first; entry != nil; entry = entry.globalNext {
		startKey, err := entry.start.reference.Key()
		if err != nil {
			panic(fmt.Sprintf("inspect automatic admission queue: %v", err))
		}
		if startKey == key {
			return true
		}
	}
	if _, exists := c.explicitReservations[key]; exists {
		return true
	}
	if _, exists := c.automaticReservations[key]; exists {
		return true
	}
	if _, exists := c.admissionWorkers[key]; exists {
		return true
	}
	return false
}

func (c *CurrentNodeController) WorkflowSessionPreparing(
	ctx context.Context,
	sessionID runtimeids.SessionID,
) (bool, error) {
	if c == nil {
		return false, errors.New("current node workflow controller is required")
	}
	if sessionID.IsZero() {
		return false, errors.New("session id is required")
	}
	input, err := c.store.ResolveCurrentSessionStartContext(ctx, sessionID)
	if err != nil {
		return false, err
	}
	if handle, live := c.authority.SessionExecution(sessionID); live {
		if _, err := c.validateReactivatedWorkflowExecution(handle, sessionID, input.CurrentNode.Reference); err != nil {
			return false, err
		}
		return false, nil
	}
	key, err := input.CurrentNode.Reference.Key()
	if err != nil {
		return false, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.currentNodePreparingLocked(key), nil
}

// PromoteConcurrencyQueuedTask moves automatic Current Nodes waiting for agent
// capacity into the explicit admission lane. Explicit admission intentionally
// bypasses the automatic concurrency limit.
func (c *CurrentNodeController) PromoteConcurrencyQueuedTask(
	ctx context.Context,
	taskID workflow.TaskID,
) ([]workflow.CurrentNode, bool, error) {
	if c == nil {
		return nil, false, errors.New("current node workflow controller is required")
	}
	if strings.TrimSpace(string(taskID)) == "" {
		return nil, false, errors.New("workflow task id is required")
	}
	var promoted []workflow.CurrentNode
	err := c.runTaskMutation(ctx, taskID, func(context.Context) error {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.closed {
			return errors.New("current node workflow controller is closed")
		}
		starts := c.automaticQueue.removeTask(taskID)
		for _, start := range starts {
			key, err := start.reference.Key()
			if err != nil {
				return err
			}
			delete(c.queued, key)
			start.policy = currentNodeAdmissionExplicitOverride
			c.explicitQueue = append(c.explicitQueue, start)
			c.explicitQueued[key] = struct{}{}
			promoted = append(promoted, workflow.CurrentNode{Reference: start.reference})
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	if len(promoted) == 0 {
		return nil, false, nil
	}
	c.wakeAdmissionWorker()
	return promoted, true, nil
}

type TaskResumeConflictError struct {
	TaskID workflow.TaskID
}

func (e *TaskResumeConflictError) Error() string {
	return fmt.Sprintf("task %q has no interrupted executable Current Nodes to resume", e.TaskID)
}

func (c *CurrentNodeController) PreflightTaskResume(
	ctx context.Context,
	taskID workflow.TaskID,
) (TaskResumePreflight, error) {
	if c == nil {
		return TaskResumePreflight{}, errors.New("current node workflow controller is required")
	}
	return runCurrentNodeTaskMutation(ctx, c, taskID, func(ctx context.Context) (TaskResumePreflight, error) {
		classification, err := c.classifyTaskResume(ctx, taskID, nil)
		if err != nil {
			return TaskResumePreflight{}, err
		}
		if len(classification.alreadyResumed) != 0 {
			return TaskResumePreflight{
				Outcome:      TaskResumePreflightNoOp,
				CurrentNodes: classification.alreadyResumed,
			}, nil
		}
		if err := classification.eligibilityError(); err != nil {
			return TaskResumePreflight{}, err
		}
		return TaskResumePreflight{
			Outcome:      TaskResumePreflightResumable,
			CurrentNodes: classification.resumable,
		}, nil
	})
}

type taskResumeClassification struct {
	resumable      []workflow.CurrentNode
	alreadyResumed []workflow.CurrentNode
	validationErr  error
}

func (c *CurrentNodeController) classifyTaskResume(
	ctx context.Context,
	taskID workflow.TaskID,
	selected *workflow.CurrentNodeReference,
) (taskResumeClassification, error) {
	c.mu.Lock()
	if err := c.ensureTaskAvailableLocked(taskID); err != nil {
		c.mu.Unlock()
		return taskResumeClassification{}, err
	}
	c.mu.Unlock()
	// Explicit Resume owns reconciliation. Reads and server startup leave saved
	// admission state untouched; live or queued work must never be reconciled.
	if err := c.EnsureTaskQuiescent(taskID); err == nil {
		if err := c.store.ReconcileTaskResume(ctx, taskID); err != nil {
			return taskResumeClassification{}, err
		}
	} else if !errors.Is(err, ErrTaskExecutionNotQuiescent) {
		return taskResumeClassification{}, err
	}
	classifications, err := c.store.PreflightTaskResume(ctx, taskID)
	if err != nil {
		return taskResumeClassification{}, err
	}
	if len(classifications) == 0 {
		currentNodes, err := c.store.ListCurrentNodes(ctx, taskID)
		if err != nil {
			return taskResumeClassification{}, err
		}
		for _, currentNode := range currentNodes {
			if selected != nil && !currentNode.Reference.Equal(*selected) {
				continue
			}
			if currentNode.Scheduling == nil {
				continue
			}
			switch currentNode.Scheduling.State {
			case workflow.CurrentNodeSchedulingReady, workflow.CurrentNodeSchedulingAdmitted:
				return taskResumeClassification{alreadyResumed: []workflow.CurrentNode{currentNode}}, nil
			}
		}
		return taskResumeClassification{}, &TaskResumeConflictError{TaskID: taskID}
	}
	result := taskResumeClassification{
		resumable: make([]workflow.CurrentNode, 0, len(classifications)),
	}
	var validationErrs []error
	for _, classification := range classifications {
		if selected != nil && !classification.CurrentNode.Reference.Equal(*selected) {
			continue
		}
		if validationErr := classification.ValidationError(); validationErr != nil {
			validationErrs = append(validationErrs, validationErr)
			continue
		}
		result.resumable = append(result.resumable, classification.CurrentNode)
	}
	result.validationErr = errors.Join(validationErrs...)
	return result, nil
}

func (c taskResumeClassification) eligibilityError() error {
	if len(c.resumable) != 0 {
		return nil
	}
	return c.validationErr
}

func (c *CurrentNodeController) resumeTask(
	ctx context.Context,
	taskID workflow.TaskID,
	candidate *workflowstore.ExecutionTargetCandidate,
	watch *workflow.CurrentNodeReference,
) (TaskResumeResult, *currentNodeAdmissionCompletion, error) {
	if c == nil {
		return TaskResumeResult{}, nil, errors.New("current node workflow controller is required")
	}
	var watchedCompletion *currentNodeAdmissionCompletion
	result, err := runCurrentNodeTaskMutation(ctx, c, taskID, func(ctx context.Context) (TaskResumeResult, error) {
		classification, err := c.classifyTaskResume(ctx, taskID, watch)
		if err != nil {
			return TaskResumeResult{}, err
		}
		if len(classification.alreadyResumed) != 0 {
			return TaskResumeResult{
				Outcome:      TaskResumeNoOp,
				CurrentNodes: classification.alreadyResumed,
			}, nil
		}
		var resumeErrs []error
		if classification.validationErr != nil {
			resumeErrs = append(resumeErrs, classification.validationErr)
		}
		eligible := make([]workflow.CurrentNodeReference, 0, len(classification.resumable))
		seen := make(map[workflow.CurrentNodeReferenceKey]struct{}, len(classification.resumable))
		for _, currentNode := range classification.resumable {
			key, keyErr := currentNode.Reference.Key()
			if keyErr != nil {
				resumeErrs = append(resumeErrs, keyErr)
				continue
			}
			if currentNode.SessionID != nil {
				if active, exists := c.authority.SessionExecution(*currentNode.SessionID); exists {
					resumeErrs = append(
						resumeErrs,
						fmt.Errorf(
							"resume current node %v: retained Session %s already has active execution scope %s: %w",
							currentNode.Reference,
							*currentNode.SessionID,
							active.Scope().ID(),
							ErrTaskExecutionNotQuiescent,
						),
					)
					continue
				}
			}
			if _, duplicate := seen[key]; duplicate {
				resumeErrs = append(resumeErrs, fmt.Errorf("resumable current node %v is duplicated", currentNode.Reference))
				continue
			}
			seen[key] = struct{}{}
			eligible = append(eligible, currentNode.Reference)
		}
		c.mu.Lock()
		if err := c.ensureTaskAvailableLocked(taskID); err != nil {
			c.mu.Unlock()
			return TaskResumeResult{}, errors.Join(errors.Join(resumeErrs...), err)
		}
		for _, reference := range eligible {
			key, _ := reference.Key()
			if c.currentNodeOwnedLocked(key) {
				c.mu.Unlock()
				return TaskResumeResult{}, errors.Join(
					errors.Join(resumeErrs...),
					fmt.Errorf("current node %v cannot resume while controller ownership remains: %w", reference, ErrTaskExecutionNotQuiescent),
				)
			}
		}
		c.mu.Unlock()

		if len(eligible) == 0 {
			return TaskResumeResult{}, errors.Join(resumeErrs...)
		}
		plan, err := c.store.PlanTaskResume(ctx, taskID, eligible, candidate)
		if err != nil {
			return TaskResumeResult{}, errors.Join(errors.Join(resumeErrs...), err)
		}
		starts, sessions, err := c.prepareStarts(ctx, plan.StartContexts(), workflowruntime.TaskPromptDeliveryResume)
		if err != nil {
			return TaskResumeResult{}, errors.Join(errors.Join(resumeErrs...), err)
		}
		resumed, err := c.store.CommitTaskResume(ctx, plan, sessions)
		if err != nil {
			return TaskResumeResult{}, errors.Join(errors.Join(resumeErrs...), err)
		}
		c.finalizeTaskAttentionResolution(resumed.TaskAttentionResolution)
		if err := c.queuePreparedStarts(starts); err != nil {
			resumeErrs = append(resumeErrs, err)
		}
		for _, start := range starts {
			if watch != nil && start.reference.Equal(*watch) {
				watchedCompletion = start.completion
			}
		}
		return TaskResumeResult{
			Outcome:      TaskResumeApplied,
			CurrentNodes: resumed.CurrentNodes,
		}, errors.Join(resumeErrs...)
	})
	return result, watchedCompletion, err
}

func (c *CurrentNodeController) ApplyPendingApproval(
	ctx context.Context,
	approvalID workflow.ApprovalID,
) (workflowstore.PendingApprovalApplyResult, error) {
	var result workflowstore.PendingApprovalApplyResult
	err := c.RunTaskOperation(ctx, func(ctx context.Context) error {
		var err error
		result, err = c.applyPendingApproval(ctx, approvalID)
		return err
	})
	return result, err
}

func (c *CurrentNodeController) applyPendingApproval(
	ctx context.Context,
	approvalID workflow.ApprovalID,
) (workflowstore.PendingApprovalApplyResult, error) {
	if c == nil {
		return workflowstore.PendingApprovalApplyResult{}, errors.New("current node workflow controller is required")
	}
	initial, err := c.store.PendingApproval(ctx, approvalID)
	if err != nil {
		return workflowstore.PendingApprovalApplyResult{}, err
	}
	scope, err := c.store.TaskExecutionScope(ctx, initial.Source.TaskID)
	if err != nil {
		return workflowstore.PendingApprovalApplyResult{}, err
	}
	if source, live := c.authority.ExecutionByCurrentNode(scope.ProjectID, scope.WorkflowID, initial.Source); live {
		// The source may have published its Approval before completing its
		// post-turn compaction. Do not hold the Task lane while it finalizes.
		if _, err := source.Wait(ctx); err != nil {
			return workflowstore.PendingApprovalApplyResult{}, err
		}
	}
	var starts []currentNodeQueuedStart
	applied, err := runCurrentNodeTaskMutation(ctx, c, initial.Source.TaskID, func(ctx context.Context) (workflowstore.PendingApprovalApplyResult, error) {
		approval, err := c.store.PendingApproval(ctx, approvalID)
		if err != nil {
			return workflowstore.PendingApprovalApplyResult{}, err
		}
		c.mu.Lock()
		if err := c.ensureTaskAvailableLocked(approval.Source.TaskID); err != nil {
			c.mu.Unlock()
			return workflowstore.PendingApprovalApplyResult{}, err
		}
		c.mu.Unlock()

		plan, err := c.store.PlanPendingApproval(ctx, approvalID)
		if err != nil {
			return workflowstore.PendingApprovalApplyResult{}, err
		}
		contexts := plan.StartContexts()
		if err := c.restoreExecutionRoot(ctx, contexts); err != nil {
			return workflowstore.PendingApprovalApplyResult{}, err
		}
		preparedStarts, sessions, err := c.prepareStarts(ctx, contexts, workflowruntime.TaskPromptDeliveryAssignment)
		if err != nil {
			return workflowstore.PendingApprovalApplyResult{}, err
		}
		applied, err := c.store.CommitPendingApproval(ctx, plan, sessions)
		if err != nil {
			return workflowstore.PendingApprovalApplyResult{}, err
		}
		starts = preparedStarts
		return applied, nil
	})
	if err != nil {
		return applied, err
	}
	starts, err = c.steerAndWaitStarts(ctx, starts)
	if err != nil {
		return applied, err
	}
	err = c.runTaskMutation(ctx, initial.Source.TaskID, func(context.Context) error {
		c.mu.Lock()
		defer c.mu.Unlock()
		for _, start := range starts {
			if err := c.queueExplicitStartLocked(start); err != nil {
				return err
			}
		}
		return nil
	})
	return applied, err
}

func (c *CurrentNodeController) RunTaskOperation(ctx context.Context, operation func(context.Context) error) error {
	if c == nil || ctx == nil || operation == nil {
		return errors.New("Task operation owner, context, and operation are required")
	}
	c.mu.Lock()
	if c.closed || c.closing {
		c.mu.Unlock()
		return errors.New("current node workflow controller is closed")
	}
	c.operationWG.Add(1)
	c.mu.Unlock()
	defer c.operationWG.Done()
	owned, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	shutdown := errors.New("workflow controller shut down")
	stop := context.AfterFunc(c.workerContext, func() { cancel(shutdown) })
	defer stop()
	defer cancel(nil)
	if c.workerContext.Err() != nil {
		return shutdown
	}
	return operation(context.WithValue(owned, currentNodeLifecycleContextKey{}, c))
}

func (c *CurrentNodeController) ApplyManualMove(
	ctx context.Context,
	prepared workflowstore.ManualMovePreparation,
	candidate *workflowstore.ExecutionTargetCandidate,
) (workflowstore.ManualMoveResult, error) {
	if c == nil {
		return workflowstore.ManualMoveResult{}, errors.New("current node workflow controller is required")
	}
	taskID := prepared.TaskID()
	return runCurrentNodeTaskMutation(ctx, c, taskID, func(ctx context.Context) (workflowstore.ManualMoveResult, error) {
		if err := c.EnsureTaskQuiescent(taskID); err != nil {
			return workflowstore.ManualMoveResult{}, err
		}
		plan, err := c.store.PlanManualMove(ctx, prepared, candidate)
		if err != nil {
			return workflowstore.ManualMoveResult{}, err
		}
		starts, sessions, err := c.prepareStarts(ctx, plan.StartContexts(), workflowruntime.TaskPromptDeliveryAssignment)
		if err != nil {
			return workflowstore.ManualMoveResult{}, err
		}
		moved, err := c.store.CommitManualMove(ctx, plan, sessions)
		if err != nil || moved.Outcome == workflowstore.ManualMoveResultOutcomeNoOp {
			return moved, err
		}
		return moved, c.queuePreparedStarts(starts)
	})
}

// EnsureTaskQuiescent rejects Task-wide state replacement while the
// controller owns live, admitted, or automatic work for the Task. Callers
// hold the Task mutation lane while invoking it and applying the durable
// replacement.
func (c *CurrentNodeController) EnsureTaskQuiescent(taskID workflow.TaskID) error {
	if c == nil {
		return errors.New("current node workflow controller is required")
	}
	if taskID == "" {
		return errors.New("workflow task id is required")
	}
	live, err := c.authority.HasLiveWorkflowTaskExecution(taskID)
	if err != nil {
		return err
	}
	if live {
		return ErrTaskExecutionNotQuiescent
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ensureTaskQuiescentLocked(taskID)
}

func (c *CurrentNodeController) ensureTaskQuiescentLocked(taskID workflow.TaskID) error {
	if err := c.ensureTaskAvailableLocked(taskID); err != nil {
		return err
	}
	if !c.taskExecutionQuiescentLocked(taskID) {
		return ErrTaskExecutionNotQuiescent
	}
	return nil
}

func (c *CurrentNodeController) taskExecutionQuiescentLocked(taskID workflow.TaskID) bool {
	if c.interrupts.taskActive(taskID) {
		return false
	}
	for entry := c.automaticQueue.first; entry != nil; entry = entry.globalNext {
		start := entry.start
		if start.reference.TaskID == taskID {
			return false
		}
	}
	for _, intent := range c.automaticReservations {
		if intent.reference.TaskID == taskID {
			return false
		}
	}
	for _, start := range c.explicitQueue {
		if start.reference.TaskID == taskID {
			return false
		}
	}
	for _, start := range c.explicitReservations {
		if start.reference.TaskID == taskID {
			return false
		}
	}
	for _, start := range c.admissionWorkers {
		if start.reference.TaskID == taskID {
			return false
		}
	}
	return true
}

func (c *CurrentNodeController) ensureTaskAvailableLocked(taskID workflow.TaskID) error {
	if err := c.ensureAvailableLocked(); err != nil {
		return err
	}
	if c.interrupts.taskActive(taskID) {
		return ErrTaskExecutionNotQuiescent
	}
	return nil
}

func (c *CurrentNodeController) ensureAvailableLocked() error {
	if c.closed {
		return errors.New("current node workflow controller is closed")
	}
	if c.workerErr != nil {
		return fmt.Errorf("workflow execution lifecycle failed: %w", c.workerErr)
	}
	return nil
}
