package controlfeedback

import "strings"

func FastModeToggleStatusMessage(enabled bool, changed bool) string {
	if enabled {
		if changed {
			return "Fast mode enabled"
		}
		return "Fast mode already enabled"
	}
	if changed {
		return "Fast mode disabled"
	}
	return "Fast mode already disabled"
}

func ReviewerToggleStatusMessage(enabled bool, mode string, changed bool) string {
	modeText := strings.ToLower(strings.TrimSpace(mode))
	if modeText == "" {
		modeText = "off"
	}
	if enabled {
		detail := ""
		switch modeText {
		case "all", "edits":
			detail = " (frequency: " + modeText + ")"
		}
		if changed {
			return "Supervisor invocation enabled" + detail
		}
		return "Supervisor invocation already enabled" + detail
	}
	if changed {
		return "Supervisor invocation disabled"
	}
	return "Supervisor invocation already disabled"
}

func QuestionsToggleStatusMessage(enabled bool, changed bool) string {
	if enabled {
		if changed {
			return "Questions enabled"
		}
		return "Questions already enabled"
	}
	if changed {
		return "Questions disabled"
	}
	return "Questions already disabled"
}

func AutoCompactionToggleStatusMessage(enabled bool, changed bool, compactionMode string) string {
	modeNote := ""
	if strings.EqualFold(strings.TrimSpace(compactionMode), "none") {
		modeNote = " (compaction_mode=none; manual/auto compaction disabled)"
	}
	if enabled {
		if changed {
			return "Auto-compaction enabled" + modeNote
		}
		return "Auto-compaction already enabled" + modeNote
	}
	if changed {
		return "Auto-compaction disabled" + modeNote
	}
	return "Auto-compaction already disabled" + modeNote
}
