package statuscollect

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"builder/cli/app/internal/serverbridge"
	appstatus "builder/cli/app/internal/status"
	"builder/shared/auth"
	"builder/shared/client"
	"builder/shared/config"
	"builder/shared/serverapi"
)

const DefaultUsageBaseURL = "https://chatgpt.com/backend-api"

type UsagePayloadFetcher func(context.Context, string, auth.State) (UsagePayload, error)

type Collector struct {
	AuthManager              AuthStateResolver
	UsagePayloadFetcher      UsagePayloadFetcher
	UsageBaseURL             string
	RequestTimeout           time.Duration
	GitTimeout               time.Duration
	ParentSessionReadTimeout time.Duration
	EnvSanitizer             func([]string) []string
}

func (c Collector) Collect(ctx context.Context, req appstatus.Request) (appstatus.Snapshot, error) {
	snapshot := c.EnrichBase(ctx, req, c.CollectBase(req))
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

func (c Collector) CollectBase(req appstatus.Request) appstatus.Snapshot {
	collectedAt := req.CurrentTime
	if collectedAt.IsZero() {
		collectedAt = time.Now()
	}
	target := appstatus.ExecutionTarget(req)
	workdir := appstatus.Workdir(req.WorkspaceRoot, target)
	contextInfo := appstatus.ContextInfo{ThresholdTokens: req.Settings.ContextCompactionThresholdTokens}
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
	return appstatus.Snapshot{
		CollectedAt:     collectedAt,
		Workdir:         filepath.ToSlash(strings.TrimSpace(workdir)),
		SessionName:     strings.TrimSpace(req.SessionName),
		SessionID:       strings.TrimSpace(req.SessionID),
		ParentSessionID: parentSessionID,
		OwnsServer:      req.OwnsServer,
		Context:         contextInfo,
		Model:           appstatus.ModelInfo{Summary: ModelSummary(req)},
		Update:          UpdateInfo(req),
		Config: appstatus.ConfigInfo{
			SettingsPath:    filepath.ToSlash(strings.TrimSpace(req.Source.SettingsPath)),
			OverrideSources: ConfigOverrideSources(req.Source),
			Supervisor:      SupervisorLabel(req.ReviewerEnabled, strings.TrimSpace(req.ReviewerMode)),
			AutoCompaction:  req.AutoCompactionEnabled,
			Debug:           req.Settings.Debug,
		},
		CompactionCount: compactionCount,
	}
}

func (c Collector) EnrichBase(ctx context.Context, req appstatus.Request, snapshot appstatus.Snapshot) appstatus.Snapshot {
	if parentSessionID := strings.TrimSpace(snapshot.ParentSessionID); parentSessionID != "" {
		if parentSessionName, warning := c.ParentSessionName(ctx, req.SessionViews, parentSessionID); strings.TrimSpace(parentSessionName) != "" {
			snapshot.ParentSessionName = parentSessionName
		} else if strings.TrimSpace(warning) != "" {
			snapshot.CollectorWarning = JoinWarnings(snapshot.CollectorWarning, warning)
		}
	}
	return snapshot
}

func JoinWarnings(existing string, warning string) string {
	parts := make([]string, 0, 2)
	if trimmed := strings.TrimSpace(existing); trimmed != "" {
		parts = append(parts, trimmed)
	}
	if trimmed := strings.TrimSpace(warning); trimmed != "" {
		parts = append(parts, trimmed)
	}
	return strings.Join(parts, " | ")
}

func (c Collector) ParentSessionName(ctx context.Context, sessionViews client.SessionViewClient, parentSessionID string) (string, string) {
	parentID := strings.TrimSpace(parentSessionID)
	if sessionViews == nil || parentID == "" {
		return "", ""
	}
	readTimeout := c.ParentSessionReadTimeout
	if readTimeout <= 0 {
		readTimeout = c.timeout()
	}
	readCtx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	resp, err := sessionViews.GetSessionMainView(readCtx, serverapi.SessionMainViewRequest{SessionID: parentID})
	if err != nil {
		return "", "parent session: " + err.Error()
	}
	return strings.TrimSpace(resp.MainView.Session.SessionName), ""
}

func (c Collector) CollectAuth(ctx context.Context, req appstatus.Request, _ appstatus.Snapshot) appstatus.AuthStageResult {
	if req.AuthStatus != nil {
		resp, err := req.AuthStatus.GetAuthStatus(ctx, serverapi.AuthStatusRequest{})
		if err != nil {
			errText := err.Error()
			return appstatus.AuthStageResult{
				Auth:         appstatus.AuthInfo{Summary: "Auth unavailable", Details: []string{errText}, Visible: true},
				Subscription: appstatus.SubscriptionInfo{Applicable: true, Summary: "Subscription unavailable: " + errText, Error: errText},
				Warning:      "auth: " + errText,
			}
		}
		return appstatus.AuthStageResult{
			Auth: appstatus.AuthInfo{
				Summary:     strings.TrimSpace(resp.Auth.Summary),
				Details:     append([]string(nil), resp.Auth.Details...),
				Visible:     resp.Auth.Visible,
				Method:      resp.Auth.Method,
				Provider:    strings.TrimSpace(resp.Auth.Provider),
				Unavailable: resp.Auth.Unavailable,
			},
			Subscription: appstatus.SubscriptionInfo{
				Applicable: resp.Subscription.Applicable,
				Summary:    strings.TrimSpace(resp.Subscription.Summary),
				Error:      strings.TrimSpace(resp.Subscription.Error),
				Windows:    SubscriptionWindowsFromAPI(resp.Subscription.Windows),
			},
			Warning: strings.TrimSpace(resp.Warning),
		}
	}
	state := auth.EmptyState()
	authStateErr := error(nil)
	authManager := NormalizeAuthStateResolver(c.AuthManager)
	if authManager != nil {
		loaded, loadErr := authManager.Load(ctx)
		if loadErr != nil {
			authStateErr = loadErr
		} else {
			state = loaded
			resolved, resolveErr := authManager.CurrentState(ctx)
			if resolveErr == nil {
				state = resolved
			} else {
				authStateErr = resolveErr
			}
		}
	}
	result := appstatus.AuthStageResult{
		Auth:         AuthInfo(state, req.Settings, authStateErr),
		Subscription: c.CollectSubscription(ctx, req, state, authStateErr),
	}
	if authStateErr != nil {
		result.Warning = "auth: " + authStateErr.Error()
	}
	return result
}

func (c Collector) CollectGit(ctx context.Context, req appstatus.Request, _ appstatus.Snapshot) appstatus.GitStageResult {
	return appstatus.GitStageResult{Git: appstatus.CollectGitStatus(ctx, appstatus.GitRoot(req), c.gitTimeout(), c.EnvSanitizer)}
}

func (Collector) CollectEnvironment(_ context.Context, req appstatus.Request, _ appstatus.Snapshot) appstatus.EnvironmentStageResult {
	result := appstatus.EnvironmentStageResult{}
	warnings := make([]string, 0, 3)
	workspaceRoot := appstatus.EnvironmentRoot(req.WorkspaceRoot, appstatus.ExecutionTarget(req))
	if recovered, err := serverbridge.RecoveredRootNonEmpty(); err != nil {
		warnings = append(warnings, "generated: "+err.Error())
	} else if recovered {
		warnings = append(warnings, serverbridge.RecoveredWarning())
	}
	inspectedSkills, skillsErr := serverbridge.InspectSkills(workspaceRoot, config.DisabledSkillToggles(req.Settings))
	if skillsErr != nil {
		warnings = append(warnings, "skills: "+skillsErr.Error())
	} else {
		skills := SkillInspectionsFromRuntime(inspectedSkills)
		result.Skills = skills
		result.SkillTokenCounts = EstimateSkillTokens(skills)
	}
	agentsPaths, agentsErr := serverbridge.InstalledAgentsPaths(workspaceRoot)
	if agentsErr != nil {
		warnings = append(warnings, "agents: "+agentsErr.Error())
	} else {
		result.AgentsPaths = agentsPaths
		result.AgentTokenCounts = EstimatePathTokens(agentsPaths)
	}
	result.CollectorWarning = strings.Join(warnings, " | ")
	return result
}

func (c Collector) CollectSubscription(ctx context.Context, req appstatus.Request, state auth.State, authStateErr error) appstatus.SubscriptionInfo {
	return CollectSubscriptionStatus(ctx, req, state, authStateErr, c.usageFetcher(), c.usageBaseURL())
}

func (c Collector) usageFetcher() UsagePayloadFetcher {
	if c.UsagePayloadFetcher != nil {
		return c.UsagePayloadFetcher
	}
	return FetchUsagePayload
}

func (c Collector) usageBaseURL() string {
	if baseURL := strings.TrimSpace(c.UsageBaseURL); baseURL != "" {
		return baseURL
	}
	return DefaultUsageBaseURL
}

func (c Collector) timeout() time.Duration {
	if c.RequestTimeout > 0 {
		return c.RequestTimeout
	}
	return 10 * time.Second
}

func (c Collector) gitTimeout() time.Duration {
	if c.GitTimeout > 0 {
		return c.GitTimeout
	}
	return 4 * time.Second
}
