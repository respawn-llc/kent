package launch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"core/server/chatcontext"
	"core/server/llm"
	"core/server/metadata"
	"core/server/session"
	"core/server/workflow"
	"core/shared/clientui"
	"core/shared/config"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/textutil"
	"core/shared/toolspec"
)

type Mode string

const (
	ModeInteractive Mode = "interactive"
	ModeHeadless    Mode = "headless"

	SubagentSessionSuffix = "subagent"
)

type SessionExecutionTargetResolver interface {
	ResolveSessionExecutionTarget(ctx context.Context, sessionID string) (*worktreepb.SessionExecutionTarget, error)
}

type SessionProjectResolver interface {
	ResolveSessionProjectID(ctx context.Context, sessionID string) (string, error)
}

type SessionManagedWorktreeRootsResolver interface {
	ListManagedWorktreeRoots(ctx context.Context) ([]string, error)
}

// MetadataExecutionTargetStore is the metadata subset needed to copy a parent
// session execution target into a newly created child session.
type MetadataExecutionTargetStore interface {
	SessionExecutionTargetResolver
	UpdateSessionExecutionTarget(ctx context.Context, update metadata.SessionExecutionTargetUpdate) error
	Close() error
}

// MetadataExecutionTargetStoreOpener opens metadata storage for launch planning.
type MetadataExecutionTargetStoreOpener func(persistenceRoot string) (MetadataExecutionTargetStore, error)

type Planner struct {
	Config               config.App
	ContainerDir         string
	StoreOptions         []session.StoreOption
	ReloadConfig         func() (config.App, error)
	PersistedSessions    session.PersistedSessionResolver
	ExecutionTargets     SessionExecutionTargetResolver
	SessionProjects      SessionProjectResolver
	ManagedWorktreeRoots SessionManagedWorktreeRootsResolver
	MetadataStoreOpener  MetadataExecutionTargetStoreOpener
}

type SessionRequest struct {
	Mode                                Mode
	Intent                              serverapi.SessionLaunchIntent
	SkipContinuationAgentRoleValidation bool
	PreparedPromptFacingTarget          *PreparedBaseTarget
	InitialChat                         *session.ChatDraftState
}

type SessionPlan struct {
	Descriptor                          session.SessionDescriptor
	ActiveSettings                      config.Settings
	BaseSettings                        config.Settings
	EnabledTools                        []toolspec.ID
	ConfiguredModelName                 string
	SessionName                         *string
	FirstPromptPreview                  string
	Goal                                *session.GoalState
	WorktreeReminder                    *session.WorktreeReminderState
	Continuation                        *session.ContinuationContext
	Locked                              *session.LockedContract
	ModelContractLocked                 bool
	SkipContinuationAgentRoleValidation bool
	WorkspaceRoot                       string
	ExecutionTarget                     *worktreepb.SessionExecutionTarget
	ProjectID                           string
	ManagedWorktreeRoots                []string
	Source                              config.SourceReport
	BaseSource                          config.SourceReport
	QuestionsEnabled                    bool
	AutoCompactionEnabled               bool
	ThinkingOverrideExplicit            bool
	ActivationAgentSelection            *session.ChatSettingsState
	ExplicitToolSelection               *config.ToolSelection
	RequiredTools                       []toolspec.ID
}

// ApplyContextPolicy resolves Context policy only after the plan's final Agent
// role, settings overrides, and persisted Session continuity are known.
func ApplyContextPolicy(plan SessionPlan, capabilities llm.ProviderCapabilities) SessionPlan {
	plan.ActiveSettings = chatcontext.ApplyPolicy(
		plan.ActiveSettings,
		chatcontext.ResolvePolicy(plan.ActiveSettings, capabilities, plan.Locked),
	)
	return plan
}

func sessionPlanWithMeta(plan SessionPlan, meta session.Meta, containerDir string) SessionPlan {
	sessionID, err := runtimeids.ParseSessionID(meta.SessionID)
	if err != nil {
		panic(fmt.Sprintf("session plan snapshot has invalid session id %q: %v", meta.SessionID, err))
	}
	descriptor, err := session.NewScopedOpenSessionDescriptor(sessionID, containerDir)
	if err != nil {
		panic(fmt.Sprintf("session plan snapshot cannot scope session %q to %q: %v", meta.SessionID, containerDir, err))
	}
	plan.Descriptor = descriptor
	plan.ManagedWorktreeRoots = append([]string(nil), plan.ManagedWorktreeRoots...)
	plan.FirstPromptPreview = meta.FirstPromptPreview
	plan.Goal = meta.Goal
	plan.WorktreeReminder = meta.WorktreeReminder
	plan.Continuation = meta.Continuation
	plan.Locked = meta.Locked
	plan.ModelContractLocked = meta.Locked != nil
	return plan
}

type RunPromptOverrideOptions struct {
	AgentSelectionPersisted bool
	RequiredTools           []toolspec.ID
	WorkflowThinking        workflow.ThinkingMutation
}

func optionalSessionName(name string) (*string, error) {
	if name == "" {
		return nil, nil
	}
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("session name cannot be blank")
	}
	return &name, nil
}

// PreparedRunPromptOverrides is the immutable, snapshot-bound portion of a
// RunPrompt override. Session launch prepares it before any new session is
// materialized; applying it later must not reload config or look up a role.
type PreparedRunPromptOverrides struct {
	OverrideConfig       config.App
	AgentRole            serverapi.RunPromptAgentRoleOverride
	BaseTarget           *PreparedBaseTarget
	NamedTarget          *PreparedSubagentTarget
	ProviderCapabilities *llm.ProviderCapabilities
}

func (p PreparedRunPromptOverrides) PromptFacingTarget() *PreparedBaseTarget {
	if p.NamedTarget != nil {
		return &PreparedBaseTarget{
			Settings:     p.NamedTarget.Settings,
			Source:       p.NamedTarget.Source,
			EnabledTools: p.NamedTarget.EnabledTools,
		}
	}
	return p.BaseTarget
}

type PreparedBaseTarget struct {
	Settings     config.Settings
	Source       config.SourceReport
	EnabledTools []toolspec.ID
}

// RunPromptPreparationContext carries a selected session's immutable,
// prompt-facing contract into pre-materialization target preparation.
type RunPromptPreparationContext struct {
	Mode                            Mode
	ModelLock                       *session.LockedContract
	ToolLock                        *session.LockedContract
	OmittedTarget                   *PreparedBaseTarget
	SkipProviderReadinessValidation bool
}

type PreparedSubagentTarget struct {
	Selector     string
	Settings     config.Settings
	Source       config.SourceReport
	EnabledTools []toolspec.ID
	Warning      *string
}

type preparedSubagentIdentity struct {
	Selector   string
	Role       config.SubagentRole
	ProviderID string
}

type PromptFacingSnapshotResolution struct {
	Settings      config.Settings
	Source        config.SourceReport
	ActiveToolIDs []toolspec.ID
	WebSearchMode string
}

