package app

import (
	"builder/cli/tui"
	"builder/server/auth"
	"builder/server/runtime"
	"builder/shared/clientui"
	"builder/shared/config"
	"context"
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

type stubStatusCollector struct {
	snapshot uiStatusSnapshot
	err      error
}

func (s *stubStatusCollector) Collect(_ context.Context, _ uiStatusRequest) (uiStatusSnapshot, error) {
	return s.snapshot, s.err
}

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
		req.AuthCacheIdentity = statusAuthCacheIdentity(manager)
	}
}

func withStatusRuntime(runtime clientui.RuntimeClient) statusRequestOption {
	return func(req *uiStatusRequest) {
		req.Runtime = runtime
	}
}

func TestStatusCommandOpensStatusSurfaceInNativeMode(t *testing.T) {
	collector := &stubStatusCollector{snapshot: uiStatusSnapshot{
		CollectedAt:       time.Date(2026, time.March, 24, 21, 15, 0, 0, time.UTC),
		Workdir:           "/tmp/workdir",
		SessionName:       "incident",
		SessionID:         "session-123",
		ParentSessionID:   "parent-456",
		ParentSessionName: "incident-root",
		OwnsServer:        true,
		Git:               uiStatusGitInfo{Visible: true, Branch: "master", Dirty: true, Ahead: 2, Behind: 1},
		Auth: uiStatusAuthInfo{
			Summary: "user@example.com",
		},
		Context: uiStatusContextInfo{UsedTokens: 120000, AvailableTokens: 280000, WindowTokens: 400000, ThresholdTokens: 300000},
		Model: uiStatusModelInfo{
			Summary: "gpt-5 high fast",
		},
		Update: uiStatusUpdateInfo{Checked: true, Available: true, LatestVersion: "1.2.3"},
		Config: uiStatusConfigInfo{
			SettingsPath:    "/Users/test/.builder/config.toml",
			OverrideSources: []string{"ENV", "CLI ARGS"},
			Supervisor:      "edits",
			AutoCompaction:  true,
		},
		Subscription: uiStatusSubscriptionInfo{
			Applicable: true,
			Summary:    "Pro subscription",
			Windows: []uiStatusSubscriptionWindow{
				{Label: "5h", UsedPercent: 12.5, ResetAt: time.Date(2026, time.March, 25, 2, 0, 0, 0, time.UTC)},
				{Label: "weekly", UsedPercent: 40.0, ResetAt: time.Date(2026, time.March, 31, 2, 0, 0, 0, time.UTC)},
			},
		},
		Skills: []uiStatusSkillInspection{
			{Name: "apiresult", Path: "/Users/test/.builder/skills/apiresult/SKILL.md", Loaded: true},
			{Name: "local helper", Path: "/Users/test/.builder/skills/local-helper/SKILL.md", Loaded: true, Disabled: true},
			{Name: "skill-creator", Path: "/Users/test/.builder/.generated/skills/skill-creator/SKILL.md", Loaded: true, SourceKind: "generated"},
			{Name: "broken", Path: "/Users/test/.builder/skills/broken/SKILL.md", Loaded: false, Reason: "missing SKILL.md"},
		},
		AgentsPaths:     []string{"/Users/test/.builder/AGENTS.md", "/tmp/workdir/AGENTS.md"},
		CompactionCount: 3,
	}}

	m := newProjectedStaticUIModel(
		WithUIStatusConfig(uiStatusConfig{
			WorkspaceRoot: "/tmp/workdir",
			Settings: config.Settings{
				ContextCompactionThresholdTokens: 300000,
			},
			Source: config.SourceReport{SettingsPath: "/Users/test/.builder/config.toml"},
		}),
		WithUIStatusCollector(collector),
	)
	m.termWidth = 100
	m.termHeight = 40
	m.windowSizeKnown = true
	m.input = "/status"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if !updated.status.isOpen() {
		t.Fatal("expected /status to open the status overlay")
	}
	if updated.surface() != uiSurfaceStatus {
		t.Fatalf("expected /status to push status surface, got %q", updated.surface())
	}
	if updated.view.Mode() != tui.ModeOngoing {
		t.Fatalf("expected /status to keep transcript mode ongoing, got %q", updated.view.Mode())
	}
	if cmd == nil {
		t.Fatal("expected /status open to emit a screen transition command")
	}

	next, _ = updated.Update(statusRefreshDoneMsg{token: updated.status.refreshToken, snapshot: collector.snapshot})
	updated = next.(*uiModel)
	plain := stripANSIAndTrimRight(updated.View())
	for _, want := range []string{"Auth", "Pro subscription", "Server: owned by this CLI", "CWD: /tmp/workdir", "Model: gpt-5 high fast", "Update: available 1.2.3", "incident", "Session ID: session-123", "Parent session: incident-root <parent-456>", "master", "dirty | ahead 2 | behind 1"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected status overlay to contain %q, got %q", want, plain)
		}
	}
	if !strings.Contains(plain, "incident\nSession ID: session-123\nParent session: incident-root <parent-456>") {
		t.Fatalf("expected session id before parent session id, got %q", plain)
	}
	for _, want := range []string{"4 skills", "/Users/test/.builder/skills", "apiresult (0k)", "local helper disabled", "! broken (missing SKILL.md)", "/Users/test/.builder/.generated/skills", "skill-creator (0k) generated"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected grouped skill rendering to contain %q, got %q", want, plain)
		}
	}

	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnd})
	updated = next.(*uiModel)
	plain = stripANSIAndTrimRight(updated.View())
	for _, want := range []string{"weekly", "60% left", "auto-compaction on", "3 compactions", "2 agents files", "/Users/test/.builder/AGENTS.md", "supervisor edits"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected scrolled status overlay to contain %q, got %q", want, plain)
		}
	}

	next, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyEsc})
	updated = next.(*uiModel)
	if updated.status.isOpen() {
		t.Fatal("expected esc to close the status overlay")
	}
	if updated.surface() == uiSurfaceStatus {
		t.Fatal("expected status overlay state cleared after close")
	}
	if updated.view.Mode() != tui.ModeOngoing {
		t.Fatalf("expected status overlay close to restore ongoing mode, got %q", updated.view.Mode())
	}
	if cmd == nil {
		t.Fatal("expected /status close to emit a screen transition command")
	}
}

