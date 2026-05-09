package app

import (
	"strings"

	"builder/shared/clientui"
)

const uiNoopFinalToken = "NO_OP"

func isNoopFinalText(text string) bool {
	return strings.TrimSpace(text) == uiNoopFinalToken
}

func isNoopProjectedAssistantEvent(evt clientui.Event) bool {
	switch evt.Kind {
	case clientui.EventAssistantDelta:
		return isNoopFinalText(evt.AssistantDelta)
	case clientui.EventAssistantMessage:
		for _, entry := range evt.TranscriptEntries {
			if isNoopFinalAssistantEntry(entry) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func isNoopFinalAssistantEntry(entry clientui.ChatEntry) bool {
	phase := strings.TrimSpace(entry.Phase)
	return strings.TrimSpace(entry.Role) == "assistant" &&
		(phase == "" || phase == clientui.ChatEntryPhaseFinalAnswer) &&
		isNoopFinalText(entry.Text)
}
