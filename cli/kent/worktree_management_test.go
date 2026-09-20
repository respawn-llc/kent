package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"core/internal/testharness/testsetup"
	"core/server/auth"
	"core/server/bootstrap"
	"core/server/core"
	"core/server/metadata"
	"core/server/session"
	"core/server/transport"
	"core/shared/client"
	"core/shared/config"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/protocol"
	"core/shared/sessioncontract"
	"core/shared/worktreecontract"

	"google.golang.org/protobuf/encoding/protojson"
	"mvdan.cc/sh/v3/shell"
)

type worktreeCommandFixture struct {
	core        *core.Core
	a, b, other metadata.Binding
	sessionID   string
}

func newWorktreeCommandFixture(t *testing.T) worktreeCommandFixture {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KENT_SESSION_ID", "")
	t.Setenv(config.PersistenceRootEnvName, t.TempDir())
	root := t.TempDir()
	t.Chdir(root)
	testsetup.InitializeGitRepository(t, root)
	cfg, err := config.Load(root, root, config.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	authSupport, err := bootstrap.BuildAuthSupport(auth.NewMemoryStore(auth.EmptyState()), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Settings = testsetup.WriteProviderSettings(t, cfg.PersistenceRoot, cfg.Settings)
	background, err := bootstrap.BuildShellManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = background.Close() })
	app, err := core.New(cfg, authSupport, background)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	store := app.MetadataStore()
	a, err := store.RegisterWorkspaceBinding(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	bRoot, otherRoot := t.TempDir(), t.TempDir()
	testsetup.InitializeGitRepository(t, bRoot)
	testsetup.InitializeGitRepository(t, otherRoot)
	other, err := store.RegisterWorkspaceBinding(context.Background(), otherRoot)
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.AttachWorkspaceToProject(context.Background(), other.ProjectID, bRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetProjectDefaultWorkspace(context.Background(), b.ProjectID, b.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	s, err := session.Create(filepath.Join(cfg.PersistenceRoot, "projects", a.ProjectID, "sessions"),
		"caller", root, sessioncontract.SessionCategoryMain, store.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureDurable(); err != nil {
		t.Fatal(err)
	}
	gateway, err := transport.NewGateway(app, protocol.ServerIdentity{ProtocolVersion: protocol.Version, ServerID: "test", PID: os.Getpid(), PersistenceRootID: config.PersistenceRootHash(cfg.PersistenceRoot)})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(gateway.Handler())
	t.Cleanup(server.Close)
	host, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("KENT_SERVER_HOST", host)
	t.Setenv("KENT_SERVER_PORT", port)
	return worktreeCommandFixture{core: app, a: a, b: b, other: other, sessionID: s.Meta().SessionID}
}

func TestWorktreeCommandExplicitProjectList(t *testing.T) {
	f := newWorktreeCommandFixture(t)
	t.Setenv("KENT_SESSION_ID", f.sessionID)
	t.Chdir(t.TempDir())
	var out, stderr bytes.Buffer
	code := worktreeSubcommand([]string{"list", "--project", f.b.ProjectID, "--json"}, &out, &stderr)
	if code != 0 {
		t.Fatalf("list exit %d: %s", code, &stderr)
	}
	var response worktreepb.WorkspaceListSuccess
	if err := protojson.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.WorkspaceId != f.b.WorkspaceID {
		t.Fatalf("workspace = %s, want %s", response.WorkspaceId, f.b.WorkspaceID)
	}
	for _, entry := range response.Worktrees {
		if entry.Projection.GetIsCurrent() {
			t.Fatal("explicit listing marked caller")
		}
	}
}

func TestWorktreeCommandListSelection(t *testing.T) {
	f := newWorktreeCommandFixture(t)
	for _, tc := range []struct {
		name      string
		args      []string
		agent     bool
		workspace string
		marker    bool
		reject    bool
	}{
		{"other workspace", []string{"ls", "--project", f.b.ProjectID, "--workspace", f.other.WorkspaceID}, true, f.other.WorkspaceID, false, false},
		{"workspace inferred from agent", []string{"list", "--workspace", f.a.WorkspaceID}, true, f.a.WorkspaceID, false, false},
		{"implicit agent", []string{"list"}, true, f.a.WorkspaceID, true, false},
		{"human session", []string{"list", "--session", f.sessionID}, false, f.a.WorkspaceID, true, false},
		{"current directory", []string{"list"}, false, f.other.WorkspaceID, false, false},
		{"workspace inferred from directory", []string{"list", "--workspace", f.b.WorkspaceID}, false, f.b.WorkspaceID, false, false},
		{"foreign workspace", []string{"list", "--project", f.b.ProjectID, "--workspace", f.a.WorkspaceID}, true, "", false, true},
		{"invalid project", []string{"list", "--project", "missing"}, true, "", false, true},
		{"blank project", []string{"list", "--project", ""}, true, "", false, true},
		{"blank workspace", []string{"list", "--workspace", " "}, true, "", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(f.other.CanonicalRoot)
			if tc.agent {
				t.Setenv("KENT_SESSION_ID", f.sessionID)
			} else {
				t.Setenv("KENT_SESSION_ID", "")
			}
			var out, stderr bytes.Buffer
			code := worktreeSubcommand(append(tc.args, "--json"), &out, &stderr)
			if tc.reject {
				if code == 0 || out.Len() != 0 {
					t.Fatalf("rejected selection returned %d: %s", code, &out)
				}
				return
			}
			if code != 0 {
				t.Fatalf("list exit %d: %s", code, &stderr)
			}
			var entries []*worktreepb.ListEntry
			var workspace string
			if tc.marker {
				var response worktreepb.ListSuccess
				if err := protojson.Unmarshal(out.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if response.Target.WorkspaceId == nil {
					t.Fatal("Session Worktree target omitted its Workspace")
				}
				workspace, entries = *response.Target.WorkspaceId, response.Worktrees
			} else {
				var response worktreepb.WorkspaceListSuccess
				if err := protojson.Unmarshal(out.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				workspace, entries = response.WorkspaceId, response.Worktrees
			}
			if workspace != tc.workspace {
				t.Fatalf("workspace = %s, want %s", workspace, tc.workspace)
			}
			if len(entries) != 1 || entries[0].Projection.GetIsCurrent() != tc.marker {
				t.Fatalf("unexpected current projection: %v", entries)
			}
		})
	}
}

func TestWorktreeCommandSessionlessCreate(t *testing.T) {
	f := newWorktreeCommandFixture(t)
	var out, stderr bytes.Buffer
	code := worktreeSubcommand([]string{"create", "--project", f.b.ProjectID, "--json", "sessionless"}, &out, &stderr)
	if code != 0 {
		t.Fatalf("create exit %d: %s", code, &stderr)
	}
	var response worktreepb.CreateSuccess
	if err := protojson.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Target != nil {
		t.Fatalf("sessionless creation manufactured target: %v", response.Target)
	}
	entries, err := f.core.MetadataStore().ListWorktreeRecordsByWorkspaceID(context.Background(), f.b.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("selected workspace worktrees: %v", entries)
	}
}

func TestWorktreeCommandCrossProjectCreate(t *testing.T) {
	f := newWorktreeCommandFixture(t)
	t.Setenv("KENT_SESSION_ID", f.sessionID)
	t.Chdir(t.TempDir())
	if err := os.Mkdir(filepath.Join(f.a.CanonicalRoot, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := f.core.MetadataStore().UpdateSessionExecutionTarget(context.Background(), metadata.SessionExecutionTargetUpdate{
		SessionID:  f.sessionID,
		Workspace:  &metadata.SessionExecutionTargetUpdateWorkspace{ID: f.a.WorkspaceID},
		CwdRelpath: "nested",
	}); err != nil {
		t.Fatal(err)
	}
	before, err := f.core.MetadataStore().ResolveSessionExecutionTarget(context.Background(), f.sessionID)
	if err != nil {
		t.Fatal(err)
	}
	for _, binding := range []metadata.Binding{f.b, f.other} {
		t.Run(binding.WorkspaceID, func(t *testing.T) {
			testsetup.RunGit(t, binding.CanonicalRoot, "branch", "selected-base")
			payloadPath := filepath.Join(t.TempDir(), "payload.json")
			if err := os.MkdirAll(filepath.Join(binding.CanonicalRoot, ".kent"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(binding.CanonicalRoot, ".kent", "config.toml"), []byte("[worktrees]\nsetup_script = 'setup.sh'\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(binding.CanonicalRoot, "setup.sh"),
				[]byte(fmt.Sprintf("#!/bin/sh\nprintf '%%s' \"$KENT_WORKTREE_PAYLOAD_JSON\" > %q\n", payloadPath)), 0o755); err != nil {
				t.Fatal(err)
			}
			args := []string{"create", "--project", f.b.ProjectID, "--session", "must-not-be-the-caller", "--base", "selected-base", "--json"}
			if binding.WorkspaceID == f.other.WorkspaceID {
				args = append(args, "--workspace", binding.WorkspaceID)
			}
			args = append(args, "cross-project")
			var out, stderr bytes.Buffer
			if code := worktreeSubcommand(args, &out, &stderr); code != 0 {
				t.Fatalf("create exit %d: %s", code, &stderr)
			}
			var response worktreepb.CreateSuccess
			if err := protojson.Unmarshal(out.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Target.GetWorkspaceId() != f.a.WorkspaceID || response.Target.GetEffectiveWorkdir() != before.EffectiveWorkdir {
				t.Fatalf("caller location lost: %v", response.Target)
			}
			record, err := f.core.MetadataStore().GetWorktreeRecordByID(context.Background(), response.Worktree.Topology.GetRegistered().Kent.WorktreeId)
			if err != nil {
				t.Fatal(err)
			}
			if record.WorkspaceID != binding.WorkspaceID || record.OriginSessionID != f.sessionID {
				t.Fatalf("created attribution: %v", record)
			}
			var payload struct {
				ProjectID           string  `json:"project_id"`
				WorkspaceID         string  `json:"workspace_id"`
				SessionID           *string `json:"session_id"`
				SourceWorkspaceRoot string  `json:"source_workspace_root"`
				WorktreeRoot        string  `json:"worktree_root"`
			}
			data, err := os.ReadFile(payloadPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &payload); err != nil {
				t.Fatal(err)
			}
			if payload.ProjectID != binding.ProjectID || payload.WorkspaceID != binding.WorkspaceID ||
				payload.SessionID == nil || *payload.SessionID != f.sessionID ||
				payload.SourceWorkspaceRoot != binding.CanonicalRoot || payload.WorktreeRoot != record.CanonicalRoot {
				t.Fatalf("setup scope: %+v", payload)
			}
		})
	}
	after, err := f.core.MetadataStore().ResolveSessionExecutionTarget(context.Background(), f.sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("management moved caller: before=%+v after=%+v", before, after)
	}
}

func TestWorktreeCommandSessionlessCreateFromDirectory(t *testing.T) {
	f := newWorktreeCommandFixture(t)
	t.Chdir(f.other.CanonicalRoot)
	var out, stderr bytes.Buffer
	if code := worktreeSubcommand([]string{"create", "human-directory"}, &out, &stderr); code != 0 {
		t.Fatalf("create exit %d: %s", code, &stderr)
	}
	records, err := f.core.MetadataStore().ListWorktreeRecordsByWorkspaceID(context.Background(), f.other.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || out.String() != records[0].CanonicalRoot+"\n" {
		t.Fatalf("sessionless output=%q worktrees=%v", &out, records)
	}
	out.Reset()
	stderr.Reset()
	if code := worktreeSubcommand([]string{"rm", records[0].ID}, &out, &stderr); code != 0 {
		t.Fatalf("sessionless directory delete exit %d: %s", code, &stderr)
	}
	if _, err := os.Stat(records[0].CanonicalRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sessionless deletion left root: %v", err)
	}
}

func TestWorktreeCommandCreateEnterHint(t *testing.T) {
	f := newWorktreeCommandFixture(t)
	for _, agent := range []bool{true, false} {
		t.Run(fmt.Sprint(agent), func(t *testing.T) {
			if agent {
				t.Setenv("KENT_SESSION_ID", f.sessionID)
			}
			var out, stderr bytes.Buffer
			root := filepath.Join(f.core.Config().Settings.Worktrees.BaseDir, fmt.Sprintf("worktree %t with ' quotes", agent))
			args := []string{"create", "--project", f.b.ProjectID, "--session", f.sessionID, fmt.Sprintf("hint-%t", agent), root}
			if code := worktreeSubcommand(args, &out, &stderr); code != 0 {
				t.Fatalf("create exit %d: %s", code, &stderr)
			}
			enter := []string{config.Command, "worktree", "enter"}
			if !agent {
				enter = append(enter, "--session", f.sessionID)
			}
			canonical, err := config.CanonicalWorkspaceRoot(root)
			if err != nil {
				t.Fatal(err)
			}
			enter = append(enter, canonical)
			lines := bufio.NewScanner(strings.NewReader(out.String()))
			if !lines.Scan() || lines.Text() != canonical {
				t.Fatalf("create output must start with its canonical root: %q", &out)
			}
			if !lines.Scan() {
				t.Fatalf("create output omitted the enter hint: %q", &out)
			}
			words, err := shell.Fields(lines.Text(), nil)
			if err != nil {
				t.Fatalf("parse enter hint arguments: %v", err)
			}
			if len(words) < len(enter) || !reflect.DeepEqual(words[len(words)-len(enter):], enter) {
				t.Fatalf("enter hint arguments = %q, want trailing command %q", words, enter)
			}
			if lines.Scan() || lines.Err() != nil {
				t.Fatalf("unexpected trailing create output or read error: %q, %v", &out, lines.Err())
			}
			target, err := f.core.MetadataStore().ResolveSessionExecutionTarget(context.Background(), f.sessionID)
			if err != nil {
				t.Fatal(err)
			}
			if target.WorkspaceId == nil || *target.WorkspaceId != f.a.WorkspaceID || target.Worktree != nil {
				t.Fatalf("create performed navigation: %+v", target)
			}
		})
	}
}

func TestWorktreeCommandCrossProjectDelete(t *testing.T) {
	f := newWorktreeCommandFixture(t)
	before, err := f.core.MetadataStore().ResolveSessionExecutionTarget(context.Background(), f.sessionID)
	if err != nil {
		t.Fatal(err)
	}
	for _, agent := range []bool{true, false} {
		for _, binding := range []metadata.Binding{f.b, f.other} {
			for _, policy := range []struct {
				name     string
				flags    []string
				unmerged bool
				outcome  worktreepb.BranchCleanupOutcomeKind
			}{
				{name: "retain", outcome: worktreepb.BranchCleanupOutcomeKind_WORKTREE_BRANCH_CLEANUP_OUTCOME_NOT_REQUESTED},
				{name: "safe", flags: []string{"--delete-branch"}, outcome: worktreepb.BranchCleanupOutcomeKind_WORKTREE_BRANCH_CLEANUP_OUTCOME_DELETED},
				{name: "safe-unmerged", flags: []string{"--delete-branch"}, unmerged: true, outcome: worktreepb.BranchCleanupOutcomeKind_WORKTREE_BRANCH_CLEANUP_OUTCOME_RETAINED},
				{name: "force", flags: []string{"--delete-branch", "--force-delete-branch"}, unmerged: true, outcome: worktreepb.BranchCleanupOutcomeKind_WORKTREE_BRANCH_CLEANUP_OUTCOME_DELETED},
			} {
				t.Run(fmt.Sprintf("%t/%s/%s", agent, binding.WorkspaceID, policy.name), func(t *testing.T) {
					if agent {
						t.Setenv("KENT_SESSION_ID", f.sessionID)
					}
					t.Chdir(f.a.CanonicalRoot)
					selectors := []string{"--project", binding.ProjectID}
					if binding.WorkspaceID == f.other.WorkspaceID {
						selectors = append(selectors, "--workspace", binding.WorkspaceID)
					}
					branch := fmt.Sprintf("delete-%t-%s", agent, policy.name)
					var out, stderr bytes.Buffer
					args := append([]string{"create", "--json"}, selectors...)
					args = append(args, branch)
					if code := worktreeSubcommand(args, &out, &stderr); code != 0 {
						t.Fatalf("create exit %d: %s", code, &stderr)
					}
					var created worktreepb.CreateSuccess
					if err := protojson.Unmarshal(out.Bytes(), &created); err != nil {
						t.Fatal(err)
					}
					facts := created.Worktree.Topology.GetRegistered()
					if policy.unmerged {
						testsetup.RunGit(t, facts.Git.CanonicalRoot, "commit", "--allow-empty", "-m", "unmerged work")
					}
					out.Reset()
					stderr.Reset()
					command := "delete"
					if !agent {
						command = "remove"
					}
					args = append([]string{command, "--json"}, selectors...)
					args = append(args, policy.flags...)
					args = append(args, facts.Kent.WorktreeId)
					if code := worktreeSubcommand(args, &out, &stderr); code != 0 {
						t.Fatalf("delete exit %d: %s", code, &stderr)
					}
					var result worktreepb.DeleteSuccess
					if err := protojson.Unmarshal(out.Bytes(), &result); err != nil {
						t.Fatal(err)
					}
					if result.GetCleanup().GetKind() != policy.outcome {
						t.Fatalf("cleanup = %v, want %v", result.GetCleanup(), policy.outcome)
					}
					if policy.outcome == worktreepb.BranchCleanupOutcomeKind_WORKTREE_BRANCH_CLEANUP_OUTCOME_RETAINED &&
						(result.GetCleanup().GetBranchName() != branch || result.GetCleanup().GetDiagnostic() == "") {
						t.Fatalf("retained cleanup omitted branch or diagnostic: %v", result.GetCleanup())
					}
					if _, err := os.Stat(facts.Git.CanonicalRoot); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("worktree directory remains: %v", err)
					}
					if _, err := f.core.MetadataStore().GetWorktreeRecordByID(context.Background(), facts.Kent.WorktreeId); !errors.Is(err, sql.ErrNoRows) {
						t.Fatalf("worktree record remains: %v", err)
					}
					branchRef := testsetup.RunGit(t, binding.CanonicalRoot, "for-each-ref", "--format=%(refname)", "refs/heads/"+branch)
					deleted := policy.outcome == worktreepb.BranchCleanupOutcomeKind_WORKTREE_BRANCH_CLEANUP_OUTCOME_DELETED
					if !deleted && strings.TrimSpace(branchRef) != "refs/heads/"+branch {
						t.Fatalf("retained branch missing: %q", branchRef)
					}
					if deleted && strings.TrimSpace(branchRef) != "" {
						t.Fatalf("branch cleanup skipped: %q", branchRef)
					}
				})
			}
		}
	}
	after, err := f.core.MetadataStore().ResolveSessionExecutionTarget(context.Background(), f.sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("deletion moved caller: before=%+v after=%+v", before, after)
	}
}

func TestWorktreeCommandRejectedManagementLeavesStateUnchanged(t *testing.T) {
	f := newWorktreeCommandFixture(t)
	t.Setenv("KENT_SESSION_ID", f.sessionID)
	var out, stderr bytes.Buffer
	if code := worktreeSubcommand([]string{"create", "--project", f.b.ProjectID, "--json", "protected"}, &out, &stderr); code != 0 {
		t.Fatalf("create exit %d: %s", code, &stderr)
	}
	var created worktreepb.CreateSuccess
	if err := protojson.Unmarshal(out.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	facts := created.Worktree.Topology.GetRegistered()
	cfg, err := config.Load(f.a.CanonicalRoot, f.a.CanonicalRoot, config.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	remote, err := client.DialConfiguredRemoteForProjectWorkspaceID(context.Background(), cfg, f.b.ProjectID, f.b.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = remote.Close() }()
	preview, err := remote.PreviewWorktreeDelete(context.Background(), &worktreepb.DeletePreviewRequest{
		Scope:    worktreecontract.WorkspaceManagementScope(f.b.ProjectID, f.b.WorkspaceID, &f.sessionID),
		Selector: facts.Kent.WorktreeId,
	})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Cleanliness.Kind != worktreepb.DirtyStateKind_DIRTY_STATE_CLEAN || preview.DeletionSelector != facts.Kent.WorktreeId {
		t.Fatalf("wrong preview: %v", preview)
	}
	if err := os.WriteFile(filepath.Join(facts.Git.CanonicalRoot, "dirty"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := f.core.MetadataStore().ResolveSessionExecutionTarget(context.Background(), f.sessionID)
	if err != nil {
		t.Fatal(err)
	}
	records, err := f.core.MetadataStore().ListWorktreeRecordsByWorkspaceID(context.Background(), f.b.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	gitBefore := testsetup.RunGit(t, f.b.CanonicalRoot, "worktree", "list", "--porcelain")
	branchesBefore := testsetup.RunGit(t, f.b.CanonicalRoot, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads")
	for _, args := range [][]string{
		{"delete", "--project", f.b.ProjectID, facts.Kent.WorktreeId},
		{"delete", "--project", f.b.ProjectID, "--force-delete-branch", "--force", facts.Kent.WorktreeId},
		{"delete", "--project", "missing", "--force", facts.Kent.WorktreeId},
		{"delete", "--project", f.b.ProjectID, "--workspace", f.a.WorkspaceID, "--force", facts.Kent.WorktreeId},
		{"delete", "--project", f.b.ProjectID, "--force", "missing"},
		{"delete", "--project", f.b.ProjectID, "--force", f.b.CanonicalRoot},
		{"create", "--project", "missing", "must-not-exist"},
		{"create", "--project", f.b.ProjectID, "--workspace", f.a.WorkspaceID, "must-not-exist"},
	} {
		out.Reset()
		stderr.Reset()
		if code := worktreeSubcommand(args, &out, &stderr); code == 0 || out.Len() != 0 {
			t.Fatalf("accepted rejected management %v: %s", args, &out)
		}
		after, err := f.core.MetadataStore().ResolveSessionExecutionTarget(context.Background(), f.sessionID)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("rejection changed caller: %+v", after)
		}
		afterRecords, err := f.core.MetadataStore().ListWorktreeRecordsByWorkspaceID(context.Background(), f.b.WorkspaceID)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(records, afterRecords) {
			t.Fatalf("rejection changed records: %v", afterRecords)
		}
		if got := testsetup.RunGit(t, f.b.CanonicalRoot, "worktree", "list", "--porcelain"); got != gitBefore {
			t.Fatalf("rejection changed Git worktrees: %s", got)
		}
		if got := testsetup.RunGit(t, f.b.CanonicalRoot, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads"); got != branchesBefore {
			t.Fatalf("rejection changed branches: %s", got)
		}
	}
	out.Reset()
	stderr.Reset()
	if code := worktreeSubcommand([]string{"delete", "--project", f.b.ProjectID, "--force", facts.Kent.WorktreeId}, &out, &stderr); code != 0 {
		t.Fatalf("authorized dirty deletion exit %d: %s", code, &stderr)
	}
	if _, err := os.Stat(facts.Git.CanonicalRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("authorized delete did not remove root: %v", err)
	}
}

func TestWorktreeCommandLeaveRequiresSession(t *testing.T) {
	f := newWorktreeCommandFixture(t)
	before, err := f.core.MetadataStore().ResolveSessionExecutionTarget(context.Background(), f.sessionID)
	if err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := worktreeSubcommand([]string{"leave"}, &out, &stderr); code != 2 || out.Len() != 0 || stderr.Len() == 0 {
		t.Fatalf("missing-session leave exit %d stdout=%q stderr=%q", code, &out, &stderr)
	}
	after, err := f.core.MetadataStore().ResolveSessionExecutionTarget(context.Background(), f.sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("missing-session leave changed target: %+v", after)
	}
	t.Log(stderr.String())
}

func TestWorktreeCommandLeaveWithSession(t *testing.T) {
	f := newWorktreeCommandFixture(t)
	var out, stderr bytes.Buffer
	if code := worktreeSubcommand([]string{"create", "--json", "leave-target"}, &out, &stderr); code != 0 {
		t.Fatalf("create exit %d: %s", code, &stderr)
	}
	var created worktreepb.CreateSuccess
	if err := protojson.Unmarshal(out.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	for _, agent := range []bool{false, true} {
		t.Run(fmt.Sprint(agent), func(t *testing.T) {
			if agent {
				t.Setenv("KENT_SESSION_ID", f.sessionID)
			}
			err := f.core.MetadataStore().UpdateSessionExecutionTarget(context.Background(), metadata.SessionExecutionTargetUpdate{
				SessionID:  f.sessionID,
				Workspace:  &metadata.SessionExecutionTargetUpdateWorkspace{ID: f.a.WorkspaceID},
				Worktree:   &metadata.SessionExecutionTargetUpdateWorktree{ID: created.Worktree.Topology.GetRegistered().Kent.WorktreeId},
				CwdRelpath: ".",
			})
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"leave", "--json"}
			if !agent {
				args = append(args, "--session", f.sessionID)
			}
			out.Reset()
			stderr.Reset()
			if code := worktreeSubcommand(args, &out, &stderr); code != 0 {
				t.Fatalf("leave exit %d: %s", code, &stderr)
			}
			var ack worktreepb.ScheduledAcknowledgement
			if err := protojson.Unmarshal(out.Bytes(), &ack); err != nil {
				t.Fatal(err)
			}
			if ack.OperationId == "" {
				t.Fatal("leave omitted operation acknowledgement")
			}
			testsetup.RequireUntil(t, time.Now().Add(5*time.Second), 10*time.Millisecond, func() bool {
				target, err := f.core.MetadataStore().ResolveSessionExecutionTarget(context.Background(), f.sessionID)
				if err != nil {
					t.Fatal(err)
				}
				return target.WorkspaceId != nil && *target.WorkspaceId == f.a.WorkspaceID && target.Worktree == nil
			}, "leave did not return the Session to its main Workspace")
		})
	}
}
