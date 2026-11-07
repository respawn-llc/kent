package app

import worktreepb "core/shared/protoapi/gen/kent/api/worktree"

import (
	"context"
	"strings"
	"time"

	"core/cli/app/internal/status"

	"core/shared/apicontract"
	"core/shared/clientui"
	"core/shared/config"
	"core/shared/textutil"

	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/protobuf/proto"
)

const (
	statusRefreshTimeout = 10 * time.Second
	statusGitTimeout     = 4 * time.Second
)

type uiStatusConfig struct {
	WorkspaceRoot   string
	PersistenceRoot string
	ExecutionTarget *worktreepb.SessionExecutionTarget
	SessionViews    apicontract.SessionViewService
	Settings        config.Settings
	Source          config.SourceReport
	AuthStatus      apicontract.AuthStatusService
}

type uiStatusCollector interface {
	CollectBase(req uiStatusRequest) uiStatusSnapshot
	CollectAuth(ctx context.Context, req uiStatusRequest, base uiStatusSnapshot) uiStatusAuthStageResult
	CollectGit(ctx context.Context, req uiStatusRequest, base uiStatusSnapshot) uiStatusGitStageResult
	CollectEnvironment(ctx context.Context, req uiStatusRequest, base uiStatusSnapshot) uiStatusEnvironmentStageResult
}

type uiStatusSection = status.Section

const (
	uiStatusSectionBase        = status.SectionBase
	uiStatusSectionAuth        = status.SectionAuth
	uiStatusSectionGit         = status.SectionGit
	uiStatusSectionEnvironment = status.SectionEnvironment
)

type uiStatusRequest = status.Request
type uiStatusSnapshot = status.Snapshot
type uiStatusAuthInfo = status.AuthInfo
type uiStatusGitInfo = status.GitInfo
type uiStatusContextInfo = status.ContextInfo
type uiStatusModelInfo = status.ModelInfo
type uiStatusConfigInfo = status.ConfigInfo
type uiStatusSubscriptionInfo = status.SubscriptionInfo
type uiStatusSubscriptionWindow = status.SubscriptionWindow
type uiStatusSkillInspection = status.SkillInspection
type uiStatusRepository = status.Repository
type uiStatusSeedResult = status.SeedResult
type uiStatusAuthStageResult = status.AuthStageResult
type uiStatusGitStageResult = status.GitStageResult
type uiStatusEnvironmentStageResult = status.EnvironmentStageResult

type statusBaseRefreshDoneMsg struct {
	owner    *uiStatusCollection
	snapshot uiStatusSnapshot
}

type statusAuthRefreshDoneMsg struct {
	owner  *uiStatusCollection
	result uiStatusAuthStageResult
}

type statusGitRefreshDoneMsg struct {
	owner    *uiStatusCollection
	cacheKey string
	result   uiStatusGitStageResult
}

type statusEnvironmentRefreshDoneMsg struct {
	owner    *uiStatusCollection
	cacheKey string
	result   uiStatusEnvironmentStageResult
}

