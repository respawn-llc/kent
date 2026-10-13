package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestProjectWorkspacesExplicitProjectIgnoresInvalidSession(t *testing.T) {
	fixture := newWorktreeCommandFixture(t)
	t.Setenv("KENT_SESSION_ID", "not-a-session-selector")

	var stdout, stderr bytes.Buffer
	exitCode := projectSubcommand(
		[]string{"workspaces", "--project", fixture.b.CanonicalRoot, "--json"},
		&stdout,
		&stderr,
	)
	if exitCode != 0 {
		t.Fatalf("explicit Project workspaces exit code = %d: %s", exitCode, stderr.String())
	}
	var response struct {
		ProjectID  string `json:"project_id"`
		Workspaces []struct {
			WorkspaceID string `json:"workspace_id"`
		} `json:"workspaces"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode Project workspaces response: %v; output=%q", err, stdout.String())
	}
	if response.ProjectID != fixture.b.ProjectID || len(response.Workspaces) != 2 {
		t.Fatalf("explicit Project response = %+v, want Project %q with two attached Workspaces", response, fixture.b.ProjectID)
	}
}

func TestProjectWorkspacesHelpIsDispatched(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if exitCode := projectSubcommand([]string{"workspaces", "--help"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("Project workspaces help exit code = %d: %s", exitCode, stderr.String())
	}
	for _, flag := range []string{"--project", "--offset", "--limit", "--json"} {
		if !strings.Contains(stderr.String(), flag) {
			t.Fatalf("Project workspaces help omitted %s: %q", flag, stderr.String())
		}
	}
}

func TestProjectWorkspacesUsesSessionProject(t *testing.T) {
	fixture := newWorktreeCommandFixture(t)
	t.Setenv("KENT_SESSION_ID", fixture.sessionID)

	var stdout, stderr bytes.Buffer
	exitCode := projectSubcommand([]string{"workspaces", "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("Session Project workspaces exit code = %d: %s", exitCode, stderr.String())
	}
	var response struct {
		ProjectID  string `json:"project_id"`
		Workspaces []struct {
			WorkspaceID string `json:"workspace_id"`
			IsDefault   bool   `json:"is_default"`
		} `json:"workspaces"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode Project workspaces response: %v; output=%q", err, stdout.String())
	}
	if response.ProjectID != fixture.a.ProjectID ||
		len(response.Workspaces) != 1 ||
		response.Workspaces[0].WorkspaceID != fixture.a.WorkspaceID ||
		!response.Workspaces[0].IsDefault {
		t.Fatalf("Session Project response = %+v, want its one default Workspace", response)
	}
}

func TestProjectWorkspacesRequiresExplicitProjectWithoutSession(t *testing.T) {
	_ = newWorktreeCommandFixture(t)
	t.Setenv("KENT_SESSION_ID", "")

	var stdout, stderr bytes.Buffer
	exitCode := projectSubcommand([]string{"workspaces"}, &stdout, &stderr)
	if exitCode == 0 || stdout.Len() != 0 || stderr.Len() == 0 {
		t.Fatalf(
			"workspaces without Project or Session returned %d, stdout=%q stderr=%q",
			exitCode,
			stdout.String(),
			stderr.String(),
		)
	}
}

func TestProjectWorkspacesInvalidSessionDoesNotFallBackToCurrentWorkspace(t *testing.T) {
	_ = newWorktreeCommandFixture(t)
	t.Setenv("KENT_SESSION_ID", "not-a-session-selector")

	var stdout, stderr bytes.Buffer
	exitCode := projectSubcommand([]string{"workspaces"}, &stdout, &stderr)
	if exitCode == 0 || stdout.Len() != 0 || stderr.Len() == 0 {
		t.Fatalf(
			"workspaces with invalid Session fell back to cwd: exit=%d stdout=%q stderr=%q",
			exitCode,
			stdout.String(),
			stderr.String(),
		)
	}
}

