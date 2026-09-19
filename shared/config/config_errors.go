package config

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

type ConfigurationFileError struct {
	Source SourceFile
	Err    error
}

func (e *ConfigurationFileError) Error() string {
	return fmt.Sprintf("%s configuration %s: %v", e.Source.Layer, e.Source.Path, e.Err)
}

func (e *ConfigurationFileError) Unwrap() error {
	return e.Err
}

type ConfigurationValidationError struct {
	Origins map[string]Origin
	Err     error
}

func (e *ConfigurationValidationError) Error() string {
	var details []string
	for _, key := range slices.Sorted(maps.Keys(e.Origins)) {
		origin := e.Origins[key]
		location := string(origin.Kind)
		if origin.File != nil {
			location = fmt.Sprintf("%s %s", origin.File.Layer, origin.File.Path)
		} else if origin.Option != nil {
			location = *origin.Option
		}
		details = append(details, fmt.Sprintf("%s from %s (%s)", key, location, origin.Property.String()))
	}
	return fmt.Sprintf("%v [%s]", e.Err, strings.Join(details, "; "))
}

func (e *ConfigurationValidationError) Unwrap() error {
	return e.Err
}

func configurationValidationError(err error, sources map[string]Origin, keys ...string) error {
	origins := make(map[string]Origin, len(keys))
	for _, key := range keys {
		if origin, present := sources[key]; present {
			origins[key] = origin
		}
	}
	return &ConfigurationValidationError{Origins: origins, Err: err}
}

// Sentinel and typed errors for configuration validation and loading. These let
// callers (and tests) match failures with errors.Is / errors.As instead of
// matching on error-message wording. Producers wrap these with %w while keeping
// human-readable context (offending field, value, computed bounds, etc.).
var (
	// errProtectedPersistenceRoot is returned when a Go test binary attempts to
	// use a real, non-isolated persistence root.
	errProtectedPersistenceRoot = newConfigError("refusing to use protected persistence root")

	// errSettingsFileAlreadyExists is returned by onboarding when a settings
	// file is already present and would be overwritten.
	errSettingsFileAlreadyExists = newConfigError("settings file already exists")

	// errInvalidSubagentKey is returned when a subagents table key does not
	// normalize to a usable role name (e.g. a reserved name).
	errInvalidSubagentKey = newConfigError("invalid subagents key")

	// errSubagentRole wraps any failure parsing or validating a single subagent
	// role; the role key is preserved in the wrapped message.
	errSubagentRole = newConfigError("invalid subagent role")

	// errPersistenceRootInConfigFile is returned when a config.toml still
	// declares the removed persistence_root setting. The config+data root is
	// set with the --persistence-root flag or the KENT_PERSISTENCE_ROOT env var.
	errPersistenceRootInConfigFile = newConfigError("persistence_root is no longer a config.toml setting; set the config and data root with the --persistence-root flag or the KENT_PERSISTENCE_ROOT environment variable")

	// errSubagentDescriptionTooLong is returned when a subagent description
	// exceeds the maximum length after normalization.
	errSubagentDescriptionTooLong = newConfigError("subagent description too long")

	// Validation-rule sentinels for individual settings fields.
	errInvalidProviderIdentifier      = newConfigError("invalid provider_identifier")
	errProviderCapabilitiesNeedID     = newConfigError("provider_capabilities.provider_id must not be empty when provider capability overrides are set")
	errInvalidModelVerbosity          = newConfigError("invalid model_verbosity")
	errReviewerContextWindowNegative  = newConfigError("reviewer.model_context_window must be >= 0")
	errModelContextWindowBelowMinimum = newConfigError("model context window below minimum")
	errInvalidCacheWarningMode        = newConfigError("invalid cache_warning_mode")

	// errCompactionThresholdBelowMinimum is returned when the configured
	// compaction threshold falls below the minimum percentage of the model
	// context window.
	errCompactionThresholdBelowMinimum = newConfigError("context_compaction_threshold_tokens below minimum window percent")

	// errPreSubmitThresholdBelowMinimum is returned when the pre-submit
	// compaction lead pushes the effective threshold below the minimum.
	errPreSubmitThresholdBelowMinimum = newConfigError("pre-submit compaction threshold below minimum window percent")

	// Workflow validation sentinels.
	errInvalidWorkflowSettings = newConfigError("invalid workflow settings")
	errWorkflowConcurrency     = newConfigError("workflow.concurrency must be > 0")
)

// configError is a comparable sentinel error type for config failures. It is
// used with errors.Is via wrapping.
type configError struct {
	msg string
}

func newConfigError(msg string) *configError {
	return &configError{msg: msg}
}

func (e *configError) Error() string {
	return e.msg
}

func IsModelContextWindowBelowMinimum(err error) bool {
	return errors.Is(err, errModelContextWindowBelowMinimum)
}

func IsSettingsFileAlreadyExists(err error) bool {
	return errors.Is(err, errSettingsFileAlreadyExists)
}

// UnknownSettingsKeysError reports settings keys that are not recognized. The
// offending keys are exposed so callers can match structurally with errors.As.
type UnknownSettingsKeysError struct {
	Keys []string
}

func (e *UnknownSettingsKeysError) Error() string {
	return fmt.Sprintf("unknown settings key(s): %s", strings.Join(e.Keys, ", "))
}

// SettingsKeyTypeError reports a settings key whose TOML value has the wrong
// type. The dotted key path and expected type are exposed for structural
// matching.
type SettingsKeyTypeError struct {
	Key          string
	ExpectedType string
}

func (e *SettingsKeyTypeError) Error() string {
	return fmt.Sprintf("invalid settings key %s: expected %s", e.Key, e.ExpectedType)
}

// SettingsFileLayerError reports a recognized setting placed in a config file
// layer where that setting is not allowed.
type SettingsFileLayerError struct {
	Key          string
	SettingsPath string
	Layer        string
}

func (e *SettingsFileLayerError) Error() string {
	return fmt.Sprintf("settings key %s is not allowed in %s config %s", e.Key, e.Layer, e.SettingsPath)
}

// DuplicateSettingsKeysError reports two settings keys that collapse to the same
// normalized form. The original keys and normalized form are exposed.
type DuplicateSettingsKeysError struct {
	Scope        string
	SettingsPath string
	KeyA         string
	KeyB         string
	Normalized   string
}

func (e *DuplicateSettingsKeysError) Error() string {
	return fmt.Sprintf(
		"duplicate %s keys in %s: %q and %q both normalize to %q",
		e.Scope, e.SettingsPath, e.KeyA, e.KeyB, e.Normalized,
	)
}
