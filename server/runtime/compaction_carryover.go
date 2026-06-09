package runtime

import (
	"strings"

	"builder/prompts"
	"builder/server/llm"
)

type compactionCarryoverCoordinator struct {
	engine *Engine
}

type postCompactionMessage struct {
	message                  llm.Message
	pendingHandoffFutureText string
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
		Content:     prompts.FormatHandoffFutureAgentMessage(trimmed),
	}
}

func (e *Engine) postCompactionMessages(mode compactionMode, manualCarryover string, wasHeadless bool) []postCompactionMessage {
	return newCompactionCarryoverCoordinator(e).postCompactionMessages(mode, manualCarryover, wasHeadless)
}

func (c compactionCarryoverCoordinator) postCompactionMessages(mode compactionMode, manualCarryover string, wasHeadless bool) []postCompactionMessage {
	e := c.engine
	out := make([]postCompactionMessage, 0, 3)
	if mode == compactionModeManual {
		if carryover := manualCompactionCarryoverMessage(manualCarryover); strings.TrimSpace(carryover.Content) != "" {
			out = append(out, postCompactionMessage{message: carryover})
		}
	}
	if mode == compactionModeHandoff {
		if req := e.pendingHandoffRequestSnapshot(); req != nil {
			if strings.TrimSpace(req.futureAgentMessage) != "" {
				futureMessage := handoffFutureAgentMessage(req.futureAgentMessage)
				out = append(out, postCompactionMessage{
					message:                  futureMessage,
					pendingHandoffFutureText: req.futureAgentMessage,
				})
			}
		}
	}
	if wasHeadless {
		if headless, ok := headlessModeMetaMessage(); ok {
			out = append(out, postCompactionMessage{message: headless})
		}
	}
	return out
}

func (e *Engine) appendPostCompactionMessages(stepID string, messages []postCompactionMessage) error {
	return newCompactionCarryoverCoordinator(e).appendPostCompactionMessages(stepID, messages)
}

func (c compactionCarryoverCoordinator) appendPostCompactionMessages(stepID string, messages []postCompactionMessage) error {
	e := c.engine
	for _, item := range messages {
		message := item.message
		switch message.MessageType {
		case llm.MessageTypeManualCompactionCarryover:
			if err := e.steer(stepID, steerMessageWithoutDerivedEventIntent(message)); err != nil {
				return err
			}
		default:
			if err := e.steer(stepID, steerMessageIntent(message)); err != nil {
				if message.MessageType == llm.MessageTypeHandoffFutureMessage {
					e.queuePendingHandoffFutureMessage(item.pendingHandoffFutureText)
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
