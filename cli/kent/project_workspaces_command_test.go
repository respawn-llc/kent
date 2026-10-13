package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectWorkspacesSelection(t *testing.T) {
	fixture := newWorktreeCommandFixture(t)
	for _, test := range []struct {
		name               string
		sessionID          string
		args               []string
		wantProjectID      string
		wantWorkspaceCount int
		wantFailure        bool
	}{
		{
			name:               "explicit workspace path overrides invalid Session",
			sessionID:          "not-a-session-selector",
			args:               []string{"workspaces", "--project", fixture.b.CanonicalRoot, "--json"},
			wantProjectID:      fixture.b.ProjectID,
			wantWorkspaceCount: 2,
		},
		{
			name:               "Session Project",
			sessionID:          fixture.sessionID,
			args:               []string{"workspaces", "--json"},
			wantProjectID:      fixture.a.ProjectID,
			wantWorkspaceCount: 1,
		},
		{
			name:        "missing selection",
			args:        []string{"workspaces"},
			wantFailure: true,
		},
		{
			name:        "invalid Session selection",
			sessionID:   "not-a-session-selector",
			args:        []string{"workspaces"},
			wantFailure: true,
		},
		{
			name:        "invalid explicit Project does not fall back to Session",
			sessionID:   fixture.sessionID,
			args:        []string{"workspaces", "--project", "missing-project"},
			wantFailure: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("KENT_SESSION_ID", test.sessionID)
			var stdout, stderr bytes.Buffer
			exitCode := projectSubcommand(test.args, &stdout, &stderr)
			if test.wantFailure {
				if exitCode == 0 || stdout.Len() != 0 || stderr.Len() == 0 {
					t.Fatalf("invalid selection returned %d, stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
				}
				return
			}
			if exitCode != 0 {
				t.Fatalf("Project selection returned %d: %s", exitCode, stderr.String())
			}
			var response projectWorkspacesListOutput
			if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
				t.Fatalf("decode Project workspaces response: %v; output=%q", err, stdout.String())
			}
			if response.ProjectID != test.wantProjectID || len(response.Workspaces) != test.wantWorkspaceCount {
				t.Fatalf("Project selection response = %+v", response)
			}
		})
	}
}

func TestProjectWorkspacesCatalogReadFailureIsVisible(t *testing.T) {
	fixture := newWorktreeCommandFixture(t)
	if err := fixture.core.MetadataStore().Close(); err != nil {
		t.Fatalf("close metadata store to exercise catalog read failure: %v", err)
	}

	var stdout, stderr bytes.Buffer
	exitCode := projectSubcommand(
		[]string{"workspaces", "--project", fixture.b.ProjectID},
		&stdout,
		&stderr,
	)
	if exitCode == 0 || stdout.Len() != 0 || stderr.Len() == 0 {
		t.Fatalf(
			"catalog read failure returned exit %d, stdout=%q stderr=%q",
			exitCode,
			stdout.String(),
			stderr.String(),
		)
	}
}

