package config

import (
	"core/shared/theme"
	"errors"
	"fmt"
	"strings"
)

const MaxSupportedSubagentDepth = 30

func validateModelNotEmpty(state settingsState, _ map[string]Origin) error {
	if strings.TrimSpace(state.Settings.Model) == "" {
		return errors.New("settings model must not be empty")
	}
	return nil
}

func validateProviderOverrideRequiresModel(state settingsState, sources map[string]Origin) error {
	if strings.TrimSpace(state.Settings.ProviderOverride) != "" && sources["model"].Kind == SourceDefault {
		return fmt.Errorf("%w; set model alongside provider_override", errProviderOverrideRequiresModel)
	}
	return nil
}

func validateProviderOverrideValue(state settingsState, _ map[string]Origin) error {
	switch strings.ToLower(strings.TrimSpace(state.Settings.ProviderOverride)) {
	case "", "openai", "anthropic":
		return nil
	default:
		return fmt.Errorf("%w %q (expected openai|anthropic)", errInvalidProviderOverride, state.Settings.ProviderOverride)
	}
}

func validateProviderIdentifier(state settingsState, _ map[string]Origin) error {
	identifier := state.Settings.ProviderIdentifier
	if identifier == "" {
		return fmt.Errorf("%w: value must not be empty", errInvalidProviderIdentifier)
	}
	for i := 0; i < len(identifier); i++ {
		if !isHTTPProductTokenByte(identifier[i]) {
			return fmt.Errorf("%w %q: expected an HTTP product token", errInvalidProviderIdentifier, identifier)
		}
	}
	return nil
}

func isHTTPProductTokenByte(value byte) bool {
	switch {
	case value >= '0' && value <= '9':
		return true
	case value >= 'A' && value <= 'Z':
		return true
	case value >= 'a' && value <= 'z':
		return true
	}
	switch value {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	default:
		return false
	}
}

func validateOpenAIBaseURL(state settingsState, _ map[string]Origin) error {
	provider := strings.ToLower(strings.TrimSpace(state.Settings.ProviderOverride))
	if strings.TrimSpace(state.Settings.OpenAIBaseURL) != "" && provider != "" && provider != "openai" {
		return fmt.Errorf("%w: provider_override %q; openai_base_url requires provider_override=openai or unset", errOpenAIBaseURLConflict, state.Settings.ProviderOverride)
	}
	return nil
}

func validateProviderCapabilitiesProviderID(state settingsState, sources map[string]Origin) error {
	capabilities := state.Settings.ProviderCapabilities
	if strings.TrimSpace(capabilities.ProviderID) != "" {
		return nil
	}
	if hasAnyConfiguredSource(sources, mainProviderCapabilitySourceKeys.values...) || hasProviderCapabilitiesOverride(capabilities) {
		return errProviderCapabilitiesNeedID
	}
	return nil
}

func validateModelVerbosity(state settingsState, _ map[string]Origin) error {
	switch strings.ToLower(strings.TrimSpace(string(state.Settings.ModelVerbosity))) {
	case "", "low", "medium", "high":
		return nil
	default:
		return fmt.Errorf("%w %q (expected low|medium|high)", errInvalidModelVerbosity, state.Settings.ModelVerbosity)
	}
}

func validateTheme(state settingsState, _ map[string]Origin) error {
	switch theme.Normalize(state.Settings.Theme) {
	case theme.Auto, theme.Light, theme.Dark:
		return nil
	default:
		return fmt.Errorf("invalid theme %q (expected auto|light|dark)", state.Settings.Theme)
	}
}

func validateNotificationMethod(state settingsState, _ map[string]Origin) error {
	switch strings.ToLower(strings.TrimSpace(state.Settings.NotificationMethod)) {
	case "auto", "osc9", "bel":
		return nil
	default:
		return fmt.Errorf("invalid notification_method %q (expected auto|osc9|bel)", state.Settings.NotificationMethod)
	}
}

func validateServerHost(state settingsState, _ map[string]Origin) error {
	if strings.TrimSpace(state.Settings.ServerHost) == "" {
		return fmt.Errorf("server_host must not be empty")
	}
	return nil
}

func validateServerPort(state settingsState, _ map[string]Origin) error {
	if state.Settings.ServerPort <= 0 || state.Settings.ServerPort > 65535 {
		return fmt.Errorf("server_port must be between 1 and 65535")
	}
	return nil
}

