package app

import (
	"fmt"

	"builder/shared/clientui"
)

func queuedUserMessagesForTest(texts ...string) []clientui.QueuedUserMessage {
	messages := make([]clientui.QueuedUserMessage, 0, len(texts))
	for index, text := range texts {
		messages = append(messages, clientui.QueuedUserMessage{ID: fmt.Sprintf("queue-test-%d", index), Text: text})
	}
	return messages
}

func queuedUserMessageIDsForTest(messages []clientui.QueuedUserMessage) []string {
	ids := make([]string, 0, len(messages))
	for _, message := range messages {
		ids = append(ids, message.ID)
	}
	return ids
}

func queuedInputsForTest(texts ...string) []queuedInputItem {
	items := make([]queuedInputItem, 0, len(texts))
	for index, text := range texts {
		items = append(items, queuedInputItem{ID: fmt.Sprintf("input-queue-test-%d", index), Text: text})
	}
	return items
}
