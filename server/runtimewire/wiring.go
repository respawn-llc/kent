package runtimewire

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"core/server/auth"
	"core/server/authservice"
	"core/server/launch"
	"core/server/llm"
	"core/server/runtime"
	"core/server/session"
	"core/server/tools"
	askquestion "core/server/tools"
	triggerhandofftool "core/server/tools"
	shelltool "core/server/tools/shell"
	"core/server/tools/shell/postprocess"
	"core/server/workflowruntime"
	"core/shared/config"
	"core/shared/textutil"
	"core/shared/toolspec"
)

type RuntimeWiring struct {
	Engine     *runtime.Engine
	AskBroker  *askquestion.AskQuestionBroker
	Background *shelltool.Manager
	LocalTools *LocalToolRegistryBinding
}

func (w *RuntimeWiring) Close() error {
	if w == nil || w.Engine == nil {
		return nil
	}
	return w.Engine.Close()
}

type RuntimeWiringOptions struct {
	Environment                         func(string) (string, bool)
	MainWorkspaceRoot                   string
	RequiredTools                       []toolspec.ID
	FilesystemContext                   tools.FilesystemContext
	Context                             context.Context
	OnEvent                             func(evt runtime.Event)
	Headless                            bool
	QuestionsEnabled                    *bool
	AutoCompactionEnabled               *bool
	Sources                             map[string]config.Origin
	Client                              llm.Client
	ClientFactory                       RuntimeClientFactory
	ReviewerClientFactory               RuntimeClientFactory
	WorkflowPrompt                      *workflowruntime.PromptContract
	AskQuestionBatchSkipped             func(askquestion.AskQuestionBatchMetadata)
	PromptFacingSnapshotReloader        runtime.PromptFacingSnapshotReloader
	ProviderCapabilitiesOverride        *llm.ProviderCapabilities
	SkipContinuationAgentRoleValidation bool
	StepLifecycle                       runtime.StepLifecycleSink
	LifecycleTaskFinished               func() error
	LifecycleRuntimeAbort               func() error
	SubmitAgentSteer                    func(context.Context, runtime.AgentSteer) error
	DurabilityObserver                  runtime.ResultGroupDurabilityObserver
	// GlobalConfigDir is the absolute persistence root that owns model-visible
	// global context (AGENTS.md, system prompt, skills). Empty falls back to
	// ~/.kent inside the runtime resolvers.
	GlobalConfigDir string
}

func NewRuntimeWiring(store *session.Store, eventLog session.MaterializedEventLog, active config.Settings, enabledTools []toolspec.ID, mgr *auth.Manager, logger Logger, opts RuntimeWiringOptions) (*RuntimeWiring, error) {
	return NewRuntimeWiringWithBackground(store, eventLog, active, enabledTools, mgr, logger, nil, opts)
}

