package runtime

import (
	"strings"

	"core/server/llm"
)

func isNoopFinalAnswer(msg llm.Message) bool {
	return msg.Phase == llm.MessagePhaseFinal && strings.TrimSpace(msg.Content) == reviewerNoopToken
}
