package app

import (
	"context"
	"core/cli/app/internal/status"

	"core/server/auth"
	"core/server/runtime"
	"core/shared/clientui"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

type stubProgressiveStatusCollector struct {
	base       uiStatusSnapshot
	authResult uiStatusAuthStageResult
	gitResult  uiStatusGitStageResult
	envResult  uiStatusEnvironmentStageResult
	gitCalls   int
}

func (s *stubProgressiveStatusCollector) Collect(_ context.Context, _ uiStatusRequest) (uiStatusSnapshot, error) {
	snapshot := s.base
	snapshot.Auth = s.authResult.Auth
	snapshot.Subscription = s.authResult.Subscription
	snapshot.Git = s.gitResult.Git
	snapshot.Skills = s.envResult.Skills
	snapshot.SkillTokenCounts = s.envResult.SkillTokenCounts
	snapshot.AgentsPaths = s.envResult.AgentsPaths
	snapshot.AgentTokenCounts = s.envResult.AgentTokenCounts
	snapshot.CollectorWarning = s.envResult.CollectorWarning
	return snapshot, nil
}

func (s *stubProgressiveStatusCollector) CollectBase(_ uiStatusRequest) uiStatusSnapshot {
	return s.base
}

func (s *stubProgressiveStatusCollector) CollectAuth(_ context.Context, _ uiStatusRequest, _ uiStatusSnapshot) uiStatusAuthStageResult {
	return s.authResult
}

func (s *stubProgressiveStatusCollector) CollectGit(_ context.Context, _ uiStatusRequest, _ uiStatusSnapshot) uiStatusGitStageResult {
	s.gitCalls++
	return s.gitResult
}

func (s *stubProgressiveStatusCollector) CollectEnvironment(_ context.Context, _ uiStatusRequest, _ uiStatusSnapshot) uiStatusEnvironmentStageResult {
	return s.envResult
}

type statusRequestOption func(*uiStatusRequest)

func newStatusRequestForTest(options ...statusRequestOption) uiStatusRequest {
	var req uiStatusRequest
	for _, option := range options {
		if option != nil {
			option(&req)
		}
	}
	return populateStatusRequestCacheKeys(req)
}

func withStatusWorkspaceRoot(root string) statusRequestOption {
	return func(req *uiStatusRequest) {
		req.WorkspaceRoot = root
	}
}

func withStatusAuthManager(manager *auth.Manager) statusRequestOption {
	return func(req *uiStatusRequest) {
		req.AuthCacheIdentity = status.AuthCacheIdentity(manager)
	}
}

func withStatusRuntime(runtime clientui.RuntimeClient) statusRequestOption {
	return func(req *uiStatusRequest) {
		req.Runtime = runtime
	}
}

func TestStatusCommandRecordsPromptHistoryWithoutBlockingOpen(t *testing.T) {
	store, eng := newAppRuntimeEngine(t, &runtimeAdapterFakeClient{}, runtime.Config{})

	m := newProjectedEngineUIModel(
		eng,
		WithUIStatusConfig(uiStatusConfig{WorkspaceRoot: store.Meta().WorkspaceRoot}),
		WithUIStatusCollector(&stubProgressiveStatusCollector{}),
	)
	m.termWidth = 100
	m.termHeight = 40
	m.windowSizeKnown = true
	m.input = "/status"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if !updated.status.open {
		t.Fatal("expected /status to open immediately before prompt-history persistence completes")
	}
	if got := updated.promptHistory[len(updated.promptHistory)-1]; got != "/status" {
		t.Fatalf("expected in-memory prompt history updated immediately, got %+v", updated.promptHistory)
	}
	for _, msg := range collectCmdMessages(t, cmd) {
		if msg == nil {
			continue
		}
		next, _ = updated.Update(msg)
		updated = next.(*uiModel)
	}
}

func TestStatusGroupSkillsByDirectoryKeepsBrokenSkillUnderSkillsRoot(t *testing.T) {
	groups := statusGroupSkillsByDirectory([]uiStatusSkillInspection{
		{Name: "apiresult", Path: "/Users/test/.kent/skills/apiresult/SKILL.md", Loaded: true},
		{Name: "broken", Path: "/Users/test/.kent/skills/broken/SKILL.md", Loaded: false, Reason: "symlink target does not exist"},
	})

	if len(groups) != 1 {
		t.Fatalf("expected one skills directory group, got %+v", groups)
	}
	if groups[0].Directory != "/Users/test/.kent/skills" {
		t.Fatalf("expected skills root grouping, got %+v", groups)
	}
	if len(groups[0].Skills) != 2 {
		t.Fatalf("expected both skills in the same group, got %+v", groups)
	}
	if groups[0].Skills[1].Path != "/Users/test/.kent/skills/broken/SKILL.md" {
		t.Fatalf("expected broken skill path to remain in SKILL.md form, got %+v", groups[0].Skills[1])
	}
}

func withTrueColor(t *testing.T) {
	t.Helper()
	previousProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previousProfile) })
}

