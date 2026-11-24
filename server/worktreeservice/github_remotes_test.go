package worktree

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGitHubRemotes(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", root}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s %v", args, output, err)
		}
	}
	git("init")
	git("remote", "add", "origin", "https://github.com/Owner/Repo.git")
	git("remote", "add", "ssh", "ssh://git@github.com/owner/repo.git")
	git("remote", "add", "scp", "git@github.com:OWNER/REPO.git")
	git("remote", "add", "otherhost", "https://gitlab.com/owner/other.git")
	inspector := NewGitInspector(nil)
	repos, err := inspector.GitHubRepositories(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].Owner != "owner" || repos[0].Name != "repo" {
		t.Fatalf("%+v", repos)
	}
	git("remote", "add", "different", "git@github.com:owner/different.git")
	repos, err = inspector.GitHubRepositories(t.Context(), root)
	if err != nil || len(repos) != 2 {
		t.Fatalf("%+v %v", repos, err)
	}
	git("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "initial")
	linked := filepath.Join(t.TempDir(), "linked")
	git("worktree", "add", "--detach", linked)
	linkedRepos, err := inspector.GitHubRepositories(t.Context(), linked)
	if err != nil || len(linkedRepos) != 2 {
		t.Fatalf("%+v %v", linkedRepos, err)
	}
	empty := t.TempDir()
	command := exec.Command("git", "-C", empty, "init")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%s %v", output, err)
	}
	emptyRepos, err := inspector.GitHubRepositories(t.Context(), empty)
	if err != nil || len(emptyRepos) != 0 {
		t.Fatalf("%+v %v", emptyRepos, err)
	}
	if _, err := inspector.GitHubRepositories(t.Context(), filepath.Join(empty, "missing")); err == nil {
		t.Fatal("Git failure hidden")
	}
}
