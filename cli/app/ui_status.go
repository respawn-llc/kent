package app

import (
	"context"
	"os"
	"strings"
	"time"

	appstatus "builder/cli/app/internal/status"
	"builder/cli/app/internal/statuscollect"
	"builder/shared/client"
	"builder/shared/clientui"
	"builder/shared/config"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	statusRefreshTimeout = 10 * time.Second
	statusGitTimeout     = 4 * time.Second
)

func newMemoryUIStatusRepository() uiStatusRepository {
	return appstatus.NewMemoryRepository()
}

func statusAuthCacheKey(req uiStatusRequest) string {
	return appstatus.AuthCacheKey(req)
}

func statusGitCacheKey(workdir string) string {
	return appstatus.GitCacheKey(workdir)
}

func statusEnvironmentCacheKey(req uiStatusRequest) string {
	return appstatus.EnvironmentCacheKey(req)
}

func cloneStatusTokenMap(input map[string]int) map[string]int {
	return appstatus.CloneTokenMap(input)
}

type uiStatusConfig struct {
	WorkspaceRoot   string
	PersistenceRoot string
	SessionViews    client.SessionViewClient
	Settings        config.Settings
	Source          config.SourceReport
	AuthManager     statuscollect.AuthStateResolver
	AuthStatus      client.AuthStatusClient
	AuthStatePath   string
	OwnsServer      bool
}

type uiStatusCollector interface {
	Collect(ctx context.Context, req uiStatusRequest) (uiStatusSnapshot, error)
}

type uiStatusProgressiveCollector interface {
	CollectBase(req uiStatusRequest) uiStatusSnapshot
	CollectAuth(ctx context.Context, req uiStatusRequest, base uiStatusSnapshot) uiStatusAuthStageResult
	CollectGit(ctx context.Context, req uiStatusRequest, base uiStatusSnapshot) uiStatusGitStageResult
	CollectEnvironment(ctx context.Context, req uiStatusRequest, base uiStatusSnapshot) uiStatusEnvironmentStageResult
}

type uiStatusSection = appstatus.Section

const (
	uiStatusSectionBase        = appstatus.SectionBase
	uiStatusSectionAuth        = appstatus.SectionAuth
	uiStatusSectionGit         = appstatus.SectionGit
	uiStatusSectionEnvironment = appstatus.SectionEnvironment
)

type uiStatusRequest = appstatus.Request
type uiStatusSnapshot = appstatus.Snapshot
type uiStatusAuthInfo = appstatus.AuthInfo
type uiStatusGitInfo = appstatus.GitInfo
type uiStatusContextInfo = appstatus.ContextInfo
type uiStatusModelInfo = appstatus.ModelInfo
type uiStatusUpdateInfo = appstatus.UpdateInfo
type uiStatusConfigInfo = appstatus.ConfigInfo
type uiStatusSubscriptionInfo = appstatus.SubscriptionInfo
type uiStatusSubscriptionWindow = appstatus.SubscriptionWindow
type uiStatusSkillInspection = appstatus.SkillInspection
type uiStatusRepository = appstatus.Repository
type uiStatusSeedResult = appstatus.SeedResult
type uiStatusAuthStageResult = appstatus.AuthStageResult
type uiStatusGitStageResult = appstatus.GitStageResult
type uiStatusEnvironmentStageResult = appstatus.EnvironmentStageResult

type statusRefreshDoneMsg struct {
	token    uint64
	snapshot uiStatusSnapshot
	err      error
}

type statusBaseRefreshDoneMsg struct {
	token    uint64
	snapshot uiStatusSnapshot
}

type statusAuthRefreshDoneMsg struct {
	token    uint64
	cacheKey string
	result   uiStatusAuthStageResult
}

type statusGitRefreshDoneMsg struct {
	token      uint64
	cacheKey   string
	result     uiStatusGitStageResult
	background bool
}

type statusEnvironmentRefreshDoneMsg struct {
	token    uint64
	cacheKey string
	result   uiStatusEnvironmentStageResult
}