func TestProjectWorkspacesPlainOutputIsHomeShortenedPathBullets(t *testing.T) {
	fixture := newWorktreeCommandFixture(t)
	home := t.TempDir()
	canonicalHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatalf("resolve fixture home: %v", err)
	}
	t.Setenv("HOME", canonicalHome)
	t.Setenv("KENT_SESSION_ID", "invalid-session-is-ignored")
	workspaceRoot := filepath.Join(canonicalHome, "selected")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("create Workspace fixture: %v", err)
	}
	binding, err := fixture.core.MetadataStore().AttachWorkspaceToProject(
		t.Context(),
		fixture.b.ProjectID,
		workspaceRoot,
	)
	if err != nil {
		t.Fatalf("attach Workspace fixture: %v", err)
	}
	if err := fixture.core.MetadataStore().SetProjectDefaultWorkspace(
		t.Context(),
		fixture.b.ProjectID,
		binding.WorkspaceID,
	); err != nil {
		t.Fatalf("set Workspace fixture as Project default: %v", err)
	}

	var stdout, stderr bytes.Buffer
	exitCode := projectSubcommand(
		[]string{"workspaces", "--project", fixture.b.ProjectID, "--limit", "1"},
		&stdout,
		&stderr,
	)
	if exitCode != 0 {
		t.Fatalf("Project workspaces exit code = %d: %s", exitCode, stderr.String())
	}
	if stdout.String() != "- ~/selected\n" {
		t.Fatalf("plain Workspace output = %q, want one home-shortened path bullet", stdout.String())
	}
	if stderr.Len() == 0 {
		t.Fatal("plain page omitted its pagination continuation")
	}

	if err := os.RemoveAll(workspaceRoot); err != nil {
		t.Fatalf("remove attached Workspace root: %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	exitCode = projectSubcommand(
		[]string{"workspaces", "--project", fixture.b.ProjectID, "--limit", "1", "--json"},
		&stdout,
		&stderr,
	)
	if exitCode != 0 {
		t.Fatalf("Project workspaces JSON exit code = %d: %s", exitCode, stderr.String())
	}
	var jsonResponse projectWorkspacesListOutput
	if err := json.Unmarshal(stdout.Bytes(), &jsonResponse); err != nil {
		t.Fatalf("decode Project workspaces JSON: %v; output=%q", err, stdout.String())
	}
	if jsonResponse.ProjectID != fixture.b.ProjectID ||
		jsonResponse.Offset != 0 ||
		jsonResponse.NextOffset == nil ||
		*jsonResponse.NextOffset != 1 ||
		len(jsonResponse.Workspaces) != 1 ||
		jsonResponse.Workspaces[0].WorkspaceID != binding.WorkspaceID ||
		jsonResponse.Workspaces[0].RootPath != workspaceRoot ||
		!jsonResponse.Workspaces[0].IsDefault {
		t.Fatalf("Project workspaces JSON = %+v, want canonical default catalog entry", jsonResponse)
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = projectSubcommand(
		[]string{"workspaces", "--project", fixture.b.ProjectID, "--offset", "1", "--limit", "1"},
		&stdout,
		&stderr,
	)
	if exitCode != 0 {
		t.Fatalf("external Project Workspace page exit code = %d: %s", exitCode, stderr.String())
	}
	catalogPage, err := fixture.core.MetadataStore().ListProjectWorkspaceCatalogPage(
		t.Context(),
		fixture.b.ProjectID,
		1,
		1,
	)
	if err != nil || len(catalogPage.Workspaces) != 1 {
		t.Fatalf("read expected external catalog row: page=%+v error=%v", catalogPage, err)
	}
	expectedRoot := catalogPage.Workspaces[0].CanonicalRoot
	if !filepath.IsAbs(expectedRoot) || stdout.String() != "- "+expectedRoot+"\n" {
		t.Fatalf("external Workspace path output = %q, want its absolute canonical path", stdout.String())
	}
}

func TestProjectWorkspacesDefaultPaginationAndEmptyPage(t *testing.T) {
	fixture := newWorktreeCommandFixture(t)
	for range 20 {
		if _, err := fixture.core.MetadataStore().AttachWorkspaceToProject(
			t.Context(),
			fixture.b.ProjectID,
			t.TempDir(),
		); err != nil {
			t.Fatalf("attach Workspace fixture: %v", err)
		}
	}

	var stdout, stderr bytes.Buffer
	exitCode := projectSubcommand(
		[]string{"workspaces", "--project", fixture.b.ProjectID, "--json"},
		&stdout,
		&stderr,
	)
	if exitCode != 0 {
		t.Fatalf("default Project workspaces page exit code = %d: %s", exitCode, stderr.String())
	}
	var firstPage projectWorkspacesListOutput
	if err := json.Unmarshal(stdout.Bytes(), &firstPage); err != nil {
		t.Fatalf("decode first Project workspaces page: %v; output=%q", err, stdout.String())
	}
	if firstPage.Offset != 0 ||
		firstPage.NextOffset == nil ||
		*firstPage.NextOffset != 20 ||
		len(firstPage.Workspaces) != 20 ||
		!firstPage.Workspaces[0].IsDefault {
		t.Fatalf("default Project workspaces page = %+v, want offset 0, limit 20, and next offset 20", firstPage)
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = projectSubcommand(
		[]string{"workspaces", "--project", fixture.b.ProjectID, "--offset", "20", "--json"},
		&stdout,
		&stderr,
	)
	if exitCode != 0 {
		t.Fatalf("second Project workspaces page exit code = %d: %s", exitCode, stderr.String())
	}
	var secondPage projectWorkspacesListOutput
	if err := json.Unmarshal(stdout.Bytes(), &secondPage); err != nil {
		t.Fatalf("decode second Project workspaces page: %v; output=%q", err, stdout.String())
	}
	if secondPage.Offset != 20 || secondPage.NextOffset != nil || len(secondPage.Workspaces) != 2 {
		t.Fatalf("final Project workspaces page = %+v, want two rows and no next offset", secondPage)
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = projectSubcommand(
		[]string{"workspaces", "--project", fixture.b.ProjectID, "--offset", "24", "--limit", "5"},
		&stdout,
		&stderr,
	)
	if exitCode != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("empty Project workspaces page = exit %d, stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
}

func TestProjectWorkspacesRejectsInvalidPaginationBeforeConnecting(t *testing.T) {
	fixture := newWorktreeCommandFixture(t)
	for _, args := range [][]string{
		{"workspaces", "--project", fixture.b.ProjectID, "--offset", "-1"},
		{"workspaces", "--project", fixture.b.ProjectID, "--limit", "0"},
		{"workspaces", "--project", fixture.b.ProjectID, "--limit", "101"},
		{"workspaces", "--project", fixture.b.ProjectID, "extra"},
	} {
		var stdout, stderr bytes.Buffer
		exitCode := projectSubcommand(args, &stdout, &stderr)
		if exitCode == 0 || stdout.Len() != 0 || stderr.Len() == 0 {
			t.Fatalf(
				"invalid Project workspaces arguments %q returned exit %d, stdout=%q stderr=%q",
				args,
				exitCode,
				stdout.String(),
				stderr.String(),
			)
		}
	}
}
