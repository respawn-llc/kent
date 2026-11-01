package session

import (
	"os"
	"sync"
	"testing"

	"core/shared/textutil"
)

func TestInitialPromptPreviewNoOpDoesNotPersist(t *testing.T) {
	for _, dispatched := range []bool{false, true} {
		t.Run(map[bool]string{false: "saved preview", true: "continued without preview"}[dispatched], func(t *testing.T) {
			observer := &recordingPersistenceObserver{}
			store, err := Create(t.TempDir(), "ws", t.TempDir(), testSessionCategory, WithPersistenceObserver(observer), WithPersistedSessionResolver(observer))
			if err != nil {
				t.Fatal(err)
			}
			want := ""
			if dispatched {
				if err := store.MarkModelDispatchLocked(LockedContract{Model: "gpt-6-sol", SystemPrompt: "prompt", HasSystemPrompt: true}); err != nil {
					t.Fatal(err)
				}
			} else {
				want = "Original task"
				if err := store.CaptureInitialPromptPreview(want); err != nil {
					t.Fatal(err)
				}
			}
			observer.err = os.ErrPermission
			if err := store.CaptureInitialPromptPreview("Later input"); err != nil {
				t.Fatalf("unchanged preview attempted persistence: %v", err)
			}
			if store.Meta().FirstPromptPreview != want {
				t.Fatal("later input replaced the original task")
			}
		})
	}
}

func TestGeneratedNameOnlyWhileUnnamed(t *testing.T) {
	store := newSessionTestLazyStore(t)
	var pending sync.WaitGroup
	start := make(chan struct{})
	pending.Go(func() {
		<-start
		if _, err := store.MutateNameIfUnnamed("generated title"); err != nil {
			t.Error(err)
		}
	})
	pending.Go(func() {
		<-start
		if _, err := store.MutateName(textutil.Value("manual title")); err != nil {
			t.Error(err)
		}
	})
	close(start)
	pending.Wait()
	if name := store.Meta().Name; name == nil || *name != "manual title" {
		t.Fatalf("manual name was overwritten: %v", name)
	}
	mutation, err := store.MutateNameIfUnnamed("another generated title")
	if err != nil || mutation.Committed || mutation.Changed {
		t.Fatalf("named Session changed: %+v, %v", mutation, err)
	}
}
