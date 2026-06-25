package nativescrollback

import (
	"fmt"
	"strings"
	"testing"

	"core/cli/tui"
)

type pendingToolModelOp uint8

const (
	pendingToolCallA pendingToolModelOp = iota
	pendingToolCallB
	pendingToolResultA
	pendingToolResultB
)

func (op pendingToolModelOp) String() string {
	switch op {
	case pendingToolCallA:
		return "call_a"
	case pendingToolCallB:
		return "call_b"
	case pendingToolResultA:
		return "result_a"
	case pendingToolResultB:
		return "result_b"
	default:
		return fmt.Sprintf("pending_tool_op_%d", op)
	}
}

type pendingToolModel struct {
	entries []tui.TranscriptEntry
	callA   bool
	callB   bool
	resultA bool
	resultB bool
}

func TestLedgerPendingToolFrontierModelPartitionsParallelToolsByStablePrefix(t *testing.T) {
	initial := pendingToolModel{
		entries: []tui.TranscriptEntry{{
			Role: tui.TranscriptRoleUser,
			Text: "prompt",
		}},
	}
	visited := 0
	walkPendingToolModel(t, initial, nil, &visited)
	if visited < 19 {
		t.Fatalf("pending tool model explored too few states: %d", visited)
	}
}

func walkPendingToolModel(t *testing.T, state pendingToolModel, path []pendingToolModelOp, visited *int) {
	t.Helper()
	state.assertInvariants(t, path)
	*visited = *visited + 1
	for _, op := range state.availableOps() {
		next := state.clone()
		next.apply(op)
		walkPendingToolModel(t, next, append(path, op), visited)
	}
}

func (m pendingToolModel) clone() pendingToolModel {
	cloned := m
	cloned.entries = append([]tui.TranscriptEntry(nil), m.entries...)
	return cloned
}

func (m pendingToolModel) availableOps() []pendingToolModelOp {
	ops := make([]pendingToolModelOp, 0, 4)
	if !m.callA {
		ops = append(ops, pendingToolCallA)
	}
	if !m.callB {
		ops = append(ops, pendingToolCallB)
	}
	if m.callA && !m.resultA {
		ops = append(ops, pendingToolResultA)
	}
	if m.callB && !m.resultB {
		ops = append(ops, pendingToolResultB)
	}
	return ops
}

func (m *pendingToolModel) apply(op pendingToolModelOp) {
	switch op {
	case pendingToolCallA:
		m.callA = true
		m.entries = append(m.entries, pendingToolModelEntry(tui.TranscriptRoleToolCall, "call_a", "echo a"))
	case pendingToolCallB:
		m.callB = true
		m.entries = append(m.entries, pendingToolModelEntry(tui.TranscriptRoleToolCall, "call_b", "echo b"))
	case pendingToolResultA:
		m.resultA = true
		m.entries = append(m.entries, pendingToolModelEntry(tui.TranscriptRoleToolResultOK, "call_a", "out-a"))
	case pendingToolResultB:
		m.resultB = true
		m.entries = append(m.entries, pendingToolModelEntry(tui.TranscriptRoleToolResultOK, "call_b", "out-b"))
	}
}

func pendingToolModelEntry(role tui.TranscriptRole, callID string, text string) tui.TranscriptEntry {
	return tui.TranscriptEntry{
		Role:       role,
		Text:       text,
		ToolCallID: callID,
	}
}

func (m pendingToolModel) assertInvariants(t *testing.T, path []pendingToolModelOp) {
	t.Helper()
	wantPrefixEnd := pendingToolModelCommittedPrefixEnd(m.entries)
	projection := CommittedOngoingProjectionEntries(m.entries)
	if projection.PrefixEnd != wantPrefixEnd {
		t.Fatalf("committed prefix end = %d, want %d on path %s", projection.PrefixEnd, wantPrefixEnd, formatPendingToolModelPath(path))
	}
	if got, want := pendingToolModelEntryKeys(projection.Entries), pendingToolModelEntryKeys(m.entries[:wantPrefixEnd]); got != want {
		t.Fatalf("committed entries = %s, want %s on path %s", got, want, formatPendingToolModelPath(path))
	}
	if got, want := pendingToolModelSourceIndexKeys(projection.SourceIndexes), pendingToolModelSourceIndexKeys(pendingToolModelSourceIndexes(wantPrefixEnd)); got != want {
		t.Fatalf("committed source indexes = %s, want %s on path %s", got, want, formatPendingToolModelPath(path))
	}
	if got, want := pendingToolModelEntryKeys(PendingOngoingEntries(m.entries)), pendingToolModelEntryKeys(m.entries[wantPrefixEnd:]); got != want {
		t.Fatalf("pending ongoing entries = %s, want %s on path %s", got, want, formatPendingToolModelPath(path))
	}
	if got, want := pendingToolModelEntryKeys(PendingToolEntries(m.entries)), pendingToolModelEntryKeys(pendingToolModelPendingToolEntries(m.entries[wantPrefixEnd:])); got != want {
		t.Fatalf("pending tool entries = %s, want %s on path %s", got, want, formatPendingToolModelPath(path))
	}
	combined := append(append([]tui.TranscriptEntry(nil), projection.Entries...), PendingOngoingEntries(m.entries)...)
	if got, want := pendingToolModelEntryKeys(combined), pendingToolModelEntryKeys(m.entries); got != want {
		t.Fatalf("committed+pending partition = %s, want complete transcript %s on path %s", got, want, formatPendingToolModelPath(path))
	}
}

