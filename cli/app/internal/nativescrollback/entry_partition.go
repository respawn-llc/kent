package nativescrollback

import (
	"strings"

	"core/cli/tui"
)

type CommittedOngoingEntryProjection struct {
	Entries       []tui.TranscriptEntry
	SourceIndexes []int
	PrefixEnd     int
}

func CommittedOngoingEntries(entries []tui.TranscriptEntry) []tui.TranscriptEntry {
	return CommittedOngoingProjectionEntries(entries).Entries
}

func CommittedOngoingProjectionEntries(entries []tui.TranscriptEntry) CommittedOngoingEntryProjection {
	projection := CommittedOngoingEntryProjection{}
	if len(entries) == 0 {
		return projection
	}
	projection.PrefixEnd = CommittedOngoingPrefixEnd(entries)
	if projection.PrefixEnd <= 0 {
		return projection
	}
	projection.Entries, projection.SourceIndexes = nonEmptyTranscriptEntriesWithSourceIndexes(entries[:projection.PrefixEnd])
	return projection
}

// CommittedOngoingPrefixEnd returns the unfiltered stable transcript boundary.
// Callers that store committed rows with transient UI flags must normalize those
// flags before calling so the boundary matches their committed stream semantics.
func CommittedOngoingPrefixEnd(entries []tui.TranscriptEntry) int {
	if len(entries) == 0 {
		return 0
	}
	return committedOngoingPrefixEnd(entries)
}

func PendingOngoingEntries(entries []tui.TranscriptEntry) []tui.TranscriptEntry {
	if len(entries) == 0 {
		return nil
	}
	prefixEnd := CommittedOngoingPrefixEnd(entries)
	if prefixEnd >= len(entries) {
		return nil
	}
	return nonEmptyTranscriptEntries(entries[prefixEnd:])
}

func PendingToolEntries(entries []tui.TranscriptEntry) []tui.TranscriptEntry {
	if len(entries) == 0 {
		return nil
	}
	start := CommittedOngoingPrefixEnd(entries)
	if start >= len(entries) {
		return nil
	}
	tail := entries[start:]
	include := make(map[int]struct{})
	consumedResults := make(map[int]struct{})
	resultIndex := buildToolResultIndex(tail)
	for idx, entry := range tail {
		if tui.TranscriptRoleFromWire(string(entry.Role)) != tui.TranscriptRoleToolCall {
			continue
		}
		text := entry.Text
		if strings.TrimSpace(entry.CondensedText) != "" {
			text = entry.CondensedText
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		include[idx] = struct{}{}
		resultIdx := resultIndex.findMatchingToolResultIndex(tail, idx, consumedResults)
		if resultIdx < 0 {
			continue
		}
		include[resultIdx] = struct{}{}
		consumedResults[resultIdx] = struct{}{}
	}
	pending := make([]tui.TranscriptEntry, 0, len(include))
	for idx, entry := range tail {
		if _, ok := include[idx]; !ok {
			continue
		}
		pending = append(pending, entry)
	}
	return pending
}

func nonEmptyTranscriptEntries(entries []tui.TranscriptEntry) []tui.TranscriptEntry {
	filtered, _ := nonEmptyTranscriptEntriesWithSourceIndexes(entries)
	return filtered
}

func nonEmptyTranscriptEntriesWithSourceIndexes(entries []tui.TranscriptEntry) ([]tui.TranscriptEntry, []int) {
	filtered := make([]tui.TranscriptEntry, 0, len(entries))
	sourceIndexes := make([]int, 0, len(entries))
	for idx, entry := range entries {
		if tui.TranscriptRoleFromWire(string(entry.Role)).IsToolResult() &&
			strings.TrimSpace(entry.Text) == "" &&
			strings.TrimSpace(entry.CondensedText) == "" {
			filtered = append(filtered, entry)
			sourceIndexes = append(sourceIndexes, idx)
			continue
		}
		if strings.TrimSpace(entry.Text) == "" && strings.TrimSpace(entry.CondensedText) == "" {
			continue
		}
		filtered = append(filtered, entry)
		sourceIndexes = append(sourceIndexes, idx)
	}
	return filtered, sourceIndexes
}

func committedOngoingPrefixEnd(entries []tui.TranscriptEntry) int {
	consumedResults := make(map[int]struct{})
	resultIndex := buildToolResultIndex(entries)
	for idx, entry := range entries {
		if entry.Transient {
			return committedOngoingPrefixEndBefore(entries, idx, resultIndex)
		}
		if tui.TranscriptRoleFromWire(string(entry.Role)) != tui.TranscriptRoleToolCall {
			continue
		}
		text := entry.Text
		if strings.TrimSpace(entry.CondensedText) != "" {
			text = entry.CondensedText
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		resultIdx := resultIndex.findMatchingToolResultIndex(entries, idx, consumedResults)
		if resultIdx < 0 || entries[resultIdx].Transient {
			return idx
		}
		consumedResults[resultIdx] = struct{}{}
	}
	return len(entries)
}

func committedOngoingPrefixEndBefore(entries []tui.TranscriptEntry, boundary int, resultIndex toolResultIndex) int {
	consumedResults := make(map[int]struct{})
	for idx := boundary - 1; idx >= 0; idx-- {
		entry := entries[idx]
		if tui.TranscriptRoleFromWire(string(entry.Role)) != tui.TranscriptRoleToolCall {
			continue
		}
		text := entry.Text
		if strings.TrimSpace(entry.CondensedText) != "" {
			text = entry.CondensedText
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		resultIdx := resultIndex.findMatchingToolResultIndex(entries, idx, consumedResults)
		if resultIdx < 0 || resultIdx >= boundary || entries[resultIdx].Transient {
			return idx
		}
		consumedResults[resultIdx] = struct{}{}
	}
	return boundary
}

type toolResultIndex struct {
	results map[string][]int
	cursors map[string]int
}

func buildToolResultIndex(entries []tui.TranscriptEntry) toolResultIndex {
	index := toolResultIndex{
		results: make(map[string][]int),
		cursors: make(map[string]int),
	}
	for idx, entry := range entries {
		if !tui.TranscriptRoleFromWire(string(entry.Role)).IsToolResult() {
			continue
		}
		callID := strings.TrimSpace(entry.ToolCallID)
		if callID == "" {
			continue
		}
		index.results[callID] = append(index.results[callID], idx)
	}
	return index
}

func (index toolResultIndex) findMatchingToolResultIndex(entries []tui.TranscriptEntry, callIdx int, consumed map[int]struct{}) int {
	if callIdx < 0 || callIdx >= len(entries) {
		return -1
	}
	callID := strings.TrimSpace(entries[callIdx].ToolCallID)
	nextIdx := callIdx + 1
	if nextIdx < len(entries) {
		if _, used := consumed[nextIdx]; !used && tui.TranscriptRoleFromWire(string(entries[nextIdx].Role)).IsToolResult() {
			nextCallID := strings.TrimSpace(entries[nextIdx].ToolCallID)
			if callID == nextCallID {
				return nextIdx
			}
		}
	}
	if callID == "" {
		return -1
	}
	results := index.results[callID]
	for cursor := index.cursors[callID]; cursor < len(results); cursor++ {
		resultIdx := results[cursor]
		if resultIdx <= callIdx {
			index.cursors[callID] = cursor + 1
			continue
		}
		if _, used := consumed[resultIdx]; used {
			index.cursors[callID] = cursor + 1
			continue
		}
		index.cursors[callID] = cursor
		return resultIdx
	}
	index.cursors[callID] = len(results)
	return -1
}
