package taskimport

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"core/internal/testharness/testsetup"
	"core/server/metadata"
	worktree "core/server/worktreeservice"
	taskimportpb "core/shared/protoapi/gen/kent/api/task_import"
	"core/shared/serverapi"
	"google.golang.org/protobuf/proto"
)

func TestDestinationFromNestedWorkspaceAndExplicitProject(t *testing.T) {
	store := testsetup.OpenStore(t, t.TempDir())
	root := t.TempDir()
	binding, err := store.CreateProjectForWorkspaceWithKey(t.Context(), root, "Import", "IMP")
	if err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(root, "nested")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	for _, request := range []*taskimportpb.Request{
		{Cwd: child},
		{Cwd: t.TempDir(), Project: proto.String(binding.ProjectID)},
		{Cwd: t.TempDir(), Project: proto.String(root)},
	} {
		destination, err := resolveDestination(t.Context(), store, request)
		if err != nil {
			t.Fatal(err)
		}
		if destination != binding.ProjectID {
			t.Fatalf("got %q, want %q", destination, binding.ProjectID)
		}
	}
}

func TestRepositorySelection(t *testing.T) {
	store := testsetup.OpenStore(t, t.TempDir())
	main := t.TempDir()
	cwd := t.TempDir()
	git := func(root string, args ...string) {
		t.Helper()
		if output, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%s %v", output, err)
		}
	}
	git(main, "init")
	git(main, "remote", "add", "origin", "https://github.com/main/repo.git")
	binding, err := store.CreateProjectForWorkspaceWithKey(t.Context(), main, "Main", "MAIN")
	if err != nil {
		t.Fatal(err)
	}
	inspector := worktree.NewGitInspector(nil)
	request := &taskimportpb.Request{Cwd: cwd}
	check := func(owner string) {
		t.Helper()
		repo, err := resolveRepository(t.Context(), store, inspector, request, binding.ProjectID)
		if err != nil || repo.Owner != owner {
			t.Fatalf("%+v %v", repo, err)
		}
	}
	check("main")
	git(cwd, "init")
	check("main")
	git(cwd, "remote", "add", "origin", "git@github.com:cwd/repo.git")
	check("cwd")
	git(cwd, "remote", "add", "another", "https://github.com/other/repo.git")
	if _, err := resolveRepository(t.Context(), store, inspector, request, binding.ProjectID); err == nil {
		t.Fatal("ambiguous remotes accepted")
	}
	request.Repository = proto.String("explicit/repo")
	check("explicit")
}

func TestDestinationRequiresResolutionForSharedWorkspace(t *testing.T) {
	store := testsetup.OpenStore(t, t.TempDir())
	root := t.TempDir()
	first, err := store.CreateProjectForWorkspaceWithKey(t.Context(), root, "One", "ONE")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateProjectForWorkspaceWithKey(t.Context(), t.TempDir(), "Two", "TWO")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AttachWorkspaceToProject(t.Context(), second.ProjectID, root); err != nil {
		t.Fatal(err)
	}
	_, err = resolveDestination(t.Context(), store, &taskimportpb.Request{Cwd: root})
	var ambiguous serverapi.WorkspaceBindingAmbiguousError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("expected ambiguity, got %v", err)
	}
	destination, err := resolveDestination(t.Context(), store, &taskimportpb.Request{Cwd: root, Project: proto.String(first.ProjectID)})
	if err != nil || destination != first.ProjectID {
		t.Fatalf("%s %v", destination, err)
	}
}

func TestDestinationFromManagedWorktree(t *testing.T) {
	store := testsetup.OpenStore(t, t.TempDir())
	binding, err := store.CreateProjectForWorkspaceWithKey(t.Context(), t.TempDir(), "Managed", "MAN")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	err = store.UpsertWorktreeRecord(t.Context(), metadata.WorktreeRecord{
		ID: "managed-worktree", WorkspaceID: binding.WorkspaceID, CanonicalRoot: root, GitMetadataJSON: "{}",
	})
	if err != nil {
		t.Fatal(err)
	}
	destination, err := resolveDestination(t.Context(), store, &taskimportpb.Request{Cwd: root})
	if err != nil || destination != binding.ProjectID {
		t.Fatalf("%s %v", destination, err)
	}
}