func TestStatusRepositorySeparatesAuthCacheByOAuthIdentity(t *testing.T) {
	repo := status.NewMemoryRepository()
	managerA := auth.NewManager(auth.NewMemoryStore(auth.State{
		Method: auth.Method{Type: auth.MethodOAuth, OAuth: &auth.OAuthMethod{AccessToken: "token-a", AccountID: "acct-a", Email: "a@example.com"}},
	}), nil, time.Now)
	managerB := auth.NewManager(auth.NewMemoryStore(auth.State{
		Method: auth.Method{Type: auth.MethodOAuth, OAuth: &auth.OAuthMethod{AccessToken: "token-b", AccountID: "acct-b", Email: "b@example.com"}},
	}), nil, time.Now)
	reqA := newStatusRequestForTest(withStatusWorkspaceRoot("/tmp/workdir"), withStatusAuthManager(managerA))
	reqB := newStatusRequestForTest(withStatusWorkspaceRoot("/tmp/workdir"), withStatusAuthManager(managerB))
	base := uiStatusSnapshot{Workdir: "/tmp/workdir"}

	repo.StoreAuth(status.AuthCacheKey(reqA), uiStatusAuthStageResult{
		Auth:         uiStatusAuthInfo{Summary: "a@example.com"},
		Subscription: uiStatusSubscriptionInfo{Applicable: true, Summary: "Pro subscription"},
	}, time.Now())

	seedA := repo.SeedSnapshot(reqA, base, time.Now())
	if got := seedA.Snapshot.Auth.Summary; got != "a@example.com" {
		t.Fatalf("expected cached auth summary for account A, got %q", got)
	}
	seedB := repo.SeedSnapshot(reqB, base, time.Now())
	if got := seedB.Snapshot.Auth.Summary; got != "" {
		t.Fatalf("expected no cached auth summary for account B, got %q", got)
	}
	if len(seedB.PendingSections) == 0 || seedB.PendingSections[0] != uiStatusSectionAuth {
		t.Fatalf("expected account B to require auth refresh, got %+v", seedB.PendingSections)
	}
}

func TestStatusRepositorySeparatesOpaqueOAuthCacheByTokenFingerprint(t *testing.T) {
	repo := status.NewMemoryRepository()
	managerA := auth.NewManager(auth.NewMemoryStore(auth.State{
		Method: auth.Method{Type: auth.MethodOAuth, OAuth: &auth.OAuthMethod{AccessToken: "token-a"}},
	}), nil, time.Now)
	managerB := auth.NewManager(auth.NewMemoryStore(auth.State{
		Method: auth.Method{Type: auth.MethodOAuth, OAuth: &auth.OAuthMethod{AccessToken: "token-b"}},
	}), nil, time.Now)
	reqA := newStatusRequestForTest(withStatusWorkspaceRoot("/tmp/workdir"), withStatusAuthManager(managerA))
	reqB := newStatusRequestForTest(withStatusWorkspaceRoot("/tmp/workdir"), withStatusAuthManager(managerB))
	base := uiStatusSnapshot{Workdir: "/tmp/workdir"}

	repo.StoreAuth(status.AuthCacheKey(reqA), uiStatusAuthStageResult{
		Auth:         uiStatusAuthInfo{Summary: "opaque-a"},
		Subscription: uiStatusSubscriptionInfo{Applicable: true, Summary: "Pro subscription"},
	}, time.Now())

	seedA := repo.SeedSnapshot(reqA, base, time.Now())
	if got := seedA.Snapshot.Auth.Summary; got != "opaque-a" {
		t.Fatalf("expected cached auth summary for opaque token A, got %q", got)
	}
	seedB := repo.SeedSnapshot(reqB, base, time.Now())
	if got := seedB.Snapshot.Auth.Summary; got != "" {
		t.Fatalf("expected no cached auth summary for opaque token B, got %q", got)
	}
	if len(seedB.PendingSections) == 0 || seedB.PendingSections[0] != uiStatusSectionAuth {
		t.Fatalf("expected opaque token B to require auth refresh, got %+v", seedB.PendingSections)
	}
}

