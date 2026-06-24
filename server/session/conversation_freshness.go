package session

import "encoding/json"

type ConversationFreshness uint8

const (
	ConversationFreshnessFresh ConversationFreshness = iota
	ConversationFreshnessEstablished
)

func (f ConversationFreshness) IsFresh() bool {
	return f == ConversationFreshnessFresh
}

func advanceConversationFreshness(current ConversationFreshness, evt Event) ConversationFreshness {
	if current == ConversationFreshnessEstablished {
		return current
	}
	if hasVisibleUserMessageEvent(evt.Kind, evt.Payload) {
		return ConversationFreshnessEstablished
	}
	return current
}

func hasVisibleUserMessageEvent(kind string, payload json.RawMessage) bool {
	_, ok := visibleUserMessageFromEvent(kind, payload)
	return ok
}
