package prompts

import "strings"

const (
	HandoffFutureAgentMessagePrefix = `The previous agent also left an additional message: "`
	HandoffFutureAgentMessageSuffix = `"`
)

func FormatHandoffFutureAgentMessage(content string) string {
	return HandoffFutureAgentMessagePrefix + strings.TrimSpace(content) + HandoffFutureAgentMessageSuffix
}
