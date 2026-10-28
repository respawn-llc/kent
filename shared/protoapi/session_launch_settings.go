package protoapi

import (
	configurationpb "core/shared/protoapi/gen/kent/api/configuration"
	"fmt"
	"sort"

	"core/shared/config"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	"core/shared/toolspec"
)

func SessionSettingsToProto(settings config.Settings) (*configurationpb.Settings, error) {
	modelVerbosity, err := modelVerbosityToProto(settings.ModelVerbosity)
	if err != nil {
		return nil, err
	}
	compactionMode, err := compactionModeToProto(settings.CompactionMode)
	if err != nil {
		return nil, err
	}
	backgroundOutput, err := backgroundShellOutputToProto(settings.BGShellsOutput)
	if err != nil {
		return nil, err
	}
	shellMode, err := shellPostprocessingModeToProto(settings.Shell.PostprocessingMode)
	if err != nil {
		return nil, err
	}
	cacheWarningMode, err := cacheWarningModeToProto(settings.CacheWarningMode)
	if err != nil {
		return nil, err
	}
	workflowMode, err := workflowCompletionModeToProto(settings.Workflow.CompletionMode)
	if err != nil {
		return nil, err
	}
	sleepMode, err := sleepPreventionModeToProto(settings.PreventSleep)
	if err != nil {
		return nil, err
	}
	systemPromptFile, err := systemPromptFileToProto(settings.SystemPromptFile)
	if err != nil {
		return nil, err
	}
	enabledTools, err := enabledToolFactsToProto(settings.EnabledTools)
	if err != nil {
		return nil, err
	}
	subagents, err := subagentRolesToProto(settings)
	if err != nil {
		return nil, err
	}
	serverPort, err := Int32(settings.ServerPort, "server port")
	if err != nil {
		return nil, err
	}
	modelContextWindow, err := Int32(settings.ModelContextWindow, "model context window")
	if err != nil {
		return nil, err
	}
	compactionThreshold, err := Int32(settings.ContextCompactionThresholdTokens, "context compaction threshold")
	if err != nil {
		return nil, err
	}
	var preSubmitLead *int32
	if settings.PreSubmitCompactionLeadTokens != nil {
		value, err := Int32(*settings.PreSubmitCompactionLeadTokens, "pre-submit compaction lead")
		if err != nil {
			return nil, err
		}
		preSubmitLead = &value
	}
	minimumExec, err := Int32(settings.MinimumExecToBgSeconds, "minimum exec-to-background seconds")
	if err != nil {
		return nil, err
	}
	modelTimeout, err := Int32(settings.Timeouts.ModelRequestSeconds, "model request timeout")
	if err != nil {
		return nil, err
	}
	shellOutputMax, err := Int32(settings.ShellOutputMaxChars, "shell output maximum")
	if err != nil {
		return nil, err
	}
	worktreeTimeout, err := Int32(settings.Worktrees.SetupTimeoutSeconds, "worktree setup timeout")
	if err != nil {
		return nil, err
	}
	workflowConcurrency, err := Int32(settings.Workflow.Concurrency, "workflow concurrency")
	if err != nil {
		return nil, err
	}
	workflowAttempts, err := Int32(settings.Workflow.MaxInvalidCompletionAttempts, "workflow invalid completion attempts")
	if err != nil {
		return nil, err
	}
	maxSubagentDepth, err := Int32(settings.MaxSubagentDepth, "maximum subagent depth")
	if err != nil {
		return nil, err
	}
	reviewer, err := reviewerSettingsToProto(settings.Reviewer)
	if err != nil {
		return nil, err
	}
	connection, err := connectionIDToProto(settings.Connection)
	if err != nil {
		return nil, err
	}
	message := &configurationpb.Settings{
		Connection:                       connection,
		Model:                            settings.Model,
		ThinkingLevel:                    settings.ThinkingLevel,
		ModelVerbosity:                   modelVerbosity,
		SystemPromptFile:                 systemPromptFile,
		ModelCapabilities:                modelCapabilitiesToProto(settings.ModelCapabilities),
		Theme:                            settings.Theme,
		TuiNativeProgressBar:             settings.TUINativeProgressBar,
		NotificationMethod:               settings.NotificationMethod,
		ToolPreambles:                    settings.ToolPreambles,
		PriorityRequestMode:              settings.PriorityRequestMode,
		Debug:                            settings.Debug,
		ServerHost:                       settings.ServerHost,
		ServerPort:                       serverPort,
		WebSearch:                        settings.WebSearch,
		ProviderIdentifier:               settings.ProviderIdentifier,
		Store:                            settings.Store,
		AllowNonCwdEdits:                 settings.AllowNonCwdEdits,
		ModelContextWindow:               modelContextWindow,
		ContextCompactionThresholdTokens: compactionThreshold,
		PreSubmitCompactionLeadTokens:    preSubmitLead,
		MinimumExecToBgSeconds:           minimumExec,
		CompactionMode:                   compactionMode,
		EnabledTools:                     enabledTools,
		SkillToggles:                     booleanFactsToProto(settings.SkillToggles),
		Timeouts:                         &configurationpb.TimeoutSettings{ModelRequestSeconds: modelTimeout},
		ShellOutputMaxChars:              shellOutputMax,
		BgShellsOutput:                   backgroundOutput,
		Shell: &configurationpb.ShellSettings{
			PostprocessingMode: shellMode,
			PostprocessHook:    clonePointer(settings.Shell.PostprocessHook),
		},
		CacheWarningMode: cacheWarningMode,
		Worktrees: &configurationpb.WorktreeSettings{
			BaseDir: settings.Worktrees.BaseDir, SetupScript: settings.Worktrees.SetupScript,
			SetupTimeoutSeconds: worktreeTimeout,
		},
		Workflow: &configurationpb.WorkflowSettings{
			CompletionMode: workflowMode, Concurrency: workflowConcurrency,
			MaxInvalidCompletionAttempts: workflowAttempts,
			UseRequiredToolCalls:         settings.Workflow.UseRequiredToolCalls,
			Subagents:                    settings.Workflow.Subagents,
		},
		Reviewer:         reviewer,
		Subagents:        subagents,
		MaxSubagentDepth: maxSubagentDepth,
		PreventSleep:     sleepMode,
	}
	if settings.Workflow.PreCompactionTokens != nil {
		value, conversionErr := Int32(*settings.Workflow.PreCompactionTokens, "workflow pre-compaction tokens")
		if conversionErr != nil {
			return nil, conversionErr
		}
		message.Workflow.PreCompactionTokens = &value
	}
	return message, nil
}

