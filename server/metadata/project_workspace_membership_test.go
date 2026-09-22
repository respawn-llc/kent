package metadata

import (
	"errors"
	"path/filepath"
	"testing"

	"core/shared/serverapi"
)

func TestContainingProjectWorkspace(t *testing.T) {
	store, _, source := newMetadataTestStore(t)
	for _, test := range []struct {
		path string
		want bool
	}{
		{filepath.Join(source.CanonicalRoot, "nested", "new.txt"), true},
		{source.CanonicalRoot + "-sibling", false},
	} {
		workspace, err := store.FindContainingProjectWorkspace(t.Context(), source.ProjectID, test.path)
		if err != nil {
			t.Fatal(err)
		}
		if (workspace != nil) != test.want || workspace != nil && workspace.ID != source.WorkspaceID {
			t.Fatalf("membership for %q = %+v", test.path, workspace)
		}
	}
}

func TestContainingProjectWorkspaceLearnsLaterAttachmentAndChoosesBroadestRoot(t *testing.T) {
	store, _, source := newMetadataTestStore(t)
	root := t.TempDir()
	path := filepath.Join(root, "child", "file.txt")
	if workspace, err := store.FindContainingProjectWorkspace(t.Context(), source.ProjectID, path); err != nil || workspace != nil {
		t.Fatalf("before attach: %+v, %v", workspace, err)
	}
	broad, err := store.AttachWorkspaceToProject(t.Context(), source.ProjectID, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AttachWorkspaceToProject(t.Context(), source.ProjectID, filepath.Join(root, "child")); err != nil {
		t.Fatal(err)
	}
	workspace, err := store.FindContainingProjectWorkspace(t.Context(), source.ProjectID, filepath.Join(broad.CanonicalRoot, "child", "file.txt"))
	if err != nil || workspace == nil || workspace.ID != broad.WorkspaceID {
		t.Fatalf("after attach: %+v, %v", workspace, err)
	}
}

func TestContainingProjectWorkspaceRejectsMissingProject(t *testing.T) {
	store, _, source := newMetadataTestStore(t)
	if _, err := store.FindContainingProjectWorkspace(t.Context(), "missing-project", source.CanonicalRoot); !errors.Is(err, serverapi.ErrProjectNotFound) {
		t.Fatalf("missing Project: %v", err)
	}
}
