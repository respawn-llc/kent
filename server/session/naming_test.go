package session

import (
	"sync"
	"testing"

	"core/shared/textutil"
)

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