func WithUIStatusConfig(statusConfig uiStatusConfig) UIOption {
	return func(m *uiModel) {
		m.statusConfig = statusConfig
		m.status.uiStatusCollection = &uiStatusCollection{}
		if statusConfig.Settings.Debug {
			m.debugMode = true
		}
		if m.statusCollector == nil {
			m.statusCollector = defaultUIStatusCollector()
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
	executionTarget := m.currentExecutionTarget()
	request := uiStatusRequest{
		WorkspaceRoot:         m.currentExecutionWorkdir(executionTarget),
		PersistenceRoot:       strings.TrimSpace(m.statusConfig.PersistenceRoot),
		ExecutionTarget:       executionTarget,
		SessionViews:          m.statusConfig.SessionViews,
		Settings:              m.statusConfig.Settings,
		Source:                m.statusConfig.Source,
		AuthStatus:            m.statusConfig.AuthStatus,
		SessionName:           m.sessionName,
		SessionID:             strings.TrimSpace(m.currentRuntimeSessionID()),
		AgentRole:             textutil.Pointer(m.status.snapshot.AgentRole),
		ConfiguredModelName:   textutil.Pointer(m.configuredModelName),
		ModelName:             strings.TrimSpace(m.modelName),
		ThinkingLevel:         strings.TrimSpace(m.thinkingLevel),
		FastModeAvailable:     m.fastModeAvailable,
		FastModeEnabled:       m.fastModeEnabled,
		ReviewerEnabled:       m.reviewerEnabled,
		ReviewerMode:          strings.TrimSpace(m.reviewerMode),
		AutoCompactionEnabled: m.autoCompactionEnabled,
		QuestionsEnabled:      m.questionsEnabled,
		CurrentTime:           now,
	}
	if m.status.destination != nil && !m.statusDestinationMatches(request) {
		request.AgentRole = nil
	}
	return populateStatusRequestCacheKeys(request)
}

func (m *uiModel) currentExecutionTarget() *worktreepb.SessionExecutionTarget {
	if m == nil {
		return nil
	}
	target := m.cachedRuntimeMainView().Session.ExecutionTarget
	if !clientui.SessionExecutionTargetIsZero(target) {
		return target
	}
	return m.statusConfig.ExecutionTarget
}

func (m *uiModel) currentExecutionWorkdir(target *worktreepb.SessionExecutionTarget) string {
	if workdir := strings.TrimSpace(target.GetEffectiveWorkdir()); workdir != "" {
		return workdir
	}
	return strings.TrimSpace(m.statusConfig.WorkspaceRoot)
}

func populateStatusRequestCacheKeys(req uiStatusRequest) uiStatusRequest {
	if strings.TrimSpace(req.CacheKeys.Git) == "" {
		req.CacheKeys.Git = status.GitCacheKey(status.GitRoot(req))
	}
	if strings.TrimSpace(req.CacheKeys.Environment) == "" {
		req.CacheKeys.Environment = strings.TrimSpace(req.WorkspaceRoot)
	}
	return req
}

func defaultUIStatusCollector() status.Collector {
	return status.Collector{
		RequestTimeout:         statusRefreshTimeout,
		GitTimeout:             statusGitTimeout,
		SessionNameReadTimeout: uiRuntimeReadTimeout,
	}
}

func (m *uiModel) openStatusOverlay() {
	m.status.open = true
	m.status.scroll = 0
	m.setInputMode(uiInputModeStatus)
}

func (m *uiModel) closeStatusOverlay() {
	m.status.open = false
	m.status.scroll = 0
	m.restorePrimaryInputMode()
}

func (m *uiModel) startStatusSectionRefresh(sections ...uiStatusSection) {
	if m.status.pendingSections == nil {
		m.status.pendingSections = map[uiStatusSection]int{}
	}
	for _, section := range sections {
		m.status.pendingSections[section]++
	}
}

func (m *uiModel) finishStatusSectionRefresh(section uiStatusSection, warning string) {
	if m.status.pendingSections[section] == 1 {
		delete(m.status.pendingSections, section)
	} else {
		m.status.pendingSections[section]--
	}
	if m.status.sectionWarnings == nil {
		m.status.sectionWarnings = map[uiStatusSection]string{}
	}
	if strings.TrimSpace(warning) == "" {
		delete(m.status.sectionWarnings, section)
	} else {
		m.status.sectionWarnings[section] = strings.TrimSpace(warning)
	}
	m.status.snapshot.CollectorWarning = m.statusCombinedWarnings()
}

func (m *uiModel) statusCombinedWarnings() string {
	warnings := ""
	for _, section := range []uiStatusSection{uiStatusSectionBase, uiStatusSectionEnvironment, uiStatusSectionGit, uiStatusSectionAuth} {
		warnings = status.JoinWarnings(warnings, m.status.sectionWarnings[section])
	}
	return warnings
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
	available := m.layout().effectiveHeight() - 1
	if available < 1 {
		return 1
	}
	return available
}

func (m *uiModel) statusRefreshCmd() tea.Cmd {
	if loading, err := m.statusPreparation(); loading || err != nil {
		return nil
	}
	request := m.newStatusRequest(time.Now())
	collector := m.statusCollector
	if collector == nil {
		collector = defaultUIStatusCollector()
	}
	collectorRequest := request
	collectorRequest.Runtime = m.engine
	seed := m.seedStatusSnapshot(request)
	m.ensureStatusCollection(request, seed)
	owner := m.status.uiStatusCollection
	m.startStatusSectionRefresh(append([]uiStatusSection{uiStatusSectionBase}, seed.PendingSections...)...)
	cmds := make([]tea.Cmd, 0, len(seed.PendingSections)+1)
	cmds = append(cmds, m.statusBaseRefreshCmd(owner, collectorRequest, collector))
	for _, section := range seed.PendingSections {
		switch section {
		case uiStatusSectionAuth:
			cmds = append(cmds, m.statusAuthRefreshCmd(owner, collectorRequest, collector, seed.Snapshot))
		case uiStatusSectionGit:
			cmds = append(cmds, m.statusGitRefreshCmd(owner, request.CacheKeys.Git, collectorRequest, collector, seed.Snapshot))
		case uiStatusSectionEnvironment:
			cmds = append(cmds, m.statusEnvironmentRefreshCmd(owner, request.CacheKeys.Environment, collectorRequest, collector, seed.Snapshot))
		}
	}
	return tea.Batch(append(cmds, m.reconcileSpinnerTicking(false))...)
}

func (m *uiModel) statusLineGitRefreshCmd() tea.Cmd {
	if loading, err := m.statusPreparation(); loading || err != nil {
		return nil
	}
	request := m.newStatusRequest(time.Now())
	collector := m.statusCollector
	if collector == nil {
		collector = defaultUIStatusCollector()
	}
	gitRoot := status.GitRoot(request)
	if strings.TrimSpace(gitRoot) == "" {
		return nil
	}
	seed := m.seedStatusSnapshot(request)
	cacheKey := status.GitCacheKey(gitRoot)
	m.ensureStatusCollection(request, seed)
	m.startStatusSectionRefresh(uiStatusSectionGit)
	request.Runtime = m.engine
	return m.statusGitRefreshCmd(m.status.uiStatusCollection, cacheKey, request, collector, seed.Snapshot)
}

func (m *uiModel) seedStatusSnapshot(request uiStatusRequest) uiStatusSeedResult {
	base := defaultUIStatusCollector().CollectBase(request)
	if m.statusRepository != nil {
		return m.statusRepository.SeedSnapshot(request, base, request.CurrentTime)
	}
	return uiStatusSeedResult{Snapshot: base}
}

func (m *uiModel) statusBaseRefreshCmd(owner *uiStatusCollection, request uiStatusRequest, collector uiStatusCollector) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), statusRefreshTimeout)
		defer cancel()
		base := collector.CollectBase(request)
		return statusBaseRefreshDoneMsg{owner: owner, snapshot: defaultUIStatusCollector().EnrichBase(ctx, request, base)}
	}
}

