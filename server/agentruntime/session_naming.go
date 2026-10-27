package runtime

import (
	"strings"

	"core/server/llm"
	"core/shared/textutil"
)

type SessionNamingInput struct {
	Task       string
	Commentary *string
	Headless   bool
}

type SessionNamingConfig struct {
	MaxInputRunes int
	ResumeTask    *string
	Submit        func(SessionNamingInput)
}

type pendingSessionNaming struct {
	config SessionNamingConfig
	task   *string
}

// The output/steering boundary owns this small, runtime-local candidate.
func (e *Engine) captureSessionNamingRaw(text string) {
	pending := e.sessionNaming
	if pending == nil || pending.task != nil || e.currentNodeExecutionActive() || strings.TrimSpace(text) == "" {
		return
	}
	if pending.config.ResumeTask != nil {
		text = *pending.config.ResumeTask
	}
	pending.task = textutil.Value(textutil.MiddleTruncate(text, pending.config.MaxInputRunes))
}

func (e *Engine) submitSessionNaming(response llm.Response) {
	e.outputMutationMu.Lock()
	pending := e.sessionNaming
	if pending == nil || pending.task == nil || e.currentNodeExecutionActive() {
		e.outputMutationMu.Unlock()
		return
	}
	e.sessionNaming = nil
	e.outputMutationMu.Unlock()
	var commentary *string
	for _, message := range llm.MessagesFromItems(response.OutputItems) {
		if message.Role != llm.RoleAssistant || message.Content == nil || strings.TrimSpace(*message.Content) == "" {
			continue
		}
		text := *message.Content
		if commentary != nil {
			text = *commentary + "\n" + text
		}
		commentary = textutil.Value(textutil.MiddleTruncate(text, pending.config.MaxInputRunes))
	}
	if commentary == nil && response.Assistant.Content != nil && strings.TrimSpace(*response.Assistant.Content) != "" {
		commentary = textutil.Value(textutil.MiddleTruncate(*response.Assistant.Content, pending.config.MaxInputRunes))
	}
	pending.config.Submit(SessionNamingInput{Task: *pending.task, Commentary: commentary, Headless: e.cfg.HeadlessMode})
}
