package serverapi

import (
	"errors"
	"testing"

	"core/shared/clientui"
)

func TestPendingWorkCapacityDirectAndNested(t *testing.T) {
	direct := &PendingWorkCapacityError{}
	var typed *PendingWorkCapacityError
	if !errors.Is(direct, ErrPendingWorkCapacity) ||
		!errors.As(direct, &typed) {
		t.Fatalf("direct capacity error = %T %v", direct, direct)
	}

	nested := NewRuntimeCommandNotAcceptedError(&PendingWorkCapacityError{})
	if !errors.Is(nested, ErrPendingWorkCapacity) || !errors.As(nested, &typed) {
		t.Fatalf("nested capacity error = %T %v", nested, nested)
	}

}

func TestPendingWorkIdentityViewsReuseDomainUUID(t *testing.T) {
	t.Parallel()
	worktreeID := clientui.NewWorktreeTransitionID()
	got, err := PendingWorkItemIDFromWorktreeOperation(worktreeID)
	if err != nil || got.String() != worktreeID.String() {
		t.Fatalf("Worktree Pending Work id = %q, %v", got, err)
	}
}
