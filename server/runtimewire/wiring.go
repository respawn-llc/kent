package runtimewire

import (
	"net/http"
	"strings"
	"time"

	"builder/server/auth"
	"builder/server/llm"
	"builder/server/runtime"
	"builder/server/session"
	askquestion "builder/server/tools/askquestion"
	shelltool "builder/server/tools/shell"
	triggerhandofftool "builder/server/tools/triggerhandoff"
	"builder/server/workflowruntime"
	"builder/shared/config"
	"builder/shared/toolspec"
)

type RuntimeWiring struct {
	Engine        *runtime.Engine
	AskBroker     *askquestion.Broker
	EventBridge   *EventBridge
	Background    *shelltool.Manager
	LocalTools    *LocalToolRegistryBinding
	PromptHistory []string
}

func (w *RuntimeWiring) Close() error {
	if w == nil || w.Engine == nil {
		return nil
	}
	return w.Engine.Close()
}

type RuntimeWiringOptions struct {
	OnEvent     func(evt runtime.Event)
	Headless    bool
	FastMode    *runtime.FastModeState
	Sources     map[string]string
	Client      llm.Client
	WorkflowRun *workflowruntime.Config
}

func NewRuntimeWiring(store *session.Store, active config.Settings, enabledTools []toolspec.ID, workspaceRoot string, mgr *auth.Manager, logger Logger, opts RuntimeWiringOptions) (*RuntimeWiring, error) {
	return NewRuntimeWiringWithBackground(store, active, enabledTools, workspaceRoot, mgr, logger, nil, opts)
}

func NewRuntimeWiringWithBackground(store *session.Store, active config.Settings, enabledTools []toolspec.ID, workspaceRoot string, mgr *auth.Manager, logger Logger, background *shelltool.Manager, opts RuntimeWiringOptions) (*RuntimeWiring, error) {
	promptHistory, err := store.ReadPromptHistory()
	if err != nil {
		return nil, err
	}

	var eng *runtime.Engine
	localTools, askBroker, background, err := NewLocalToolRegistryBinding(
		workspaceRoot,
		store.Meta().SessionID,
		enabledTools,
		time.Duration(active.MinimumExecToBgSeconds)*time.Second,
		active.ShellOutputMaxChars,
		active.AllowNonCwdEdits,
		llm.LockedContractSupportsVisionInputs(store.Meta().Locked, active.Model),
		logger,
		background,
		func() triggerhandofftool.Controller { return eng },
	)
	if err != nil {
		return nil, err
	}
	toolRegistry := localTools.Registry()

	mainProvider := mainProviderRuntimeSettings(active)
	var client llm.Client
	if opts.Client != nil {
		client = opts.Client
	} else {
		client, err = newRuntimeProviderClient(mainProvider, mgr, llm.NewHTTPClient(time.Duration(active.Timeouts.ModelRequestSeconds)*time.Second))
		if err != nil {
			return nil, err
		}
	}

	reviewerProvider := reviewerProviderRuntimeSettings(active)
	newReviewerClient := func() (llm.Client, error) {
		return newRuntimeProviderClient(reviewerProvider, mgr, llm.NewHTTPClient(time.Duration(active.Reviewer.TimeoutSeconds)*time.Second))
	}

	var reviewerClient llm.Client
	if strings.ToLower(strings.TrimSpace(active.Reviewer.Frequency)) != "off" {
		reviewerClient, err = newReviewerClient()
		if err != nil {
			return nil, err
		}
	}

	eventBridge := NewEventBridge(2048, func(total uint64, evt runtime.Event) {
		if logger == nil {
			return
		}
		if total == 1 || total%100 == 0 {
			logger.Logf("runtime.event.drop count=%d kind=%s step_id=%s", total, evt.Kind, evt.StepID)
		}
	})
	eng, err = runtime.New(store, client, toolRegistry, runtime.Config{
		Model:                         active.Model,
		Temperature:                   1,
		MaxTokens:                     0,
		ThinkingLevel:                 active.ThinkingLevel,
		ModelCapabilities:             llm.LockedModelCapabilitiesForConfig(active.Model, active.ModelCapabilities),
		FastModeEnabled:               active.PriorityRequestMode,
		FastModeState:                 opts.FastMode,
		WebSearchMode:                 active.WebSearch,
		ProviderCapabilitiesOverride:  mainProvider.ProviderCapabilitiesOverride,
		EnabledTools:                  enabledTools,
		DisabledSkills:                config.DisabledSkillToggles(active),
		SubagentCatalogSettings:       active,
		SystemPromptFiles:             active.SystemPromptFiles,
		AutoCompactTokenLimit:         active.ContextCompactionThresholdTokens,
		PreSubmitCompactionLeadTokens: active.PreSubmitCompactionLeadTokens,
		ContextWindowTokens:           active.ModelContextWindow,
		EffectiveContextWindowPercent: 95,
		LocalCompactionCarryoverLimit: 20_000,
		CompactionMode:                string(active.CompactionMode),
		CacheWarningMode:              active.CacheWarningMode,
		AutoCompactionEnabled:         boolRef(true),
		HeadlessMode:                  opts.Headless,
		ToolPreambles:                 active.ToolPreambles,
		WorkflowRun:                   opts.WorkflowRun,
		TranscriptWorkingDir:          workspaceRoot,
		Reviewer: runtime.ReviewerConfig{
			Frequency:         active.Reviewer.Frequency,
			Model:             active.Reviewer.Model,
			ThinkingLevel:     active.Reviewer.ThinkingLevel,
			ModelCapabilities: lockedModelCapabilitiesForConfig(active.Reviewer.Model, active.Reviewer.ModelCapabilities, opts.Sources, "reviewer.model_capabilities.supports_reasoning_effort", "reviewer.model_capabilities.supports_vision_inputs"),
			SystemPromptFile:  active.Reviewer.SystemPromptFile,
			VerboseOutput:     active.Reviewer.VerboseOutput,
			Client:            reviewerClient,
			ClientFactory:     newReviewerClient,
		},
		OnEvent: func(evt runtime.Event) {
			if opts.OnEvent != nil {
				opts.OnEvent(evt)
			}
			eventBridge.Publish(evt)
		},
	})
	if err != nil {
		return nil, err
	}
	return &RuntimeWiring{
		Engine:        eng,
		AskBroker:     askBroker,
		EventBridge:   eventBridge,
		Background:    background,
		LocalTools:    localTools,
		PromptHistory: append([]string(nil), promptHistory...),
	}, nil
}