type defaultUIStatusCollector struct {
	authManager statuscollect.AuthStateResolver
}

func WithUIStatusConfig(statusConfig uiStatusConfig) UIOption {
	return func(m *uiModel) {
		statusConfig.AuthManager = statuscollect.NormalizeAuthStateResolver(statusConfig.AuthManager)
		m.statusConfig = statusConfig
		if statusConfig.Settings.Debug {
			m.debugMode = true
		}
		m.updateTranscriptDiagnosticsMode()
		if m.statusCollector == nil {
			m.statusCollector = defaultUIStatusCollector{authManager: statusConfig.AuthManager}
		}
	}
}

func WithUIStatusCollector(collector uiStatusCollector) UIOption {
	return func(m *uiModel) {
		if collector != nil {
			m.statusCollector = collector
		}
	}
}

func WithUIStatusRepository(repository uiStatusRepository) UIOption {
	return func(m *uiModel) {
		if repository != nil {
			m.statusRepository = repository
		}
	}
}

func (m *uiModel) newStatusRequest(now time.Time) uiStatusRequest {
	request := uiStatusRequest{
		Runtime:               m.engine,
		WorkspaceRoot:         strings.TrimSpace(m.statusConfig.WorkspaceRoot),
		PersistenceRoot:       strings.TrimSpace(m.statusConfig.PersistenceRoot),
		SessionViews:          m.statusConfig.SessionViews,
		Settings:              m.statusConfig.Settings,
		Source:                m.statusConfig.Source,
		AuthCacheIdentity:     statusAuthCacheIdentity(m.statusConfig.AuthManager),
		AuthStatus:            m.statusConfig.AuthStatus,
		AuthStatePath:         strings.TrimSpace(m.statusConfig.AuthStatePath),
		SessionName:           strings.TrimSpace(m.sessionName),
		SessionID:             strings.TrimSpace(m.sessionID),
		ConfiguredModelName:   strings.TrimSpace(m.configuredModelName),
		ModelName:             strings.TrimSpace(m.modelName),
		ThinkingLevel:         strings.TrimSpace(m.thinkingLevel),
		FastModeAvailable:     m.fastModeAvailable,
		FastModeEnabled:       m.fastModeEnabled,
		ReviewerEnabled:       m.reviewerEnabled,
		ReviewerMode:          strings.TrimSpace(m.reviewerMode),
		AutoCompactionEnabled: m.autoCompactionEnabled,
		OwnsServer:            m.statusConfig.OwnsServer,
		CurrentTime:           now,
	}
	return populateStatusRequestCacheKeys(request)
}

func populateStatusRequestCacheKeys(req uiStatusRequest) uiStatusRequest {
	if strings.TrimSpace(req.CacheKeys.Auth) == "" {
		req.CacheKeys.Auth = statusAuthCacheKey(req)
	}
	if strings.TrimSpace(req.CacheKeys.Git) == "" {
		req.CacheKeys.Git = statusGitCacheKey(statusGitRoot(req))
	}
	if strings.TrimSpace(req.CacheKeys.Environment) == "" {
		req.CacheKeys.Environment = statusEnvironmentCacheKey(req)
	}
	return req
}

func (c defaultUIStatusCollector) Collect(ctx context.Context, req uiStatusRequest) (uiStatusSnapshot, error) {
	return c.adapter().Collect(ctx, req)
}

func (c defaultUIStatusCollector) CollectBase(req uiStatusRequest) uiStatusSnapshot {
	return c.adapter().CollectBase(req)
}

func (c defaultUIStatusCollector) CollectAuth(ctx context.Context, req uiStatusRequest, base uiStatusSnapshot) uiStatusAuthStageResult {
	return c.adapter().CollectAuth(ctx, req, base)
}

func (c defaultUIStatusCollector) CollectGit(ctx context.Context, req uiStatusRequest, base uiStatusSnapshot) uiStatusGitStageResult {
	return c.adapter().CollectGit(ctx, req, base)
}

