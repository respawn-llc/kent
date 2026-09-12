package serverapi

import (
	"errors"
	"testing"

	"core/shared/clientui"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
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
	compactionID := runtimeids.NewCompactionRequestID()
	got, err := PendingWorkItemIDFromCompactionRequest(compactionID)
	if err != nil || got.String() != compactionID.String() {
		t.Fatalf("compaction Pending Work id = %q, %v", got, err)
	}
	worktreeID := clientui.NewWorktreeTransitionID()
	got, err = PendingWorkItemIDFromWorktreeOperation(worktreeID)
	if err != nil || got.String() != worktreeID.String() {
		t.Fatalf("Worktree Pending Work id = %q, %v", got, err)
	}
}

func TestPendingWorkRemovalResponseValidatesTypedCanonicalRestoration(t *testing.T) {
	t.Parallel()
	valid := runtimeinput.PendingWorkRestoration{Kind: runtimeinput.PendingWorkItemKindWorktreeTransition, CanonicalInput: "/wt leave"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	invalid := valid
	invalid.CanonicalInput = ""
	if err := invalid.Validate(); err == nil {
		t.Fatal("Validate accepted missing canonical input")
	}
}
