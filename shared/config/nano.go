package config

import (
	"fmt"
	"strings"
)

type NanoSettings struct {
	Connection              *ConnectionSelection
	Model                   *string
	ThinkingLevel           *string
	ModelVerbosity          *ModelVerbosity
	ModelContextWindow      *int
	SupportsReasoningEffort *bool
	SupportsVisionInputs    *bool
	TimeoutSeconds          int
	SessionNaming           bool
}

type nanoSectionSetting struct {
	settings []registrySetting
}

func newNanoSectionSetting() nanoSectionSetting {
	return nanoSectionSetting{settings: []registrySetting{
		newConnectionReference("nano.connection", true,
			func(s *settingsState, v *ConnectionSelection) { s.Settings.Nano.Connection = v },
			func(s settingsState) *ConnectionSelection { return s.Settings.Nano.Connection }),
		newOptionalStringSetting("nano.model",
			func(s *settingsState, v *string) { s.Settings.Nano.Model = v },
			func(s settingsState) *string { return s.Settings.Nano.Model }, "", settingDocOptions{}),
		newOptionalStringSetting("nano.thinking_level",
			func(s *settingsState, v *string) { s.Settings.Nano.ThinkingLevel = v },
			func(s settingsState) *string { return s.Settings.Nano.ThinkingLevel }, "", settingDocOptions{}),
		nanoOptionalScalar(newStringSetting("nano.model_verbosity", ModelVerbosityLow,
			nil, nil, "", nil, normalizeModelVerbosity, settingDocOptions{rejectEmptyString: true}),
			func(s *settingsState, v *ModelVerbosity) { s.Settings.Nano.ModelVerbosity = v },
			func(s settingsState) *ModelVerbosity { return s.Settings.Nano.ModelVerbosity }),
		nanoOptionalScalar(newIntSetting("nano.model_context_window", 0, nil, nil, "", nil, settingDocOptions{}),
			func(s *settingsState, v *int) { s.Settings.Nano.ModelContextWindow = v },
			func(s settingsState) *int { return s.Settings.Nano.ModelContextWindow }),
		nanoOptionalScalar(newBoolSetting("nano.model_capabilities.supports_reasoning_effort", false, nil, nil, "", settingDocOptions{}),
			func(s *settingsState, v *bool) { s.Settings.Nano.SupportsReasoningEffort = v },
			func(s settingsState) *bool { return s.Settings.Nano.SupportsReasoningEffort }),
		nanoOptionalScalar(newBoolSetting("nano.model_capabilities.supports_vision_inputs", false, nil, nil, "", settingDocOptions{}),
			func(s *settingsState, v *bool) { s.Settings.Nano.SupportsVisionInputs = v },
			func(s settingsState) *bool { return s.Settings.Nano.SupportsVisionInputs }),
		newIntSetting("nano.timeout_seconds", 120,
			func(s *settingsState, v int) { s.Settings.Nano.TimeoutSeconds = v },
			func(s settingsState) int { return s.Settings.Nano.TimeoutSeconds }, "", nil, settingDocOptions{}),
		newBoolSetting("nano.features.session_naming", true,
			func(s *settingsState, v bool) { s.Settings.Nano.SessionNaming = v },
			func(s settingsState) bool { return s.Settings.Nano.SessionNaming }, "", settingDocOptions{}),
	}}
}

func nanoOptionalScalar[T comparable](base scalarSetting[T], apply func(*settingsState, *T), get func(settingsState) *T) scalarSetting[*T] {
	return scalarSetting[*T]{
		key: base.key, apply: apply, get: get,
		equal: func(a, b *T) bool { return a == b || a != nil && b != nil && *a == *b },
		decodeFile: func(raw settingsFile, path []string) (*T, bool, error) {
			v, ok, err := base.decodeFile(raw, path)
			if err != nil || !ok {
				return nil, ok, err
			}
			return &v, true, nil
		},
		doc: settingDocOptions{
			defaultValue: func(s settingsState) any {
				if v := get(s); v != nil {
					return *v
				}
				return nil
			},
		},
	}
}

func (nanoSectionSetting) applyDefault(*settingsState) {}
func (nanoSectionSetting) appliesToSubagentRole() bool { return false }

func (n nanoSectionSetting) registerFileKeys(tree *fileKeyTree) {
	for _, setting := range n.settings {
		setting.(fileKeyRegisteringSetting).registerFileKeys(tree)
	}
}

func (n nanoSectionSetting) initSources(sources map[string]Origin) {
	for _, setting := range n.settings {
		setting.(sourceInitializingSetting).initSources(sources)
	}
}

func (n nanoSectionSetting) applyFile(raw settingsFile, file SourceFile, state *settingsState, sources map[string]Origin) error {
	_, present, err := lookupFileTable(raw, []string{"nano"})
	if err != nil || !present {
		return err
	}
	settings := NanoSettings{TimeoutSeconds: 120, SessionNaming: true}
	if state.Settings.Nano != nil {
		settings = *state.Settings.Nano
	}
	state.Settings.Nano = &settings
	for _, setting := range n.settings {
		if err := setting.applyFile(raw, file, state, sources); err != nil {
			return err
		}
	}
	return nil
}

func (n nanoSectionSetting) appendDefaultLines(lines *[]defaultConfigLine, state settingsState) {
	if state.Settings.Nano == nil {
		return
	}
	*lines = append(*lines, defaultConfigLine{Path: []string{"nano", "concurrency"}, Value: state.Settings.NanoConcurrency})
	if selection := state.Settings.Nano.Connection; selection != nil {
		*lines = append(*lines, defaultConfigLine{Path: []string{"nano", "connection"}, Value: selection.TOMLValue()})
	}
	for _, setting := range n.settings {
		var added []defaultConfigLine
		setting.(defaultLineAppendingSetting).appendDefaultLines(&added, state)
		for _, line := range added {
			if line.Value != nil {
				*lines = append(*lines, line)
			}
		}
	}
}

func ValidateNanoSettings(settings Settings) error {
	return validateNano(settingsState{Settings: settings}, nil)
}

func validateNano(state settingsState, sources map[string]Origin) error {
	if state.Settings.NanoConcurrency < 1 {
		return configurationValidationError(fmt.Errorf("nano.concurrency must be positive"), sources, "nano.concurrency")
	}
	n := state.Settings.Nano
	if n == nil {
		return nil
	}
	if n.Connection == nil || n.Model == nil || strings.TrimSpace(*n.Model) == "" {
		return configurationValidationError(fmt.Errorf("nano requires an explicit connection and model"), sources, "nano.connection", "nano.model")
	}
	if len(*n.Connection) == 0 {
		return fmt.Errorf("nano.connection must contain at least one connection")
	}
	selected := state.Settings
	selected.Connection = n.Connection
	if _, err := selected.ConnectionMembers(); err != nil {
		return err
	}
	if n.ModelContextWindow != nil && *n.ModelContextWindow < 2500 {
		return configurationValidationError(fmt.Errorf("nano.model_context_window must be at least 2500"), sources, "nano.model_context_window")
	}
	if n.TimeoutSeconds < 1 {
		return configurationValidationError(fmt.Errorf("nano.timeout_seconds must be positive"), sources, "nano.timeout_seconds")
	}
	if n.ModelVerbosity != nil {
		switch *n.ModelVerbosity {
		case ModelVerbosityLow, ModelVerbosityMedium, ModelVerbosityHigh:
		default:
			return configurationValidationError(fmt.Errorf("invalid nano.model_verbosity"), sources, "nano.model_verbosity")
		}
	}
	return nil
}