func TestStatusRepositoryDoesNotSeedPathBackedAuthCache(t *testing.T) {
	repo := status.NewMemoryRepository()
	req := newStatusRequestForTest(withStatusWorkspaceRoot("/tmp/workdir"))
	req.AuthCacheIdentity = "auth:path:/tmp/kent-auth.json"
	req.AuthCacheUnseedable = true
	req.CacheKeys.Auth = status.AuthCacheKey(req)
	base := uiStatusSnapshot{Workdir: "/tmp/workdir"}

	repo.StoreAuth(req.CacheKeys.Auth, uiStatusAuthStageResult{
		Auth:         uiStatusAuthInfo{Summary: "previous@example.com"},
		Subscription: uiStatusSubscriptionInfo{Applicable: true, Summary: "Previous subscription"},
	}, time.Now())

	seed := repo.SeedSnapshot(req, base, time.Now())
	if got := seed.Snapshot.Auth.Summary; got != "" {
		t.Fatalf("expected path-backed auth cache not to seed stale auth, got %q", got)
	}
	if got := seed.Snapshot.Subscription.Summary; got != "" {
		t.Fatalf("expected path-backed auth cache not to seed stale subscription, got %q", got)
	}
	if len(seed.PendingSections) == 0 || seed.PendingSections[0] != uiStatusSectionAuth {
		t.Fatalf("expected auth refresh pending for unseedable auth cache, got %+v", seed.PendingSections)
	}
}

func TestStatusRequestMarksAuthStatePathCacheUnseedable(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIStatusConfig(uiStatusConfig{WorkspaceRoot: "/tmp/workdir", AuthStatePath: "/tmp/kent-auth.json"}),
	)

	req := m.newStatusRequest(time.Now())
	if !req.AuthCacheUnseedable {
		t.Fatal("expected auth-state-path status request to disable auth cache seeding")
	}
}

func TestStatusRepositoryStoresAuthUnderCapturedIdentityKey(t *testing.T) {
	store := auth.NewMemoryStore(auth.State{
		Method: auth.Method{Type: auth.MethodOAuth, OAuth: &auth.OAuthMethod{AccessToken: "token-a", AccountID: "acct-a", Email: "a@example.com"}},
	})
	manager := auth.NewManager(store, nil, time.Now)
	req := newStatusRequestForTest(withStatusWorkspaceRoot("/tmp/workdir"), withStatusAuthManager(manager))
	base := uiStatusSnapshot{Workdir: "/tmp/workdir"}
	cacheKey := status.AuthCacheKey(req)

	if err := store.Save(context.Background(), auth.State{
		Method: auth.Method{Type: auth.MethodOAuth, OAuth: &auth.OAuthMethod{AccessToken: "token-b", AccountID: "acct-b", Email: "b@example.com"}},
	}); err != nil {
		t.Fatalf("switch auth identity: %v", err)
	}

	repo := status.NewMemoryRepository()
	repo.StoreAuth(cacheKey, uiStatusAuthStageResult{
		Auth:         uiStatusAuthInfo{Summary: "a@example.com"},
		Subscription: uiStatusSubscriptionInfo{Applicable: true, Summary: "Pro subscription"},
	}, time.Now())

	reqB := req
	reqB.AuthCacheIdentity = status.AuthCacheIdentity(manager)
	reqB.CacheKeys.Auth = status.AuthCacheKey(reqB)
	seedB := repo.SeedSnapshot(reqB, base, time.Now())
	if got := seedB.Snapshot.Auth.Summary; got != "" {
		t.Fatalf("expected no auth cached under switched identity, got %q", got)
	}

	if err := store.Save(context.Background(), auth.State{
		Method: auth.Method{Type: auth.MethodOAuth, OAuth: &auth.OAuthMethod{AccessToken: "token-a", AccountID: "acct-a", Email: "a@example.com"}},
	}); err != nil {
		t.Fatalf("restore auth identity: %v", err)
	}
	seedA := repo.SeedSnapshot(req, base, time.Now())
	if got := seedA.Snapshot.Auth.Summary; got != "a@example.com" {
		t.Fatalf("expected cached auth under original captured identity, got %q", got)
	}
}