func pendingToolModelCommittedPrefixEnd(entries []tui.TranscriptEntry) int {
	consumed := make(map[int]struct{})
	for idx, entry := range entries {
		if tui.TranscriptRoleFromWire(string(entry.Role)) != tui.TranscriptRoleToolCall {
			continue
		}
		if strings.TrimSpace(entry.Text) == "" && strings.TrimSpace(entry.CondensedText) == "" {
			continue
		}
		resultIdx := pendingToolModelFindMatchingResult(entries, idx, consumed)
		if resultIdx < 0 {
			return idx
		}
		consumed[resultIdx] = struct{}{}
	}
	return len(entries)
}

func pendingToolModelPendingToolEntries(entries []tui.TranscriptEntry) []tui.TranscriptEntry {
	if len(entries) == 0 {
		return nil
	}
	resultConsumed := make(map[int]struct{})
	included := make(map[int]struct{})
	for idx, entry := range entries {
		if tui.TranscriptRoleFromWire(string(entry.Role)) != tui.TranscriptRoleToolCall {
			continue
		}
		if strings.TrimSpace(entry.Text) == "" && strings.TrimSpace(entry.CondensedText) == "" {
			continue
		}
		included[idx] = struct{}{}
		resultIdx := pendingToolModelFindMatchingResult(entries, idx, resultConsumed)
		if resultIdx < 0 {
			continue
		}
		included[resultIdx] = struct{}{}
		resultConsumed[resultIdx] = struct{}{}
	}
	pending := make([]tui.TranscriptEntry, 0, len(included))
	for idx, entry := range entries {
		if _, ok := included[idx]; ok {
			pending = append(pending, entry)
		}
	}
	return pending
}

func pendingToolModelFindMatchingResult(entries []tui.TranscriptEntry, callIdx int, consumed map[int]struct{}) int {
	callID := strings.TrimSpace(entries[callIdx].ToolCallID)
	for idx := callIdx + 1; idx < len(entries); idx++ {
		if _, ok := consumed[idx]; ok {
			continue
		}
		entry := entries[idx]
		if !tui.TranscriptRoleFromWire(string(entry.Role)).IsToolResult() {
			continue
		}
		if strings.TrimSpace(entry.ToolCallID) != callID {
			continue
		}
		return idx
	}
	return -1
}

func pendingToolModelSourceIndexes(count int) []int {
	indexes := make([]int, 0, count)
	for idx := 0; idx < count; idx++ {
		indexes = append(indexes, idx)
	}
	return indexes
}

func pendingToolModelEntryKeys(entries []tui.TranscriptEntry) string {
	if len(entries) == 0 {
		return ""
	}
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		parts = append(parts, string(entry.Role)+":"+entry.ToolCallID+":"+entry.Text)
	}
	return strings.Join(parts, ",")
}

func pendingToolModelSourceIndexKeys(indexes []int) string {
	if len(indexes) == 0 {
		return ""
	}
	parts := make([]string, 0, len(indexes))
	for _, index := range indexes {
		parts = append(parts, fmt.Sprint(index))
	}
	return strings.Join(parts, ",")
}

func formatPendingToolModelPath(path []pendingToolModelOp) string {
	if len(path) == 0 {
		return "<start>"
	}
	parts := make([]string, 0, len(path))
	for _, op := range path {
		parts = append(parts, op.String())
	}
	return strings.Join(parts, " -> ")
}
