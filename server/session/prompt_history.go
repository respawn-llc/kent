package session

import (
	"encoding/json"
	"strings"
)

const promptHistoryEventKind = "prompt_history"

type promptHistoryEnvelope struct {
	Text string `json:"text"`
}

func normalizePromptHistoryText(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return text
}

func (s *Store) ReadPromptHistory() ([]string, error) {
	legacy := make([]string, 0)
	history := make([]string, 0)
	seenPromptHistory := false
	if err := s.WalkEvents(func(evt Event) error {
		kind := strings.TrimSpace(evt.Kind)
		if kind == promptHistoryEventKind {
			seenPromptHistory = true
			if len(evt.Payload) == 0 {
				return nil
			}
			var entry promptHistoryEnvelope
			if err := json.Unmarshal(evt.Payload, &entry); err != nil {
				return nil
			}
			if text := normalizePromptHistoryText(entry.Text); text != "" {
				history = append(history, text)
			}
			return nil
		}
		if seenPromptHistory || kind != "message" || len(evt.Payload) == 0 {
			return nil
		}
		var msg persistedMessageEnvelope
		if err := json.Unmarshal(evt.Payload, &msg); err != nil {
			return nil
		}
		if !isVisibleUserMessage(msg) {
			return nil
		}
		if text := normalizePromptHistoryText(msg.Content); text != "" {
			legacy = append(legacy, text)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return append(legacy, history...), nil
}