func ResolvePromptFacingSnapshotConfig(app config.App, store *session.Store, skipContinuationAgentRoleValidation bool) (PromptFacingSnapshotResolution, error) {
	plan, err := ResolvePromptFacingSnapshotPlan(app, store, skipContinuationAgentRoleValidation)
	if err != nil {
		return PromptFacingSnapshotResolution{}, err
	}
	return PromptFacingSnapshotResolution{
		Settings:      plan.ActiveSettings,
		Source:        plan.Source,
		ActiveToolIDs: plan.EnabledTools,
		WebSearchMode: strings.TrimSpace(plan.ActiveSettings.WebSearch),
	}, nil
}

type ReadOnlySessionContextSettings struct {
	Settings              config.Settings
	AutoCompactionEnabled bool
	QuestionsEnabled      bool
}

// ResolveReadOnlySessionContextSettings projects current persisted Agent-role
// and Chat settings from a bounded Meta snapshot.
func ResolveReadOnlySessionContextSettings(app config.App, meta session.Meta, skipContinuationAgentRoleValidation bool) (ReadOnlySessionContextSettings, error) {
	active, _, chatSettings, err := resolveReadOnlySessionContextSettings(app, meta, skipContinuationAgentRoleValidation)
	if err != nil {
		return ReadOnlySessionContextSettings{}, err
	}
	return ReadOnlySessionContextSettings{
		Settings:              active,
		AutoCompactionEnabled: chatSettings.AutoCompaction,
		QuestionsEnabled:      chatSettings.Questions,
	}, nil
}

func resolveReadOnlySessionContextSettings(
	app config.App,
	meta session.Meta,
	skipContinuationAgentRoleValidation bool,
) (config.Settings, config.SourceReport, session.ChatSettings, error) {
	var selectionErr error
	app, selectionErr = ApplyRetainedToolSelection(app, meta)
	if selectionErr != nil {
		return config.Settings{}, config.SourceReport{}, session.ChatSettings{}, selectionErr
	}
	baseActive := EffectiveSettings(app.Settings, meta.Locked)
	active, source := baseActive, app.Source
	if meta.Continuation != nil {
		var err error
		active, source, err = applyPersistedSubagentRoleSettings(baseActive, source, meta.Continuation.AgentRole, meta.Locked == nil, !skipContinuationAgentRoleValidation)
		if err != nil {
			return config.Settings{}, config.SourceReport{}, session.ChatSettings{}, err
		}
	}
	if err := projectSessionConnection(&active, source, meta); err != nil {
		return config.Settings{}, config.SourceReport{}, session.ChatSettings{}, err
	}
	active, chatSettings, err := applySessionChatSettings(meta, active)
	if err != nil {
		return config.Settings{}, config.SourceReport{}, session.ChatSettings{}, err
	}
	return active, source, chatSettings, nil
}

// ResolvePromptFacingSnapshotPlan reconstructs the request-facing session plan
// from a persisted store without creating or selecting a session. It is shared
// by diagnostic paths that need the same settings, source, tools, and
// locked-contract semantics as launch planning.
func ResolvePromptFacingSnapshotPlan(app config.App, store *session.Store, skipContinuationAgentRoleValidation bool) (SessionPlan, error) {
	plan, err := resolvePromptFacingSnapshotPlan(app, store, skipContinuationAgentRoleValidation)
	if err != nil {
		return SessionPlan{}, err
	}
	meta := store.Meta()
	if meta.Locked != nil && (!meta.Locked.HasEnabledTools || strings.TrimSpace(meta.Locked.WebSearchMode) == "") {
		backfill, backfillErr := store.BackfillLockedRequestShape(session.LockedRequestShapeBackfill{
			EnabledTools:    toolspec.IDStrings(plan.EnabledTools),
			HasEnabledTools: true,
			WebSearchMode:   strings.TrimSpace(plan.ActiveSettings.WebSearch),
		})
		if backfillErr != nil && !backfill.Committed {
			return SessionPlan{}, backfillErr
		}
	}
	return sessionPlanWithMeta(plan, store.Meta(), filepath.Dir(store.Dir())), nil
}

func resolvePromptFacingSnapshotPlan(app config.App, store *session.Store, skipContinuationAgentRoleValidation bool) (SessionPlan, error) {
	if store == nil {
		return SessionPlan{}, errors.New("session store is required")
	}
	meta := store.Meta()
	baseActive := EffectiveSettings(app.Settings, meta.Locked)
	baseSource := app.Source
	active, source, chatSettings, err := resolveReadOnlySessionContextSettings(app, meta, skipContinuationAgentRoleValidation)
	if err != nil {
		return SessionPlan{}, err
	}
	enabledTools, err := ActiveToolIDsForPlan(active, source, meta.Locked)
	if err != nil {
		return SessionPlan{}, err
	}
	configuredModelName := app.Settings.Model
	if meta.Locked == nil {
		configuredModelName = active.Model
	}
	sessionName, err := optionalSessionName(meta.Name)
	if err != nil {
		return SessionPlan{}, err
	}
	return sessionPlanWithMeta(SessionPlan{
		ActiveSettings:                      active,
		BaseSettings:                        baseActive,
		EnabledTools:                        enabledTools,
		ConfiguredModelName:                 configuredModelName,
		SessionName:                         sessionName,
		ModelContractLocked:                 meta.Locked != nil,
		SkipContinuationAgentRoleValidation: skipContinuationAgentRoleValidation,
		WorkspaceRoot:                       app.WorkspaceRoot,
		Source:                              source,
		BaseSource:                          baseSource,
		QuestionsEnabled:                    chatSettings.Questions,
		AutoCompactionEnabled:               chatSettings.AutoCompaction,
	}, meta, filepath.Dir(store.Dir())), nil
}

func (p Planner) PlanSession(ctx context.Context, req SessionRequest) (SessionPlan, error) {
	if err := validateInitialChatSessionRequest(req); err != nil {
		return SessionPlan{}, err
	}
	if p.ReloadConfig != nil {
		cfg, err := p.ReloadConfig()
		if err != nil {
			return SessionPlan{}, err
		}
		p.Config = cfg
	}
	if req.Intent.Kind() == serverapi.SessionLaunchIntentOpenExisting {
		sessionID, _ := req.Intent.SessionID()
		record, err := session.ResolveScopedPersistedSessionRecord(
			ctx,
			p.PersistedSessions,
			p.ContainerDir,
			sessionID.String(),
		)
		if err != nil {
			return SessionPlan{}, err
		}
		return p.planSession(ctx, req, *record.Meta, nil)
	}
	store, err := p.openStore(ctx, req)
	if err != nil {
		return SessionPlan{}, err
	}
	return p.planSessionWithStore(ctx, req, store)
}

