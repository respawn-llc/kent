package config

import (
	"fmt"

	"core/shared/runtimeids"
)

func (app App) ValidateDeclarationEvidence() error {
	for key := range configRegistry.defaultSourceMap() {
		origin, present := app.Source.Sources[key]
		if !present || origin.Property.Key == "" {
			return fmt.Errorf("configuration declaration evidence is required for %s", key)
		}
		switch origin.Kind {
		case SourceDefault, SourceFileKind, SourceEnv, SourceCLI, SourceInput, SourceSession:
		default:
			return fmt.Errorf("configuration declaration origin for %s is invalid", key)
		}
	}
	for name := range app.Settings.SkillToggles {
		key := skillSourceKey(name)
		if _, present := app.Source.Sources[key]; !present {
			return fmt.Errorf("configuration declaration evidence is required for %s", key)
		}
	}
	return nil
}

// Origin identifies the declaration, even when its value is inherited under a
// different effective property key.
type Origin struct {
	Kind              SourceKind            `json:"kind"`
	Property          PropertyAddress       `json:"property"`
	File              *SourceFile           `json:"file,omitempty"`
	Option            *string               `json:"option,omitempty"`
	RetainedSessionID *runtimeids.SessionID `json:"retained_session_id,omitempty"`
}

type PropertyAddress struct {
	Key  string  `json:"key"`
	Role *string `json:"role,omitempty"`
}

func (p PropertyAddress) String() string {
	if p.Role != nil {
		return "subagents." + *p.Role + "." + p.Key
	}
	return p.Key
}

type SourceKind string

const (
	SourceDefault  SourceKind = "default"
	SourceFileKind SourceKind = "file"
	SourceEnv      SourceKind = "env"
	SourceCLI      SourceKind = "cli"
	SourceInput    SourceKind = "input"
	SourceSession  SourceKind = "session"
)

type FileLayer string

const (
	FileGlobal    FileLayer = "global"
	FileWorkspace FileLayer = "workspace"
	FilePrivate   FileLayer = "private"
)

type SourceFile struct {
	Layer FileLayer `json:"layer"`
	Path  string    `json:"path"`
}

func defaultOrigin(key string) Origin {
	return Origin{Kind: SourceDefault, Property: PropertyAddress{Key: key}}
}

func fileOrigin(key string, file SourceFile) Origin {
	return Origin{Kind: SourceFileKind, Property: PropertyAddress{Key: key}, File: &file}
}

func optionOrigin(key string, kind SourceKind, option string) Origin {
	return Origin{Kind: kind, Property: PropertyAddress{Key: key}, Option: &option}
}

func (o Origin) Configured() bool {
	return o.Kind == SourceFileKind || o.Kind == SourceEnv || o.Kind == SourceCLI || o.Kind == SourceInput
}

func (o Origin) Declares(key string) bool {
	return o.Configured() && o.Property.Key == key
}

func (o Origin) Inherited(key string) bool {
	return o.Kind == SourceDefault || (o.Configured() && o.Property.Key != key)
}

func (o Origin) OverridesRole(key string) bool {
	return (o.Kind == SourceEnv || o.Kind == SourceCLI) && (o.Property.Key == key || o.Property.Key == "tools")
}

func inheritSource(sources map[string]Origin, target, source string) {
	if sources == nil {
		return
	}
	if origin, exists := sources[source]; exists {
		sources[target] = origin
	} else {
		delete(sources, target)
	}
}

func (s SourceReport) File(layer FileLayer) *ConfigFileReport {
	for _, file := range s.Files {
		if file.Layer == layer {
			return &file
		}
	}
	return nil
}

func (s SourceReport) SettingsFileExists() bool {
	for _, file := range s.Files {
		if file.Exists {
			return true
		}
	}
	return false
}

func (s SourceReport) SettingsPath() *string {
	var selected *string
	for _, layer := range []FileLayer{FileGlobal, FileWorkspace, FilePrivate} {
		file := s.File(layer)
		if file != nil && file.Enabled && (file.Exists || layer == FileGlobal) {
			selected = &file.Path
		}
	}
	return selected
}