func NewRuntimeWiringWithBackground(
	store *session.Store,
	eventLog session.MaterializedEventLog,
	active config.Settings,
	enabledTools []toolspec.ID,
	mgr *auth.Manager,
	logger Logger,
	background *shelltool.Manager,
	opts RuntimeWiringOptions,
) (*RuntimeWiring, error) {
	if opts.PromptFacingSnapshotReloader == nil && strings.TrimSpace(opts.MainWorkspaceRoot) == "" {
		return nil, errors.New("Main Workspace root is required for configuration reload")
	}
	if opts.Client != nil && opts.ClientFactory != nil {
		return nil, ErrRuntimeClientFactoryConflict
	}
	catalog, err := config.LoadGlobal(config.LoadOptions{ConfigRoot: opts.GlobalConfigDir})
	if err != nil {
		return nil, err
	}
	active.Connections = catalog.Settings.Connections
	selected, _, err := launch.ResolveSessionConnection(active, store.Meta().ConnectionID)
	if err != nil {
		return nil, err
	}
	active.Connection = &selected
	config.InheritReviewerSettings(&active, opts.Sources)
	shellPostprocessor, err := postprocess.NewRunner(postprocess.Settings{
		PersistenceRoot: opts.GlobalConfigDir,
		Mode:            active.Shell.PostprocessingMode,
		HookPath:        active.Shell.PostprocessHook,
	})
	if err != nil {
		return nil, fmt.Errorf("compile effective shell postprocessor: %w", err)
	}
	filesystemContext := opts.FilesystemContext.Clone()
	if err := validateFilesystemContext(filesystemContext); err != nil {
		return nil, err
	}
	workingDirectory := filesystemContext.Access.WorkingDirectory.LexicalPath
	factoryContext := opts.Context
	if factoryContext == nil {
		factoryContext = context.Background()
	}

	resolver := authservice.NewConnectionResolver(opts.GlobalConfigDir, mgr, opts.Environment)
	newClient := func(settings config.Settings, purpose RuntimeClientPurpose, factory RuntimeClientFactory) (llm.Client, error) {
		connection, err := resolver.Resolve(settings)
		if err != nil {
			return nil, err
		}
		return NewRuntimeClient(factoryContext, factory, RuntimeClientRequest{
			Purpose: purpose, SessionID: store.Meta().SessionID, ActiveSettings: settings,
			EnabledTools: enabledTools, Sources: opts.Sources, Connection: connection,
		})
	}
	var client llm.Client
	if opts.Client != nil {
		client = opts.Client
	} else {
		client, err = newClient(active, RuntimeClientPurposeMain, opts.ClientFactory)
		if err != nil {
			return nil, err
		}
	}

	newReviewerClient := func() (llm.Client, error) {
		factory := opts.ClientFactory
		if factory == nil {
			factory = opts.ReviewerClientFactory
		}
		settings := active
		settings.Connection = active.Reviewer.Connection
		settings.Model = active.Reviewer.Model
		settings.ThinkingLevel = active.Reviewer.ThinkingLevel
		settings.ModelVerbosity = active.Reviewer.ModelVerbosity
		settings.ModelContextWindow = active.Reviewer.ModelContextWindow
		settings.ModelCapabilities = active.Reviewer.ModelCapabilities
		settings.Timeouts.ModelRequestSeconds = active.Reviewer.TimeoutSeconds
		settings.Store = false
		return newClient(settings, RuntimeClientPurposeReviewer, factory)
	}

	var reviewerClient llm.Client
	if strings.ToLower(strings.TrimSpace(active.Reviewer.Frequency)) != "off" {
		reviewerClient, err = newReviewerClient()
		if err != nil {
			return nil, err
		}
	}

	providerCapabilities, err := llm.ResolveEffectiveProviderCapabilities(store.Meta().Locked, active)
	if err != nil {
		return nil, err
	}
	if opts.ProviderCapabilitiesOverride != nil {
		providerCapabilities = *opts.ProviderCapabilitiesOverride
	}
	modelCapabilities := lockedModelCapabilitiesForConfig(active.Model, active.ModelCapabilities, providerCapabilities, opts.Sources, "model_capabilities.supports_reasoning_effort", "model_capabilities.supports_vision_inputs")
	var eng *runtime.Engine
	localTools, askBroker, background, err := NewLocalToolRegistryBinding(LocalToolRegistryOptions{
		FilesystemContext:   filesystemContext,
		OwnerSessionID:      store.Meta().SessionID,
		Enabled:             enabledTools,
		MinimumExecToBgTime: time.Duration(active.MinimumExecToBgSeconds) * time.Second,
		ShellOutputMaxChars: active.ShellOutputMaxChars,
		ModelContextWindow:  active.ModelContextWindow,
		AllowNonCwdEdits:    active.AllowNonCwdEdits,
		SupportsVision: func() bool {
			if locked := store.Meta().Locked; locked != nil {
				return llm.LockedContractSupportsVisionInputs(locked, active.Model)
			}
			return modelCapabilities.SupportsVisionInputs
		},
		Logger:                   logger,
		Background:               background,
		ShellPostprocessor:       shellPostprocessor,
		GlobalConfigDir:          opts.GlobalConfigDir,
		Debug:                    active.Debug,
		TriggerHandoffController: func() triggerhandofftool.TriggerHandoffController { return eng },
		QuestionsEnabledGetter: func() bool {
			if eng == nil {
				return true
			}
			return eng.QuestionsEnabled()
		},
	})
	if err != nil {
		return nil, err
	}
	toolRegistry := localTools.registry
	promptReloader := opts.PromptFacingSnapshotReloader
	if promptReloader == nil {
		promptReloader = launchPromptFacingSnapshotReloader{
			store:                               store,
			localTools:                          localTools,
			configRoot:                          opts.GlobalConfigDir,
			mainWorkspaceRoot:                   opts.MainWorkspaceRoot,
			skipContinuationAgentRoleValidation: opts.SkipContinuationAgentRoleValidation,
		}
	}
	if len(opts.RequiredTools) != 0 {
		promptReloader = requiredToolsSnapshotReloader{base: promptReloader, required: append([]toolspec.ID(nil), opts.RequiredTools...)}
	}
	eng, err = runtime.New(store, eventLog, client, toolRegistry, runtime.Config{
		Model:                           active.Model,
		Debug:                           active.Debug,
		Temperature:                     1,
		MaxTokens:                       0,
		ThinkingLevel:                   active.ThinkingLevel,
		SupportedThinkingValues:         launch.SupportedChatThinkingValues(active.Model, active.ThinkingLevel),
		ModelCapabilities:               &modelCapabilities,
		FastModeEnabled:                 active.PriorityRequestMode,
		WebSearchMode:                   active.WebSearch,
		PromptFacingSnapshotReloader:    promptReloader,
		ProviderCapabilitiesOverride:    &providerCapabilities,
		EnabledTools:                    enabledTools,
		SkillPolicy:                     config.ResolveSkillPolicy(active),
		SubagentCatalog:                 config.App{Settings: active, Source: config.SourceReport{Sources: opts.Sources}},
		SystemPromptFile:                active.SystemPromptFile,
		RefreshToolRegistry:             localTools.ReplaceEnabledTools,
		AutoCompactTokenLimit:           active.ContextCompactionThresholdTokens,
		PreSubmitCompactionLeadTokens:   active.PreSubmitCompactionLeadTokens,
		ContextWindowTokens:             active.ModelContextWindow,
		EffectiveContextWindowPercent:   95,
		LocalCompactionCarryoverLimit:   20_000,
		CompactionMode:                  string(active.CompactionMode),
		CacheWarningMode:                active.CacheWarningMode,
		AutoCompactionEnabled:           textutil.Pointer(opts.AutoCompactionEnabled),
		QuestionsEnabled:                textutil.Pointer(opts.QuestionsEnabled),
		HeadlessMode:                    opts.Headless,
		ToolPreambles:                   active.ToolPreambles,
		WorkflowPrompt:                  opts.WorkflowPrompt,
		BackgroundShellManager:          background,
		AskQuestionBatchSkipped:         opts.AskQuestionBatchSkipped,
		TranscriptWorkingDir:            workingDirectory,
		GlobalConfigDir:                 opts.GlobalConfigDir,
		WorkflowPreCompactionTokenLimit: config.ResolveWorkflowPreCompactionTokens(active),
		Reviewer: runtime.ReviewerConfig{
			Frequency:         active.Reviewer.Frequency,
			Model:             active.Reviewer.Model,
			ThinkingLevel:     active.Reviewer.ThinkingLevel,
			ModelCapabilities: lockedModelCapabilitiesForConfig(active.Reviewer.Model, active.Reviewer.ModelCapabilities, llm.ProviderCapabilities{}, opts.Sources, "reviewer.model_capabilities.supports_reasoning_effort", "reviewer.model_capabilities.supports_vision_inputs"),
			SystemPromptFile:  active.Reviewer.SystemPromptFile,
			VerboseOutput:     active.Reviewer.VerboseOutput,
			Client:            reviewerClient,
			ClientFactory:     newReviewerClient,
		},
		OnEvent:               opts.OnEvent,
		StepLifecycle:         opts.StepLifecycle,
		LifecycleTaskFinished: opts.LifecycleTaskFinished,
		LifecycleRuntimeAbort: opts.LifecycleRuntimeAbort,
		SubmitAgentSteer:      opts.SubmitAgentSteer,
		DurabilityObserver:    opts.DurabilityObserver,
	})
	if err != nil {
		return nil, err
	}
	return &RuntimeWiring{
		Engine:     eng,
		AskBroker:  askBroker,
		Background: background,
		LocalTools: localTools,
	}, nil
}