func (c defaultUIStatusCollector) CollectEnvironment(ctx context.Context, req uiStatusRequest, base uiStatusSnapshot) uiStatusEnvironmentStageResult {
	return c.adapter().CollectEnvironment(ctx, req, base)
}

func (c defaultUIStatusCollector) adapter() statuscollect.Collector {
	return statuscollect.Collector{
		AuthManager:              c.authManager,
		RequestTimeout:           statusRefreshTimeout,
		GitTimeout:               statusGitTimeout,
		ParentSessionReadTimeout: uiRuntimeReadTimeout,
		EnvSanitizer:             sanitizedGitEnv,
	}
}

func enrichStatusBaseSnapshot(ctx context.Context, req uiStatusRequest, snapshot uiStatusSnapshot) uiStatusSnapshot {
	return defaultUIStatusCollector{}.adapter().EnrichBase(ctx, req, snapshot)
}

func statusExecutionTarget(req uiStatusRequest) clientui.SessionExecutionTarget {
	return appstatus.ExecutionTarget(req)
}

func statusEnvironmentRoot(workspaceRoot string, target clientui.SessionExecutionTarget) string {
	return appstatus.EnvironmentRoot(workspaceRoot, target)
}

func statusWorkdir(workspaceRoot string, target clientui.SessionExecutionTarget) string {
	return appstatus.Workdir(workspaceRoot, target)
}

func statusGitRoot(req uiStatusRequest) string {
	return appstatus.GitRoot(req)
}

func collectGitStatus(ctx context.Context, workdir string) uiStatusGitInfo {
	return appstatus.CollectGitStatus(ctx, workdir, statusGitTimeout, sanitizedGitEnv)
}

func statusGitRepositoryProbe(workdir string) (bool, error) {
	return appstatus.GitRepositoryProbe(workdir)
}

func statusGitError(err error, output string) string {
	return appstatus.GitError(err, output)
}

func statusAuthCacheIdentity(manager statuscollect.AuthStateLoader) string {
	return statuscollect.AuthCacheIdentity(manager)
}

func statusOnOff(value bool) string {
	if value {
		return "on"
	}
	return "off"
}

func statusYesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func (m *uiModel) openStatusOverlay() {
	m.status.open = true
	m.status.scroll = 0
	m.status.error = ""
	m.status.loading = false
	m.status.pendingSections = nil
	m.status.sectionWarnings = nil
	m.setInputMode(uiInputModeStatus)
	if m.statusCollector == nil {
		m.statusCollector = defaultUIStatusCollector{authManager: m.statusConfig.AuthManager}
	}
}

func (m *uiModel) closeStatusOverlay() {
	m.status.open = false
	m.status.scroll = 0
	m.status.loading = false
	m.status.pendingSections = nil
	m.status.sectionWarnings = nil
	m.restorePrimaryInputMode()
}

func (m *uiModel) startStatusSectionRefresh(sections ...uiStatusSection) {
	if len(sections) == 0 {
		m.status.loading = false
		return
	}
	if m.status.pendingSections == nil {
		m.status.pendingSections = map[uiStatusSection]bool{}
	}
	if m.status.sectionWarnings == nil {
		m.status.sectionWarnings = map[uiStatusSection]string{}
	}
	for _, section := range sections {
		m.status.pendingSections[section] = true
		delete(m.status.sectionWarnings, section)
	}
	m.status.loading = len(m.status.pendingSections) > 0
}

func (m *uiModel) finishStatusSectionRefresh(section uiStatusSection, warning string) {
	if m.status.pendingSections != nil {
		delete(m.status.pendingSections, section)
	}
	if m.status.sectionWarnings == nil {
		m.status.sectionWarnings = map[uiStatusSection]string{}
	}
	if strings.TrimSpace(warning) == "" {
		delete(m.status.sectionWarnings, section)
	} else {
		m.status.sectionWarnings[section] = strings.TrimSpace(warning)
	}
	m.status.loading = len(m.status.pendingSections) > 0
	m.status.snapshot.CollectorWarning = m.statusCombinedWarnings()
}

