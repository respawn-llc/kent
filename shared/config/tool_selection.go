package config

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"core/shared/textutil"
	"core/shared/toolspec"
)

type ToolSelection struct {
	Tools  []toolspec.ID `json:"tools"`
	Origin Origin        `json:"origin"`
}

func (s ToolSelection) Validate() error {
	if (s.Origin.Kind != SourceEnv && s.Origin.Kind != SourceCLI) ||
		s.Origin.Property.Key != "tools" || s.Origin.Property.Role != nil ||
		s.Origin.Option == nil || strings.TrimSpace(*s.Origin.Option) == "" || s.Origin.File != nil {
		return errors.New("tool selection requires an explicit environment or CLI list origin")
	}
	seen := make(map[toolspec.ID]bool, len(s.Tools))
	for _, id := range s.Tools {
		parsed, valid := toolspec.ParseID(string(id))
		if !valid || parsed != id || seen[id] {
			return fmt.Errorf("invalid or duplicate selected tool %q", id)
		}
		seen[id] = true
	}
	return nil
}

func CloneToolSelection(selection *ToolSelection) *ToolSelection {
	if selection == nil {
		return nil
	}
	copy := *selection
	copy.Tools = slices.Clone(selection.Tools)
	copy.Origin.Option = textutil.Pointer(selection.Origin.Option)
	copy.Origin.RetainedSessionID = textutil.Pointer(selection.Origin.RetainedSessionID)
	return &copy
}

func ExplicitToolSelection(settings Settings, sources map[string]Origin) *ToolSelection {
	for _, id := range toolspec.CatalogIDs() {
		origin := sources[toolSourceKey(id)]
		if origin.OverridesRole(toolSourceKey(id)) && origin.Property.Key == "tools" && origin.RetainedSessionID == nil {
			return &ToolSelection{Tools: EnabledToolIDs(settings), Origin: origin}
		}
	}
	return nil
}

func OverlayToolSelection(settings Settings, sources map[string]Origin, selection ToolSelection) (Settings, map[string]Origin) {
	result := make(map[string]Origin, len(sources))
	maps.Copy(result, sources)
	applyToolSelection(&settings, result, selection)
	return settings, result
}

func applyToolSelection(settings *Settings, sources map[string]Origin, selection ToolSelection) {
	settings.EnabledTools = resetEnabledToolMap(selection.Tools)
	for _, id := range toolspec.CatalogIDs() {
		sources[toolSourceKey(id)] = selection.Origin
	}
}