func TestStatusOverlaySessionSectionLabelsSessionIDBeforeMutedParent(t *testing.T) {
	snapshot := uiStatusSnapshot{
		Workdir:           "/tmp/workdir",
		SessionName:       "incident",
		SessionID:         "session-123",
		ParentSessionID:   "parent-456",
		ParentSessionName: "incident-root",
		Model:             uiStatusModelInfo{Summary: "gpt-5 high"},
	}
	sessionLines := statusOverlaySessionLines(snapshot)
	if len(sessionLines) != 3 {
		t.Fatalf("session lines = %+v, want 3 lines", sessionLines)
	}
	if sessionLines[0].Text != "incident" || sessionLines[0].Style != statusOverlayLineStyleBold {
		t.Fatalf("session name line = %+v, want bold incident", sessionLines[0])
	}
	if sessionLines[1].Text != "Session ID: session-123" || sessionLines[1].Style != statusOverlayLineStyleNormal {
		t.Fatalf("session id line = %+v, want full-color labeled session id", sessionLines[1])
	}
	if sessionLines[2].Text != "Parent session: incident-root <parent-456>" || sessionLines[2].Style != statusOverlayLineStyleSubtle {
		t.Fatalf("parent session line = %+v, want muted parent session", sessionLines[2])
	}

	m := newProjectedStaticUIModel()
	m.status.snapshot = snapshot
	lines := m.layout().statusOverlayContentLines(100)
	plain := stripANSIAndTrimRight(strings.Join(lines, "\n"))
	if !strings.Contains(plain, "incident\nSession ID: session-123\nParent session: incident-root <parent-456>") {
		t.Fatalf("expected session id before parent session in focused status section, got %q", plain)
	}
}