func (m *uiModel) statusAuthRefreshCmd(owner *uiStatusCollection, request uiStatusRequest, collector uiStatusCollector, base uiStatusSnapshot) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), statusRefreshTimeout)
		defer cancel()
		return statusAuthRefreshDoneMsg{owner: owner, result: collector.CollectAuth(ctx, request, base)}
	}
}

func (m *uiModel) statusGitRefreshCmd(owner *uiStatusCollection, cacheKey string, request uiStatusRequest, collector uiStatusCollector, base uiStatusSnapshot) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), statusRefreshTimeout)
		defer cancel()
		return statusGitRefreshDoneMsg{owner: owner, cacheKey: cacheKey, result: collector.CollectGit(ctx, request, base)}
	}
}

func (m *uiModel) statusEnvironmentRefreshCmd(owner *uiStatusCollection, cacheKey string, request uiStatusRequest, collector uiStatusCollector, base uiStatusSnapshot) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), statusRefreshTimeout)
		defer cancel()
		return statusEnvironmentRefreshDoneMsg{owner: owner, cacheKey: cacheKey, result: collector.CollectEnvironment(ctx, request, base)}
	}
}

type uiStatusDestination struct {
	session         uiPresentationTarget
	workdir         string
	executionTarget *worktreepb.SessionExecutionTarget
}

