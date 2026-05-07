package config

import (
	"net"
	"path/filepath"
	"strconv"

	"builder/shared/protocol"
	"builder/shared/toolspec"
)

const (
	DefaultAppName       = "builder"
	DefaultPersistence   = "~/.builder"
	sessionsDirName      = "sessions"
	databaseDirName      = "db"
	workspaceIndexName   = "workspaces.json"
	globalAuthConfigName = "auth.json"
)

type CompactionMode string
type BGShellsOutputMode string
type CacheWarningMode string
type ModelVerbosity string
type ShellPostprocessingMode string

type WorktreeSettings struct {
	BaseDir     string
	SetupScript string
}

const (
	CompactionModeNative CompactionMode = "native"
	CompactionModeLocal  CompactionMode = "local"
	CompactionModeNone   CompactionMode = "none"

	BGShellsOutputDefault BGShellsOutputMode = "default"
	BGShellsOutputVerbose BGShellsOutputMode = "verbose"
	BGShellsOutputConcise BGShellsOutputMode = "concise"

	CacheWarningModeOff     CacheWarningMode = "off"
	CacheWarningModeDefault CacheWarningMode = "default"
	CacheWarningModeVerbose CacheWarningMode = "verbose"

	ModelVerbosityLow    ModelVerbosity = "low"
	ModelVerbosityMedium ModelVerbosity = "medium"
	ModelVerbosityHigh   ModelVerbosity = "high"

	ShellPostprocessingModeNone    ShellPostprocessingMode = "none"
	ShellPostprocessingModeBuiltin ShellPostprocessingMode = "builtin"
	ShellPostprocessingModeUser    ShellPostprocessingMode = "user"
	ShellPostprocessingModeAll     ShellPostprocessingMode = "all"
)

type LoadOptions struct {
	Model               string
	ProviderOverride    string
	ThinkingLevel       string
	Theme               string
	ModelTimeoutSeconds int
	Tools               string
	OpenAIBaseURL       string
	ConfigRoot          string
}

type Timeouts struct {
	ModelRequestSeconds int
}

type ShellSettings struct {
	PostprocessingMode ShellPostprocessingMode
	PostprocessHook    string
}

type SubagentRole struct {
	Settings Settings
	Sources  map[string]string
}

type SystemPromptFileScope string

const (
	SystemPromptFileScopeHomeConfig      SystemPromptFileScope = "home_config"
	SystemPromptFileScopeWorkspaceConfig SystemPromptFileScope = "workspace_config"
	SystemPromptFileScopeSubagent        SystemPromptFileScope = "subagent"
)

type SystemPromptFile struct {
	Path  string
	Scope SystemPromptFileScope
}

type Settings struct {
	Model                            string
	ThinkingLevel                    string
	ModelVerbosity                   ModelVerbosity
	SystemPromptFile                 string
	SystemPromptFiles                []SystemPromptFile
	ModelCapabilities                ModelCapabilitiesOverride
	Theme                            string
	NotificationMethod               string
	ToolPreambles                    bool
	PriorityRequestMode              bool
	Debug                            bool
	ServerHost                       string
	ServerPort                       int
	WebSearch                        string
	ProviderOverride                 string
	OpenAIBaseURL                    string
	ProviderCapabilities             ProviderCapabilitiesOverride
	Store                            bool
	AllowNonCwdEdits                 bool
	ModelContextWindow               int
	ContextCompactionThresholdTokens int
	PreSubmitCompactionLeadTokens    int
	MinimumExecToBgSeconds           int
	CompactionMode                   CompactionMode
	EnabledTools                     map[toolspec.ID]bool
	SkillToggles                     map[string]bool
	Timeouts                         Timeouts
	ShellOutputMaxChars              int
	BGShellsOutput                   BGShellsOutputMode
	Shell                            ShellSettings
	CacheWarningMode                 CacheWarningMode
	Worktrees                        WorktreeSettings
	Reviewer                         ReviewerSettings
	Subagents                        map[string]SubagentRole
}