// PlanNewSessionWithPreparedOverrides creates and plans a new Session, then
// applies its prepared overrides through the same Store.
func (p Planner) PlanNewSessionWithPreparedOverrides(
	ctx context.Context,
	req SessionRequest,
	overrides serverapi.RunPromptOverrides,
	prepared PreparedRunPromptOverrides,
) (SessionPlan, []string, error) {
	if err := validateInitialChatSessionRequest(req); err != nil {
		return SessionPlan{}, nil, err
	}
	if req.Intent.Kind() != serverapi.SessionLaunchIntentCreateNew {
		return SessionPlan{}, nil, errors.New("new-session planning requires a create-new intent")
	}
	if p.ReloadConfig != nil {
		cfg, err := p.ReloadConfig()
		if err != nil {
			return SessionPlan{}, nil, err
		}
		p.Config = cfg
	}
	store, err := p.openStore(ctx, req)
	if err != nil {
		return SessionPlan{}, nil, err
	}
	plan, err := p.planSessionWithStore(ctx, req, store)
	if err != nil {
		return SessionPlan{}, nil, err
	}
	return p.ApplyPreparedRunPromptOverridesWithStore(plan, store, overrides, prepared, RunPromptOverrideOptions{})
}

func (p Planner) planSessionWithStore(ctx context.Context, req SessionRequest, store *session.Store) (SessionPlan, error) {
	if store == nil {
		return SessionPlan{}, errors.New("session store is required")
	}
	return p.planSession(ctx, req, store.Meta(), store)
}

func (p Planner) PlanPersistedSessionWithPreparedOverrides(ctx context.Context, req SessionRequest, meta session.Meta, overrides serverapi.RunPromptOverrides, prepared PreparedRunPromptOverrides, options RunPromptOverrideOptions) (SessionPlan, []string, error) {
	if err := validateInitialChatSessionRequest(req); err != nil {
		return SessionPlan{}, nil, err
	}
	plan, err := p.planSession(ctx, req, meta, nil)
	if err != nil {
		return SessionPlan{}, nil, err
	}
	return p.applyPreparedRunPromptOverrides(plan, meta, nil, overrides, prepared, options)
}

func validateInitialChatSessionRequest(req SessionRequest) error {
	if req.InitialChat == nil {
		return nil
	}
	return ValidateInitialChatCreationTarget(req.Mode, req.Intent)
}

func ValidateInitialChatCreationTarget(mode Mode, intent serverapi.SessionLaunchIntent) error {
	if mode != ModeInteractive {
		return errors.New("initial Chat creation requires interactive Session launch")
	}
	if intent.Kind() != serverapi.SessionLaunchIntentCreateNew {
		return errors.New("initial Chat creation requires a new Session")
	}
	origin, ok := intent.CreateOrigin()
	if !ok || origin.Kind() != serverapi.SessionCreateOriginIndependent {
		return errors.New("initial Chat creation requires an independent Session")
	}
	return nil
}

func (p Planner) planSession(ctx context.Context, req SessionRequest, meta session.Meta, store *session.Store) (SessionPlan, error) {
	return p.planSessionWithExecutionContext(ctx, req, meta, store, nil)
}

func (p Planner) planSessionWithExecutionContext(ctx context.Context, req SessionRequest, meta session.Meta, store *session.Store, preparedContext *PreparedExecutionContext) (SessionPlan, error) {
	explicitTools := config.ExplicitToolSelection(p.Config.Settings, p.Config.Source.Sources)
	var selectionErr error
	p.Config, selectionErr = ApplyRetainedToolSelection(p.Config, meta)
	if selectionErr != nil {
		return SessionPlan{}, selectionErr
	}
	if store == nil && preparedContext == nil {
		if req.Intent.Kind() != serverapi.SessionLaunchIntentOpenExisting {
			return SessionPlan{}, errors.New("persisted session planning requires an existing-session intent")
		}
		sessionID, _ := req.Intent.SessionID()
		if meta.SessionID != sessionID.String() {
			return SessionPlan{}, fmt.Errorf(
				"persisted session %q does not match requested session %q",
				meta.SessionID,
				sessionID,
			)
		}
	}
	if req.Mode == ModeHeadless && (store != nil || preparedContext != nil) {
		if store != nil {
			if err := EnsureSubagentSessionName(store); err != nil {
				return SessionPlan{}, err
			}
			meta = store.Meta()
		} else if strings.TrimSpace(meta.Name) == "" {
			meta.Name = subagentSessionName(meta)
		}
	}
	baseActive := EffectiveSettings(p.Config.Settings, meta.Locked)
	baseSource := p.Config.Source
	var continuationAgentRole *string
	if meta.Continuation != nil {
		continuationAgentRole = cloneContinuationRole(meta.Continuation.AgentRole)
	}
	active, source := baseActive, baseSource
	enabledTools := []toolspec.ID(nil)
	var err error
	if req.PreparedPromptFacingTarget != nil {
		active = cloneSettings(req.PreparedPromptFacingTarget.Settings)
		source = cloneSourceReport(req.PreparedPromptFacingTarget.Source)
		enabledTools = append([]toolspec.ID(nil), req.PreparedPromptFacingTarget.EnabledTools...)
	} else if meta.Continuation != nil {
		active, source, err = applyPersistedSubagentRoleSettings(baseActive, baseSource, continuationAgentRole, meta.Locked == nil, !req.SkipContinuationAgentRoleValidation)
		if err != nil {
			return SessionPlan{}, err
		}
	}
	if meta.ConnectionID != nil {
		if err := projectSessionConnection(&active, source, meta); err != nil {
			return SessionPlan{}, err
		}
	}
	continuation := session.ContinuationContext{}
	if meta.Continuation != nil {
		continuation.AgentRole = continuationAgentRole
	}
	if store != nil {
		if err := store.SetContinuationContext(continuation); err != nil {
			return SessionPlan{}, err
		}
		meta = store.Meta()
	} else if preparedContext != nil {
		meta.Continuation, err = session.NormalizeContinuationContext(continuation)
		if err != nil {
			return SessionPlan{}, err
		}
	}
	if req.PreparedPromptFacingTarget == nil {
		enabledTools, err = ActiveToolIDsForPlan(active, source, meta.Locked)
		if err != nil {
			return SessionPlan{}, err
		}
	}
	active, chatSettings, err := applySessionChatSettings(meta, active)
	if err != nil {
		return SessionPlan{}, err
	}
	if meta.Locked != nil &&
		(!meta.Locked.HasEnabledTools || strings.TrimSpace(meta.Locked.WebSearchMode) == "") &&
		store != nil {
		backfill, backfillErr := store.BackfillLockedRequestShape(session.LockedRequestShapeBackfill{
			EnabledTools:    toolspec.IDStrings(enabledTools),
			HasEnabledTools: true,
			WebSearchMode:   strings.TrimSpace(active.WebSearch),
		})
		if backfillErr != nil && !backfill.Committed {
			return SessionPlan{}, backfillErr
		}
		if backfill.Committed && backfill.Locked != nil {
			meta.Locked = backfill.Locked
		}
	}
	configuredModelName := p.Config.Settings.Model
	if meta.Locked == nil {
		configuredModelName = active.Model
	}
	sessionName, err := optionalSessionName(meta.Name)
	if err != nil {
		return SessionPlan{}, err
	}
	executionContext, err := p.resolveSessionPlanExecutionContext(ctx, meta.SessionID, preparedContext)
	if err != nil {
		return SessionPlan{}, err
	}
	return sessionPlanWithMeta(SessionPlan{
		ActiveSettings:                      active,
		BaseSettings:                        baseActive,
		EnabledTools:                        enabledTools,
		ConfiguredModelName:                 configuredModelName,
		SessionName:                         sessionName,
		ModelContractLocked:                 meta.Locked != nil,
		SkipContinuationAgentRoleValidation: req.SkipContinuationAgentRoleValidation,
		WorkspaceRoot:                       p.Config.WorkspaceRoot,
		ExplicitToolSelection:               explicitTools,
		ExecutionTarget:                     executionContext.ExecutionTarget,
		ProjectID:                           executionContext.ProjectID,
		ManagedWorktreeRoots:                append([]string(nil), executionContext.ManagedWorktreeRoots...),
		Source:                              source,
		BaseSource:                          baseSource,
		QuestionsEnabled:                    chatSettings.Questions,
		AutoCompactionEnabled:               chatSettings.AutoCompaction,
	}, meta, p.ContainerDir), nil
}

