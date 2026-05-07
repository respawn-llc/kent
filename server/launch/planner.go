package launch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"builder/server/auth"
	"builder/server/metadata"
	"builder/server/session"
	"builder/server/sessionpath"
	"builder/shared/clientui"
	"builder/shared/config"
	"builder/shared/serverapi"
	"builder/shared/toolspec"
)

type Mode string

const (
	ModeInteractive Mode = "interactive"
	ModeHeadless    Mode = "headless"

	SubagentSessionSuffix = "subagent"
)

// MetadataExecutionTargetStore is the metadata subset needed to copy a parent
// session execution target into a newly created child session.
type MetadataExecutionTargetStore interface {
	ResolveSessionExecutionTarget(ctx context.Context, sessionID string) (clientui.SessionExecutionTarget, error)
	UpdateSessionExecutionTargetByID(ctx context.Context, sessionID string, workspaceID string, worktreeID string, cwdRelpath string) error
	DeleteSessionRecordByID(ctx context.Context, sessionID string) error
	Close() error
}

// MetadataExecutionTargetStoreOpener opens metadata storage for launch planning.
type MetadataExecutionTargetStoreOpener func(persistenceRoot string) (MetadataExecutionTargetStore, error)

type Planner struct {
	Config              config.App
	ContainerDir        string
	ProjectID           string
	StoreOptions        []session.StoreOption
	ReloadConfig        func() (config.App, error)
	MetadataStoreOpener MetadataExecutionTargetStoreOpener
}

type SessionRequest struct {
	Mode              Mode
	SelectedSessionID string
	ForceNewSession   bool
	ParentSessionID   string
}

type SessionPlan struct {
	Store               *session.Store
	ActiveSettings      config.Settings
	EnabledTools        []toolspec.ID
	ConfiguredModelName string
	SessionName         string
	ModelContractLocked bool
	WorkspaceRoot       string
	Source              config.SourceReport
}

func (p Planner) PlanSession(ctx context.Context, req SessionRequest) (SessionPlan, error) {
	if p.ReloadConfig != nil {
		cfg, err := p.ReloadConfig()
		if err != nil {
			return SessionPlan{}, err
		}
		p.Config = cfg
	}
	store, err := p.openStore(ctx, req)
	if err != nil {
		return SessionPlan{}, err
	}
	if req.Mode == ModeHeadless {
		if err := EnsureSubagentSessionName(store); err != nil {
			return SessionPlan{}, err
		}
	}
	meta := store.Meta()
	active := EffectiveSettings(p.Config.Settings, meta.Locked)
	if meta.Continuation != nil {
		if baseURL := strings.TrimSpace(meta.Continuation.OpenAIBaseURL); baseURL != "" {
			active.OpenAIBaseURL = baseURL
		}
	}
	if err := store.SetContinuationContext(session.ContinuationContext{OpenAIBaseURL: active.OpenAIBaseURL}); err != nil {
		return SessionPlan{}, err
	}
	enabledTools, err := ActiveToolIDsForPlan(active, p.Config.Source, meta.Locked)
	if err != nil {
		return SessionPlan{}, err
	}
	return SessionPlan{
		Store:               store,
		ActiveSettings:      active,
		EnabledTools:        enabledTools,
		ConfiguredModelName: p.Config.Settings.Model,
		SessionName:         meta.Name,
		ModelContractLocked: meta.Locked != nil,
		WorkspaceRoot:       p.Config.WorkspaceRoot,
		Source:              p.Config.Source,
	}, nil
}

