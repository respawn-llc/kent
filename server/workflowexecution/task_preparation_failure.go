package workflowexecution

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"core/server/workflow"
)

func taskPreparationReferenceLess(left, right workflow.CurrentNodeReference) bool {
	if left.NodeID != right.NodeID {
		return left.NodeID < right.NodeID
	}
	leftBranch, leftScoped := left.TransitionBranchKey()
	rightBranch, rightScoped := right.TransitionBranchKey()
	if leftScoped != rightScoped {
		return !leftScoped
	}
	return leftScoped && leftBranch < rightBranch
}

func persistTaskPreparationFailure(
	ctx context.Context,
	references []workflow.CurrentNodeReference,
	cause error,
	persist func(context.Context, workflow.CurrentNodeReference, workflow.CurrentNodeInterruptionReason, workflow.CurrentNodeInterruptionDetail) error,
) ([]workflow.CurrentNodeReference, error) {
	if len(references) == 0 {
		return nil, errors.New("Task preparation failure requires an executable Current Node")
	}
	ordered := append([]workflow.CurrentNodeReference(nil), references...)
	sort.Slice(ordered, func(i, j int) bool { return taskPreparationReferenceLess(ordered[i], ordered[j]) })
	detail := workflow.NewCurrentNodeInterruptionDetail(string(reasonCurrentNodeRuntimeStartFailed), cause)
	canonicalDetail := detail
	var preparationErr *TaskStartPreparationError
	if errors.As(cause, &preparationErr) {
		canonicalDetail = preparationErr.InterruptionDetail()
	}
	interrupted := make([]workflow.CurrentNodeReference, 0, len(ordered))
	for _, reference := range ordered[1:] {
		if err := persist(ctx, reference, reasonCurrentNodeRuntimeStartFailed, detail); err != nil {
			return interrupted, fmt.Errorf("interrupt Task preparation sibling %v: %w", reference, err)
		}
		interrupted = append(interrupted, reference)
	}
	canonical := ordered[0]
	if err := persist(ctx, canonical, reasonCurrentNodeRuntimeStartFailed, canonicalDetail); err != nil {
		return interrupted, fmt.Errorf("interrupt canonical Task preparation Current Node %v: %w", canonical, err)
	}
	return append([]workflow.CurrentNodeReference{canonical}, interrupted...), nil
}

func (c *CurrentNodeController) RecordTaskPreparationFailure(ctx context.Context, taskID workflow.TaskID, cause error) error {
	replacementStore, ok := c.store.(interface {
		ReplaceCurrentNodeInterruptionWithPreparationFailure(context.Context, workflow.CurrentNodeReference, workflow.CurrentNodeInterruptionReason, workflow.CurrentNodeInterruptionDetail) error
	})
	if !ok {
		return errors.New("Task preparation recovery persistence is required")
	}
	var interrupted []workflow.CurrentNodeReference
	err := c.runTaskMutation(ctx, taskID, func(ctx context.Context) error {
		if err := c.EnsureTaskQuiescent(taskID); err != nil {
			return err
		}
		nodes, err := c.store.ListCurrentNodes(ctx, taskID)
		if err != nil {
			return err
		}
		references := make([]workflow.CurrentNodeReference, 0, len(nodes))
		scheduling := make(map[workflow.CurrentNodeReferenceKey]*workflow.CurrentNodeScheduling)
		for _, node := range nodes {
			if node.Scheduling == nil {
				continue
			}
			key, err := node.Reference.Key()
			if err != nil {
				return err
			}
			references = append(references, node.Reference)
			scheduling[key] = node.Scheduling
		}
		interrupted, err = persistTaskPreparationFailure(ctx, references, cause, func(
			ctx context.Context, reference workflow.CurrentNodeReference, reason workflow.CurrentNodeInterruptionReason, detail workflow.CurrentNodeInterruptionDetail,
		) error {
			key, err := reference.Key()
			if err != nil {
				return err
			}
			if previous := scheduling[key].Interruption; previous != nil {
				return replacementStore.ReplaceCurrentNodeInterruptionWithPreparationFailure(ctx, reference, previous.Reason, detail)
			}
			return c.store.InterruptCurrentNode(ctx, reference, reason, detail)
		})
		return err
	})
	for _, reference := range interrupted {
		c.publishPendingInterruptedCurrentNode(ctx, reference, reasonCurrentNodeRuntimeStartFailed)
	}
	return err
}