type ModelCapabilitiesOverride struct {
	SupportsReasoningEffort bool
	SupportsVisionInputs    bool
}

type ProviderCapabilitiesOverride struct {
	ProviderID                     string
	SupportsResponsesAPI           bool
	SupportsResponsesCompact       bool
	SupportsRequestInputTokenCount bool
	SupportsPromptCacheKey         bool
	SupportsNativeWebSearch        bool
	SupportsReasoningEncrypted     bool
	SupportsServerSideContextEdit  bool
	IsOpenAIFirstParty             bool
}

type ReviewerSettings struct {
	Frequency            string
	Model                string
	ThinkingLevel        string
	ModelVerbosity       ModelVerbosity
	ProviderOverride     string
	OpenAIBaseURL        string
	ModelCapabilities    ModelCapabilitiesOverride
	ProviderCapabilities ProviderCapabilitiesOverride
	ModelContextWindow   int
	Auth                 string
	SystemPromptFile     string
	TimeoutSeconds       int
	VerboseOutput        bool
}

type ReviewerProviderSettings struct {
	ProviderOverride string
	OpenAIBaseURL    string
}

type SourceReport struct {
	SettingsPath                  string
	SettingsFileExists            bool
	CreatedDefaultConfig          bool
	HomeSettingsPath              string
	HomeSettingsFileExists        bool
	WorkspaceSettingsPath         string
	WorkspaceSettingsFileExists   bool
	WorkspaceSettingsLayerEnabled bool
	Sources                       map[string]string
}

type App struct {
	AppName         string
	WorkspaceRoot   string
	PersistenceRoot string
	Settings        Settings
	Source          SourceReport
}

type settingsFile map[string]any

func EnabledToolIDs(v Settings) []toolspec.ID {
	ids := make([]toolspec.ID, 0, len(v.EnabledTools))
	for _, id := range toolspec.CatalogIDs() {
		if v.EnabledTools[id] {
			ids = append(ids, id)
		}
	}
	return ids
}

func SessionsRoot(cfg App) string {
	return filepath.Join(cfg.PersistenceRoot, sessionsDirName)
}

func ProjectsRoot(cfg App) string {
	return filepath.Join(cfg.PersistenceRoot, "projects")
}

func ProjectRoot(cfg App, projectID string) string {
	return filepath.Join(ProjectsRoot(cfg), projectID)
}

func ProjectSessionsRoot(cfg App, projectID string) string {
	return filepath.Join(ProjectRoot(cfg, projectID), sessionsDirName)
}

func ProjectSessionDir(cfg App, projectID string, sessionID string) string {
	return filepath.Join(ProjectSessionsRoot(cfg, projectID), sessionID)
}

func DatabaseRoot(cfg App) string {
	return filepath.Join(cfg.PersistenceRoot, databaseDirName)
}

func MainDatabasePath(cfg App) string {
	return filepath.Join(DatabaseRoot(cfg), "main.sqlite3")
}

func GlobalAuthConfigPath(cfg App) string {
	return filepath.Join(cfg.PersistenceRoot, globalAuthConfigName)
}

func MigrationBackupsRoot(cfg App) string {
	return filepath.Join(cfg.PersistenceRoot, "migration-backups")
}

func MigrationsRoot(cfg App) string {
	return filepath.Join(cfg.PersistenceRoot, "migrations")
}

func WorktreesRoot(cfg App) string {
	return cfg.Settings.Worktrees.BaseDir
}

func ServerListenAddress(cfg App) string {
	return net.JoinHostPort(cfg.Settings.ServerHost, strconv.Itoa(cfg.Settings.ServerPort))
}

func ServerRPCURL(cfg App) string {
	return "ws://" + ServerListenAddress(cfg) + protocol.RPCPath
}

func ServerHTTPBaseURL(cfg App) string {
	return "http://" + ServerListenAddress(cfg)
}