func TestStatusOverlaySectionOrderPrioritizesSessionGitContext(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.status.snapshot = uiStatusSnapshot{
		Workdir:   "/tmp/workdir",
		SessionID: "session-123",
		Git:       uiStatusGitInfo{Visible: true, Branch: "main"},
		Context:   uiStatusContextInfo{AvailableTokens: 100, ThresholdTokens: 50},
		Config:    uiStatusConfigInfo{SettingsPath: "/tmp/workdir/.builder/config.toml", Supervisor: "edits"},
		Subscription: uiStatusSubscriptionInfo{
			Applicable: true,
			Summary:    "Pro subscription",
		},
		Skills: []uiStatusSkillInspection{{Name: "apiresult", Path: "/tmp/workdir/.builder/skills/apiresult/SKILL.md", Loaded: true}},
	}
	lines := stripANSIAndTrimRight(strings.Join(m.layout().statusOverlayContentLines(100), "\n"))

	session := statusLineIndex(t, lines, "Session")
	git := statusLineIndex(t, lines, "Git")
	context := statusLineIndex(t, lines, "Context")
	auth := statusLineIndex(t, lines, "Auth")
	config := statusLineIndex(t, lines, "Config")
	skills := statusLineIndex(t, lines, "1 skills")
	if !(session < git && git < context && context < auth && auth < config && config < skills) {
		t.Fatalf("unexpected status section order: session=%d git=%d context=%d auth=%d config=%d skills=%d\n%s", session, git, context, auth, config, skills, lines)
	}
}

func TestStatusOverlayAuthSectionShowsNoAuthAndAPIKey(t *testing.T) {
	withTrueColor(t)
	noAuth := newProjectedStaticUIModel()
	noAuth.status.snapshot = uiStatusSnapshot{Auth: uiStatusAuthInfo{Summary: "No Auth", Visible: true}}
	noAuthRawLines := noAuth.layout().statusOverlayContentLines(100)
	noAuthLines := stripANSIAndTrimRight(strings.Join(noAuthRawLines, "\n"))
	if !strings.Contains(noAuthLines, "Auth\nNo Auth") {
		t.Fatalf("expected no-auth status section, got %q", noAuthLines)
	}
	assertStatusOverlayPrimaryLine(t, findRawStatusOverlayLine(t, noAuthRawLines, "No Auth"), "No Auth")

	apiKey := newProjectedStaticUIModel()
	apiKey.status.snapshot = uiStatusSnapshot{Auth: uiStatusAuthInfo{Summary: "API Key ...1234", Visible: true}}
	apiKeyRawLines := apiKey.layout().statusOverlayContentLines(100)
	apiKeyLines := stripANSIAndTrimRight(strings.Join(apiKeyRawLines, "\n"))
	if !strings.Contains(apiKeyLines, "Auth\nAPI Key ...1234") {
		t.Fatalf("expected api-key status section, got %q", apiKeyLines)
	}
	assertStatusOverlayPrimaryLine(t, findRawStatusOverlayLine(t, apiKeyRawLines, "API Key ...1234"), "API Key ...1234")
}

func statusLineIndex(t *testing.T, lines string, want string) int {
	t.Helper()
	for idx, line := range strings.Split(lines, "\n") {
		if strings.TrimSpace(line) == want {
			return idx
		}
	}
	t.Fatalf("status line %q not found in %q", want, lines)
	return -1
}

func TestStatusCommandProgressivelyLoadsSections(t *testing.T) {
	collector := &stubProgressiveStatusCollector{
		base: uiStatusSnapshot{
			CollectedAt: time.Date(2026, time.March, 24, 21, 15, 0, 0, time.UTC),
			Workdir:     "/tmp/workdir",
			SessionName: "incident",
			SessionID:   "session-123",
			Model:       uiStatusModelInfo{Summary: "gpt-5 high fast"},
			Config:      uiStatusConfigInfo{Supervisor: "edits", AutoCompaction: true},
		},
		gitResult: uiStatusGitStageResult{Git: uiStatusGitInfo{Visible: true, Branch: "master", Dirty: true, Ahead: 1}},
	}

	m := newProjectedStaticUIModel(
		WithUIStatusConfig(uiStatusConfig{WorkspaceRoot: "/tmp/workdir"}),
		WithUIStatusCollector(collector),
	)
	m.termWidth = 100
	m.termHeight = 40
	m.windowSizeKnown = true
	m.input = "/status"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected progressive status command")
	}
	plain := stripANSIAndTrimRight(updated.View())
	for _, want := range []string{"Loading account...", "CWD: /tmp/workdir", "Model: <unset>", "Loading git..."} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected pure status seed render to contain %q, got %q", want, plain)
		}
	}
	if strings.Contains(plain, "gpt-5 high fast") {
		t.Fatalf("did not expect custom collector base before command completion, got %q", plain)
	}

	next, _ = updated.Update(statusGitRefreshDoneMsg{token: updated.status.refreshToken, result: collector.gitResult})
	updated = next.(*uiModel)
	plain = stripANSIAndTrimRight(updated.View())
	if !strings.Contains(plain, "master") || !strings.Contains(plain, "dirty | ahead 1 | behind 0") {
		t.Fatalf("expected parallel git render before base snapshot, got %q", plain)
	}

	next, _ = updated.Update(statusBaseRefreshDoneMsg{token: updated.status.refreshToken, snapshot: collector.base})
	updated = next.(*uiModel)
	plain = stripANSIAndTrimRight(updated.View())
	if !strings.Contains(plain, "Model: gpt-5 high fast") {
		t.Fatalf("expected custom base snapshot after base completion, got %q", plain)
	}
}