func ApplyRunPromptOverrides(plan SessionPlan, overrides serverapi.RunPromptOverrides, authState auth.State) (SessionPlan, []string, error) {
	if !overrides.HasAny() {
		return plan, nil, nil
	}
	var warnings []string
	next := plan
	shouldPersistContinuation := false
	persistContinuation := func() error {
		return next.Store.SetContinuationContext(session.ContinuationContext{OpenAIBaseURL: next.ActiveSettings.OpenAIBaseURL})
	}
	if trimmedRole := strings.TrimSpace(overrides.AgentRole); trimmedRole != "" && config.NormalizeSubagentRole(trimmedRole) == "" {
		return SessionPlan{}, nil, fmt.Errorf("invalid agent role %q", trimmedRole)
	}
	roleName := config.NormalizeSubagentRole(overrides.AgentRole)
	if roleName != "" {
		shouldPersistContinuation = true
		providerBase := cloneSettings(plan.ActiveSettings)
		if value := strings.TrimSpace(overrides.ProviderOverride); value != "" {
			providerBase.ProviderOverride = value
		}
		if value := strings.TrimSpace(overrides.OpenAIBaseURL); value != "" {
			providerBase.OpenAIBaseURL = value
		}
		resolved, warning, err := resolveSubagentSettings(plan.ActiveSettings, providerBase, plan.Source.Sources, roleName, authState, !plan.ModelContractLocked)
		if err != nil {
			return SessionPlan{}, nil, err
		}
		next.ActiveSettings = resolved
		if !plan.ModelContractLocked {
			next.ConfiguredModelName = resolved.Model
		}
		roleSource := sourceReportWithSubagentRoleSources(next.Source, plan.ActiveSettings, roleName)
		enabledTools, err := ActiveToolIDsForPlan(next.ActiveSettings, roleSource, plan.Store.Meta().Locked)
		if err != nil {
			return SessionPlan{}, nil, err
		}
		next.EnabledTools = enabledTools
		next.Source = roleSource
		if strings.TrimSpace(warning) != "" {
			warnings = append(warnings, warning)
		}
	}
	if !overrides.HasConfigOverrides() {
		if shouldPersistContinuation {
			if err := persistContinuation(); err != nil {
				return SessionPlan{}, nil, err
			}
		}
		return next, warnings, nil
	}
	loaded, err := config.Load(plan.WorkspaceRoot, config.LoadOptions{
		Model:               strings.TrimSpace(overrides.Model),
		ProviderOverride:    strings.TrimSpace(overrides.ProviderOverride),
		ThinkingLevel:       strings.TrimSpace(overrides.ThinkingLevel),
		Theme:               strings.TrimSpace(overrides.Theme),
		ModelTimeoutSeconds: overrides.ModelTimeoutSeconds,
		Tools:               strings.TrimSpace(overrides.Tools),
		OpenAIBaseURL:       strings.TrimSpace(overrides.OpenAIBaseURL),
	})
	if err != nil {
		return SessionPlan{}, nil, err
	}
	locked := plan.Store.Meta().Locked
	mergedSource := mergeOverrideSources(next.Source, loaded.Source)
	if strings.TrimSpace(overrides.Model) != "" && !next.ModelContractLocked {
		originalModel := strings.TrimSpace(next.ActiveSettings.Model)
		explicitSources := map[string]string{}
		for key, source := range mergedSource.Sources {
			if strings.TrimSpace(source) == "" || strings.TrimSpace(source) == "default" {
				continue
			}
			explicitSources[key] = source
		}
		next.ActiveSettings.Model = loaded.Settings.Model
		applyDerivedModelContextBudgetOverrides(&next.ActiveSettings, explicitSources, originalModel, true)
		next.ConfiguredModelName = loaded.Settings.Model
	}
	if strings.TrimSpace(overrides.ProviderOverride) != "" {
		next.ActiveSettings.ProviderOverride = loaded.Settings.ProviderOverride
	}
	if strings.TrimSpace(overrides.ThinkingLevel) != "" {
		next.ActiveSettings.ThinkingLevel = loaded.Settings.ThinkingLevel
	}
	if strings.TrimSpace(overrides.Theme) != "" {
		next.ActiveSettings.Theme = loaded.Settings.Theme
	}
	if overrides.ModelTimeoutSeconds > 0 {
		next.ActiveSettings.Timeouts.ModelRequestSeconds = loaded.Settings.Timeouts.ModelRequestSeconds
	}
	if locked == nil {
		if strings.TrimSpace(overrides.Tools) != "" {
			next.ActiveSettings.EnabledTools = cloneEnabledToolSet(loaded.Settings.EnabledTools)
		}
		if strings.TrimSpace(overrides.Tools) != "" || strings.TrimSpace(overrides.Model) != "" {
			enabledTools, err := ActiveToolIDsForPlan(next.ActiveSettings, mergedSource, locked)
			if err != nil {
				return SessionPlan{}, nil, err
			}
			next.EnabledTools = enabledTools
		}
	}
	if strings.TrimSpace(overrides.OpenAIBaseURL) != "" {
		shouldPersistContinuation = true
		next.ActiveSettings.OpenAIBaseURL = loaded.Settings.OpenAIBaseURL
	}
	next.Source = mergedSource
	if shouldPersistContinuation {
		if err := persistContinuation(); err != nil {
			return SessionPlan{}, nil, err
		}
	}
	return next, warnings, nil
}