func (m *uiModel) statusCombinedWarnings() string {
	if len(m.status.sectionWarnings) == 0 {
		return ""
	}
	parts := make([]string, 0, len(m.status.sectionWarnings))
	for _, section := range []uiStatusSection{uiStatusSectionBase, uiStatusSectionEnvironment, uiStatusSectionGit, uiStatusSectionAuth} {
		if warning := strings.TrimSpace(m.status.sectionWarnings[section]); warning != "" {
			parts = append(parts, warning)
		}
	}
	return strings.Join(parts, " | ")
}

func (m *uiModel) pushStatusOverlayIfNeeded() tea.Cmd {
	return m.activateSurface(uiSurfaceStatus)
}

func (m *uiModel) popStatusOverlayIfNeeded() tea.Cmd {
	return m.restoreTranscriptSurface()
}

func (m *uiModel) moveStatusScroll(delta int) {
	m.status.scroll += delta
	if m.status.scroll < 0 {
		m.status.scroll = 0
	}
}

func (m *uiModel) moveStatusScrollPage(deltaPages int) {
	rowsPerPage := m.statusRowsPerPage()
	m.moveStatusScroll(deltaPages * rowsPerPage)
}

func (m *uiModel) statusRowsPerPage() int {
	available := m.termHeight - 1
	if available < 1 {
		return 1
	}
	return available
}

func (m *uiModel) statusRefreshCmd() tea.Cmd {
	m.status.refreshToken++
	token := m.status.refreshToken
	request := m.newStatusRequest(time.Now())
	collector := m.statusCollector
	if collector == nil {
		collector = defaultUIStatusCollector{authManager: m.statusConfig.AuthManager}
	}
	if progressive, ok := collector.(uiStatusProgressiveCollector); ok {
		base := progressive.CollectBase(request)
		seed := uiStatusSeedResult{Snapshot: base}
		if m.statusRepository != nil {
			seed = m.statusRepository.SeedSnapshot(request, base, request.CurrentTime)
		}
		m.status.snapshot = seed.Snapshot
		m.status.error = ""
		m.status.pendingSections = nil
		m.status.sectionWarnings = seed.Warnings
		m.startStatusSectionRefresh(append([]uiStatusSection{uiStatusSectionBase}, seed.PendingSections...)...)
		cmds := make([]tea.Cmd, 0, len(seed.PendingSections)+1)
		cmds = append(cmds, m.statusBaseRefreshCmd(token, request, base))
		for _, section := range seed.PendingSections {
			switch section {
			case uiStatusSectionAuth:
				cmds = append(cmds, m.statusAuthRefreshCmd(token, request.CacheKeys.Auth, request, progressive, base))
			case uiStatusSectionGit:
				cmds = append(cmds, m.statusGitRefreshCmd(token, request.CacheKeys.Git, request, progressive, base))
			case uiStatusSectionEnvironment:
				cmds = append(cmds, m.statusEnvironmentRefreshCmd(token, request.CacheKeys.Environment, request, progressive, base))
			}
		}
		if len(cmds) == 0 {
			m.status.loading = false
			m.status.snapshot.CollectorWarning = m.statusCombinedWarnings()
			return nil
		}
		return tea.Batch(cmds...)
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), statusRefreshTimeout)
		defer cancel()
		snapshot, err := collector.Collect(ctx, request)
		return statusRefreshDoneMsg{token: token, snapshot: snapshot, err: err}
	}
}