func SessionSettingsFromProto(message *configurationpb.Settings) (config.Settings, error) {
	modelVerbosity := modelVerbosityFromProto(message.ModelVerbosity)
	compactionMode := compactionModeFromProto(message.CompactionMode)
	backgroundOutput := backgroundShellOutputFromProto(message.BgShellsOutput)
	shellMode := shellPostprocessingModeFromProto(message.Shell.PostprocessingMode)
	cacheWarningMode := cacheWarningModeFromProto(message.CacheWarningMode)
	workflowMode := workflowCompletionModeFromProto(message.Workflow.CompletionMode)
	sleepMode := sleepPreventionModeFromProto(message.PreventSleep)
	systemPromptFile, err := systemPromptFileFromProto(message.SystemPromptFile)
	if err != nil {
		return config.Settings{}, err
	}
	enabledTools, err := enabledToolFactsFromProto(message.EnabledTools)
	if err != nil {
		return config.Settings{}, err
	}
	skillToggles := booleanFactsFromProto(message.SkillToggles)
	subagents, err := subagentRolesFromProto(message.Subagents)
	if err != nil {
		return config.Settings{}, err
	}
	reviewer, err := reviewerSettingsFromProto(message.Reviewer)
	if err != nil {
		return config.Settings{}, err
	}
	connection := connectionIDFromProto(message.Connection)
	settings := config.Settings{
		Connection:                       connection,
		Model:                            message.Model,
		ThinkingLevel:                    message.ThinkingLevel,
		ModelVerbosity:                   modelVerbosity,
		SystemPromptFile:                 systemPromptFile,
		ModelCapabilities:                modelCapabilitiesFromProto(message.ModelCapabilities),
		Theme:                            message.Theme,
		TUINativeProgressBar:             message.TuiNativeProgressBar,
		NotificationMethod:               message.NotificationMethod,
		ToolPreambles:                    message.ToolPreambles,
		PriorityRequestMode:              message.PriorityRequestMode,
		Debug:                            message.Debug,
		ServerHost:                       message.ServerHost,
		ServerPort:                       int(message.ServerPort),
		WebSearch:                        message.WebSearch,
		ProviderIdentifier:               message.ProviderIdentifier,
		Store:                            message.Store,
		AllowNonCwdEdits:                 message.AllowNonCwdEdits,
		ModelContextWindow:               int(message.ModelContextWindow),
		ContextCompactionThresholdTokens: int(message.ContextCompactionThresholdTokens),
		MinimumExecToBgSeconds:           int(message.MinimumExecToBgSeconds),
		CompactionMode:                   compactionMode,
		EnabledTools:                     enabledTools,
		SkillToggles:                     skillToggles,
		Timeouts:                         config.Timeouts{ModelRequestSeconds: int(message.Timeouts.ModelRequestSeconds)},
		ShellOutputMaxChars:              int(message.ShellOutputMaxChars),
		BGShellsOutput:                   backgroundOutput,
		Shell: config.ShellSettings{
			PostprocessingMode: shellMode,
			PostprocessHook:    clonePointer(message.Shell.PostprocessHook),
		},
		CacheWarningMode: cacheWarningMode,
		Worktrees: config.WorktreeSettings{
			BaseDir: message.Worktrees.BaseDir, SetupScript: message.Worktrees.SetupScript,
			SetupTimeoutSeconds: int(message.Worktrees.SetupTimeoutSeconds),
		},
		Workflow: config.WorkflowSettings{
			CompletionMode: workflowMode, Concurrency: int(message.Workflow.Concurrency),
			MaxInvalidCompletionAttempts: int(message.Workflow.MaxInvalidCompletionAttempts),
			UseRequiredToolCalls:         message.Workflow.UseRequiredToolCalls,
			Subagents:                    message.Workflow.Subagents,
		},
		Reviewer:         reviewer,
		Subagents:        subagents,
		MaxSubagentDepth: int(message.MaxSubagentDepth),
		PreventSleep:     sleepMode,
	}
	if message.Workflow.PreCompactionTokens != nil {
		value := int(*message.Workflow.PreCompactionTokens)
		settings.Workflow.PreCompactionTokens = &value
	}
	if message.PreSubmitCompactionLeadTokens != nil {
		value := int(*message.PreSubmitCompactionLeadTokens)
		settings.PreSubmitCompactionLeadTokens = &value
	}
	return settings, nil
}

