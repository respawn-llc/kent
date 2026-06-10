package session

import (
	"encoding/json"
	"strings"
)

const promptHistoryEventKind = "prompt_history"

type promptHistoryEnvelope struct {
	Text string `json:"text"`
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
			if strings.TrimSpace(entry.Text) != "" {
				history = append(history, entry.Text)
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
		if strings.TrimSpace(msg.Content) != "" {
			legacy = append(legacy, msg.Content)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return append(legacy, history...), nil
}
