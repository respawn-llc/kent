package session

import "testing"

func newSessionTestStore(t *testing.T) *Store {
	t.Helper()
	return newSessionTestStoreAt(t, t.TempDir())
}

func newSessionTestStoreAt(t *testing.T, root string) *Store {
	t.Helper()
	store, err := Create(root, "workspace-x", "/tmp/work")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	return store
}

func newSessionTestLazyStore(t *testing.T) *Store {
	t.Helper()
	return newSessionTestLazyStoreAt(t, t.TempDir())
}

func newSessionTestLazyStoreAt(t *testing.T, root string) *Store {
	t.Helper()
	store, err := NewLazy(root, "workspace-x", "/tmp/work")
	if err != nil {
		t.Fatalf("new lazy store: %v", err)
	}
	return store
}