func reviewerSettingsToProto(settings config.ReviewerSettings) (*configurationpb.ReviewerSettings, error) {
	verbosity, err := modelVerbosityToProto(settings.ModelVerbosity)
	if err != nil {
		return nil, err
	}
	window, err := Int32(settings.ModelContextWindow, "reviewer model context window")
	if err != nil {
		return nil, err
	}
	timeout, err := Int32(settings.TimeoutSeconds, "reviewer timeout")
	if err != nil {
		return nil, err
	}
	connection, err := connectionIDToProto(settings.Connection)
	if err != nil {
		return nil, err
	}
	return &configurationpb.ReviewerSettings{
		Frequency: settings.Frequency, Model: settings.Model, ThinkingLevel: settings.ThinkingLevel,
		ModelVerbosity: verbosity, Connection: connection,
		ModelCapabilities:  modelCapabilitiesToProto(settings.ModelCapabilities),
		ModelContextWindow: window, SystemPromptFile: settings.SystemPromptFile,
		TimeoutSeconds: timeout, VerboseOutput: settings.VerboseOutput,
	}, nil
}

func reviewerSettingsFromProto(message *configurationpb.ReviewerSettings) (config.ReviewerSettings, error) {
	verbosity := modelVerbosityFromProto(message.ModelVerbosity)
	connection := connectionIDFromProto(message.Connection)
	return config.ReviewerSettings{
		Frequency: message.Frequency, Model: message.Model, ThinkingLevel: message.ThinkingLevel,
		ModelVerbosity: verbosity, Connection: connection,
		ModelCapabilities:  modelCapabilitiesFromProto(message.ModelCapabilities),
		ModelContextWindow: int(message.ModelContextWindow),
		SystemPromptFile:   message.SystemPromptFile, TimeoutSeconds: int(message.TimeoutSeconds),
		VerboseOutput: message.VerboseOutput,
	}, nil
}

