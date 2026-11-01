package protoapi

import (
	configurationpb "core/shared/protoapi/gen/kent/api/configuration"
	"fmt"

	"core/shared/config"
)

func enumToProto[Wire ~int32, Native ~string](values map[Wire]Native, value Native, field string) (Wire, error) {
	for wire, native := range values {
		if native == value {
			return wire, nil
		}
	}
	var zero Wire
	return zero, fmt.Errorf("%s %q is unsupported", field, value)
}

var modelVerbosityValues = map[configurationpb.ModelVerbosity]config.ModelVerbosity{
	configurationpb.ModelVerbosity_MODEL_VERBOSITY_UNSPECIFIED: "",
	configurationpb.ModelVerbosity_MODEL_VERBOSITY_LOW:         config.ModelVerbosityLow,
	configurationpb.ModelVerbosity_MODEL_VERBOSITY_MEDIUM:      config.ModelVerbosityMedium,
	configurationpb.ModelVerbosity_MODEL_VERBOSITY_HIGH:        config.ModelVerbosityHigh,
}

func modelVerbosityToProto(value config.ModelVerbosity) (configurationpb.ModelVerbosity, error) {
	return enumToProto(modelVerbosityValues, value, "model verbosity")
}

func modelVerbosityFromProto(value configurationpb.ModelVerbosity) config.ModelVerbosity {
	return modelVerbosityValues[value]
}

var compactionModeValues = map[configurationpb.CompactionMode]config.CompactionMode{
	configurationpb.CompactionMode_COMPACTION_MODE_UNSPECIFIED: "",
	configurationpb.CompactionMode_COMPACTION_MODE_NATIVE:      config.CompactionModeNative,
	configurationpb.CompactionMode_COMPACTION_MODE_LOCAL:       config.CompactionModeLocal,
	configurationpb.CompactionMode_COMPACTION_MODE_NONE:        config.CompactionModeNone,
}

func compactionModeToProto(value config.CompactionMode) (configurationpb.CompactionMode, error) {
	return enumToProto(compactionModeValues, value, "compaction mode")
}

func compactionModeFromProto(value configurationpb.CompactionMode) config.CompactionMode {
	return compactionModeValues[value]
}

var backgroundShellOutputValues = map[configurationpb.BackgroundShellOutputMode]config.BGShellsOutputMode{
	configurationpb.BackgroundShellOutputMode_BACKGROUND_SHELL_OUTPUT_MODE_UNSPECIFIED: "",
	configurationpb.BackgroundShellOutputMode_BACKGROUND_SHELL_OUTPUT_MODE_DEFAULT:     config.BGShellsOutputDefault,
	configurationpb.BackgroundShellOutputMode_BACKGROUND_SHELL_OUTPUT_MODE_VERBOSE:     config.BGShellsOutputVerbose,
	configurationpb.BackgroundShellOutputMode_BACKGROUND_SHELL_OUTPUT_MODE_CONCISE:     config.BGShellsOutputConcise,
}

func backgroundShellOutputToProto(value config.BGShellsOutputMode) (configurationpb.BackgroundShellOutputMode, error) {
	return enumToProto(backgroundShellOutputValues, value, "background shell output mode")
}

func backgroundShellOutputFromProto(value configurationpb.BackgroundShellOutputMode) config.BGShellsOutputMode {
	return backgroundShellOutputValues[value]
}

var shellPostprocessingValues = map[configurationpb.ShellPostprocessingMode]config.ShellPostprocessingMode{
	configurationpb.ShellPostprocessingMode_SHELL_POSTPROCESSING_MODE_UNSPECIFIED: "",
	configurationpb.ShellPostprocessingMode_SHELL_POSTPROCESSING_MODE_NONE:        config.ShellPostprocessingModeNone,
	configurationpb.ShellPostprocessingMode_SHELL_POSTPROCESSING_MODE_BUILTIN:     config.ShellPostprocessingModeBuiltin,
	configurationpb.ShellPostprocessingMode_SHELL_POSTPROCESSING_MODE_USER:        config.ShellPostprocessingModeUser,
	configurationpb.ShellPostprocessingMode_SHELL_POSTPROCESSING_MODE_ALL:         config.ShellPostprocessingModeAll,
}

func shellPostprocessingModeToProto(value config.ShellPostprocessingMode) (configurationpb.ShellPostprocessingMode, error) {
	return enumToProto(shellPostprocessingValues, value, "shell postprocessing mode")
}

func shellPostprocessingModeFromProto(value configurationpb.ShellPostprocessingMode) config.ShellPostprocessingMode {
	return shellPostprocessingValues[value]
}