func (p Planner) resolvePlannedExecutionTarget(ctx context.Context, sessionID string) (*worktreepb.SessionExecutionTarget, error) {
	resolver := p.ExecutionTargets
	if resolver == nil {
		resolver, _ = p.PersistedSessions.(SessionExecutionTargetResolver)
	}
	if resolver == nil {
		return &worktreepb.SessionExecutionTarget{}, nil
	}
	target, err := resolver.ResolveSessionExecutionTarget(ctx, sessionID)
	if err != nil {
		return &worktreepb.SessionExecutionTarget{}, err
	}
	target = clientui.NormalizeSessionExecutionTarget(target)
	if clientui.SessionExecutionTargetIsZero(target) {
		return &worktreepb.SessionExecutionTarget{}, fmt.Errorf("session %q execution target is empty", sessionID)
	}
	return target, nil
}

func applyPersistedSubagentRoleSettings(base config.Settings, source config.SourceReport, roleName *string, allowModelOverride bool, validate bool) (config.Settings, config.SourceReport, error) {
	if roleName == nil {
		return base, source, nil
	}
	lookup := config.LookupSubagentRole(base, *roleName)
	if lookup.Status == config.SubagentRoleLookupInvalid {
		return base, source, nil
	}
	if lookup.Status == config.SubagentRoleLookupMissing {
		return base, source, nil
	}
	providerSettings := cloneSettings(base)
	providerSettings, err := config.OverlaySubagentRoleProviderSettings(config.App{Settings: providerSettings, Source: source}, lookup.Role)
	if err != nil {
		return config.Settings{}, config.SourceReport{}, err
	}
	providerID, err := persistedRoleProviderID(providerSettings)
	if err != nil {
		return config.Settings{}, config.SourceReport{}, err
	}
	resolved, effectiveSource, _, err := resolveSubagentSettingsWithProviderID(base, source, *lookup.NormalizedSelector, providerID, allowModelOverride, validate)
	if err != nil {
		return config.Settings{}, config.SourceReport{}, err
	}
	return resolved, effectiveSource, nil
}

func persistedRoleProviderID(settings config.Settings) (string, error) {
	capabilities, err := llm.ResolveRuntimeProviderCapabilities(settings)
	if err != nil {
		return "", err
	}
	return capabilities.ProviderID, nil
}

// ApplyRunPromptOverridesWithStore applies overrides through an already-admitted
// Store. It never reconstructs a Store from the plan.
func (p Planner) ApplyRunPromptOverridesWithStore(plan SessionPlan, store *session.Store, overrides serverapi.RunPromptOverrides, options RunPromptOverrideOptions) (SessionPlan, []string, error) {
	if store == nil {
		return SessionPlan{}, nil, errors.New("session store is required")
	}
	next, warnings, err := p.applyRunPromptOverridesWithBudgetApplier(plan, store, overrides, options, applyDerivedModelContextBudgetOverrides)
	if err != nil {
		return SessionPlan{}, nil, err
	}
	capabilities, err := llm.ResolveRuntimeProviderCapabilities(next.ActiveSettings)
	if err != nil {
		return SessionPlan{}, nil, err
	}
	fastAvailable := llm.SupportsFastModeProvider(capabilities)
	var chatSettings session.ChatSettings
	next.ActiveSettings, chatSettings, err = applySessionChatSettingsWithRunOverrides(
		store.Meta(),
		next.ActiveSettings,
		overrides,
		&fastAvailable,
	)
	if err != nil {
		return SessionPlan{}, nil, err
	}
	next.QuestionsEnabled = chatSettings.Questions
	next.AutoCompactionEnabled = chatSettings.AutoCompaction
	next, err = finalizeRunPromptOverrides(next, overrides, options)
	return next, warnings, err
}

func finalizeRunPromptOverrides(plan SessionPlan, overrides serverapi.RunPromptOverrides, options RunPromptOverrideOptions) (SessionPlan, error) {
	next, err := WithRequiredRunPromptTools(plan, options.RequiredTools)
	if err != nil {
		return SessionPlan{}, err
	}
	next, err = withWorkflowThinking(next, options.WorkflowThinking)
	next.ThinkingOverrideExplicit = strings.TrimSpace(overrides.ThinkingLevel) != ""
	return next, err
}

func WithRequiredRunPromptTools(plan SessionPlan, required []toolspec.ID) (SessionPlan, error) {
	if len(required) == 0 {
		return plan, nil
	}
	enabled := cloneMapOrEmpty(plan.ActiveSettings.EnabledTools)
	for _, tool := range required {
		enabled[tool] = true
		if tool == toolspec.ToolAskQuestion {
			plan.QuestionsEnabled = true
		}
	}
	plan.RequiredTools = DedupeSortToolIDs(append(append([]toolspec.ID(nil), plan.RequiredTools...), required...))
	plan.ActiveSettings.EnabledTools = enabled
	plan.EnabledTools = DedupeSortToolIDs(append(append([]toolspec.ID(nil), plan.EnabledTools...), required...))
	return plan, nil
}

func withWorkflowThinking(plan SessionPlan, mutation workflow.ThinkingMutation) (SessionPlan, error) {
	switch mutation.Kind() {
	case workflow.ThinkingMutationUnchanged:
		return plan, nil
	case workflow.ThinkingMutationSet:
		if err := mutation.Value().Validate(); err != nil {
			return SessionPlan{}, err
		}
	case workflow.ThinkingMutationClear:
	default:
		return SessionPlan{}, errors.New("workflow thinking mutation is invalid")
	}
	plan.ActiveSettings = cloneSettings(plan.ActiveSettings)
	switch mutation.Kind() {
	case workflow.ThinkingMutationClear:
		configured, err := ResolveReadOnlySessionContextSettings(baseConfigForPlan(plan), session.Meta{
			Continuation: plan.Continuation,
			Locked:       plan.Locked,
		}, plan.SkipContinuationAgentRoleValidation)
		if err != nil {
			return SessionPlan{}, err
		}
		plan.ActiveSettings.ThinkingLevel = configured.Settings.ThinkingLevel
	case workflow.ThinkingMutationSet:
		plan.ActiveSettings.ThinkingLevel = string(mutation.Value())
	}
	return plan, nil
}