func (m *uiModel) statusDestinationMatches(request uiStatusRequest) bool {
	destination := m.status.destination
	return destination != nil &&
		sameStatusSession(destination.session, m.presentationTarget) &&
		destination.workdir == request.WorkspaceRoot &&
		proto.Equal(destination.executionTarget, request.ExecutionTarget)
}

func sameStatusSession(previous, current uiPresentationTarget) bool {
	if previousSession, ok := previous.(uiKnownSessionTarget); ok {
		currentSession, ok := current.(uiKnownSessionTarget)
		return ok && previousSession.id == currentSession.id
	}
	return previous == current
}

func (m *uiModel) ensureStatusCollection(request uiStatusRequest, seed uiStatusSeedResult) {
	if m.statusDestinationMatches(request) {
		return
	}
	m.status.uiStatusCollection = &uiStatusCollection{
		destination: &uiStatusDestination{
			session:         m.presentationTarget,
			workdir:         request.WorkspaceRoot,
			executionTarget: proto.CloneOf(request.ExecutionTarget),
		},
		snapshot:        seed.Snapshot,
		sectionWarnings: seed.Warnings,
	}
	m.status.snapshot.CollectorWarning = m.statusCombinedWarnings()
}

func (m *uiModel) reconcileStatusDestination() tea.Cmd {
	if loading, err := m.statusPreparation(); loading || err != nil {
		return nil
	}
	request := m.newStatusRequest(time.Now())
	if m.statusDestinationMatches(request) {
		return nil
	}
	if m.status.open {
		return m.statusRefreshCmd()
	}
	m.ensureStatusCollection(request, m.seedStatusSnapshot(request))
	return m.statusLineGitRefreshCmd()
}

func (m *uiModel) statusPreparation() (loading bool, err error) {
	if m.sessionObservationErr != nil {
		return false, m.sessionObservationErr
	}
	if m.sessionObservationLoading() {
		return true, nil
	}
	if target := m.newSessionTarget(); target != nil &&
		(m.status.destination == nil || !sameStatusSession(m.status.destination.session, target)) {
		switch state := target.settingsLoad.(type) {
		case uiNewSettingsReady:
			return false, nil
		case uiNewSettingsFailed:
			return false, state.err
		default:
			return true, nil
		}
	}
	return false, nil
}

func (m *uiModel) acceptsStatusCompletion(owner *uiStatusCollection) bool {
	return owner == m.status.uiStatusCollection &&
		m.statusDestinationMatches(m.newStatusRequest(time.Now()))
}

func (s *uiStatusCollection) mergeBase(snapshot uiStatusSnapshot) {
	snapshot.Auth = s.snapshot.Auth
	snapshot.Subscription = s.snapshot.Subscription
	snapshot.Git = s.snapshot.Git
	snapshot.SkillPolicy = s.snapshot.SkillPolicy
	snapshot.Skills = s.snapshot.Skills
	snapshot.SkillTokenCounts = s.snapshot.SkillTokenCounts
	snapshot.AgentsPaths = s.snapshot.AgentsPaths
	snapshot.AgentTokenCounts = s.snapshot.AgentTokenCounts
	s.snapshot = snapshot
}

func (c uiInputController) startStatusFlowCmd() tea.Cmd {
	m := c.model
	m.openStatusOverlay()
	overlayCmd := m.activateSurface(uiSurfaceStatus)
	return tea.Batch(overlayCmd, m.statusRefreshCmd(), m.reconcileSpinnerTicking(false))
}

func (c uiInputController) stopStatusFlowCmd() tea.Cmd {
	m := c.model
	overlayCmd := m.restoreTranscriptSurface()
	m.closeStatusOverlay()
	return tea.Batch(overlayCmd, m.reconcileSpinnerTicking(false))
}

func (c uiInputController) handleStatusOverlayKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := c.model
	switch strings.ToLower(msg.String()) {
	case "ctrl+c":
		return c.handleRuntimeCtrlC(c.closeTranscriptSurfaceForRuntimeCtrlC(m.closeStatusOverlay))
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