func TestStatusCommandRunsForegroundGitRefreshWhileStartupGitInFlight(t *testing.T) {
	collector := &stubProgressiveStatusCollector{
		base: uiStatusSnapshot{
			CollectedAt: time.Date(2026, time.March, 24, 21, 15, 0, 0, time.UTC),
			Workdir:     "/tmp/workdir",
			SessionName: "incident",
			SessionID:   "session-123",
			Model:       uiStatusModelInfo{Summary: "gpt-5 high fast"},
		},
		gitResult: uiStatusGitStageResult{Git: uiStatusGitInfo{Visible: true, Branch: "foreground"}},
	}

	m := newProjectedStaticUIModel(
		WithUIStatusConfig(uiStatusConfig{WorkspaceRoot: "/tmp/workdir"}),
		WithUIStatusCollector(collector),
	)
	m.termWidth = 100
	m.termHeight = 40
	m.windowSizeKnown = true
	m.statusGitBackgroundInFlight = true
	m.input = "/status"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected status refresh command")
	}
	for _, msg := range collectCmdMessages(t, cmd) {
		if git, ok := msg.(statusGitRefreshDoneMsg); ok && git.token == updated.status.refreshToken && !git.background {
			next, _ = updated.Update(git)
			updated = next.(*uiModel)
			if !strings.Contains(stripANSIAndTrimRight(updated.View()), "foreground") {
				t.Fatalf("expected foreground git result in status overlay, got %q", stripANSIAndTrimRight(updated.View()))
			}
			return
		}
	}
	t.Fatalf("expected foreground git refresh command while background git is in flight; git calls=%d", collector.gitCalls)
}