func TestStatusRequestCacheKeysSeedSnapshotLockstep(t *testing.T) {
	repo := status.NewMemoryRepository()
	req := newStatusRequestForTest(withStatusWorkspaceRoot("/tmp/workdir"))
	now := time.Now()
	base := uiStatusSnapshot{Workdir: "/tmp/workdir"}
	repo.StoreAuth(req.CacheKeys.Auth, uiStatusAuthStageResult{
		Auth:         uiStatusAuthInfo{Summary: "cached-auth"},
		Subscription: uiStatusSubscriptionInfo{Applicable: true, Summary: "cached-subscription"},
	}, now)
	repo.StoreGit(req.CacheKeys.Git, uiStatusGitStageResult{Git: uiStatusGitInfo{Visible: true, Branch: "main"}}, now)
	repo.StoreEnvironment(req.CacheKeys.Environment, uiStatusEnvironmentStageResult{
		Skills:           []uiStatusSkillInspection{{Name: "skill-a", Path: "/tmp/skill-a/SKILL.md", Loaded: true}},
		SkillTokenCounts: map[string]int{"/tmp/skill-a/SKILL.md": 12},
		AgentsPaths:      []string{"/tmp/agent.md"},
		AgentTokenCounts: map[string]int{"/tmp/agent.md": 7},
	}, now)

	seed := repo.SeedSnapshot(req, base, now)
	if seed.Snapshot.Auth.Summary != "cached-auth" || seed.Snapshot.Subscription.Summary != "cached-subscription" {
		t.Fatalf("expected auth cache seeded via request key, got %+v / %+v", seed.Snapshot.Auth, seed.Snapshot.Subscription)
	}
	if seed.Snapshot.Git.Branch != "main" {
		t.Fatalf("expected git cache seeded via request key, got %+v", seed.Snapshot.Git)
	}
	if len(seed.Snapshot.Skills) != 1 || seed.Snapshot.Skills[0].Name != "skill-a" {
		t.Fatalf("expected environment cache seeded via request key, got %+v", seed.Snapshot.Skills)
	}
	if len(seed.PendingSections) != 0 {
		t.Fatalf("expected all sections cached without pending refreshes, got %+v", seed.PendingSections)
	}
}

func TestStatusRepositoryNormalizesGitCacheKeysAcrossSlashStyles(t *testing.T) {
	repo := status.NewMemoryRepository()
	now := time.Now()
	repo.StoreGit(
		status.GitCacheKey(`C:\repo`),
		uiStatusGitStageResult{Git: uiStatusGitInfo{Visible: true, Branch: "main", Ahead: 1}},
		now,
	)

	seed := repo.SeedSnapshot(
		newStatusRequestForTest(withStatusWorkspaceRoot(`C:\repo`)),
		uiStatusSnapshot{Workdir: "C:/repo"},
		now,
	)
	if !seed.Snapshot.Git.Visible || seed.Snapshot.Git.Branch != "main" {
		t.Fatalf("expected cached git snapshot reused across slash styles, got %+v", seed.Snapshot.Git)
	}
	for _, section := range seed.PendingSections {
		if section == uiStatusSectionGit {
			t.Fatalf("did not expect git refresh when normalized cache key matches, got %+v", seed.PendingSections)
		}
	}
}

func TestStatusCollectorUsesRuntimeWorkspaceRootForGitBranch(t *testing.T) {
	processRoot := initStatusLineGitRepo(t, "main")
	sessionRoot := initStatusLineGitRepo(t, "session-branch")
	t.Chdir(processRoot)
	collector := defaultUIStatusCollector{}

	snapshot, err := collector.Collect(context.Background(), newStatusRequestForTest(
		withStatusRuntime(&runtimeControlFakeClient{sessionView: clientui.RuntimeSessionView{
			ExecutionTarget: clientui.SessionExecutionTarget{
				EffectiveWorkdir: processRoot,
			},
		}}),
		withStatusWorkspaceRoot(sessionRoot),
	))
	if err != nil {
		t.Fatalf("collect status: %v", err)
	}
	if !snapshot.Git.Visible {
		t.Fatalf("expected git section visible for session root, got %+v", snapshot.Git)
	}
	if snapshot.Git.Branch != "session-branch" {
		t.Fatalf("git branch = %q, want session-branch", snapshot.Git.Branch)
	}
}