func (p Planner) applyRunPromptOverridesWithBudgetApplier(plan SessionPlan, store *session.Store, overrides serverapi.RunPromptOverrides, options RunPromptOverrideOptions, applyBudget modelContextBudgetApplier) (SessionPlan, []string, error) {
	locked := store.Meta().Locked
	effectiveOverrides := overrides
	if locked != nil {
		effectiveOverrides.AgentRole = nil
	}
	prepared, err := prepareRunPromptOverridesWithBudget(baseConfigForPlan(plan), effectiveOverrides, RunPromptPreparationContext{
		Mode:      ModeInteractive,
		ModelLock: locked,
		ToolLock:  locked,
		OmittedTarget: &PreparedBaseTarget{
			Settings:     plan.ActiveSettings,
			Source:       plan.Source,
			EnabledTools: plan.EnabledTools,
		},
	}, applyBudget)

	if err != nil {
		return SessionPlan{}, nil, err
	}
	return p.applyPreparedRunPromptOverridesWithBudgetApplier(plan, store.Meta(), store.SetContinuationContext, effectiveOverrides, prepared, options, applyBudget)
}

func baseConfigForPlan(plan SessionPlan) config.App {
	settings := plan.BaseSettings
	if strings.TrimSpace(settings.Model) == "" {
		settings = plan.ActiveSettings
	}
	source := plan.BaseSource
	if source.Sources == nil {
		source = plan.Source
	}
	return config.App{
		WorkspaceRoot: plan.WorkspaceRoot,
		Settings:      settings,
		Source:        source,
	}
}

type modelContextBudgetApplier func(settings *config.Settings, explicitSources map[string]config.Origin, originalModel string, allowModelOverride bool)

// PrepareRunPromptOverrides resolves every config-backed part of a RunPrompt
// target from one loaded application snapshot. It intentionally performs no
// store mutation, config reload, or session materialization.
func PrepareRunPromptOverrides(app config.App, overrides serverapi.RunPromptOverrides) (PreparedRunPromptOverrides, error) {
	return PrepareRunPromptOverridesWithContext(app, overrides, RunPromptPreparationContext{Mode: ModeInteractive})
}

func PrepareRunPromptOverridesForLockedSession(app config.App, overrides serverapi.RunPromptOverrides, locked *session.LockedContract) (PreparedRunPromptOverrides, error) {
	return PrepareRunPromptOverridesWithContext(app, overrides, RunPromptPreparationContext{
		Mode:      ModeInteractive,
		ModelLock: locked,
		ToolLock:  locked,
	})

}

func PrepareRunPromptOverridesWithContext(app config.App, overrides serverapi.RunPromptOverrides, preparation RunPromptPreparationContext) (PreparedRunPromptOverrides, error) {
	return prepareRunPromptOverridesWithBudget(app, overrides, preparation, applyDerivedModelContextBudgetOverrides)
}

func prepareRunPromptOverridesWithBudget(app config.App, overrides serverapi.RunPromptOverrides, preparation RunPromptPreparationContext, applyBudget modelContextBudgetApplier) (PreparedRunPromptOverrides, error) {
	switch preparation.Mode {
	case ModeInteractive, ModeHeadless:
	default:
		return PreparedRunPromptOverrides{}, fmt.Errorf("invalid launch mode %q", preparation.Mode)
	}
	roleOverride, err := overrides.AgentRoleOverride()
	if err != nil {
		return PreparedRunPromptOverrides{}, fmt.Errorf("%w: %v", errInvalidAgentRole, err)
	}
	if preparation.Mode == ModeHeadless &&
		(roleOverride.Default || (!roleOverride.Present && preparation.OmittedTarget == nil)) {
		roleOverride = serverapi.RunPromptAgentRoleOverride{Present: true, Role: config.DefaultSubagentRole}
	}
	overrideConfig := app
	if overrides.HasConfigOverrides() {
		overrideConfig, err = config.ApplyLoadOptionsToSnapshot(app, runPromptLoadOptions(overrides))
		if err != nil {
			return PreparedRunPromptOverrides{}, err
		}
	}
	prepared := PreparedRunPromptOverrides{
		OverrideConfig: overrideConfig,
		AgentRole:      roleOverride,
	}
	if !roleOverride.Present || roleOverride.Default {
		if !roleOverride.Present && preparation.OmittedTarget != nil {
			target, targetErr := preparePreparedBaseTarget(*preparation.OmittedTarget, overrideConfig, overrides, preparation.ModelLock, preparation.ToolLock, applyBudget)
			if targetErr != nil {
				return PreparedRunPromptOverrides{}, targetErr
			}
			prepared.BaseTarget = &target
		} else if preparation.SkipProviderReadinessValidation {
			target, targetErr := prepareBaseTargetWithoutProviderReadiness(app, preparation.ModelLock, preparation.ToolLock)
			if targetErr != nil {
				return PreparedRunPromptOverrides{}, targetErr
			}
			prepared.BaseTarget = &target
		} else {
			target, targetErr := prepareBaseTarget(app, overrideConfig, overrides, preparation.ModelLock, preparation.ToolLock, applyBudget)
			if targetErr != nil {
				return PreparedRunPromptOverrides{}, targetErr
			}
			prepared.BaseTarget = &target
		}
		if !preparation.SkipProviderReadinessValidation && prepared.BaseTarget != nil {
			capabilities, capabilityErr := llm.ResolveRuntimeProviderCapabilities(prepared.BaseTarget.Settings)
			if capabilityErr != nil {
				return PreparedRunPromptOverrides{}, capabilityErr
			}
			prepared.ProviderCapabilities = &capabilities
		}
		return prepared, nil
	}
	lookup := config.LookupSubagentRole(app.Settings, roleOverride.Role)
	switch lookup.Status {
	case config.SubagentRoleLookupInvalid:
		return PreparedRunPromptOverrides{}, fmt.Errorf("%w: invalid subagent role %q", errInvalidAgentRole, roleOverride.Role)
	case config.SubagentRoleLookupMissing:
		return PreparedRunPromptOverrides{}, fmt.Errorf("%w: unrecognized role %q", errInvalidAgentRole, roleOverride.Role)
	}
	providerSettings := EffectiveSettings(app.Settings, preparation.ModelLock)
	providerSettings.Connection = overrideConfig.Settings.Connection
	providerSettings.Subagents = nil
	providerSettings, err = config.OverlaySubagentRoleProviderSettings(config.App{Settings: providerSettings, Source: overrideConfig.Source}, lookup.Role)
	if err != nil {
		return PreparedRunPromptOverrides{}, err
	}
	providerID, err := persistedRoleProviderID(providerSettings)
	if err != nil {
		return PreparedRunPromptOverrides{}, err
	}
	var providerCapabilities *llm.ProviderCapabilities
	if !preparation.SkipProviderReadinessValidation {
		providerCaps, err := llm.ResolveRuntimeProviderCapabilities(providerSettings)
		if err != nil {
			return PreparedRunPromptOverrides{}, err
		}
		providerID = strings.TrimSpace(providerCaps.ProviderID)
		providerCapabilities = &providerCaps
	}
	target, err := prepareNamedTarget(
		app,
		overrideConfig,
		overrides,
		*lookup.NormalizedSelector,
		lookup.Role,
		providerID,
		preparation.ModelLock,
		preparation.ToolLock,
		!preparation.SkipProviderReadinessValidation,
		applyBudget,
	)
	if err != nil {
		return PreparedRunPromptOverrides{}, err
	}
	prepared.NamedTarget = &target
	prepared.ProviderCapabilities = providerCapabilities
	return prepared, nil
}