func validateWebSearch(state settingsState, _ map[string]Origin) error {
	switch strings.ToLower(strings.TrimSpace(state.Settings.WebSearch)) {
	case "off", "native":
		return nil
	case "custom":
		return fmt.Errorf("web_search=custom is not implemented yet")
	default:
		return fmt.Errorf("invalid web_search %q (expected off|native|custom)", state.Settings.WebSearch)
	}
}

func validateTimeouts(state settingsState, _ map[string]Origin) error {
	if state.Settings.Timeouts.ModelRequestSeconds <= 0 {
		return fmt.Errorf("timeouts.model_request_seconds must be > 0")
	}
	return nil
}

func validateShellOutputMaxChars(state settingsState, _ map[string]Origin) error {
	if state.Settings.ShellOutputMaxChars <= 0 {
		return fmt.Errorf("shell_output_max_chars must be > 0")
	}
	return nil
}

func validateMinimumExecToBgSeconds(state settingsState, _ map[string]Origin) error {
	if state.Settings.MinimumExecToBgSeconds <= 0 {
		return fmt.Errorf("minimum_exec_to_bg_seconds must be > 0")
	}
	return nil
}

func validateBGShellsOutput(state settingsState, _ map[string]Origin) error {
	switch strings.ToLower(strings.TrimSpace(string(state.Settings.BGShellsOutput))) {
	case "default", "verbose", "concise":
		return nil
	default:
		return fmt.Errorf("invalid bg_shells_output %q (expected default|verbose|concise)", state.Settings.BGShellsOutput)
	}
}

func validateShellPostprocessing(state settingsState, _ map[string]Origin) error {
	switch normalizeShellPostprocessingMode(string(state.Settings.Shell.PostprocessingMode)) {
	case ShellPostprocessingModeNone, ShellPostprocessingModeBuiltin, ShellPostprocessingModeUser, ShellPostprocessingModeAll:
	default:
		return fmt.Errorf("invalid shell.postprocessing_mode %q (expected none|builtin|user|all)", state.Settings.Shell.PostprocessingMode)
	}
	if state.Settings.Shell.PostprocessHook != nil && strings.TrimSpace(*state.Settings.Shell.PostprocessHook) == "" {
		return fmt.Errorf("shell.postprocess_hook cannot be empty; remove the setting to leave it unset")
	}
	return nil
}

func validateCacheWarningMode(state settingsState, _ map[string]Origin) error {
	switch strings.ToLower(strings.TrimSpace(string(state.Settings.CacheWarningMode))) {
	case "off", "default", "verbose":
		return nil
	default:
		return fmt.Errorf("%w %q (expected off|default|verbose)", errInvalidCacheWarningMode, state.Settings.CacheWarningMode)
	}
}

func validateWorkflowSettings(state settingsState, _ map[string]Origin) error {
	if value := state.Settings.Workflow.PreCompactionTokens; value != nil {
		if *value <= 0 {
			return fmt.Errorf("%w: workflow.pre_compaction_tokens must be > 0", errInvalidWorkflowSettings)
		}
		if *value > state.Settings.ContextCompactionThresholdTokens {
			return fmt.Errorf(
				"%w: workflow.pre_compaction_tokens must be <= context_compaction_threshold_tokens (%d)",
				errInvalidWorkflowSettings,
				state.Settings.ContextCompactionThresholdTokens,
			)
		}
	}
	if state.Settings.Workflow.CompletionMode == "" &&
		state.Settings.Workflow.Concurrency == 0 &&
		state.Settings.Workflow.MaxInvalidCompletionAttempts == 0 {
		return nil
	}
	switch state.Settings.Workflow.CompletionMode {
	case WorkflowCompletionModeAuto, WorkflowCompletionModeStructuredOutput, WorkflowCompletionModeTool, WorkflowCompletionModeShellCommand, WorkflowCompletionModeUnstructured:
	default:
		return fmt.Errorf("%w: invalid workflow.completion_mode %q (expected auto|structured_output|tool|shell_command|unstructured_output)", errInvalidWorkflowSettings, state.Settings.Workflow.CompletionMode)
	}
	if state.Settings.Workflow.Concurrency <= 0 {
		return fmt.Errorf("%w: %w must be > 0", errInvalidWorkflowSettings, errWorkflowConcurrency)
	}
	if state.Settings.Workflow.MaxInvalidCompletionAttempts <= 0 {
		return fmt.Errorf("%w: workflow.max_invalid_completion_attempts must be > 0", errInvalidWorkflowSettings)
	}
	return nil
}