func TestStatusCommandPersistsPromptHistoryWithoutBlockingOpen(t *testing.T) {
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
	if !updated.status.isOpen() {
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
	history, err := store.ReadPromptHistory()
	if err != nil {
		t.Fatalf("read prompt history: %v", err)
	}
	if len(history) == 0 || history[len(history)-1] != "/status" {
		t.Fatalf("expected persisted /status prompt history entry, got %+v", history)
	}
}

func TestStatusGroupSkillsByDirectoryKeepsBrokenSkillUnderSkillsRoot(t *testing.T) {
	groups := statusGroupSkillsByDirectory([]uiStatusSkillInspection{
		{Name: "apiresult", Path: "/Users/test/.builder/skills/apiresult/SKILL.md", Loaded: true},
		{Name: "broken", Path: "/Users/test/.builder/skills/broken/SKILL.md", Loaded: false, Reason: "symlink target does not exist"},
	})

	if len(groups) != 1 {
		t.Fatalf("expected one skills directory group, got %+v", groups)
	}
	if groups[0].Directory != "/Users/test/.builder/skills" {
		t.Fatalf("expected skills root grouping, got %+v", groups)
	}
	if len(groups[0].Skills) != 2 {
		t.Fatalf("expected both skills in the same group, got %+v", groups)
	}
	if groups[0].Skills[1].Path != "/Users/test/.builder/skills/broken/SKILL.md" {
		t.Fatalf("expected broken skill path to remain in SKILL.md form, got %+v", groups[0].Skills[1])
	}
}

func TestStatusSkillLineMarksGeneratedAndShadowed(t *testing.T) {
	line := stripANSIAndTrimRight(statusSkillLine(uiStatusSkillInspection{
		Name:       "skill-creator",
		Path:       "/Users/test/.builder/.generated/skills/skill-creator/SKILL.md",
		Loaded:     true,
		SourceKind: "generated",
		Shadowed:   true,
	}, nil))
	for _, want := range []string{"skill-creator", "generated", "shadowed"} {
		if !strings.Contains(line, want) {
			t.Fatalf("expected status skill line to contain %q, got %q", want, line)
		}
	}
}

func TestStatusSkillLineRendersGeneratedLabelWithMutedStyle(t *testing.T) {
	withTrueColor(t)
	line := statusSkillLineStyled(uiStatusSkillInspection{
		Name:       "skill-creator",
		Path:       "/Users/test/.builder/.generated/skills/skill-creator/SKILL.md",
		Loaded:     true,
		SourceKind: "generated",
	}, nil, generatedSkillTestStyle())
	if !strings.Contains(stripANSIAndTrimRight(line), "skill-creator (0k) generated") || !strings.Contains(line, "\x1b[") {
		t.Fatalf("expected generated label to use muted ANSI style, got %q", line)
	}
}

func TestStatusSkillLinePreservesTokenCountForActiveGeneratedSkill(t *testing.T) {
	path := "/Users/test/.builder/.generated/skills/skill-creator/SKILL.md"
	withTrueColor(t)
	activeRaw := statusSkillLineStyled(uiStatusSkillInspection{
		Name:       "skill-creator",
		Path:       path,
		Loaded:     true,
		SourceKind: "generated",
	}, map[string]int{path: 1234}, generatedSkillTestStyle())
	active := stripANSIAndTrimRight(activeRaw)
	for _, want := range []string{"skill-creator", "(1.2k)", "generated"} {
		if !strings.Contains(active, want) {
			t.Fatalf("expected active generated line to contain %q, got %q", want, active)
		}
	}
	assertGeneratedLabelStyled(t, activeRaw)
	disabledRaw := statusSkillLineStyled(uiStatusSkillInspection{
		Name:       "disabled-skill",
		Path:       path,
		Loaded:     true,
		SourceKind: "generated",
		Disabled:   true,
	}, map[string]int{path: 1234}, generatedSkillTestStyle())
	disabled := stripANSIAndTrimRight(disabledRaw)
	if !strings.Contains(disabled, "generated") || !strings.Contains(disabled, "disabled") || strings.Contains(disabled, "(1.2k)") {
		t.Fatalf("expected disabled generated line to be label-only, got %q", disabled)
	}
	assertGeneratedLabelStyled(t, disabledRaw)
	shadowedRaw := statusSkillLineStyled(uiStatusSkillInspection{
		Name:       "shadowed-skill",
		Path:       path,
		Loaded:     true,
		SourceKind: "generated",
		Shadowed:   true,
	}, map[string]int{path: 1234}, generatedSkillTestStyle())
	shadowed := stripANSIAndTrimRight(shadowedRaw)
	if !strings.Contains(shadowed, "generated") || !strings.Contains(shadowed, "shadowed") || strings.Contains(shadowed, "(1.2k)") {
		t.Fatalf("expected shadowed generated line to be label-only, got %q", shadowed)
	}
	assertGeneratedLabelStyled(t, shadowedRaw)
}

func TestStatusOverlayGeneratedSkillLabelRendersMuted(t *testing.T) {
	withTrueColor(t)
	m := newProjectedStaticUIModel()
	m.status.snapshot = uiStatusSnapshot{
		Workdir: "/tmp/workdir",
		Skills: []uiStatusSkillInspection{{
			Name:       "skill-creator",
			Path:       "/tmp/workdir/.builder/.generated/skills/skill-creator/SKILL.md",
			Loaded:     true,
			SourceKind: "generated",
		}},
	}
	lines := m.layout().statusOverlayContentLines(100)
	raw := findRawStatusOverlayLine(t, lines, "skill-creator (0k) generated")
	assertGeneratedLabelStyled(t, raw)
}

func withTrueColor(t *testing.T) {
	t.Helper()
	previousProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previousProfile) })
}

func generatedSkillTestStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(uiPalette("dark").muted).Faint(true)
}

func assertGeneratedLabelStyled(t *testing.T, rawLine string) {
	t.Helper()
	if !strings.Contains(stripANSIAndTrimRight(rawLine), "generated") || !strings.Contains(rawLine, "\x1b[") {
		t.Fatalf("expected generated label to be styled in %q", rawLine)
	}
}