func TestProjectWorkspacesInvalidProjectDoesNotFallBackToSessionProject(t *testing.T) {
	fixture := newWorktreeCommandFixture(t)
	t.Setenv("KENT_SESSION_ID", fixture.sessionID)

	var stdout, stderr bytes.Buffer
	exitCode := projectSubcommand(
		[]string{"workspaces", "--project", "missing-project"},
		&stdout,
		&stderr,
	)
	if exitCode == 0 || stdout.Len() != 0 || stderr.Len() == 0 {
		t.Fatalf("invalid explicit Project returned exit %d, stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
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
	if !strings.Contains(stderr.String(), strconv.Itoa(1)) {
		t.Fatalf("paging guidance omitted the next offset: %q", stderr.String())
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

func TestProjectWorkspacesJSONPreservesCanonicalCatalogPaths(t *testing.T) {
	fixture := newWorktreeCommandFixture(t)
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve fixture home: %v", err)
	}
	t.Setenv("HOME", home)
	workspaceRoot := filepath.Join(home, "canonical")
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
		[]string{"workspaces", "--project", fixture.b.ProjectID, "--limit", "1", "--json"},
		&stdout,
		&stderr,
	)
	if exitCode != 0 {
		t.Fatalf("Project workspaces JSON exit code = %d: %s", exitCode, stderr.String())
	}
	var response struct {
		ProjectID  string `json:"project_id"`
		Offset     int    `json:"offset"`
		NextOffset *int   `json:"next_offset"`
		Workspaces []struct {
			WorkspaceID string `json:"workspace_id"`
			RootPath    string `json:"root_path"`
			IsDefault   bool   `json:"is_default"`
		} `json:"workspaces"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode Project workspaces JSON: %v; output=%q", err, stdout.String())
	}
	if response.ProjectID != fixture.b.ProjectID ||
		response.Offset != 0 ||
		response.NextOffset == nil ||
		*response.NextOffset != 1 ||
		len(response.Workspaces) != 1 ||
		response.Workspaces[0].WorkspaceID != binding.WorkspaceID ||
		response.Workspaces[0].RootPath != workspaceRoot ||
		!response.Workspaces[0].IsDefault {
		t.Fatalf("Project workspaces JSON = %+v, want canonical default catalog entry", response)
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
	var firstPage struct {
		Offset     int  `json:"offset"`
		NextOffset *int `json:"next_offset"`
		Workspaces []struct {
			IsDefault bool `json:"is_default"`
		} `json:"workspaces"`
	}
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
	var secondPage struct {
		Offset     int               `json:"offset"`
		NextOffset *int              `json:"next_offset"`
		Workspaces []json.RawMessage `json:"workspaces"`
	}
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

func TestProjectWorkspacesCommandListsUnavailableAttachments(t *testing.T) {
	fixture := newWorktreeCommandFixture(t)
	removedRoot := t.TempDir()
	binding, err := fixture.core.MetadataStore().AttachWorkspaceToProject(
		t.Context(),
		fixture.b.ProjectID,
		removedRoot,
	)
	if err != nil {
		t.Fatalf("attach Workspace fixture: %v", err)
	}
	if err := os.RemoveAll(removedRoot); err != nil {
		t.Fatalf("remove attached Workspace root: %v", err)
	}

	var stdout, stderr bytes.Buffer
	exitCode := projectSubcommand(
		[]string{"workspaces", "--project", fixture.b.ProjectID, "--offset", "1", "--limit", "1", "--json"},
		&stdout,
		&stderr,
	)
	if exitCode != 0 {
		t.Fatalf("Project workspaces with unavailable attachment exit code = %d: %s", exitCode, stderr.String())
	}
	var response struct {
		Workspaces []struct {
			WorkspaceID string `json:"workspace_id"`
			RootPath    string `json:"root_path"`
		} `json:"workspaces"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode unavailable attachment page: %v; output=%q", err, stdout.String())
	}
	if len(response.Workspaces) != 1 ||
		response.Workspaces[0].WorkspaceID != binding.WorkspaceID ||
		response.Workspaces[0].RootPath != binding.CanonicalRoot {
		t.Fatalf("unavailable attachment response = %+v, want its catalog record", response)
	}
}
