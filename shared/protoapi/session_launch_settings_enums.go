package protoapi

import (
	"fmt"

	"core/shared/config"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
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

var modelVerbosityValues = map[sessionlaunchpb.ModelVerbosity]config.ModelVerbosity{
	sessionlaunchpb.ModelVerbosity_MODEL_VERBOSITY_UNSPECIFIED: "",
	sessionlaunchpb.ModelVerbosity_MODEL_VERBOSITY_LOW:         config.ModelVerbosityLow,
	sessionlaunchpb.ModelVerbosity_MODEL_VERBOSITY_MEDIUM:      config.ModelVerbosityMedium,
	sessionlaunchpb.ModelVerbosity_MODEL_VERBOSITY_HIGH:        config.ModelVerbosityHigh,
}

func modelVerbosityToProto(value config.ModelVerbosity) (sessionlaunchpb.ModelVerbosity, error) {
	return enumToProto(modelVerbosityValues, value, "model verbosity")
}

func modelVerbosityFromProto(value sessionlaunchpb.ModelVerbosity) config.ModelVerbosity {
	return modelVerbosityValues[value]
}

var compactionModeValues = map[sessionlaunchpb.CompactionMode]config.CompactionMode{
	sessionlaunchpb.CompactionMode_COMPACTION_MODE_UNSPECIFIED: "",
	sessionlaunchpb.CompactionMode_COMPACTION_MODE_NATIVE:      config.CompactionModeNative,
	sessionlaunchpb.CompactionMode_COMPACTION_MODE_LOCAL:       config.CompactionModeLocal,
	sessionlaunchpb.CompactionMode_COMPACTION_MODE_NONE:        config.CompactionModeNone,
}

func compactionModeToProto(value config.CompactionMode) (sessionlaunchpb.CompactionMode, error) {
	return enumToProto(compactionModeValues, value, "compaction mode")
}

func compactionModeFromProto(value sessionlaunchpb.CompactionMode) config.CompactionMode {
	return compactionModeValues[value]
}

var backgroundShellOutputValues = map[sessionlaunchpb.BackgroundShellOutputMode]config.BGShellsOutputMode{
	sessionlaunchpb.BackgroundShellOutputMode_BACKGROUND_SHELL_OUTPUT_MODE_UNSPECIFIED: "",
	sessionlaunchpb.BackgroundShellOutputMode_BACKGROUND_SHELL_OUTPUT_MODE_DEFAULT:     config.BGShellsOutputDefault,
	sessionlaunchpb.BackgroundShellOutputMode_BACKGROUND_SHELL_OUTPUT_MODE_VERBOSE:     config.BGShellsOutputVerbose,
	sessionlaunchpb.BackgroundShellOutputMode_BACKGROUND_SHELL_OUTPUT_MODE_CONCISE:     config.BGShellsOutputConcise,
}

func backgroundShellOutputToProto(value config.BGShellsOutputMode) (sessionlaunchpb.BackgroundShellOutputMode, error) {
	return enumToProto(backgroundShellOutputValues, value, "background shell output mode")
}

func backgroundShellOutputFromProto(value sessionlaunchpb.BackgroundShellOutputMode) config.BGShellsOutputMode {
	return backgroundShellOutputValues[value]
}

var shellPostprocessingValues = map[sessionlaunchpb.ShellPostprocessingMode]config.ShellPostprocessingMode{
	sessionlaunchpb.ShellPostprocessingMode_SHELL_POSTPROCESSING_MODE_UNSPECIFIED: "",
	sessionlaunchpb.ShellPostprocessingMode_SHELL_POSTPROCESSING_MODE_NONE:        config.ShellPostprocessingModeNone,
	sessionlaunchpb.ShellPostprocessingMode_SHELL_POSTPROCESSING_MODE_BUILTIN:     config.ShellPostprocessingModeBuiltin,
	sessionlaunchpb.ShellPostprocessingMode_SHELL_POSTPROCESSING_MODE_USER:        config.ShellPostprocessingModeUser,
	sessionlaunchpb.ShellPostprocessingMode_SHELL_POSTPROCESSING_MODE_ALL:         config.ShellPostprocessingModeAll,
}

func shellPostprocessingModeToProto(value config.ShellPostprocessingMode) (sessionlaunchpb.ShellPostprocessingMode, error) {
	return enumToProto(shellPostprocessingValues, value, "shell postprocessing mode")
}

func shellPostprocessingModeFromProto(value sessionlaunchpb.ShellPostprocessingMode) config.ShellPostprocessingMode {
	return shellPostprocessingValues[value]
}

var cacheWarningValues = map[sessionlaunchpb.CacheWarningMode]config.CacheWarningMode{
	sessionlaunchpb.CacheWarningMode_CACHE_WARNING_MODE_UNSPECIFIED: "",
	sessionlaunchpb.CacheWarningMode_CACHE_WARNING_MODE_OFF:         config.CacheWarningModeOff,
	sessionlaunchpb.CacheWarningMode_CACHE_WARNING_MODE_DEFAULT:     config.CacheWarningModeDefault,
	sessionlaunchpb.CacheWarningMode_CACHE_WARNING_MODE_VERBOSE:     config.CacheWarningModeVerbose,
}

func cacheWarningModeToProto(value config.CacheWarningMode) (sessionlaunchpb.CacheWarningMode, error) {
	return enumToProto(cacheWarningValues, value, "cache warning mode")
}

func cacheWarningModeFromProto(value sessionlaunchpb.CacheWarningMode) config.CacheWarningMode {
	return cacheWarningValues[value]
}

var workflowCompletionValues = map[sessionlaunchpb.WorkflowCompletionMode]config.WorkflowCompletionMode{
	sessionlaunchpb.WorkflowCompletionMode_WORKFLOW_COMPLETION_MODE_UNSPECIFIED:         "",
	sessionlaunchpb.WorkflowCompletionMode_WORKFLOW_COMPLETION_MODE_AUTO:                config.WorkflowCompletionModeAuto,
	sessionlaunchpb.WorkflowCompletionMode_WORKFLOW_COMPLETION_MODE_STRUCTURED_OUTPUT:   config.WorkflowCompletionModeStructuredOutput,
	sessionlaunchpb.WorkflowCompletionMode_WORKFLOW_COMPLETION_MODE_TOOL:                config.WorkflowCompletionModeTool,
	sessionlaunchpb.WorkflowCompletionMode_WORKFLOW_COMPLETION_MODE_SHELL_COMMAND:       config.WorkflowCompletionModeShellCommand,
	sessionlaunchpb.WorkflowCompletionMode_WORKFLOW_COMPLETION_MODE_UNSTRUCTURED_OUTPUT: config.WorkflowCompletionModeUnstructured,
}

func workflowCompletionModeToProto(value config.WorkflowCompletionMode) (sessionlaunchpb.WorkflowCompletionMode, error) {
	return enumToProto(workflowCompletionValues, value, "workflow completion mode")
}

func workflowCompletionModeFromProto(value sessionlaunchpb.WorkflowCompletionMode) config.WorkflowCompletionMode {
	return workflowCompletionValues[value]
}

var sleepPreventionValues = map[sessionlaunchpb.SleepPreventionMode]config.SleepPreventionMode{
	sessionlaunchpb.SleepPreventionMode_SLEEP_PREVENTION_MODE_UNSPECIFIED: "",
	sessionlaunchpb.SleepPreventionMode_SLEEP_PREVENTION_MODE_ALWAYS:      config.SleepPreventionModeAlways,
	sessionlaunchpb.SleepPreventionMode_SLEEP_PREVENTION_MODE_ACTIVE:      config.SleepPreventionModeActive,
	sessionlaunchpb.SleepPreventionMode_SLEEP_PREVENTION_MODE_NEVER:       config.SleepPreventionModeNever,
}

func sleepPreventionModeToProto(value config.SleepPreventionMode) (sessionlaunchpb.SleepPreventionMode, error) {
	return enumToProto(sleepPreventionValues, value, "sleep prevention mode")
}

func sleepPreventionModeFromProto(value sessionlaunchpb.SleepPreventionMode) config.SleepPreventionMode {
	return sleepPreventionValues[value]
}

var systemPromptFileScopeValues = map[sessionlaunchpb.SystemPromptFileScope]config.SystemPromptFileScope{
	sessionlaunchpb.SystemPromptFileScope_SYSTEM_PROMPT_FILE_SCOPE_HOME_CONFIG:      config.SystemPromptFileScopeHomeConfig,
	sessionlaunchpb.SystemPromptFileScope_SYSTEM_PROMPT_FILE_SCOPE_WORKSPACE_CONFIG: config.SystemPromptFileScopeWorkspaceConfig,
	sessionlaunchpb.SystemPromptFileScope_SYSTEM_PROMPT_FILE_SCOPE_SUBAGENT:         config.SystemPromptFileScopeSubagent,
}

func systemPromptFileScopeToProto(value config.SystemPromptFileScope) (sessionlaunchpb.SystemPromptFileScope, error) {
	return enumToProto(systemPromptFileScopeValues, value, "system prompt file scope")
}

func systemPromptFileScopeFromProto(value sessionlaunchpb.SystemPromptFileScope) config.SystemPromptFileScope {
	return systemPromptFileScopeValues[value]
}