func prepareBaseTargetWithoutProviderReadiness(app config.App, modelLock, toolLock *session.LockedContract) (PreparedBaseTarget, error) {
	resolved := EffectiveSettings(app.Settings, modelLock)
	source := cloneSourceReport(app.Source)
	config.InheritReviewerSettings(&resolved, source.Sources)
	enabledTools, err := ActiveToolIDsForPlan(resolved, source, toolLock)
	if err != nil {
		return PreparedBaseTarget{}, err
	}
	return PreparedBaseTarget{
		Settings:     resolved,
		Source:       source,
		EnabledTools: enabledTools,
	}, nil
}

func prepareBaseTarget(app, overrideConfig config.App, overrides serverapi.RunPromptOverrides, modelLock, toolLock *session.LockedContract, applyBudget modelContextBudgetApplier) (PreparedBaseTarget, error) {
	target, err := prepareBaseTargetWithoutProviderReadiness(app, modelLock, toolLock)
	if err != nil {
		return PreparedBaseTarget{}, err
	}
	return preparePreparedBaseTarget(target, overrideConfig, overrides, modelLock, toolLock, applyBudget)
}

func preparePreparedBaseTarget(target PreparedBaseTarget, overrideConfig config.App, overrides serverapi.RunPromptOverrides, modelLock, toolLock *session.LockedContract, applyBudget modelContextBudgetApplier) (PreparedBaseTarget, error) {
	resolved := cloneSettings(target.Settings)
	source := cloneSourceReport(target.Source)
	enabledTools := append([]toolspec.ID(nil), target.EnabledTools...)
	var err error
	resolved, source, enabledTools, err = applyPreparedConfigOverrides(resolved, source, enabledTools, overrideConfig, overrides, modelLock, toolLock, applyBudget)
	if err != nil {
		return PreparedBaseTarget{}, err
	}
	if overrides.HasConfigOverrides() {
		resolved, err = validateRunPromptOverrideSettings(resolved, source)
		if err != nil {
			return PreparedBaseTarget{}, err
		}
	}
	return PreparedBaseTarget{Settings: resolved, Source: source, EnabledTools: append([]toolspec.ID(nil), enabledTools...)}, nil
}

func prepareNamedTarget(
	app, overrideConfig config.App,
	overrides serverapi.RunPromptOverrides,
	selector string,
	role config.SubagentRole,
	providerID string,
	modelLock, toolLock *session.LockedContract,
	validate bool,
	applyBudget modelContextBudgetApplier,
) (PreparedSubagentTarget, error) {
	input := preparedSubagentIdentity{Selector: selector, Role: role, ProviderID: providerID}
	baseSettings := EffectiveSettings(app.Settings, modelLock)
	resolved, source, warning, err := resolvePreparedSubagentSettings(baseSettings, app.Source, input, modelLock == nil, false)
	if err != nil {
		return PreparedSubagentTarget{}, err
	}
	enabledTools, err := ActiveToolIDsForPlan(resolved, source, toolLock)
	if err != nil {
		return PreparedSubagentTarget{}, err
	}
	resolved, source, enabledTools, err = applyPreparedConfigOverrides(resolved, source, enabledTools, overrideConfig, overrides, modelLock, toolLock, applyBudget)
	if err != nil {
		return PreparedSubagentTarget{}, err
	}
	if validate {
		resolved, err = validateRunPromptOverrideSettings(resolved, source)
		if err != nil {
			return PreparedSubagentTarget{}, err
		}
	}
	return PreparedSubagentTarget{
		Selector:     selector,
		Settings:     resolved,
		Source:       source,
		EnabledTools: append([]toolspec.ID(nil), enabledTools...),
		Warning:      warning,
	}, nil
}

func applyPreparedConfigOverrides(settings config.Settings, source config.SourceReport, enabledTools []toolspec.ID, overrideConfig config.App, overrides serverapi.RunPromptOverrides, modelLock, toolLock *session.LockedContract, applyBudget modelContextBudgetApplier) (config.Settings, config.SourceReport, []toolspec.ID, error) {
	if !overrides.HasConfigOverrides() {
		return settings, source, enabledTools, nil
	}
	originalModel := settings.Model
	settings, source.Sources = config.OverlayCLIOverrides(settings, source.Sources, overrideConfig.Settings, overrideConfig.Source.Sources, modelLock == nil, toolLock == nil)
	if strings.TrimSpace(overrides.Model) != "" && modelLock == nil {
		explicitSources := map[string]config.Origin{}
		for key, value := range source.Sources {
			if value.Configured() {
				explicitSources[key] = value
			}
		}
		applyBudget(&settings, explicitSources, originalModel, true)
	}
	if toolLock == nil && (strings.TrimSpace(overrides.Tools) != "" || strings.TrimSpace(overrides.Model) != "") {
		var err error
		enabledTools, err = ActiveToolIDsForPlan(settings, source, nil)
		if err != nil {
			return config.Settings{}, config.SourceReport{}, nil, err
		}
	}
	return settings, source, enabledTools, nil
}

func (p Planner) ApplyPreparedRunPromptOverridesWithStore(plan SessionPlan, store *session.Store, overrides serverapi.RunPromptOverrides, prepared PreparedRunPromptOverrides, options RunPromptOverrideOptions) (SessionPlan, []string, error) {
	if store == nil {
		return SessionPlan{}, nil, errors.New("session store is required")
	}
	return p.applyPreparedRunPromptOverrides(plan, store.Meta(), store.SetContinuationContext, overrides, prepared, options)
}

func (p Planner) applyPreparedRunPromptOverrides(plan SessionPlan, meta session.Meta, persistContinuation func(session.ContinuationContext) error, overrides serverapi.RunPromptOverrides, prepared PreparedRunPromptOverrides, options RunPromptOverrideOptions) (SessionPlan, []string, error) {
	next, warnings, err := p.applyPreparedRunPromptOverridesWithBudgetApplier(plan, meta, persistContinuation, overrides, prepared, options, applyDerivedModelContextBudgetOverrides)
	if err != nil {
		return SessionPlan{}, nil, err
	}
	meta.Continuation = next.Continuation
	var fastAvailable *bool
	if prepared.ProviderCapabilities != nil {
		value := llm.SupportsFastModeProvider(*prepared.ProviderCapabilities)
		fastAvailable = &value
	}
	var chatSettings session.ChatSettings
	next.ActiveSettings, chatSettings, err = applySessionChatSettingsWithRunOverrides(
		meta,
		next.ActiveSettings,
		overrides,
		fastAvailable,
	)
	if err != nil {
		return SessionPlan{}, nil, err
	}
	next.QuestionsEnabled = chatSettings.Questions
	next.AutoCompactionEnabled = chatSettings.AutoCompaction
	next.ThinkingOverrideExplicit = strings.TrimSpace(overrides.ThinkingLevel) != ""
	return next, warnings, nil
}

