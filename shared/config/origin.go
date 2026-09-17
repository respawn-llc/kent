package config

// Origin identifies the declaration, even when its value is inherited under a
// different effective property key.
type Origin struct {
	Kind     SourceKind
	Property PropertyAddress
	File     *SourceFile
	Option   *string
}

type PropertyAddress struct {
	Key  string
	Role *string
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
	Layer FileLayer
	Path  string
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