func TestStatusCollectorPrefersWorktreeRootForGitBranch(t *testing.T) {
	workspaceRoot := initStatusLineGitRepo(t, "main")
	worktreeRoot := initStatusLineGitRepo(t, "worktree-branch")
	collector := defaultUIStatusCollector{}

	snapshot, err := collector.Collect(context.Background(), newStatusRequestForTest(
		withStatusRuntime(&runtimeControlFakeClient{sessionView: clientui.RuntimeSessionView{
			ExecutionTarget: clientui.SessionExecutionTarget{
				WorkspaceRoot:    workspaceRoot,
				WorktreeRoot:     worktreeRoot,
				EffectiveWorkdir: filepath.Join(worktreeRoot, "pkg"),
			},
		}}),
		withStatusWorkspaceRoot(workspaceRoot),
	))
	if err != nil {
		t.Fatalf("collect status: %v", err)
	}
	if !snapshot.Git.Visible {
		t.Fatalf("expected git section visible for worktree root, got %+v", snapshot.Git)
	}
	if snapshot.Git.Branch != "worktree-branch" {
		t.Fatalf("git branch = %q, want worktree-branch", snapshot.Git.Branch)
	}
}

func TestCollectGitStatusHidesOutsideRepository(t *testing.T) {
	git := status.CollectGitStatus(context.Background(), t.TempDir(), statusGitTimeout, sanitizedGitEnv)
	if git.Visible {
		t.Fatalf("expected git section hidden outside repositories, got %+v", git)
	}
	if git.Error != "" {
		t.Fatalf("expected no git error outside repositories, got %+v", git)
	}
}

func TestCollectGitStatusDetectsNestedRepositorySubdirectory(t *testing.T) {
	repoRoot := t.TempDir()
	cmd := exec.Command("git", "-C", repoRoot, "init")
	cmd.Env = sanitizedGitEnv(os.Environ())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v (%s)", err, out)
	}
	nestedDir := filepath.Join(repoRoot, "a", "b", "c")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("mkdir nested dir: %v", err)
	}

	git := status.CollectGitStatus(context.Background(), nestedDir, statusGitTimeout, sanitizedGitEnv)
	if !git.Visible {
		t.Fatalf("expected git section visible for nested repository dir, got %+v", git)
	}
	if git.Error != "" {
		t.Fatalf("expected no git error for nested repository dir, got %+v", git)
	}
	if strings.TrimSpace(git.Branch) == "" {
		t.Fatalf("expected git branch detected for nested repository dir, got %+v", git)
	}
}

func TestCollectGitStatusDetectsSymlinkedRepositorySubdirectory(t *testing.T) {
	repoRoot := t.TempDir()
	cmd := exec.Command("git", "-C", repoRoot, "init")
	cmd.Env = sanitizedGitEnv(os.Environ())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v (%s)", err, out)
	}
	realDir := filepath.Join(repoRoot, "real", "nested")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("mkdir real dir: %v", err)
	}
	linkPath := filepath.Join(t.TempDir(), "workspace-link")
	if err := os.Symlink(realDir, linkPath); err != nil {
		t.Fatalf("symlink workdir: %v", err)
	}

	git := status.CollectGitStatus(context.Background(), linkPath, statusGitTimeout, sanitizedGitEnv)
	if !git.Visible {
		t.Fatalf("expected git section visible for symlinked repository dir, got %+v", git)
	}
	if git.Error != "" {
		t.Fatalf("expected no git error for symlinked repository dir, got %+v", git)
	}
	if strings.TrimSpace(git.Branch) == "" {
		t.Fatalf("expected branch detected for symlinked repository dir, got %+v", git)
	}
}

func TestCollectGitStatusIgnoresInheritedGitRepositoryEnv(t *testing.T) {
	repoRoot := t.TempDir()
	cmd := exec.Command("git", "-C", repoRoot, "init")
	cmd.Env = sanitizedGitEnv(os.Environ())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v (%s)", err, out)
	}
	nestedDir := filepath.Join(repoRoot, "nested")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("mkdir nested dir: %v", err)
	}
	t.Setenv("GIT_DIR", filepath.Join(t.TempDir(), ".git"))
	t.Setenv("GIT_WORK_TREE", t.TempDir())
	t.Setenv("GIT_COMMON_DIR", t.TempDir())

	git := status.CollectGitStatus(context.Background(), nestedDir, statusGitTimeout, sanitizedGitEnv)
	if !git.Visible {
		t.Fatalf("expected git section visible when inherited git env points elsewhere, got %+v", git)
	}
	if git.Error != "" {
		t.Fatalf("expected no git error when inherited git env points elsewhere, got %+v", git)
	}
}
