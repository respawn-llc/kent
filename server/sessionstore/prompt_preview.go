package session

import "strings"

const firstPromptPreviewMaxChars = 120

// Typed initial assignments are developer messages, so their original task
// must be supplied separately from the model-visible agent-steer envelope.
func (s *Store) CaptureInitialPromptPreview(content string) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	s.mu.Lock()
	if err := s.requireMetadataPersistenceLocked(); err != nil {
		s.mu.Unlock()
		return err
	}
	if !s.captureFirstPromptPreviewLocked(content) {
		s.mu.Unlock()
		return nil
	}
	return s.unlockAndObservePersistence(s.persistMetaAfterRecoveryVerifiedLocked())
}

func (s *Store) captureFirstPromptPreviewLocked(content string) bool {
	if s.meta.FirstPromptPreview == "" && s.meta.ModelRequestCount == 0 {
		preview := normalizeFirstPromptPreview(content)
		if preview != "" {
			s.meta.FirstPromptPreview = preview
			return true
		}
	}
	return false
}

func normalizeFirstPromptPreview(content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if trimmed == "" {
			continue
		}
		return truncatePromptPreview(trimmed, firstPromptPreviewMaxChars)
	}
	return ""
}

func truncatePromptPreview(text string, maxChars int) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || maxChars <= 0 {
		return trimmed
	}
	runes := []rune(trimmed)
	if len(runes) <= maxChars {
		return trimmed
	}
	if maxChars == 1 {
		return "…"
	}
	return string(runes[:maxChars-1]) + "…"
}

func isVisibleUserMessageFields(role string, messageType string, content string) bool {
	if strings.TrimSpace(role) != "user" {
		return false
	}
	if strings.TrimSpace(content) == "" {
		return false
	}
	switch MessageType(strings.TrimSpace(messageType)) {
	case MessageTypeCompactionSummary, MessageTypeUserShellCommand:
		return false
	}
	return true
}