func mergeOverrideSources(base config.SourceReport, override config.SourceReport) config.SourceReport {
	merged := base
	merged.SettingsPath = override.SettingsPath
	merged.SettingsFileExists = override.SettingsFileExists
	merged.CreatedDefaultConfig = override.CreatedDefaultConfig
	merged.Sources = make(map[string]string, len(base.Sources)+len(override.Sources))
	for key, value := range base.Sources {
		merged.Sources[key] = value
	}
	for key, value := range override.Sources {
		if strings.TrimSpace(value) == "cli" {
			merged.Sources[key] = value
		}
	}
	return merged
}

func sourceReportWithSubagentRoleSources(base config.SourceReport, settings config.Settings, roleName string) config.SourceReport {
	normalizedRole := config.NormalizeSubagentRole(roleName)
	if normalizedRole == "" {
		return base
	}
	role, ok := settings.Subagents[normalizedRole]
	if !ok || len(role.Sources) == 0 {
		return base
	}
	next := base
	next.Sources = cloneStringMap(base.Sources)
	for key := range role.Sources {
		next.Sources[key] = "subagent"
	}
	return next
}

func (p Planner) openStore(ctx context.Context, req SessionRequest) (*session.Store, error) {
	if strings.TrimSpace(p.Config.PersistenceRoot) == "" {
		return nil, errors.New("launch planner persistence root is required")
	}
	if strings.TrimSpace(p.ContainerDir) == "" {
		return nil, errors.New("launch planner container dir is required")
	}
	if strings.TrimSpace(req.SelectedSessionID) != "" {
		return p.openScopedSession(req.SelectedSessionID)
	}
	if req.ForceNewSession || req.Mode == ModeHeadless {
		return p.createSession(ctx, req.ParentSessionID)
	}
	return nil, errors.New("selected_session_id or force_new_session is required")
}

func (p Planner) openScopedSession(sessionID string) (*session.Store, error) {
	realSessionDir, err := sessionpath.ResolveScopedSessionDir(p.ContainerDir, sessionID)
	if err != nil {
		return nil, err
	}
	return session.Open(realSessionDir, p.StoreOptions...)
}

func (p Planner) createSession(ctx context.Context, parentSessionID string) (*session.Store, error) {
	containerName := filepath.Base(p.ContainerDir)
	created, err := session.NewLazy(p.ContainerDir, containerName, p.Config.WorkspaceRoot, p.StoreOptions...)
	if err != nil {
		return nil, err
	}
	parentID := strings.TrimSpace(parentSessionID)
	if parentID != "" {
		if err := p.initializeChildSessionContext(ctx, created, parentID); err != nil {
			return nil, err
		}
	} else {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := created.EnsureDurable(); err != nil {
			return nil, err
		}
	}
	return created, nil
}

func (p Planner) initializeChildSessionContext(ctx context.Context, child *session.Store, parentSessionID string) error {
	if child == nil {
		return errors.New("child session store is required")
	}
	parentID := strings.TrimSpace(parentSessionID)
	if parentID == "" {
		return child.EnsureDurable()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	parent, err := p.openParentSession(parentID)
	if err != nil {
		return err
	}
	if parent != nil {
		if err := session.InitializeChildFromParent(child, parent); err != nil {
			return err
		}
	}
	if parent == nil {
		return child.SetParentSessionID(parentID)
	}
	target, hasTarget, err := p.resolveParentExecutionTarget(ctx, parentID)
	if err != nil {
		return err
	}
	if err := child.EnsureDurable(); err != nil {
		return err
	}
	if !hasTarget {
		return nil
	}
	if err := p.updateChildExecutionTarget(ctx, child.Meta().SessionID, target); err != nil {
		return errors.Join(err, p.rollbackChildSession(child))
	}
	return nil
}

func (p Planner) openParentSession(parentSessionID string) (*session.Store, error) {
	parent, err := p.openScopedSession(parentSessionID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return parent, nil
}

func (p Planner) openMetadataStore() (MetadataExecutionTargetStore, error) {
	if p.MetadataStoreOpener != nil {
		return p.MetadataStoreOpener(p.Config.PersistenceRoot)
	}
	return metadata.Open(p.Config.PersistenceRoot)
}

func (p Planner) resolveParentExecutionTarget(ctx context.Context, parentSessionID string) (clientui.SessionExecutionTarget, bool, error) {
	if err := ctx.Err(); err != nil {
		return clientui.SessionExecutionTarget{}, false, err
	}
	store, err := p.openMetadataStore()
	if err != nil {
		return clientui.SessionExecutionTarget{}, false, err
	}
	defer func() { _ = store.Close() }()
	target, err := store.ResolveSessionExecutionTarget(ctx, parentSessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, session.ErrSessionNotFound) {
			return clientui.SessionExecutionTarget{}, false, nil
		}
		return clientui.SessionExecutionTarget{}, false, err
	}
	return target, true, nil
}

func (p Planner) updateChildExecutionTarget(ctx context.Context, childSessionID string, target clientui.SessionExecutionTarget) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store, err := p.openMetadataStore()
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	return store.UpdateSessionExecutionTargetByID(ctx, childSessionID, target.WorkspaceID, target.WorktreeID, target.CwdRelpath)
}