func (m *uiModel) statusLineGitStartupCmd() tea.Cmd {
	request := m.newStatusRequest(time.Now())
	token := m.status.refreshToken
	request.CurrentTime = time.Now()
	collector := m.statusCollector
	if collector == nil {
		collector = defaultUIStatusCollector{authManager: m.statusConfig.AuthManager}
	}
	progressive, ok := collector.(uiStatusProgressiveCollector)
	if !ok {
		progressive = defaultUIStatusCollector{authManager: m.statusConfig.AuthManager}
	}
	base := progressive.CollectBase(request)
	gitRoot := statusGitRoot(request)
	if trimmedGitRoot := strings.TrimSpace(gitRoot); trimmedGitRoot == "" {
		return nil
	} else if info, err := os.Stat(trimmedGitRoot); err != nil || !info.IsDir() {
		return nil
	}
	cacheKey := statusGitCacheKey(gitRoot)
	return m.statusGitRefreshCmd(token, cacheKey, request, progressive, base, true)
}

func (m *uiModel) statusBaseRefreshCmd(token uint64, request uiStatusRequest, base uiStatusSnapshot) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), statusRefreshTimeout)
		defer cancel()
		return statusBaseRefreshDoneMsg{token: token, snapshot: enrichStatusBaseSnapshot(ctx, request, base)}
	}
}

func (m *uiModel) statusAuthRefreshCmd(token uint64, cacheKey string, request uiStatusRequest, collector uiStatusProgressiveCollector, base uiStatusSnapshot) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), statusRefreshTimeout)
		defer cancel()
		return statusAuthRefreshDoneMsg{token: token, cacheKey: cacheKey, result: collector.CollectAuth(ctx, request, base)}
	}
}

func (m *uiModel) statusGitRefreshCmd(token uint64, cacheKey string, request uiStatusRequest, collector uiStatusProgressiveCollector, base uiStatusSnapshot, background ...bool) tea.Cmd {
	isBackground := len(background) > 0 && background[0]
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), statusRefreshTimeout)
		defer cancel()
		return statusGitRefreshDoneMsg{token: token, cacheKey: cacheKey, result: collector.CollectGit(ctx, request, base), background: isBackground}
	}
}

func (m *uiModel) statusEnvironmentRefreshCmd(token uint64, cacheKey string, request uiStatusRequest, collector uiStatusProgressiveCollector, base uiStatusSnapshot) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), statusRefreshTimeout)
		defer cancel()
		return statusEnvironmentRefreshDoneMsg{token: token, cacheKey: cacheKey, result: collector.CollectEnvironment(ctx, request, base)}
	}
}

func (c uiInputController) startStatusFlowCmd() tea.Cmd {
	m := c.model
	m.openStatusOverlay()
	refreshCmd := m.statusRefreshCmd()
	if overlayCmd := m.pushStatusOverlayIfNeeded(); overlayCmd != nil {
		return tea.Batch(overlayCmd, refreshCmd)
	}
	return refreshCmd
}

func (c uiInputController) stopStatusFlowCmd() tea.Cmd {
	m := c.model
	overlayCmd := m.popStatusOverlayIfNeeded()
	m.closeStatusOverlay()
	if overlayCmd != nil {
		return overlayCmd
	}
	return nil
}

func (c uiInputController) handleStatusOverlayKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := c.model
	switch strings.ToLower(msg.String()) {
	case "ctrl+c":
		if m.busy {
			c.interruptBusyRuntime()
			return m, nil
		}
		m.exitAction = UIActionExit
		if overlayCmd := m.popStatusOverlayIfNeeded(); overlayCmd != nil {
			m.closeStatusOverlay()
			return m, tea.Sequence(overlayCmd, tea.Quit)
		}
		return m, tea.Quit
	case "esc", "q":
		return m, c.stopStatusFlowCmd()
	case "up":
		m.moveStatusScroll(-1)
		return m, nil
	case "down":
		m.moveStatusScroll(1)
		return m, nil
	case "pgup":
		m.moveStatusScrollPage(-1)
		return m, nil
	case "pgdown":
		m.moveStatusScrollPage(1)
		return m, nil
	case "home":
		m.status.scroll = 0
		return m, nil
	case "end":
		m.status.scroll = 1 << 30
		return m, nil
	default:
		return m, nil
	}
}
