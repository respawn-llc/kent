package statuscollect

import (
	"strings"

	"builder/cli/app/internal/serverbridge"
	appstatus "builder/cli/app/internal/status"
	"builder/shared/config"
)

func ConfigOverrideSources(src config.SourceReport) []string {
	present := map[string]bool{}
	for _, source := range src.Sources {
		switch strings.TrimSpace(source) {
		case "env":
			present["ENV"] = true
		case "cli":
			present["CLI ARGS"] = true
		}
	}
	ordered := make([]string, 0, len(present))
	for _, label := range []string{"ENV", "CLI ARGS"} {
		if present[label] {
			ordered = append(ordered, label)
		}
	}
	return ordered
}

func ModelSummary(req appstatus.Request) string {
	resolved := strings.TrimSpace(req.ModelName)
	configured := strings.TrimSpace(req.ConfiguredModelName)
	modelName := resolved
	if modelName == "" {
		modelName = configured
	}
	if modelName == "" {
		modelName = "<unset>"
	}
	parts := []string{ModelDisplayLabel(modelName, strings.TrimSpace(req.ThinkingLevel))}
	if req.FastModeAvailable && req.FastModeEnabled {
		parts = append(parts, "fast")
	}
	return strings.Join(parts, " ")
}

func ModelDisplayLabel(modelName string, thinkingLevel string) string {
	return serverbridge.ModelDisplayLabel(modelName, thinkingLevel)
}

func UpdateInfo(req appstatus.Request) appstatus.UpdateInfo {
	if req.Runtime == nil {
		return appstatus.UpdateInfo{}
	}
	status := req.Runtime.Status().Update
	return appstatus.UpdateInfo{
		Checked:       status.Checked,
		Available:     status.Available,
		LatestVersion: strings.TrimSpace(status.LatestVersion),
	}
}

func SupervisorLabel(enabled bool, mode string) string {
	if !enabled {
		return "off"
	}
	trimmed := strings.TrimSpace(mode)
	if trimmed == "" || trimmed == "off" {
		return "on"
	}
	return trimmed
}