func applySessionChatSettingsWithRunOverrides(
	meta session.Meta,
	active config.Settings,
	overrides serverapi.RunPromptOverrides,
	fastAvailable *bool,
) (config.Settings, session.ChatSettings, error) {
	settings, err := ResolveSessionChatSettings(meta, active)
	if err != nil {
		return config.Settings{}, session.ChatSettings{}, err
	}
	var thinkingOverride *string
	if strings.TrimSpace(overrides.ThinkingLevel) != "" {
		thinkingOverride = textutil.Value(active.ThinkingLevel)
	}
	return applyResolvedSessionChatSettings(
		active,
		settings,
		thinkingOverride,
		fastAvailable != nil,
		fastAvailable,
	)
}

func (p Planner) applyPreparedRunPromptOverridesWithBudgetApplier(plan SessionPlan, meta session.Meta, persistContinuation func(session.ContinuationContext) error, overrides serverapi.RunPromptOverrides, prepared PreparedRunPromptOverrides, options RunPromptOverrideOptions, applyBudget modelContextBudgetApplier) (SessionPlan, []string, error) {
	plan.ExplicitToolSelection = config.ExplicitToolSelection(prepared.OverrideConfig.Settings, prepared.OverrideConfig.Source.Sources)
	var retainedErr error
	prepared, retainedErr = retainedPreparedToolTargets(prepared, meta)
	if retainedErr != nil {
		return SessionPlan{}, nil, retainedErr
	}
	if !overrides.HasAny() && !prepared.AgentRole.Present && prepared.BaseTarget == nil {
		return sessionPlanWithMeta(plan, meta, p.ContainerDir), nil, nil
	}
	var warnings []string
	next := plan
	baseSettings := plan.BaseSettings
	if strings.TrimSpace(baseSettings.Model) == "" {
		baseSettings = plan.ActiveSettings
	}
	baseSource := plan.BaseSource
	if baseSource.Sources == nil {
		baseSource = plan.Source
	}
	shouldPersistContinuation := false
	var continuationAgentRole *string
	if meta.Continuation != nil {
		continuationAgentRole = cloneContinuationRole(meta.Continuation.AgentRole)
	}
	applyContinuation := func() error {
		continuation := session.ContinuationContext{
			AgentRole: continuationAgentRole,
		}
		normalized, err := session.NormalizeContinuationContext(continuation)
		if err != nil {
			return err
		}
		meta.Continuation = normalized
		if persistContinuation != nil {
			err = persistContinuation(continuation)
		}
		return err
	}
	roleOverride := prepared.AgentRole
	if plan.ModelContractLocked {
		roleOverride = serverapi.RunPromptAgentRoleOverride{}
	}
	if !roleOverride.Present && prepared.BaseTarget != nil {
		next.ActiveSettings = cloneSettings(prepared.BaseTarget.Settings)
		next.Source = cloneSourceReport(prepared.BaseTarget.Source)
		next.EnabledTools = append([]toolspec.ID(nil), prepared.BaseTarget.EnabledTools...)
		if !plan.ModelContractLocked {
			next.ConfiguredModelName = next.ActiveSettings.Model
		}
		return sessionPlanWithMeta(next, meta, p.ContainerDir), warnings, nil
	}
	var requestedContinuationRole *string
	if roleOverride.Present && !roleOverride.Default {
		requestedContinuationRole = cloneContinuationRole(&roleOverride.Role)
	}
	if roleOverride.Present {
		shouldPersistContinuation = shouldPersistContinuation || !options.AgentSelectionPersisted
		continuationAgentRole = requestedContinuationRole
		next.ActiveSettings = cloneSettings(baseSettings)
		next.Source = baseSource
		if !plan.ModelContractLocked {
			next.ConfiguredModelName = next.ActiveSettings.Model
		}
		if roleOverride.Default {
			if prepared.BaseTarget == nil {
				return SessionPlan{}, nil, errors.New("prepared base target is required for explicit default selector")
			}
			next.ActiveSettings = cloneSettings(prepared.BaseTarget.Settings)
			next.Source = prepared.BaseTarget.Source
			next.EnabledTools = append([]toolspec.ID(nil), prepared.BaseTarget.EnabledTools...)
			if !plan.ModelContractLocked {
				next.ConfiguredModelName = next.ActiveSettings.Model
			}
			if shouldPersistContinuation {
				if err := applyContinuation(); err != nil {
					return SessionPlan{}, nil, err
				}
			}
			return sessionPlanWithMeta(next, meta, p.ContainerDir), warnings, nil
		}
	}
	if roleOverride.Role != "" {
		if prepared.NamedTarget == nil || prepared.NamedTarget.Selector != roleOverride.Role {
			return SessionPlan{}, nil, errors.New("prepared named subagent target is required")
		}
		next.ActiveSettings = cloneSettings(prepared.NamedTarget.Settings)
		if !plan.ModelContractLocked {
			next.ConfiguredModelName = next.ActiveSettings.Model
		}
		next.EnabledTools = append([]toolspec.ID(nil), prepared.NamedTarget.EnabledTools...)
		next.Source = prepared.NamedTarget.Source
		if prepared.NamedTarget.Warning != nil {
			warnings = append(warnings, *prepared.NamedTarget.Warning)
		}
		if shouldPersistContinuation {
			if err := applyContinuation(); err != nil {
				return SessionPlan{}, nil, err
			}
		}
		return sessionPlanWithMeta(next, meta, p.ContainerDir), warnings, nil
	}
	if !overrides.HasConfigOverrides() {
		if shouldPersistContinuation {
			if err := applyContinuation(); err != nil {
				return SessionPlan{}, nil, err
			}
		}
		return sessionPlanWithMeta(next, meta, p.ContainerDir), warnings, nil
	}
	loaded := prepared.OverrideConfig
	locked := meta.Locked
	var err error
	next.ActiveSettings, next.Source, next.EnabledTools, err = applyPreparedConfigOverrides(
		cloneSettings(next.ActiveSettings),
		cloneSourceReport(next.Source),
		append([]toolspec.ID(nil), next.EnabledTools...),
		loaded,
		overrides,
		locked,
		locked,
		applyBudget,
	)
	if err != nil {
		return SessionPlan{}, nil, err
	}
	if strings.TrimSpace(overrides.Model) != "" && !next.ModelContractLocked {
		next.ConfiguredModelName = loaded.Settings.Model
	}
	validated, err := validateRunPromptOverrideSettings(next.ActiveSettings, next.Source)
	if err != nil {
		return SessionPlan{}, nil, err
	}
	next.ActiveSettings = validated
	if shouldPersistContinuation {
		if err := applyContinuation(); err != nil {
			return SessionPlan{}, nil, err
		}
	}
	return sessionPlanWithMeta(next, meta, p.ContainerDir), warnings, nil
}