type launchPromptFacingSnapshotReloader struct {
	store                               *session.Store
	localTools                          *LocalToolRegistryBinding
	configRoot                          string
	mainWorkspaceRoot                   string
	skipContinuationAgentRoleValidation bool
}

type requiredToolsSnapshotReloader struct {
	base     runtime.PromptFacingSnapshotReloader
	required []toolspec.ID
}

func (r requiredToolsSnapshotReloader) ReloadPromptFacingSnapshotConfig(ctx context.Context, sessionID string) (runtime.PromptFacingSnapshotConfig, error) {
	snapshot, err := r.base.ReloadPromptFacingSnapshotConfig(ctx, sessionID)
	if err != nil {
		return runtime.PromptFacingSnapshotConfig{}, err
	}
	plan, err := launch.WithRequiredRunPromptTools(launch.SessionPlan{ActiveSettings: snapshot.Settings, EnabledTools: snapshot.ActiveToolIDs}, r.required)
	if err != nil {
		return runtime.PromptFacingSnapshotConfig{}, err
	}
	snapshot.Settings, snapshot.ActiveToolIDs = plan.ActiveSettings, plan.EnabledTools
	return snapshot, nil
}

func (r launchPromptFacingSnapshotReloader) ReloadPromptFacingSnapshotConfig(context.Context, string) (runtime.PromptFacingSnapshotConfig, error) {
	workingDirectory := r.localTools.FilesystemContext().Access.WorkingDirectory.LexicalPath
	app, err := config.Load(workingDirectory, r.mainWorkspaceRoot, config.LoadOptions{ConfigRoot: r.configRoot})
	if err != nil {
		return runtime.PromptFacingSnapshotConfig{}, err
	}
	resolved, err := launch.ResolvePromptFacingSnapshotConfig(app, r.store, r.skipContinuationAgentRoleValidation)
	if err != nil {
		return runtime.PromptFacingSnapshotConfig{}, err
	}
	meta := r.store.Meta()
	meta.ChatSettings = nil
	configured, err := launch.ResolveReadOnlySessionContextSettings(app, meta, r.skipContinuationAgentRoleValidation)
	if err != nil {
		return runtime.PromptFacingSnapshotConfig{}, err
	}
	return runtime.PromptFacingSnapshotConfig{
		ConfiguredThinking: configured.Settings.ThinkingLevel,
		Settings:           resolved.Settings,
		Source:             resolved.Source,
		ActiveToolIDs:      append([]toolspec.ID(nil), resolved.ActiveToolIDs...),
		WebSearchMode:      resolved.WebSearchMode,
	}, nil
}