func (p Planner) rollbackChildSession(child *session.Store) error {
	if child == nil {
		return nil
	}
	childMeta := child.Meta()
	rollbackCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var rollbackErrs []error
	if store, err := p.openMetadataStore(); err == nil {
		if err := store.DeleteSessionRecordByID(rollbackCtx, childMeta.SessionID); err != nil {
			rollbackErrs = append(rollbackErrs, err)
		}
		if err := store.Close(); err != nil {
			rollbackErrs = append(rollbackErrs, err)
		}
	} else {
		rollbackErrs = append(rollbackErrs, err)
	}
	if err := child.RemoveDurable(); err != nil {
		rollbackErrs = append(rollbackErrs, err)
	}
	return errors.Join(rollbackErrs...)
}

func EnsureSubagentSessionName(store *session.Store) error {
	if store == nil {
		return errors.New("session store is required")
	}
	meta := store.Meta()
	if strings.TrimSpace(meta.Name) != "" {
		return nil
	}
	name := strings.TrimSpace(meta.SessionID + " " + SubagentSessionSuffix)
	if name == "" {
		return nil
	}
	return store.SetName(name)
}

func EffectiveSettings(base config.Settings, locked *session.LockedContract) config.Settings {
	out := base
	if locked == nil {
		return out
	}
	if strings.TrimSpace(locked.Model) != "" {
		out.Model = locked.Model
	}
	return out
}

func ActiveToolIDs(settings config.Settings, source config.SourceReport, locked *session.LockedContract) ([]toolspec.ID, error) {
	return ActiveToolIDsForPlan(settings, source, locked)
}

func ActiveToolIDsForPlan(settings config.Settings, source config.SourceReport, locked *session.LockedContract) ([]toolspec.ID, error) {
	if locked != nil {
		ids := make([]toolspec.ID, 0, len(locked.EnabledTools))
		for _, raw := range locked.EnabledTools {
			if id, ok := toolspec.ParseID(raw); ok {
				ids = append(ids, id)
			}
		}
		return DedupeSortToolIDs(ids), nil
	}
	enabled := cloneEnabledToolSet(settings.EnabledTools)
	if bothEditToolSourcesDefault(source) {
		if prefersPatchTool(settings) {
			enabled[toolspec.ToolPatch] = true
			enabled[toolspec.ToolEdit] = false
		} else {
			enabled[toolspec.ToolPatch] = false
			enabled[toolspec.ToolEdit] = true
		}
	}
	if enabled[toolspec.ToolPatch] && enabled[toolspec.ToolEdit] {
		return nil, fmt.Errorf("tools.patch and tools.edit cannot both be enabled; set one to false")
	}
	return DedupeSortToolIDs(enabledToolIDs(enabled)), nil
}

func bothEditToolSourcesDefault(source config.SourceReport) bool {
	return strings.TrimSpace(source.Sources["tools.patch"]) == "default" && strings.TrimSpace(source.Sources["tools.edit"]) == "default"
}

func prefersPatchTool(settings config.Settings) bool {
	if settings.ProviderCapabilities.IsOpenAIFirstParty {
		return true
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(settings.Model)), "gpt-")
}

func enabledToolIDs(enabled map[toolspec.ID]bool) []toolspec.ID {
	ids := make([]toolspec.ID, 0, len(enabled))
	for _, id := range toolspec.CatalogIDs() {
		if enabled[id] {
			ids = append(ids, id)
		}
	}
	return ids
}

func cloneEnabledToolSet(in map[toolspec.ID]bool) map[toolspec.ID]bool {
	if len(in) == 0 {
		return map[toolspec.ID]bool{}
	}
	out := make(map[toolspec.ID]bool, len(in))
	for id, enabled := range in {
		out[id] = enabled
	}
	return out
}

func DedupeSortToolIDs(ids []toolspec.ID) []toolspec.ID {
	seen := map[toolspec.ID]bool{}
	out := make([]toolspec.ID, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