func runPromptLoadOptions(overrides serverapi.RunPromptOverrides) config.LoadOptions {
	return config.LoadOptions{
		Model:               strings.TrimSpace(overrides.Model),
		ThinkingLevel:       strings.TrimSpace(overrides.ThinkingLevel),
		Theme:               strings.TrimSpace(overrides.Theme),
		ModelTimeoutSeconds: overrides.ModelTimeoutSeconds,
		Tools:               strings.TrimSpace(overrides.Tools),
	}
}

func resolvePreparedSubagentSettings(base config.Settings, baseSource config.SourceReport, target preparedSubagentIdentity, allowModelOverride bool, validate bool) (config.Settings, config.SourceReport, *string, error) {
	return resolveSubagentSettingsFromRole(base, baseSource, target.Selector, target.Role, target.ProviderID, allowModelOverride, validate)
}

func cloneContinuationRole(role *string) *string {
	if role == nil {
		return nil
	}
	copyRole := *role
	return &copyRole
}

func validateRunPromptOverrideSettings(settings config.Settings, source config.SourceReport) (config.Settings, error) {
	validated := cloneSettings(settings)
	sources := cloneMapOrEmpty(source.Sources)
	config.InheritReviewerSettings(&validated, sources)
	if err := config.ValidateSettingsWithSources(validated, sources); err != nil {
		return config.Settings{}, err
	}
	return validated, nil
}

func cloneSourceReport(source config.SourceReport) config.SourceReport {
	next := source
	next.Sources = cloneMapOrEmpty(source.Sources)
	return next
}

func (p Planner) openStore(ctx context.Context, req SessionRequest) (*session.Store, error) {
	if strings.TrimSpace(p.Config.PersistenceRoot) == "" {
		return nil, errors.New("launch planner persistence root is required")
	}
	if strings.TrimSpace(p.ContainerDir) == "" {
		return nil, errors.New("launch planner container dir is required")
	}
	switch req.Intent.Kind() {
	case serverapi.SessionLaunchIntentCreateNew:
		origin, ok := req.Intent.CreateOrigin()
		if !ok {
			return nil, errors.New("create-new session launch intent requires origin")
		}
		return p.createSession(ctx, origin, req.Mode, req.InitialChat)
	default:
		return nil, errSessionLaunchIntentRequired
	}
}

func (p Planner) SelectedSessionPromptFacingTargetFromMeta(meta session.Meta) (PreparedBaseTarget, error) {
	active, source, _, err := resolveReadOnlySessionContextSettings(p.Config, meta, false)
	if err != nil {
		return PreparedBaseTarget{}, err
	}
	enabledTools, err := ActiveToolIDsForPlan(active, source, meta.Locked)
	if err != nil {
		return PreparedBaseTarget{}, err
	}
	return PreparedBaseTarget{
		Settings:     cloneSettings(active),
		Source:       cloneSourceReport(source),
		EnabledTools: append([]toolspec.ID(nil), enabledTools...),
	}, nil
}

func (p Planner) createSession(
	ctx context.Context,
	origin serverapi.SessionCreateOrigin,
	mode Mode,
	initialChat *session.ChatDraftState,
) (*session.Store, error) {
	plan, err := p.prepareCreation(ctx, origin, mode, runtimeids.NewSessionID(), initialChat)
	if err != nil {
		return nil, err
	}
	var target *worktreepb.SessionExecutionTarget
	if sourceID, present := origin.SessionID(); present {
		resolved, hasTarget, err := p.resolveParentExecutionTarget(ctx, sourceID.String())
		if err != nil {
			return nil, err
		}
		if hasTarget {
			target = resolved
		}
	}
	created, err := session.MaterializeCreation(ctx, plan, p.StoreOptions...)
	if err != nil {
		return nil, err
	}
	if target != nil {
		if err := p.updateChildExecutionTarget(ctx, created.Meta().SessionID, target); err != nil {
			return nil, err
		}
	}
	return created, nil
}

func (p Planner) openMetadataStore() (MetadataExecutionTargetStore, error) {
	if p.MetadataStoreOpener != nil {
		return p.MetadataStoreOpener(p.Config.PersistenceRoot)
	}
	return metadata.Open(p.Config.PersistenceRoot)
}

func (p Planner) resolveParentExecutionTarget(ctx context.Context, parentSessionID string) (*worktreepb.SessionExecutionTarget, bool, error) {
	if err := ctx.Err(); err != nil {
		return &worktreepb.SessionExecutionTarget{}, false, err
	}
	store, err := p.openMetadataStore()
	if err != nil {
		return &worktreepb.SessionExecutionTarget{}, false, err
	}
	defer func() { _ = store.Close() }()
	target, err := store.ResolveSessionExecutionTarget(ctx, parentSessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, session.ErrSessionNotFound) {
			return &worktreepb.SessionExecutionTarget{}, false, nil
		}
		return &worktreepb.SessionExecutionTarget{}, false, err
	}
	return target, true, nil
}

func (p Planner) updateChildExecutionTarget(ctx context.Context, childSessionID string, target *worktreepb.SessionExecutionTarget) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store, err := p.openMetadataStore()
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	return store.UpdateSessionExecutionTarget(ctx, metadata.SessionExecutionTargetUpdateFromReadModel(childSessionID, target))
}

func EnsureSubagentSessionName(store *session.Store) error {
	if store == nil {
		return errors.New("session store is required")
	}
	meta := store.Meta()
	if strings.TrimSpace(meta.Name) != "" {
		return nil
	}
	return store.SetName(subagentSessionName(meta))
}

func subagentSessionName(meta session.Meta) string {
	return strings.TrimSpace(meta.SessionID + " " + SubagentSessionSuffix)
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

func ActiveToolIDsForPlan(settings config.Settings, source config.SourceReport, locked *session.LockedContract) ([]toolspec.ID, error) {
	if locked != nil && (locked.HasEnabledTools || len(locked.EnabledTools) > 0) {
		ids := make([]toolspec.ID, 0, len(locked.EnabledTools))
		for _, raw := range locked.EnabledTools {
			if id, ok := toolspec.ParseID(raw); ok {
				ids = append(ids, id)
			}
		}
		return DedupeSortToolIDs(ids), nil
	}
	enabled := cloneMapOrEmpty(settings.EnabledTools)
	if bothEditToolSourcesDefault(source) {
		capabilities, err := llm.ResolveRuntimeProviderCapabilities(settings)
		if err != nil {
			return nil, err
		}
		if capabilities.IsOpenAIFirstParty || strings.HasPrefix(strings.ToLower(strings.TrimSpace(settings.Model)), "gpt-") {
			enabled[toolspec.ToolPatch] = true
			enabled[toolspec.ToolEdit] = false
		} else {
			enabled[toolspec.ToolPatch] = false
			enabled[toolspec.ToolEdit] = true
		}
	}
	if enabled[toolspec.ToolPatch] && enabled[toolspec.ToolEdit] {
		return nil, ErrPatchEditToolsConflict
	}
	return DedupeSortToolIDs(enabledToolIDs(enabled)), nil
}

func bothEditToolSourcesDefault(source config.SourceReport) bool {
	return source.Sources["tools.patch"].Kind == config.SourceDefault && source.Sources["tools.edit"].Kind == config.SourceDefault
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
