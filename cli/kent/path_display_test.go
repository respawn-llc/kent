package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
)

func TestProjectListPathDisplay(t *testing.T) {
	fixture := newWorktreeCommandFixture(t)
	root, err := filepath.EvalSymlinks(fixture.a.CanonicalRoot)
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Dir(root)
	t.Setenv("HOME", home)
	t.Chdir(fixture.a.CanonicalRoot)
	var out, stderr bytes.Buffer
	if code := projectListSubcommand(nil, &out, &stderr); code != 0 {
		t.Fatalf("project list exited %d: %s", code, &stderr)
	}
	found := false
	for line := range strings.SplitSeq(strings.TrimSpace(out.String()), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			t.Fatalf("invalid project list row: %q", line)
		}
		if fields[0] == fixture.a.ProjectID {
			found = true
			want := "./"
			if fields[2] != want {
				t.Fatalf("project list path = %q, want %q", fields[2], want)
			}
		}
	}
	if !found {
		t.Fatal("project list omitted fixture project")
	}
}

func TestWorktreeListPathDisplayPreservesNonPathSelectors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, "external")
	entries := []*worktreepb.ListEntry{
		{Topology: &worktreepb.TopologyEntry{Topology: &worktreepb.TopologyEntry_External{
			External: &worktreepb.ExternalFacts{Git: &worktreepb.GitFacts{CanonicalRoot: path}},
		}}, Projection: &worktreepb.ListProjection{Selector: path}},
	}
	var out bytes.Buffer
	writeWorktreeList(&out, entries, false)
	fields := strings.Split(strings.TrimSpace(out.String()), "\t")
	if fields[0] != "~/external" || entries[0].Projection.Selector != path {
		t.Fatalf("worktree list path = %q; canonical selector = %q", fields[0], entries[0].Projection.Selector)
	}
	entries[0].Projection.Selector = "feature/path-like-branch"
	out.Reset()
	writeWorktreeList(&out, entries, false)
	if fields := strings.Split(strings.TrimSpace(out.String()), "\t"); fields[0] != entries[0].Projection.Selector {
		t.Fatalf("non-path selector changed: %q", fields[0])
	}
}