func validateMaxSubagentDepth(state settingsState, _ map[string]Origin) error {
	if state.Settings.MaxSubagentDepth < 0 {
		return fmt.Errorf("max_subagent_depth must be between 0 and %d", MaxSupportedSubagentDepth)
	}
	if state.Settings.MaxSubagentDepth > MaxSupportedSubagentDepth {
		return fmt.Errorf(
			"max_subagent_depth must be between 0 and %d; Kent does not support recursion chains that deep",
			MaxSupportedSubagentDepth,
		)
	}
	return nil
}

type contextConstraints struct {
	window    *int
	threshold *int
	lead      *int
}

func resolvedContextConstraints(settings Settings) contextConstraints {
	return contextConstraints{
		window:    &settings.ModelContextWindow,
		threshold: &settings.ContextCompactionThresholdTokens,
		lead:      &settings.PreSubmitCompactionLeadTokens,
	}
}

func declaredContextConstraints(settings Settings, sources map[string]Origin) contextConstraints {
	constraints := resolvedContextConstraints(settings)
	if !sources["model_context_window"].Configured() {
		constraints.window = nil
	}
	if !sources["context_compaction_threshold_tokens"].Configured() {
		constraints.threshold = nil
	}
	if !sources["pre_submit_compaction_lead_tokens"].Configured() {
		constraints.lead = nil
	}
	return constraints
}

func validateContextConstraints(constraints contextConstraints) error {
	if constraints.threshold != nil && *constraints.threshold <= 0 {
		return fmt.Errorf("context_compaction_threshold_tokens must be > 0")
	}
	if constraints.window != nil {
		if err := validateModelContextWindowMinimum("model_context_window", *constraints.window); err != nil {
			return err
		}
	}
	if constraints.lead != nil && *constraints.lead <= 0 {
		return fmt.Errorf("pre_submit_compaction_lead_tokens must be > 0")
	}
	if constraints.window == nil || constraints.threshold == nil {
		return nil
	}
	window, threshold := *constraints.window, *constraints.threshold
	if threshold >= window {
		return fmt.Errorf("context_compaction_threshold_tokens must be < model_context_window")
	}
	minimumThreshold := MinimumThresholdTokens(window)
	if threshold < minimumThreshold {
		return fmt.Errorf(
			"%w: context_compaction_threshold_tokens must be >= %d (%d%% of model_context_window=%d)",
			errCompactionThresholdBelowMinimum,
			minimumThreshold,
			MinimumWindowPercent,
			window,
		)
	}
	if constraints.lead == nil {
		return nil
	}
	effectivePreSubmitThreshold := EffectivePreSubmitThresholdTokens(
		threshold,
		*constraints.lead,
	)
	if effectivePreSubmitThreshold < minimumThreshold {
		return fmt.Errorf(
			"%w: pre_submit_compaction_lead_tokens makes the effective pre-submit threshold %d, below %d (%d%% of model_context_window=%d)",
			errPreSubmitThresholdBelowMinimum,
			effectivePreSubmitThreshold,
			minimumThreshold,
			MinimumWindowPercent,
			window,
		)
	}
	return nil
}

func validateCompactionMode(state settingsState, _ map[string]Origin) error {
	switch strings.ToLower(strings.TrimSpace(string(state.Settings.CompactionMode))) {
	case "native", "local", "none":
		return nil
	default:
		return fmt.Errorf("invalid compaction_mode %q (expected native|local|none)", state.Settings.CompactionMode)
	}
}