func lockedModelCapabilitiesForConfig(model string, override config.ModelCapabilitiesOverride, provider llm.ProviderCapabilities, sources map[string]config.Origin, reasoningKey string, visionKey string) session.LockedModelCapabilities {
	locked := llm.LockedModelCapabilitiesForModel(model, provider)
	reasoningConfigured := inheritedModelCapabilitySourceConfigured(sources, reasoningKey)
	visionConfigured := inheritedModelCapabilitySourceConfigured(sources, visionKey)
	if reasoningConfigured || override.SupportsReasoningEffort {
		locked.SupportsReasoningEffort = override.SupportsReasoningEffort
	}
	if visionConfigured || override.SupportsVisionInputs {
		locked.SupportsVisionInputs = override.SupportsVisionInputs
	}
	return locked
}

func inheritedModelCapabilitySourceConfigured(sources map[string]config.Origin, key string) bool {
	if modelCapabilitySourceConfigured(sources, key) {
		return true
	}
	switch key {
	case "reviewer.model_capabilities.supports_reasoning_effort":
		return modelCapabilitySourceConfigured(sources, "model_capabilities.supports_reasoning_effort")
	case "reviewer.model_capabilities.supports_vision_inputs":
		return modelCapabilitySourceConfigured(sources, "model_capabilities.supports_vision_inputs")
	default:
		return false
	}
}

func modelCapabilitySourceConfigured(sources map[string]config.Origin, key string) bool {
	return sources[key].Configured()
}

func boolRef(v bool) *bool { return &v }