func connectionIDToProto(selection *config.ConnectionSelection) (*string, error) {
	if selection == nil {
		return nil, nil
	}
	id, err := selection.ConcreteID()
	if err != nil {
		return nil, err
	}
	value := string(*id)
	return &value, nil
}

func connectionIDFromProto(raw *string) *config.ConnectionSelection {
	if raw == nil {
		return nil
	}
	id := config.ConnectionID(*raw)
	return config.SingleConnection(id)
}

func modelCapabilitiesToProto(value config.ModelCapabilitiesOverride) *configurationpb.ModelCapabilitiesOverride {
	return &configurationpb.ModelCapabilitiesOverride{
		SupportsReasoningEffort: value.SupportsReasoningEffort,
		SupportsVisionInputs:    value.SupportsVisionInputs,
	}
}

func modelCapabilitiesFromProto(value *configurationpb.ModelCapabilitiesOverride) config.ModelCapabilitiesOverride {
	return config.ModelCapabilitiesOverride{
		SupportsReasoningEffort: value.SupportsReasoningEffort,
		SupportsVisionInputs:    value.SupportsVisionInputs,
	}
}

func providerCapabilitiesToProto(value config.ProviderCapabilitiesOverride) *authpb.ProviderCapabilitiesOverride {
	return &authpb.ProviderCapabilitiesOverride{
		ProviderId: value.ProviderID, SupportsResponsesApi: value.SupportsResponsesAPI,
		SupportsFastMode:         value.SupportsFastMode,
		SupportsResponsesCompact: value.SupportsResponsesCompact,
		SupportsPromptCacheKey:   value.SupportsPromptCacheKey, SupportsNativeWebSearch: value.SupportsNativeWebSearch,
		SupportsReasoningEncrypted:    value.SupportsReasoningEncrypted,
		SupportsServerSideContextEdit: value.SupportsServerSideContextEdit,
		SupportsProviderVerbosity:     value.SupportsProviderVerbosity, IsOpenaiFirstParty: value.IsOpenAIFirstParty,
	}
}

func providerCapabilitiesFromProto(value *authpb.ProviderCapabilitiesOverride) config.ProviderCapabilitiesOverride {
	return config.ProviderCapabilitiesOverride{
		ProviderID: value.ProviderId, SupportsResponsesAPI: value.SupportsResponsesApi,
		SupportsFastMode:         value.SupportsFastMode,
		SupportsResponsesCompact: value.SupportsResponsesCompact,
		SupportsPromptCacheKey:   value.SupportsPromptCacheKey, SupportsNativeWebSearch: value.SupportsNativeWebSearch,
		SupportsReasoningEncrypted:    value.SupportsReasoningEncrypted,
		SupportsServerSideContextEdit: value.SupportsServerSideContextEdit,
		SupportsProviderVerbosity:     value.SupportsProviderVerbosity, IsOpenAIFirstParty: value.IsOpenaiFirstParty,
	}
}

func systemPromptFileToProto(value *config.SystemPromptFile) (*configurationpb.SystemPromptFile, error) {
	if value == nil {
		return nil, nil
	}
	scope, err := systemPromptFileScopeToProto(value.Scope)
	if err != nil {
		return nil, err
	}
	return &configurationpb.SystemPromptFile{Path: value.Path, Scope: scope}, nil
}

func systemPromptFileFromProto(value *configurationpb.SystemPromptFile) (*config.SystemPromptFile, error) {
	if value == nil {
		return nil, nil
	}
	scope := systemPromptFileScopeFromProto(value.Scope)
	return &config.SystemPromptFile{Path: value.Path, Scope: scope}, nil
}

func enabledToolFactsToProto(values map[toolspec.ID]bool) ([]*configurationpb.ToolEnabledFact, error) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, string(key))
	}
	sort.Strings(keys)
	result := make([]*configurationpb.ToolEnabledFact, 0, len(keys))
	for _, key := range keys {
		toolID, err := SessionToolIDToProto(toolspec.ID(key))
		if err != nil {
			return nil, err
		}
		result = append(result, &configurationpb.ToolEnabledFact{ToolId: toolID, Enabled: values[toolspec.ID(key)]})
	}
	return result, nil
}