func assertStatusOverlayPrimaryLine(t *testing.T, rawLine string, want string) {
	t.Helper()
	if strings.TrimSpace(stripANSIAndTrimRight(rawLine)) != want || !strings.Contains(rawLine, "\x1b[") {
		t.Fatalf("expected primary styled status line %q, got %q", want, rawLine)
	}
}

func findRawStatusOverlayLine(t *testing.T, lines []string, want string) string {
	t.Helper()
	for _, line := range lines {
		if strings.Contains(stripANSIAndTrimRight(line), want) {
			return line
		}
	}
	t.Fatalf("status overlay line %q not found in %q", want, stripANSIAndTrimRight(strings.Join(lines, "\n")))
	return ""
}

func TestStatusEnvironmentWarnsWhenRecoveredGeneratedFilesExist(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".builder", "recovered", "old"), 0o755); err != nil {
		t.Fatalf("mkdir recovered: %v", err)
	}
	result := defaultUIStatusCollector{}.CollectEnvironment(context.Background(), newStatusRequestForTest(withStatusWorkspaceRoot(t.TempDir())), uiStatusSnapshot{})
	if !strings.Contains(result.CollectorWarning, "~/.builder/.generated folder was edited") {
		t.Fatalf("expected recovered generated warning, got %q", result.CollectorWarning)
	}
}

