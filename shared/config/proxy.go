package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Proxy is process-local configuration, never Agent or Session settings.
type Proxy struct {
	HTTP    *string
	HTTPS   *string
	NoProxy *string
	Sources map[string]Origin
}

type proxyProperty struct {
	key string
	env string
	set func(*Proxy, *string)
}

var proxyProperties = []proxyProperty{
	{"http", "HTTP_PROXY", func(p *Proxy, v *string) { p.HTTP = v }},
	{"https", "HTTPS_PROXY", func(p *Proxy, v *string) { p.HTTPS = v }},
	{"no_proxy", "NO_PROXY", func(p *Proxy, v *string) { p.NoProxy = v }},
}

func ResolveServerProxy(root string, dotenv func(string) (string, bool)) (Proxy, error) {
	path, err := resolveSettingsFilePathInRoot(root)
	if err != nil {
		return Proxy{}, err
	}
	result := Proxy{Sources: make(map[string]Origin)}
	if err := result.applyFile(SourceFile{Layer: FileGlobal, Path: path}); err != nil {
		return Proxy{}, err
	}
	result.applyEnvironment(dotenv, func(key, name string) Origin {
		return Origin{Kind: SourceEnv, Property: PropertyAddress{Key: key}, File: &SourceFile{Layer: FileGlobal, Path: filepath.Join(root, ".env")}, Option: &name}
	})
	result.applyEnvironment(os.LookupEnv, func(key, name string) Origin {
		return optionOrigin(key, SourceEnv, name)
	})
	return result, nil
}

func resolveClientProxy(root string) (Proxy, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return Proxy{}, err
	}
	path, err := resolveSettingsFilePathInRoot(root)
	if err != nil {
		return Proxy{}, err
	}
	result := Proxy{Sources: make(map[string]Origin)}
	for _, file := range []SourceFile{
		{Layer: FileGlobal, Path: path},
		{Layer: FileWorkspace, Path: filepath.Join(cwd, ConfigDirName, "config.toml")},
		{Layer: FilePrivate, Path: filepath.Join(cwd, ConfigDirName, "config.local.toml")},
	} {
		if err := result.applyFile(file); err != nil {
			return Proxy{}, err
		}
	}
	result.applyEnvironment(os.LookupEnv, func(key, name string) Origin {
		return optionOrigin(key, SourceEnv, name)
	})
	return result, nil
}

func (p *Proxy) applyFile(file SourceFile) error {
	raw, _, err := readSettingsFile(file.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return &ConfigurationFileError{Source: file, Err: err}
	}
	for _, property := range proxyProperties {
		value, present, err := lookupFileString(raw, []string{"proxy", property.key})
		if err != nil {
			return &ConfigurationFileError{Source: file, Err: err}
		}
		if present {
			property.set(p, &value)
			p.Sources["proxy."+property.key] = fileOrigin("proxy."+property.key, file)
		}
	}
	return nil
}

func (p *Proxy) applyEnvironment(lookup func(string) (string, bool), origin func(string, string) Origin) {
	if lookup == nil {
		return
	}
	for _, property := range proxyProperties {
		for _, name := range []string{property.env, strings.ToLower(property.env)} {
			if value, ok := lookup(name); ok && strings.TrimSpace(value) != "" {
				value = strings.TrimSpace(value)
				property.set(p, &value)
				p.Sources["proxy."+property.key] = origin("proxy."+property.key, name)
				break
			}
		}
	}
}

// Proxy sections are client-local declarations in workspace files. They are
// recognized by the shared parser but never applied to server runtime settings.
type proxySetting struct{}

func (proxySetting) applyDefault(*settingsState) {}
func (proxySetting) applyFile(settingsFile, SourceFile, *settingsState, map[string]Origin) error {
	return nil
}
func (proxySetting) appliesToSubagentRole() bool { return false }
func (proxySetting) registerFileKeys(tree *fileKeyTree) {
	for _, property := range proxyProperties {
		tree.allowPath([]string{"proxy", property.key})
	}
}