func validateReviewer(state settingsState, sources map[string]Origin) error {
	reviewer := state.Settings.Reviewer
	switch strings.ToLower(strings.TrimSpace(reviewer.Frequency)) {
	case "off", "all", "edits":
	default:
		return configurationValidationError(fmt.Errorf("invalid reviewer.frequency %q (expected off|all|edits)", reviewer.Frequency), sources, "reviewer.frequency")
	}
	if strings.TrimSpace(reviewer.Model) == "" {
		return configurationValidationError(fmt.Errorf("reviewer.model must not be empty"), sources, "reviewer.model")
	}
	switch strings.ToLower(strings.TrimSpace(string(reviewer.ModelVerbosity))) {
	case "", "low", "medium", "high":
	default:
		return configurationValidationError(fmt.Errorf("invalid reviewer.model_verbosity %q (expected low|medium|high)", reviewer.ModelVerbosity), sources, "reviewer.model_verbosity")
	}
	provider := strings.ToLower(strings.TrimSpace(reviewer.ProviderOverride))
	switch provider {
	case "", "openai", "anthropic":
	default:
		return configurationValidationError(fmt.Errorf("%w %q (expected openai|anthropic)", errInvalidReviewerProvider, reviewer.ProviderOverride), sources, "reviewer.provider_override")
	}
	if strings.TrimSpace(reviewer.OpenAIBaseURL) != "" && provider != "" && provider != "openai" {
		return configurationValidationError(fmt.Errorf("reviewer.provider_override %q conflicts with reviewer.openai_base_url; reviewer.openai_base_url requires reviewer.provider_override=openai or unset", reviewer.ProviderOverride), sources, "reviewer.provider_override", "reviewer.openai_base_url")
	}
	if err := validateReviewerProviderCapabilities(reviewer.ProviderCapabilities, sources); err != nil {
		return configurationValidationError(err, sources, reviewerProviderCapabilityKeys...)
	}
	if reviewer.ModelContextWindow < 0 {
		return configurationValidationError(errReviewerContextWindowNegative, sources, "reviewer.model_context_window")
	}
	if err := validateModelContextWindowMinimum("reviewer.model_context_window", reviewer.ModelContextWindow); err != nil {
		return configurationValidationError(err, sources, "reviewer.model_context_window")
	}
	switch normalizeReviewerAuth(reviewer.Auth) {
	case "inherit":
	case "none":
	default:
		return configurationValidationError(fmt.Errorf("invalid reviewer.auth %q (expected inherit|none)", reviewer.Auth), sources, "reviewer.auth")
	}
	if reviewer.TimeoutSeconds <= 0 {
		return configurationValidationError(fmt.Errorf("reviewer.timeout_seconds must be > 0"), sources, "reviewer.timeout_seconds")
	}
	return nil
}

func validateModelContextWindowMinimum(field string, window int) error {
	if window >= minimumModelContextWindow {
		return nil
	}
	return fmt.Errorf(
		"%w: %s must be >= %d",
		errModelContextWindowBelowMinimum,
		field,
		minimumModelContextWindow,
	)
}

func validateReviewerProviderCapabilities(capabilities ProviderCapabilitiesOverride, sources map[string]Origin) error {
	if strings.TrimSpace(capabilities.ProviderID) != "" {
		return nil
	}
	if hasAnyConfiguredSource(sources, reviewerProviderCapabilitySourceKeys.values...) || hasProviderCapabilitiesOverride(capabilities) {
		return errReviewerProviderCapabilitiesNeedID
	}
	return nil
}

func hasConfiguredSource(sources map[string]Origin, key string) bool {
	return sources[key].Declares(key)
}

func validateSleepPreventionMode(state settingsState, _ map[string]Origin) error {
	switch state.Settings.PreventSleep {
	case SleepPreventionModeAlways, SleepPreventionModeActive, SleepPreventionModeNever:
		return nil
	default:
		return fmt.Errorf("invalid prevent_sleep %q (expected always|active|never)", state.Settings.PreventSleep)
	}
}

func normalizeCompactionMode(raw string) CompactionMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "native":
		return CompactionModeNative
	case "local":
		return CompactionModeLocal
	case "none":
		return CompactionModeNone
	default:
		return CompactionMode(strings.TrimSpace(raw))
	}
}

func normalizeCacheWarningMode(raw string) CacheWarningMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "off":
		return CacheWarningModeOff
	case "default":
		return CacheWarningModeDefault
	case "verbose":
		return CacheWarningModeVerbose
	default:
		return CacheWarningMode(strings.TrimSpace(raw))
	}
}

func normalizeShellPostprocessingMode(raw string) ShellPostprocessingMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "none":
		return ShellPostprocessingModeNone
	case "builtin":
		return ShellPostprocessingModeBuiltin
	case "user":
		return ShellPostprocessingModeUser
	case "all":
		return ShellPostprocessingModeAll
	default:
		return ShellPostprocessingMode(strings.TrimSpace(raw))
	}
}

func normalizeModelVerbosity(raw string) ModelVerbosity {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "low":
		return ModelVerbosityLow
	case "medium":
		return ModelVerbosityMedium
	case "high":
		return ModelVerbosityHigh
	default:
		return ModelVerbosity(strings.TrimSpace(raw))
	}
}

func normalizeReviewerAuth(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "inherit":
		return "inherit"
	case "none":
		return "none"
	default:
		return strings.TrimSpace(raw)
	}
}