func enabledToolFactsFromProto(values []*configurationpb.ToolEnabledFact) (map[toolspec.ID]bool, error) {
	result := make(map[toolspec.ID]bool, len(values))
	for _, value := range values {
		toolID := SessionToolIDFromProto(value.ToolId)
		result[toolID] = value.Enabled
	}
	return result, nil
}

func booleanFactsToProto(values map[string]bool) []*configurationpb.BooleanFact {
	keys := sortedStringKeys(values)
	result := make([]*configurationpb.BooleanFact, 0, len(keys))
	for _, key := range keys {
		result = append(result, &configurationpb.BooleanFact{Key: key, Value: values[key]})
	}
	return result
}

func booleanFactsFromProto(values []*configurationpb.BooleanFact) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value.Key] = value.Value
	}
	return result
}

func subagentRolesToProto(base config.Settings) ([]*configurationpb.NamedSubagentRole, error) {
	values := base.Subagents
	keys := sortedStringKeys(values)
	result := make([]*configurationpb.NamedSubagentRole, 0, len(keys))
	for _, key := range keys {
		value := values[key]
		declaration := config.MaterializeSubagentRoleDeclaration(base, value)
		selection := declaration.Connection
		declaration.Connection = nil
		settings, err := SessionSettingsToProto(declaration)
		if err != nil {
			return nil, fmt.Errorf("subagent %q settings: %w", key, err)
		}
		sources, err := sourceFactsToProto(value.Sources)
		if err != nil {
			return nil, err
		}
		result = append(result, &configurationpb.NamedSubagentRole{
			Name: key,
			Role: &configurationpb.SubagentRole{
				Settings: settings, Sources: sources, Description: value.Description,
				AgentCallable: value.AgentCallable, WorkflowSubagent: value.WorkflowSubagent,
				ConnectionSelection: ConnectionSelectionToProto(selection),
			},
		})
	}
	return result, nil
}

func subagentRolesFromProto(values []*configurationpb.NamedSubagentRole) (map[string]config.SubagentRole, error) {
	result := make(map[string]config.SubagentRole, len(values))
	for _, value := range values {
		settings, err := SessionSettingsFromProto(value.Role.Settings)
		if err != nil {
			return nil, fmt.Errorf("subagent %q settings: %w", value.Name, err)
		}
		settings.Connection = ConnectionSelectionFromProto(value.Role.ConnectionSelection)
		sources, err := sourceFactsFromProto(value.Role.Sources)
		if err != nil {
			return nil, fmt.Errorf("subagent %q sources: %w", value.Name, err)
		}
		result[value.Name] = config.SubagentRole{
			Settings: settings, Sources: sources, Description: value.Role.Description,
			AgentCallable: value.Role.AgentCallable, WorkflowSubagent: value.Role.WorkflowSubagent,
		}
	}
	return result, nil
}

func SessionSourceReportToProto(source config.SourceReport) (*configurationpb.SourceReport, error) {
	sources, err := sourceFactsToProto(source.Sources)
	if err != nil {
		return nil, err
	}
	message := &configurationpb.SourceReport{
		CreatedDefaultConfig: source.CreatedDefaultConfig,
		Sources:              sources,
	}
	for _, file := range source.Files {
		layer, err := configFileLayerToProto(file.Layer)
		if err != nil {
			return nil, err
		}
		message.Files = append(message.Files, &configurationpb.ConfigFileReport{
			File:   &configurationpb.ConfigFileSource{Layer: layer, Path: file.Path},
			Exists: file.Exists, Enabled: file.Enabled, Applied: file.Applied,
		})
	}
	return message, nil
}

func SessionSourceReportFromProto(message *configurationpb.SourceReport) (config.SourceReport, error) {
	sources, err := sourceFactsFromProto(message.Sources)
	if err != nil {
		return config.SourceReport{}, err
	}
	report := config.SourceReport{CreatedDefaultConfig: message.CreatedDefaultConfig, Sources: sources}
	for _, file := range message.Files {
		layer := configFileLayerFromProto(file.File.Layer)
		report.Files = append(report.Files, config.ConfigFileReport{
			SourceFile: config.SourceFile{Layer: layer, Path: file.File.Path},
			Exists:     file.Exists, Enabled: file.Enabled, Applied: file.Applied,
		})
	}
	return report, nil
}

func sortedStringKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