var cacheWarningValues = map[configurationpb.CacheWarningMode]config.CacheWarningMode{
	configurationpb.CacheWarningMode_CACHE_WARNING_MODE_UNSPECIFIED: "",
	configurationpb.CacheWarningMode_CACHE_WARNING_MODE_OFF:         config.CacheWarningModeOff,
	configurationpb.CacheWarningMode_CACHE_WARNING_MODE_DEFAULT:     config.CacheWarningModeDefault,
	configurationpb.CacheWarningMode_CACHE_WARNING_MODE_VERBOSE:     config.CacheWarningModeVerbose,
}

func cacheWarningModeToProto(value config.CacheWarningMode) (configurationpb.CacheWarningMode, error) {
	return enumToProto(cacheWarningValues, value, "cache warning mode")
}

func cacheWarningModeFromProto(value configurationpb.CacheWarningMode) config.CacheWarningMode {
	return cacheWarningValues[value]
}

var workflowCompletionValues = map[configurationpb.WorkflowCompletionMode]config.WorkflowCompletionMode{
	configurationpb.WorkflowCompletionMode_WORKFLOW_COMPLETION_MODE_UNSPECIFIED:         "",
	configurationpb.WorkflowCompletionMode_WORKFLOW_COMPLETION_MODE_AUTO:                config.WorkflowCompletionModeAuto,
	configurationpb.WorkflowCompletionMode_WORKFLOW_COMPLETION_MODE_STRUCTURED_OUTPUT:   config.WorkflowCompletionModeStructuredOutput,
	configurationpb.WorkflowCompletionMode_WORKFLOW_COMPLETION_MODE_TOOL:                config.WorkflowCompletionModeTool,
	configurationpb.WorkflowCompletionMode_WORKFLOW_COMPLETION_MODE_SHELL_COMMAND:       config.WorkflowCompletionModeShellCommand,
	configurationpb.WorkflowCompletionMode_WORKFLOW_COMPLETION_MODE_UNSTRUCTURED_OUTPUT: config.WorkflowCompletionModeUnstructured,
}

func workflowCompletionModeToProto(value config.WorkflowCompletionMode) (configurationpb.WorkflowCompletionMode, error) {
	return enumToProto(workflowCompletionValues, value, "workflow completion mode")
}

func workflowCompletionModeFromProto(value configurationpb.WorkflowCompletionMode) config.WorkflowCompletionMode {
	return workflowCompletionValues[value]
}

var sleepPreventionValues = map[configurationpb.SleepPreventionMode]config.SleepPreventionMode{
	configurationpb.SleepPreventionMode_SLEEP_PREVENTION_MODE_UNSPECIFIED: "",
	configurationpb.SleepPreventionMode_SLEEP_PREVENTION_MODE_ALWAYS:      config.SleepPreventionModeAlways,
	configurationpb.SleepPreventionMode_SLEEP_PREVENTION_MODE_ACTIVE:      config.SleepPreventionModeActive,
	configurationpb.SleepPreventionMode_SLEEP_PREVENTION_MODE_NEVER:       config.SleepPreventionModeNever,
}

func sleepPreventionModeToProto(value config.SleepPreventionMode) (configurationpb.SleepPreventionMode, error) {
	return enumToProto(sleepPreventionValues, value, "sleep prevention mode")
}

func sleepPreventionModeFromProto(value configurationpb.SleepPreventionMode) config.SleepPreventionMode {
	return sleepPreventionValues[value]
}

var systemPromptFileScopeValues = map[configurationpb.SystemPromptFileScope]config.SystemPromptFileScope{
	configurationpb.SystemPromptFileScope_SYSTEM_PROMPT_FILE_SCOPE_HOME_CONFIG:      config.SystemPromptFileScopeHomeConfig,
	configurationpb.SystemPromptFileScope_SYSTEM_PROMPT_FILE_SCOPE_WORKSPACE_CONFIG: config.SystemPromptFileScopeWorkspaceConfig,
	configurationpb.SystemPromptFileScope_SYSTEM_PROMPT_FILE_SCOPE_SUBAGENT:         config.SystemPromptFileScopeSubagent,
}

func systemPromptFileScopeToProto(value config.SystemPromptFileScope) (configurationpb.SystemPromptFileScope, error) {
	return enumToProto(systemPromptFileScopeValues, value, "system prompt file scope")
}

func systemPromptFileScopeFromProto(value configurationpb.SystemPromptFileScope) config.SystemPromptFileScope {
	return systemPromptFileScopeValues[value]
}
