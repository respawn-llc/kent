package runtime

import "builder/server/llm"

func visibleChatEntriesFromResponseItems(items []llm.ResponseItem) []ChatEntry {
	entries := make([]ChatEntry, 0, len(items))
	walker := newResponseItemMessageWalker(func(msg llm.Message) {
		entries = append(entries, VisibleChatEntriesFromMessage(msg)...)
	})
	for _, item := range items {
		walker.Apply(item)
	}
	walker.Flush()
	return entries
}