func TestStatusRepositorySeparatesAuthCacheByOAuthIdentity(t *testing.T) {
	repo := newMemoryUIStatusRepository()
	managerA := auth.NewManager(auth.NewMemoryStore(auth.State{
		Method: auth.Method{Type: auth.MethodOAuth, OAuth: &auth.OAuthMethod{AccessToken: "token-a", AccountID: "acct-a", Email: "a@example.com"}},
	}), nil, time.Now)
	managerB := auth.NewManager(auth.NewMemoryStore(auth.State{
		Method: auth.Method{Type: auth.MethodOAuth, OAuth: &auth.OAuthMethod{AccessToken: "token-b", AccountID: "acct-b", Email: "b@example.com"}},
	}), nil, time.Now)
	reqA := newStatusRequestForTest(withStatusWorkspaceRoot("/tmp/workdir"), withStatusAuthManager(managerA))
	reqB := newStatusRequestForTest(withStatusWorkspaceRoot("/tmp/workdir"), withStatusAuthManager(managerB))
	base := uiStatusSnapshot{Workdir: "/tmp/workdir"}

	repo.StoreAuth(statusAuthCacheKey(reqA), uiStatusAuthStageResult{
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
	repo := newMemoryUIStatusRepository()
	managerA := auth.NewManager(auth.NewMemoryStore(auth.State{
		Method: auth.Method{Type: auth.MethodOAuth, OAuth: &auth.OAuthMethod{AccessToken: "token-a"}},
	}), nil, time.Now)
	managerB := auth.NewManager(auth.NewMemoryStore(auth.State{
		Method: auth.Method{Type: auth.MethodOAuth, OAuth: &auth.OAuthMethod{AccessToken: "token-b"}},
	}), nil, time.Now)
	reqA := newStatusRequestForTest(withStatusWorkspaceRoot("/tmp/workdir"), withStatusAuthManager(managerA))
	reqB := newStatusRequestForTest(withStatusWorkspaceRoot("/tmp/workdir"), withStatusAuthManager(managerB))
	base := uiStatusSnapshot{Workdir: "/tmp/workdir"}

	repo.StoreAuth(statusAuthCacheKey(reqA), uiStatusAuthStageResult{
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

func TestStatusRepositoryStoresAuthUnderCapturedIdentityKey(t *testing.T) {
	store := auth.NewMemoryStore(auth.State{
		Method: auth.Method{Type: auth.MethodOAuth, OAuth: &auth.OAuthMethod{AccessToken: "token-a", AccountID: "acct-a", Email: "a@example.com"}},
	})
	manager := auth.NewManager(store, nil, time.Now)
	req := newStatusRequestForTest(withStatusWorkspaceRoot("/tmp/workdir"), withStatusAuthManager(manager))
	base := uiStatusSnapshot{Workdir: "/tmp/workdir"}
	cacheKey := statusAuthCacheKey(req)

	if err := store.Save(context.Background(), auth.State{
		Method: auth.Method{Type: auth.MethodOAuth, OAuth: &auth.OAuthMethod{AccessToken: "token-b", AccountID: "acct-b", Email: "b@example.com"}},
	}); err != nil {
		t.Fatalf("switch auth identity: %v", err)
	}

	repo := newMemoryUIStatusRepository()
	repo.StoreAuth(cacheKey, uiStatusAuthStageResult{
		Auth:         uiStatusAuthInfo{Summary: "a@example.com"},
		Subscription: uiStatusSubscriptionInfo{Applicable: true, Summary: "Pro subscription"},
	}, time.Now())

	reqB := req
	reqB.AuthCacheIdentity = statusAuthCacheIdentity(manager)
	reqB.CacheKeys.Auth = statusAuthCacheKey(reqB)
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
	repo := newMemoryUIStatusRepository()
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

func TestStatusCommandRefreshesGitWhenCachedResultIsInvisible(t *testing.T) {
	repo := newMemoryUIStatusRepository()
	repo.StoreGit(
		statusGitCacheKey("/tmp/workdir"),
		uiStatusGitStageResult{Git: uiStatusGitInfo{}},
		time.Now(),
	)
	collector := &stubProgressiveStatusCollector{
		base: uiStatusSnapshot{
			CollectedAt: time.Date(2026, time.March, 24, 21, 15, 0, 0, time.UTC),
			Workdir:     "/tmp/workdir",
			SessionName: "incident",
			SessionID:   "session-123",
			Model:       uiStatusModelInfo{Summary: "gpt-5 high fast"},
		},
		gitResult: uiStatusGitStageResult{Git: uiStatusGitInfo{Visible: true, Branch: "master", Dirty: true, Ahead: 2, Behind: 1}},
	}

	m := newProjectedStaticUIModel(
		WithUIStatusConfig(uiStatusConfig{WorkspaceRoot: "/tmp/workdir"}),
		WithUIStatusCollector(collector),
		WithUIStatusRepository(repo),
	)
	m.termWidth = 100
	m.termHeight = 40
	m.windowSizeKnown = true
	m.input = "/status"

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if updated.status.pendingSections == nil || !updated.status.pendingSections[uiStatusSectionGit] {
		t.Fatalf("expected git section to refresh when cached git result is invisible, got %+v", updated.status.pendingSections)
	}
	plain := stripANSIAndTrimRight(updated.View())
	if !strings.Contains(plain, "Loading git...") {
		t.Fatalf("expected git section placeholder before refreshed result, got %q", plain)
	}

	next, _ = updated.Update(statusGitRefreshDoneMsg{token: updated.status.refreshToken, result: collector.gitResult})
	updated = next.(*uiModel)
	plain = stripANSIAndTrimRight(updated.View())
	if !strings.Contains(plain, "master") || !strings.Contains(plain, "dirty | ahead 2 | behind 1") {
		t.Fatalf("expected refreshed git summary after invisible cached result, got %q", plain)
	}
}

func TestStatusRepositoryNormalizesGitCacheKeysAcrossSlashStyles(t *testing.T) {
	repo := newMemoryUIStatusRepository()
	now := time.Now()
	repo.StoreGit(
		statusGitCacheKey(`C:\repo`),
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

func TestCollectGitStatusSurfacesUnexpectedErrors(t *testing.T) {
	workdir := t.TempDir()
	cmd := exec.Command("git", "-C", workdir, "init")
	cmd.Env = sanitizedGitEnv(os.Environ())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v (%s)", err, out)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	git := collectGitStatus(ctx, workdir)
	if !git.Visible {
		t.Fatalf("expected git section to remain visible on unexpected errors, got %+v", git)
	}
	if !strings.Contains(git.Error, "git status failed") {
		t.Fatalf("expected git error surfaced, got %+v", git)
	}
	if !strings.Contains(git.Error, context.Canceled.Error()) {
		t.Fatalf("expected git error to include underlying failure, got %+v", git)
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
	git := collectGitStatus(context.Background(), t.TempDir())
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

	git := collectGitStatus(context.Background(), nestedDir)
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

	git := collectGitStatus(context.Background(), linkPath)
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

	git := collectGitStatus(context.Background(), nestedDir)
	if !git.Visible {
		t.Fatalf("expected git section visible when inherited git env points elsewhere, got %+v", git)
	}
	if git.Error != "" {
		t.Fatalf("expected no git error when inherited git env points elsewhere, got %+v", git)
	}
}
