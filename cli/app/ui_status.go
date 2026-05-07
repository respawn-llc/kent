package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"builder/server/auth"
	"builder/server/generated"
	"builder/server/llm"
	"builder/server/runtime"
	"builder/shared/client"
	"builder/shared/clientui"
	"builder/shared/config"
	"builder/shared/serverapi"
	"builder/shared/tokenutil"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	statusRefreshTimeout = 10 * time.Second
	statusGitTimeout     = 4 * time.Second
	statusUsageBaseURL   = "https://chatgpt.com/backend-api"
)

var statusUsagePayloadFetcher = fetchStatusUsagePayload

type uiStatusConfig struct {
	WorkspaceRoot   string
	PersistenceRoot string
	SessionViews    client.SessionViewClient
	Settings        config.Settings
	Source          config.SourceReport
	AuthManager     *auth.Manager
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

type uiStatusSection string

const (
	uiStatusSectionBase        uiStatusSection = "base"
	uiStatusSectionAuth        uiStatusSection = "account"
	uiStatusSectionGit         uiStatusSection = "git"
	uiStatusSectionEnvironment uiStatusSection = "environment"
)

type uiStatusRequest struct {
	Runtime               clientui.RuntimeClient
	WorkspaceRoot         string
	PersistenceRoot       string
	SessionViews          client.SessionViewClient
	Settings              config.Settings
	Source                config.SourceReport
	AuthManager           *auth.Manager
	AuthStatus            client.AuthStatusClient
	AuthStatePath         string
	SessionName           string
	SessionID             string
	ConfiguredModelName   string
	ModelName             string
	ThinkingLevel         string
	FastModeAvailable     bool
	FastModeEnabled       bool
	ReviewerEnabled       bool
	ReviewerMode          string
	AutoCompactionEnabled bool
	OwnsServer            bool
	CurrentTime           time.Time
}

type uiStatusSnapshot struct {
	CollectedAt       time.Time
	Workdir           string
	SessionName       string
	SessionID         string
	ParentSessionID   string
	ParentSessionName string
	OwnsServer        bool
	Git               uiStatusGitInfo
	Auth              uiStatusAuthInfo
	Context           uiStatusContextInfo
	Model             uiStatusModelInfo
	Update            uiStatusUpdateInfo
	Config            uiStatusConfigInfo
	Subscription      uiStatusSubscriptionInfo
	Skills            []runtime.SkillInspection
	SkillTokenCounts  map[string]int
	AgentsPaths       []string
	AgentTokenCounts  map[string]int
	CompactionCount   int
	CollectorWarning  string
}

type uiStatusAuthInfo struct {
	Summary string
	Details []string
	Visible bool
}

type uiStatusGitInfo struct {
	Visible bool
	Branch  string
	Dirty   bool
	Ahead   int
	Behind  int
	Error   string
}

type uiStatusContextInfo struct {
	UsedTokens      int
	AvailableTokens int
	WindowTokens    int
	ThresholdTokens int
}

type uiStatusModelInfo struct {
	Summary string
}

type uiStatusUpdateInfo struct {
	Checked       bool
	Available     bool
	LatestVersion string
}

type uiStatusConfigInfo struct {
	SettingsPath    string
	OverrideSources []string
	Supervisor      string
	AutoCompaction  bool
	Debug           bool
}

type uiStatusSubscriptionInfo struct {
	Applicable bool
	Summary    string
	Error      string
	Windows    []uiStatusSubscriptionWindow
}

type uiStatusSubscriptionWindow struct {
	Label       string
	Qualifier   string
	UsedPercent float64
	ResetAt     time.Time
}

type statusUsagePayload struct {
	PlanType             string                   `json:"plan_type"`
	RateLimit            *statusUsageRateLimit    `json:"rate_limit"`
	AdditionalRateLimits []statusUsageExtraBucket `json:"additional_rate_limits"`
}

type statusUsageExtraBucket struct {
	MeteredFeature string                `json:"metered_feature"`
	LimitName      string                `json:"limit_name"`
	RateLimit      *statusUsageRateLimit `json:"rate_limit"`
}

type statusUsageRateLimit struct {
	PrimaryWindow   *statusUsageWindow `json:"primary_window"`
	SecondaryWindow *statusUsageWindow `json:"secondary_window"`
}

type statusUsageWindow struct {
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds int     `json:"limit_window_seconds"`
	ResetAt            int64   `json:"reset_at"`
}

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

type uiStatusAuthStageResult struct {
	Auth         uiStatusAuthInfo
	Subscription uiStatusSubscriptionInfo
	Warning      string
}

type uiStatusGitStageResult struct {
	Git uiStatusGitInfo
}

type uiStatusEnvironmentStageResult struct {
	Skills           []runtime.SkillInspection
	SkillTokenCounts map[string]int
	AgentsPaths      []string
	AgentTokenCounts map[string]int
	CollectorWarning string
}

type defaultUIStatusCollector struct{}

func WithUIStatusConfig(statusConfig uiStatusConfig) UIOption {
	return func(m *uiModel) {
		m.statusConfig = statusConfig
		if statusConfig.Settings.Debug {
			m.debugMode = true
		}
		m.updateTranscriptDiagnosticsMode()
		if m.statusCollector == nil {
			m.statusCollector = defaultUIStatusCollector{}
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
	return uiStatusRequest{
		Runtime:               m.engine,
		WorkspaceRoot:         strings.TrimSpace(m.statusConfig.WorkspaceRoot),
		PersistenceRoot:       strings.TrimSpace(m.statusConfig.PersistenceRoot),
		SessionViews:          m.statusConfig.SessionViews,
		Settings:              m.statusConfig.Settings,
		Source:                m.statusConfig.Source,
		AuthManager:           m.statusConfig.AuthManager,
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
}

func (c defaultUIStatusCollector) Collect(ctx context.Context, req uiStatusRequest) (uiStatusSnapshot, error) {
	snapshot := enrichStatusBaseSnapshot(ctx, req, c.CollectBase(req))
	authResult := c.CollectAuth(ctx, req, snapshot)
	gitResult := c.CollectGit(ctx, req, snapshot)
	envResult := c.CollectEnvironment(ctx, req, snapshot)
	snapshot.Auth = authResult.Auth
	snapshot.Subscription = authResult.Subscription
	snapshot.Git = gitResult.Git
	snapshot.Skills = envResult.Skills
	snapshot.SkillTokenCounts = envResult.SkillTokenCounts
	snapshot.AgentsPaths = envResult.AgentsPaths
	snapshot.AgentTokenCounts = envResult.AgentTokenCounts
	warnings := make([]string, 0, 3)
	if strings.TrimSpace(snapshot.CollectorWarning) != "" {
		warnings = append(warnings, strings.TrimSpace(snapshot.CollectorWarning))
	}
	if strings.TrimSpace(authResult.Warning) != "" {
		warnings = append(warnings, strings.TrimSpace(authResult.Warning))
	}
	if strings.TrimSpace(envResult.CollectorWarning) != "" {
		warnings = append(warnings, strings.TrimSpace(envResult.CollectorWarning))
	}
	snapshot.CollectorWarning = strings.Join(warnings, " | ")
	return snapshot, nil
}

func (defaultUIStatusCollector) CollectBase(req uiStatusRequest) uiStatusSnapshot {
	collectedAt := req.CurrentTime
	if collectedAt.IsZero() {
		collectedAt = time.Now()
	}
	target := statusExecutionTarget(req)
	workdir := statusWorkdir(req.WorkspaceRoot, target)
	contextInfo := uiStatusContextInfo{ThresholdTokens: req.Settings.ContextCompactionThresholdTokens}
	parentSessionID := ""
	compactionCount := 0
	if req.Runtime != nil {
		status := req.Runtime.Status()
		usage := status.ContextUsage
		contextInfo.UsedTokens = usage.UsedTokens
		contextInfo.WindowTokens = usage.WindowTokens
		contextInfo.AvailableTokens = usage.WindowTokens - usage.UsedTokens
		if contextInfo.AvailableTokens < 0 {
			contextInfo.AvailableTokens = 0
		}
		parentSessionID = strings.TrimSpace(status.ParentSessionID)
		compactionCount = status.CompactionCount
	}
	return uiStatusSnapshot{
		CollectedAt:     collectedAt,
		Workdir:         filepath.ToSlash(strings.TrimSpace(workdir)),
		SessionName:     strings.TrimSpace(req.SessionName),
		SessionID:       strings.TrimSpace(req.SessionID),
		ParentSessionID: parentSessionID,
		OwnsServer:      req.OwnsServer,
		Context:         contextInfo,
		Model:           uiStatusModelInfo{Summary: statusModelSummary(req)},
		Update:          statusUpdateInfo(req),
		Config: uiStatusConfigInfo{
			SettingsPath:    filepath.ToSlash(strings.TrimSpace(req.Source.SettingsPath)),
			OverrideSources: statusConfigOverrideSources(req.Source),
			Supervisor:      statusSupervisorLabel(req.ReviewerEnabled, strings.TrimSpace(req.ReviewerMode)),
			AutoCompaction:  req.AutoCompactionEnabled,
			Debug:           req.Settings.Debug,
		},
		CompactionCount: compactionCount,
	}
}

func statusUpdateInfo(req uiStatusRequest) uiStatusUpdateInfo {
	if req.Runtime == nil {
		return uiStatusUpdateInfo{}
	}
	status := req.Runtime.Status().Update
	return uiStatusUpdateInfo{
		Checked:       status.Checked,
		Available:     status.Available,
		LatestVersion: strings.TrimSpace(status.LatestVersion),
	}
}

func enrichStatusBaseSnapshot(ctx context.Context, req uiStatusRequest, snapshot uiStatusSnapshot) uiStatusSnapshot {
	if parentSessionID := strings.TrimSpace(snapshot.ParentSessionID); parentSessionID != "" {
		if parentSessionName, warning := statusParentSessionName(ctx, req.SessionViews, parentSessionID); strings.TrimSpace(parentSessionName) != "" {
			snapshot.ParentSessionName = parentSessionName
		} else if strings.TrimSpace(warning) != "" {
			snapshot.CollectorWarning = joinStatusWarnings(snapshot.CollectorWarning, warning)
		}
	}
	return snapshot
}

func joinStatusWarnings(existing string, warning string) string {
	parts := make([]string, 0, 2)
	if trimmed := strings.TrimSpace(existing); trimmed != "" {
		parts = append(parts, trimmed)
	}
	if trimmed := strings.TrimSpace(warning); trimmed != "" {
		parts = append(parts, trimmed)
	}
	return strings.Join(parts, " | ")
}

func statusParentSessionName(ctx context.Context, sessionViews client.SessionViewClient, parentSessionID string) (string, string) {
	parentID := strings.TrimSpace(parentSessionID)
	if sessionViews == nil || parentID == "" {
		return "", ""
	}
	readCtx, cancel := context.WithTimeout(ctx, uiRuntimeReadTimeout)
	defer cancel()
	resp, err := sessionViews.GetSessionMainView(readCtx, serverapi.SessionMainViewRequest{SessionID: parentID})
	if err != nil {
		return "", "parent session: " + err.Error()
	}
	return strings.TrimSpace(resp.MainView.Session.SessionName), ""
}

func (defaultUIStatusCollector) CollectAuth(ctx context.Context, req uiStatusRequest, _ uiStatusSnapshot) uiStatusAuthStageResult {
	if req.AuthStatus != nil {
		resp, err := req.AuthStatus.GetAuthStatus(ctx, serverapi.AuthStatusRequest{})
		if err != nil {
			errText := err.Error()
			return uiStatusAuthStageResult{
				Auth:         uiStatusAuthInfo{Summary: "Auth unavailable", Details: []string{errText}, Visible: true},
				Subscription: uiStatusSubscriptionInfo{Applicable: true, Summary: "Subscription unavailable: " + errText, Error: errText},
				Warning:      "auth: " + errText,
			}
		}
		return uiStatusAuthStageResult{
			Auth: uiStatusAuthInfo{
				Summary: strings.TrimSpace(resp.Auth.Summary),
				Details: append([]string(nil), resp.Auth.Details...),
				Visible: resp.Auth.Visible,
			},
			Subscription: uiStatusSubscriptionInfo{
				Applicable: resp.Subscription.Applicable,
				Summary:    strings.TrimSpace(resp.Subscription.Summary),
				Error:      strings.TrimSpace(resp.Subscription.Error),
				Windows:    statusSubscriptionWindowsFromAPI(resp.Subscription.Windows),
			},
			Warning: strings.TrimSpace(resp.Warning),
		}
	}
	state := auth.EmptyState()
	authStateErr := error(nil)
	if req.AuthManager != nil {
		loaded, loadErr := req.AuthManager.Load(ctx)
		if loadErr != nil {
			authStateErr = loadErr
		} else {
			state = loaded
			resolved, resolveErr := req.AuthManager.CurrentState(ctx)
			if resolveErr == nil {
				state = resolved
			} else {
				authStateErr = resolveErr
			}
		}
	}
	result := uiStatusAuthStageResult{
		Auth:         statusAuthInfo(state, req.Settings, authStateErr),
		Subscription: collectSubscriptionStatus(ctx, req, state, authStateErr),
	}
	if authStateErr != nil {
		result.Warning = "auth: " + authStateErr.Error()
	}
	return result
}

func (defaultUIStatusCollector) CollectGit(ctx context.Context, req uiStatusRequest, _ uiStatusSnapshot) uiStatusGitStageResult {
	return uiStatusGitStageResult{Git: collectGitStatus(ctx, statusGitRoot(req))}
}

func (defaultUIStatusCollector) CollectEnvironment(_ context.Context, req uiStatusRequest, _ uiStatusSnapshot) uiStatusEnvironmentStageResult {
	result := uiStatusEnvironmentStageResult{}
	warnings := make([]string, 0, 3)
	workspaceRoot := statusEnvironmentRoot(req.WorkspaceRoot, statusExecutionTarget(req))
	if recovered, err := generated.RecoveredRootNonEmpty(); err != nil {
		warnings = append(warnings, "generated: "+err.Error())
	} else if recovered {
		warnings = append(warnings, generated.RecoveredWarning())
	}
	skills, skillsErr := runtime.InspectSkills(workspaceRoot, config.DisabledSkillToggles(req.Settings))
	if skillsErr != nil {
		warnings = append(warnings, "skills: "+skillsErr.Error())
	} else {
		result.Skills = skills
		result.SkillTokenCounts = statusEstimateSkillTokens(skills)
	}
	agentsPaths, agentsErr := runtime.InstalledAgentsPaths(workspaceRoot)
	if agentsErr != nil {
		warnings = append(warnings, "agents: "+agentsErr.Error())
	} else {
		result.AgentsPaths = agentsPaths
		result.AgentTokenCounts = statusEstimatePathTokens(agentsPaths)
	}
	result.CollectorWarning = strings.Join(warnings, " | ")
	return result
}

func statusExecutionTarget(req uiStatusRequest) clientui.SessionExecutionTarget {
	if req.Runtime == nil {
		return clientui.SessionExecutionTarget{}
	}
	return req.Runtime.SessionView().ExecutionTarget
}

func statusEnvironmentRoot(workspaceRoot string, target clientui.SessionExecutionTarget) string {
	if worktreeRoot := strings.TrimSpace(target.WorktreeRoot); worktreeRoot != "" {
		return worktreeRoot
	}
	if registeredWorkspaceRoot := strings.TrimSpace(target.WorkspaceRoot); registeredWorkspaceRoot != "" {
		return registeredWorkspaceRoot
	}
	return strings.TrimSpace(workspaceRoot)
}

func statusWorkdir(workspaceRoot string, target clientui.SessionExecutionTarget) string {
	if workdir := strings.TrimSpace(target.EffectiveWorkdir); workdir != "" {
		return workdir
	}
	workdir := strings.TrimSpace(workspaceRoot)
	if workdir != "" {
		return workdir
	}
	if cwd, err := os.Getwd(); err == nil {
		return strings.TrimSpace(cwd)
	}
	return ""
}

func statusGitRoot(req uiStatusRequest) string {
	target := statusExecutionTarget(req)
	if worktreeRoot := strings.TrimSpace(target.WorktreeRoot); worktreeRoot != "" {
		return worktreeRoot
	}
	if workspaceRoot := strings.TrimSpace(req.WorkspaceRoot); workspaceRoot != "" {
		return workspaceRoot
	}
	if workspaceRoot := strings.TrimSpace(target.WorkspaceRoot); workspaceRoot != "" {
		return workspaceRoot
	}
	return ""
}

func collectGitStatus(ctx context.Context, workdir string) uiStatusGitInfo {
	trimmedWorkdir := strings.TrimSpace(workdir)
	if trimmedWorkdir == "" {
		return uiStatusGitInfo{}
	}
	if _, err := exec.LookPath("git"); err != nil {
		return uiStatusGitInfo{}
	}
	isRepo, probeErr := statusGitRepositoryProbe(trimmedWorkdir)
	if probeErr != nil {
		return uiStatusGitInfo{Visible: true, Error: statusGitError(probeErr, "")}
	}
	if !isRepo {
		return uiStatusGitInfo{}
	}
	gitCtx, cancel := context.WithTimeout(ctx, statusGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(gitCtx, "git", "-C", trimmedWorkdir, "status", "--porcelain=v2", "--branch")
	cmd.Env = sanitizedGitEnv(os.Environ())
	out, err := cmd.CombinedOutput()
	if gitCtx.Err() == context.DeadlineExceeded || err != nil {
		return uiStatusGitInfo{Visible: true, Error: statusGitError(err, string(out))}
	}
	gitInfo := uiStatusGitInfo{Visible: true}
	for _, line := range splitPlainLines(strings.TrimSpace(string(out))) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "# branch.head ") {
			gitInfo.Branch = strings.TrimSpace(strings.TrimPrefix(trimmed, "# branch.head "))
			if gitInfo.Branch == "(detached)" {
				gitInfo.Branch = "detached"
			}
			continue
		}
		if strings.HasPrefix(trimmed, "# branch.ab ") {
			fields := strings.Fields(strings.TrimPrefix(trimmed, "# branch.ab "))
			for _, field := range fields {
				if strings.HasPrefix(field, "+") {
					fmt.Sscanf(strings.TrimPrefix(field, "+"), "%d", &gitInfo.Ahead)
				}
				if strings.HasPrefix(field, "-") {
					fmt.Sscanf(strings.TrimPrefix(field, "-"), "%d", &gitInfo.Behind)
				}
			}
			continue
		}
		if !strings.HasPrefix(trimmed, "#") {
			gitInfo.Dirty = true
		}
	}
	if gitInfo.Branch == "" {
		gitInfo.Branch = "unknown"
	}
	return gitInfo
}

func statusGitRepositoryProbe(workdir string) (bool, error) {
	info, err := os.Stat(workdir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("inspect git workdir: %w", err)
	}
	if !info.IsDir() {
		return false, nil
	}
	current := filepath.Clean(workdir)
	if resolved, err := filepath.EvalSymlinks(current); err == nil {
		current = filepath.Clean(resolved)
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("resolve git workdir: %w", err)
	}
	for {
		gitMetadataPath := filepath.Join(current, ".git")
		if _, err := os.Lstat(gitMetadataPath); err == nil {
			return true, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("inspect git metadata: %w", err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return false, nil
		}
		current = parent
	}
}

func statusGitError(err error, output string) string {
	message := strings.TrimSpace(output)
	if message == "" && err != nil {
		message = strings.TrimSpace(err.Error())
	}
	if message == "" {
		return "git status failed"
	}
	return "git status failed: " + message
}

func collectSubscriptionStatus(ctx context.Context, req uiStatusRequest, state auth.State, authStateErr error) uiStatusSubscriptionInfo {
	if !statusShouldFetchSubscriptionUsage(req.Settings, state) {
		return uiStatusSubscriptionInfo{}
	}
	if authStateErr != nil {
		errText := authStateErr.Error()
		return uiStatusSubscriptionInfo{Applicable: true, Summary: "Subscription unavailable: " + errText, Error: errText}
	}
	payload, err := statusUsagePayloadFetcher(ctx, statusUsageBaseURL, state)
	if err != nil {
		errText := err.Error()
		return uiStatusSubscriptionInfo{Applicable: true, Summary: "Subscription unavailable: " + errText, Error: errText}
	}
	windows := statusUsageWindowsByLabel(payload)
	summary := statusSubscriptionPlanSummary(payload.PlanType)
	return uiStatusSubscriptionInfo{Applicable: true, Summary: summary, Windows: windows}
}

func statusSubscriptionWindowsFromAPI(windows []serverapi.AuthSubscriptionWindow) []uiStatusSubscriptionWindow {
	if len(windows) == 0 {
		return nil
	}
	result := make([]uiStatusSubscriptionWindow, 0, len(windows))
	for _, window := range windows {
		result = append(result, uiStatusSubscriptionWindow{
			Label:       strings.TrimSpace(window.Label),
			Qualifier:   strings.TrimSpace(window.Qualifier),
			UsedPercent: window.UsedPercent,
			ResetAt:     window.ResetAt,
		})
	}
	return result
}

func statusShouldFetchSubscriptionUsage(settings config.Settings, state auth.State) bool {
	if state.Method.Type != auth.MethodOAuth || state.Method.OAuth == nil {
		return false
	}
	if strings.TrimSpace(settings.ProviderOverride) != "" {
		return false
	}
	if baseURL := strings.TrimSpace(settings.OpenAIBaseURL); baseURL != "" && !statusIsOfficialChatGPTBaseURL(baseURL) {
		return false
	}
	return true
}

func statusSubscriptionPlanSummary(plan string) string {
	trimmed := strings.TrimSpace(plan)
	if trimmed == "" {
		return "Subscription"
	}
	normalized := strings.ToLower(trimmed)
	return strings.ToUpper(normalized[:1]) + normalized[1:] + " subscription"
}

func fetchStatusUsagePayload(ctx context.Context, baseURL string, state auth.State) (statusUsagePayload, error) {
	authorization, err := state.Method.AuthHeaderValue()
	if err != nil {
		return statusUsagePayload{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/wham/usage", nil)
	if err != nil {
		return statusUsagePayload{}, err
	}
	request.Header.Set("Authorization", authorization)
	request.Header.Set("User-Agent", "builder/dev")
	if accountID := strings.TrimSpace(state.Method.OAuth.AccountID); accountID != "" {
		request.Header.Set("ChatGPT-Account-Id", accountID)
	}
	response, err := (&http.Client{Timeout: statusRefreshTimeout}).Do(request)
	if err != nil {
		return statusUsagePayload{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return statusUsagePayload{}, fmt.Errorf("usage request failed: %s", response.Status)
	}
	var payload statusUsagePayload
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return statusUsagePayload{}, fmt.Errorf("decode usage response: %w", err)
	}
	return payload, nil
}

func statusUsageWindowsByLabel(payload statusUsagePayload) []uiStatusSubscriptionWindow {
	type orderedWindow struct {
		window        uiStatusSubscriptionWindow
		durationSecs  int
		discoveryRank int
	}
	qualifierCounts := map[string]int{}
	ordered := make([]orderedWindow, 0, 2+len(payload.AdditionalRateLimits)*2)
	discoveryRank := 0
	addWindow := func(window *statusUsageWindow, qualifier string) {
		if window == nil {
			return
		}
		label := statusLimitDuration(window.LimitWindowSeconds / 60)
		if label == "" {
			return
		}
		snapshot := uiStatusSubscriptionWindow{
			Label:       label,
			Qualifier:   qualifier,
			UsedPercent: window.UsedPercent,
		}
		if window.ResetAt > 0 {
			snapshot.ResetAt = time.Unix(window.ResetAt, 0).UTC()
		}
		ordered = append(ordered, orderedWindow{
			window:        snapshot,
			durationSecs:  window.LimitWindowSeconds,
			discoveryRank: discoveryRank,
		})
		discoveryRank++
	}
	if payload.RateLimit != nil {
		addWindow(payload.RateLimit.PrimaryWindow, "")
		addWindow(payload.RateLimit.SecondaryWindow, "")
	}
	for _, extra := range payload.AdditionalRateLimits {
		if extra.RateLimit == nil {
			continue
		}
		qualifier := statusUsageWindowQualifier(extra, qualifierCounts)
		addWindow(extra.RateLimit.PrimaryWindow, qualifier)
		addWindow(extra.RateLimit.SecondaryWindow, qualifier)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].durationSecs != ordered[j].durationSecs {
			return ordered[i].durationSecs < ordered[j].durationSecs
		}
		return ordered[i].discoveryRank < ordered[j].discoveryRank
	})
	windows := make([]uiStatusSubscriptionWindow, 0, len(ordered))
	for _, window := range ordered {
		windows = append(windows, window.window)
	}
	return windows
}

func statusUsageWindowQualifier(bucket statusUsageExtraBucket, counts map[string]int) string {
	limitName := strings.TrimSpace(bucket.LimitName)
	feature := strings.TrimSpace(bucket.MeteredFeature)
	base := ""
	switch {
	case limitName == "" && feature == "":
		base = "extra"
	case limitName == "":
		base = feature
	case feature == "" || strings.EqualFold(limitName, feature):
		base = limitName
	default:
		base = limitName + " / " + feature
	}
	counts[base]++
	if counts[base] == 1 {
		return base
	}
	return fmt.Sprintf("%s #%d", base, counts[base])
}

func statusLimitDuration(windowMinutes int) string {
	const minutesPerHour = 60
	const minutesPerDay = 24 * minutesPerHour
	const minutesPerWeek = 7 * minutesPerDay
	const minutesPerMonth = 30 * minutesPerDay
	const roundingBiasMinutes = 3

	if windowMinutes < 0 {
		windowMinutes = 0
	}
	if windowMinutes <= minutesPerDay+roundingBiasMinutes {
		hours := (windowMinutes + roundingBiasMinutes) / minutesPerHour
		if hours < 1 {
			hours = 1
		}
		return fmt.Sprintf("%dh", hours)
	}
	if windowMinutes <= minutesPerWeek+roundingBiasMinutes {
		return "weekly"
	}
	if windowMinutes <= minutesPerMonth+roundingBiasMinutes {
		return "monthly"
	}
	return "annual"
}

func statusAuthInfo(state auth.State, settings config.Settings, statusErr error) uiStatusAuthInfo {
	if statusErr != nil && !state.IsConfigured() {
		return uiStatusAuthInfo{Summary: "Auth unavailable", Details: []string{statusErr.Error()}, Visible: true}
	}
	details := make([]string, 0, 2)
	baseURL := strings.TrimSpace(settings.OpenAIBaseURL)
	if baseURL != "" && !statusIsOfficialChatGPTBaseURL(baseURL) {
		details = append(details, filepath.ToSlash(baseURL))
	}
	switch state.Method.Type {
	case auth.MethodOAuth:
		summary := "Subscription"
		if state.Method.OAuth != nil && strings.TrimSpace(state.Method.OAuth.Email) != "" {
			summary = strings.TrimSpace(state.Method.OAuth.Email)
		}
		if statusErr != nil {
			details = append(details, statusErr.Error())
		}
		return uiStatusAuthInfo{Summary: summary, Details: details, Visible: true}
	case auth.MethodAPIKey:
		summary := "API key"
		if provider := statusProviderLabel(state, settings); provider != "" {
			details = append(details, provider)
		}
		if pref := statusEnvPreferenceLabel(state.EnvAPIKeyPreference); pref != "" {
			details = append(details, pref)
		}
		if statusErr != nil {
			details = append(details, statusErr.Error())
		}
		return uiStatusAuthInfo{Summary: summary, Details: details, Visible: true}
	default:
		if statusErr != nil {
			return uiStatusAuthInfo{Summary: "Auth unavailable", Details: []string{statusErr.Error()}, Visible: true}
		}
		return uiStatusAuthInfo{}
	}
}

func statusEstimateSkillTokens(skills []runtime.SkillInspection) map[string]int {
	paths := make([]string, 0, len(skills))
	for _, skill := range skills {
		if !skill.Loaded || skill.Disabled {
			continue
		}
		path := strings.TrimSpace(skill.Path)
		if path == "" {
			continue
		}
		paths = append(paths, path)
	}
	return statusEstimatePathTokens(paths)
}

func statusEstimatePathTokens(paths []string) map[string]int {
	counts := map[string]int{}
	for _, rawPath := range paths {
		path := strings.TrimSpace(rawPath)
		if path == "" {
			continue
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		counts[path] = tokenutil.ApproxTextTokenCount(string(contents))
	}
	return counts
}

func statusProviderLabel(state auth.State, settings config.Settings) string {
	providerOverride := strings.ToLower(strings.TrimSpace(settings.ProviderOverride))
	if providerOverride != "" {
		return providerOverride
	}
	if state.Method.Type == auth.MethodOAuth {
		return "chatgpt-codex"
	}
	if strings.TrimSpace(settings.OpenAIBaseURL) != "" {
		return "openai-compatible"
	}
	return "openai"
}

func statusEnvPreferenceLabel(preference auth.EnvAPIKeyPreference) string {
	switch preference {
	case auth.EnvAPIKeyPreferencePreferEnv:
		return "prefer env"
	case auth.EnvAPIKeyPreferencePreferSaved:
		return "prefer saved"
	default:
		return ""
	}
}

func statusConfigOverrideSources(src config.SourceReport) []string {
	present := map[string]bool{}
	for _, source := range src.Sources {
		switch strings.TrimSpace(source) {
		case "env":
			present["ENV"] = true
		case "cli":
			present["CLI ARGS"] = true
		}
	}
	ordered := make([]string, 0, len(present))
	for _, label := range []string{"ENV", "CLI ARGS"} {
		if present[label] {
			ordered = append(ordered, label)
		}
	}
	return ordered
}

func statusIsOfficialChatGPTBaseURL(baseURL string) bool {
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		return true
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return false
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host != "chatgpt.com" && host != "chat.openai.com" {
		return false
	}
	pathValue := strings.TrimRight(strings.TrimSpace(parsed.EscapedPath()), "/")
	return pathValue == "" || pathValue == "/backend-api"
}

func statusModelSummary(req uiStatusRequest) string {
	resolved := strings.TrimSpace(req.ModelName)
	configured := strings.TrimSpace(req.ConfiguredModelName)
	modelName := resolved
	if modelName == "" {
		modelName = configured
	}
	if modelName == "" {
		modelName = "<unset>"
	}
	parts := []string{llm.ModelDisplayLabel(modelName, strings.TrimSpace(req.ThinkingLevel))}
	if req.FastModeAvailable && req.FastModeEnabled {
		parts = append(parts, "fast")
	}
	return strings.Join(parts, " ")
}

func statusSupervisorLabel(enabled bool, mode string) string {
	if !enabled {
		return "off"
	}
	trimmed := strings.TrimSpace(mode)
	if trimmed == "" || trimmed == "off" {
		return "on"
	}
	return trimmed
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
		m.statusCollector = defaultUIStatusCollector{}
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
		collector = defaultUIStatusCollector{}
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
				cmds = append(cmds, m.statusAuthRefreshCmd(token, statusAuthCacheKey(request), request, progressive, base))
			case uiStatusSectionGit:
				cmds = append(cmds, m.statusGitRefreshCmd(token, statusGitCacheKey(statusGitRoot(request)), request, progressive, base))
			case uiStatusSectionEnvironment:
				cmds = append(cmds, m.statusEnvironmentRefreshCmd(token, statusEnvironmentCacheKey(request), request, progressive, base))
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
		collector = defaultUIStatusCollector{}
	}
	progressive, ok := collector.(uiStatusProgressiveCollector)
	if !ok {
		progressive = defaultUIStatusCollector{}
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
