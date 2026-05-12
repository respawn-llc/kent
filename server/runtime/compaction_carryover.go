package runtime

import (
	"strings"

	"builder/server/llm"
)

type compactionCarryoverCoordinator struct {
	engine *Engine
}

func newCompactionCarryoverCoordinator(engine *Engine) compactionCarryoverCoordinator {
	return compactionCarryoverCoordinator{engine: engine}
}

func manualCompactionCarryoverMessage(text string) llm.Message {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return llm.Message{}
	}
	content := trimCompactionCarryoverText(trimmed, manualCompactionCarryoverMaxChars)
	return llm.Message{
		Role:        llm.RoleDeveloper,
		MessageType: llm.MessageTypeManualCompactionCarryover,
		Content:     manualCompactionCarryoverHeader + "\n\n" + content,
	}
}

func handoffFutureAgentMessage(text string) llm.Message {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return llm.Message{}
	}
	return llm.Message{
		Role:        llm.RoleDeveloper,
		MessageType: llm.MessageTypeHandoffFutureMessage,
		Content:     trimmed,
	}
}

func (e *Engine) postCompactionMessages(mode compactionMode, manualCarryover string, wasHeadless bool) []llm.Message {
	return newCompactionCarryoverCoordinator(e).postCompactionMessages(mode, manualCarryover, wasHeadless)
}

func (c compactionCarryoverCoordinator) postCompactionMessages(mode compactionMode, manualCarryover string, wasHeadless bool) []llm.Message {
	e := c.engine
	out := make([]llm.Message, 0, 3)
	if mode == compactionModeManual {
		if carryover := manualCompactionCarryoverMessage(manualCarryover); strings.TrimSpace(carryover.Content) != "" {
			out = append(out, carryover)
		}
	}
	if mode == compactionModeHandoff {
		if req := e.pendingHandoffRequestSnapshot(); req != nil {
			if futureMessage := handoffFutureAgentMessage(req.futureAgentMessage); strings.TrimSpace(futureMessage.Content) != "" {
				out = append(out, futureMessage)
			}
		}
	}
	if wasHeadless {
		if headless, ok := headlessModeMetaMessage(); ok {
			out = append(out, headless)
		}
	}
	return out
}

func (e *Engine) appendPostCompactionMessages(stepID string, messages []llm.Message) error {
	return newCompactionCarryoverCoordinator(e).appendPostCompactionMessages(stepID, messages)
}

func (c compactionCarryoverCoordinator) appendPostCompactionMessages(stepID string, messages []llm.Message) error {
	e := c.engine
	for _, message := range messages {
		switch message.MessageType {
		case llm.MessageTypeManualCompactionCarryover:
			if err := e.appendMessageWithoutConversationUpdate(stepID, message); err != nil {
				return err
			}
		default:
			if err := e.appendMessage(stepID, message); err != nil {
				if message.MessageType == llm.MessageTypeHandoffFutureMessage {
					e.queuePendingHandoffFutureMessage(message.Content)
				}
				return err
			}
			if message.MessageType == llm.MessageTypeHandoffFutureMessage {
				e.clearPendingHandoffFutureMessage()
			}
		}
	}
	return nil
}

func trimCompactionCarryoverText(text string, maxChars int) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || maxChars <= 0 {
		return trimmed
	}
	runes := []rune(trimmed)
	if len(runes) <= maxChars {
		return trimmed
	}
	if maxChars < 4 {
		return string(runes[:maxChars])
	}
	return string(runes[:maxChars-4]) + "\n..."
}
