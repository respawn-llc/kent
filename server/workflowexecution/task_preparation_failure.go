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