type providerRuntimeSettings struct {
	Model                        string
	ProviderOverride             string
	OpenAIBaseURL                string
	ModelVerbosity               config.ModelVerbosity
	Store                        bool
	ContextWindowTokens          int
	Auth                         string
	ProviderCapabilitiesOverride *llm.ProviderCapabilities
}

func mainProviderRuntimeSettings(active config.Settings) providerRuntimeSettings {
	return providerRuntimeSettings{
		Model:                        active.Model,
		ProviderOverride:             active.ProviderOverride,
		OpenAIBaseURL:                active.OpenAIBaseURL,
		ModelVerbosity:               active.ModelVerbosity,
		Store:                        active.Store,
		ContextWindowTokens:          active.ModelContextWindow,
		Auth:                         "inherit",
		ProviderCapabilitiesOverride: providerCapabilitiesOverridePtr(active.ProviderCapabilities),
	}
}

func lockedModelCapabilitiesForConfig(model string, override config.ModelCapabilitiesOverride, sources map[string]string, reasoningKey string, visionKey string) session.LockedModelCapabilities {
	locked := llm.LockedModelCapabilitiesForModel(model)
	reasoningConfigured := inheritedModelCapabilitySourceConfigured(sources, reasoningKey)
	visionConfigured := inheritedModelCapabilitySourceConfigured(sources, visionKey)
	if reasoningConfigured {
		locked.SupportsReasoningEffort = override.SupportsReasoningEffort
	}
	if visionConfigured {
		locked.SupportsVisionInputs = override.SupportsVisionInputs
	}
	if reasoningConfigured || visionConfigured {
		return locked
	}
	return llm.LockedModelCapabilitiesForConfig(model, override)
}

func inheritedModelCapabilitySourceConfigured(sources map[string]string, key string) bool {
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

func modelCapabilitySourceConfigured(sources map[string]string, key string) bool {
	switch strings.TrimSpace(sources[key]) {
	case "file", "env", "cli", "subagent":
		return true
	default:
		return false
	}
}

func reviewerProviderRuntimeSettings(active config.Settings) providerRuntimeSettings {
	reviewer := config.EffectiveReviewerSettings(active)
	reviewerProvider := config.ResolveReviewerProviderSettings(config.Settings{
		ProviderOverride: active.ProviderOverride,
		OpenAIBaseURL:    active.OpenAIBaseURL,
		Reviewer:         reviewer,
	})
	return providerRuntimeSettings{
		Model:                        reviewer.Model,
		ProviderOverride:             reviewerProvider.ProviderOverride,
		OpenAIBaseURL:                reviewerProvider.OpenAIBaseURL,
		ModelVerbosity:               reviewer.ModelVerbosity,
		Store:                        false,
		ContextWindowTokens:          reviewer.ModelContextWindow,
		Auth:                         reviewer.Auth,
		ProviderCapabilitiesOverride: providerCapabilitiesOverridePtr(reviewer.ProviderCapabilities),
	}
}

func providerCapabilitiesOverridePtr(override config.ProviderCapabilitiesOverride) *llm.ProviderCapabilities {
	caps, ok := llm.ProviderCapabilitiesFromOverride(override)
	if !ok {
		return nil
	}
	return &caps
}

func newRuntimeProviderClient(settings providerRuntimeSettings, mgr *auth.Manager, httpClient *http.Client) (llm.Client, error) {
	return llm.NewProviderClient(llm.ProviderClientOptions{
		Provider:                     llm.Provider(strings.TrimSpace(settings.ProviderOverride)),
		Model:                        settings.Model,
		Auth:                         authProviderForPolicy(settings.Auth, mgr),
		HTTPClient:                   httpClient,
		OpenAIBaseURL:                settings.OpenAIBaseURL,
		ModelVerbosity:               string(settings.ModelVerbosity),
		Store:                        settings.Store,
		ContextWindowTokens:          settings.ContextWindowTokens,
		ProviderCapabilitiesOverride: settings.ProviderCapabilitiesOverride,
	})
}

func authProviderForPolicy(policy string, mgr *auth.Manager) llm.AuthHeaderProvider {
	if mgr == nil || strings.EqualFold(strings.TrimSpace(policy), "none") {
		return nil
	}
	return mgr
}

func boolRef(v bool) *bool { return &v }
