package tui

import (
	"builder/shared/transcript"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

type sgrStyleState struct {
	hasForeground bool
	faint         bool
}

func mutedShellStyleStateAtLineStarts(text string) []sgrStyleState {
	parser := ansi.GetParser()
	defer ansi.PutParser(parser)

	states := []sgrStyleState{{}}
	state := byte(0)
	input := text
	current := sgrStyleState{}
	for len(input) > 0 {
		seq, width, n, newState := ansi.GraphemeWidth.DecodeSequenceInString(input, state, parser)
		if n <= 0 {
			break
		}
		state = newState
		input = input[n:]
		if width > 0 {
			continue
		}
		if strings.Contains(seq, "\n") {
			for range strings.Count(seq, "\n") {
				states = append(states, current)
			}
			continue
		}
		if ansi.Cmd(parser.Command()).Final() != 'm' {
			continue
		}
		current = applySGRStyleState(current, parser.Params())
	}
	return states
}

func applySGRStyleState(current sgrStyleState, params ansi.Params) sgrStyleState {
	if len(params) == 0 {
		return sgrStyleState{}
	}
	for idx := 0; idx < len(params); {
		param, _, ok := params.Param(idx, 0)
		if !ok {
			break
		}
		switch {
		case param == 0:
			current = sgrStyleState{}
			idx++
		case param == 2:
			current.faint = true
			idx++
		case param == 22:
			current.faint = false
			idx++
		case param == 39:
			current.hasForeground = false
			idx++
		case (30 <= param && param <= 37) || (90 <= param && param <= 97):
			current.hasForeground = true
			idx++
		case param == 38:
			_, consumed, ok := parseANSIForegroundColor(params, idx)
			if !ok {
				idx++
				continue
			}
			current.hasForeground = true
			idx += consumed
		default:
			idx++
		}
	}
	return current
}

func lineContaining(text, substring string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(ansi.Strip(line), substring) {
			return line
		}
	}
	return ""
}

func oldFormatterBaseForegroundEscapes(theme string) []string {
	if strings.EqualFold(strings.TrimSpace(theme), "light") {
		return []string{"\x1b[38;5;234m"}
	}
	return []string{"\x1b[38;5;252m", "\x1b[97m", "\x1b[38;2;255;255;255m"}
}

func TestDetailProjectionViewportKeepsLineCountAcrossScrollUpdates(t *testing.T) {
	m := NewModel(WithTheme("dark"))
	m = updateModel(t, m, SetViewportSizeMsg{Lines: 24, Width: 100})
	m = updateModel(t, m, SetConversationMsg{Entries: []TranscriptEntry{
		{Role: "user", Text: "hello"},
		{Role: "assistant", Text: "world"},
	}})
	m = updateModel(t, m, ToggleModeMsg{})

	if len(m.currentDetailViewport().Lines) == 0 {
		t.Fatal("expected detail projection viewport lines on detail entry")
	}
	startLen := len(m.currentDetailViewport().Lines)

	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if got := len(m.currentDetailViewport().Lines); got != startLen {
		t.Fatalf("expected detail projection viewport length to stay stable across scroll updates, got %d want %d", got, startLen)
	}
}

func TestDetailScrollStepAllocsStayBounded(t *testing.T) {
	entries := benchmarkDetailEntries(300)
	m := NewModel(WithTheme("dark"))
	m = updateModel(t, m, SetViewportSizeMsg{Lines: 40, Width: 120})
	m = updateModel(t, m, SetConversationMsg{Entries: entries})
	m = updateModel(t, m, ToggleModeMsg{})
	m = updateModel(t, m, ScrollOngoingMsg{Delta: -120})

	allocs := testing.AllocsPerRun(20, func() {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = next.(Model)
		_ = m.View()
	})
	if allocs > 120 {
		t.Fatalf("expected detail scroll allocations to stay bounded, got %.2f allocs/op", allocs)
	}
}

func TestOngoingScrollStepAllocsStayBounded(t *testing.T) {
	entries := benchmarkDetailEntries(300)
	m := NewModel(WithTheme("dark"))
	m = updateModel(t, m, SetViewportSizeMsg{Lines: 40, Width: 120})
	m = updateModel(t, m, SetConversationMsg{Entries: entries})
	m = updateModel(t, m, ScrollOngoingMsg{Delta: -120})

	allocs := testing.AllocsPerRun(20, func() {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = next.(Model)
		_ = m.View()
	})
	if allocs > 100 {
		t.Fatalf("expected ongoing scroll allocations to stay bounded, got %.2f allocs/op", allocs)
	}
}

func TestDetailStreamingUpdateAllocsStayBounded(t *testing.T) {
	entries := benchmarkDetailEntries(300)
	m := NewModel(WithTheme("dark"))
	m = updateModel(t, m, SetViewportSizeMsg{Lines: 40, Width: 120})
	m = updateModel(t, m, SetConversationMsg{Entries: entries})
	m = updateModel(t, m, ToggleModeMsg{})

	base := m
	allocs := testing.AllocsPerRun(20, func() {
		local := base
		next, _ := local.Update(StreamAssistantMsg{Delta: "x"})
		local = next.(Model)
		_ = local.View()
	})
	if allocs > 300 {
		t.Fatalf("expected detail streaming update allocations to stay bounded, got %.2f allocs/op", allocs)
	}
}

func TestOngoingStreamingUpdateAllocsStayBounded(t *testing.T) {
	entries := benchmarkDetailEntries(300)
	m := NewModel(WithTheme("dark"))
	m = updateModel(t, m, SetViewportSizeMsg{Lines: 40, Width: 120})
	m = updateModel(t, m, SetConversationMsg{Entries: entries})

	base := m
	allocs := testing.AllocsPerRun(20, func() {
		local := base
		next, _ := local.Update(StreamAssistantMsg{Delta: "x"})
		local = next.(Model)
		_ = local.View()
	})
	if allocs > 120 {
		t.Fatalf("expected ongoing streaming update allocations to stay bounded, got %.2f allocs/op", allocs)
	}
}

type errString string

func (e errString) Error() string {
	return string(e)
}

func updateModel(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()

	next, _ := m.Update(msg)
	updated, ok := next.(Model)
	if !ok {
		t.Fatalf("unexpected model type %T", next)
	}
	return updated
}

func transcriptEntriesRange(start, end int) []TranscriptEntry {
	entries := make([]TranscriptEntry, 0, max(0, end-start))
	for i := start; i < end; i++ {
		entries = append(entries, TranscriptEntry{Role: "assistant", Text: fmt.Sprintf("line %d", i)})
	}
	return entries
}

func plainTranscript(view string) string {
	stripped := ansi.Strip(view)
	lines := strings.Split(stripped, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return strings.Join(lines, "\n")
}

func trimTrailingBlankLines(text string) string {
	lines := strings.Split(text, "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

func appendShellToolCall(t *testing.T, m Model, command string) Model {
	t.Helper()
	return updateModel(t, m, AppendTranscriptMsg{
		Role: "tool_call",
		Text: command,
		ToolCall: &transcript.ToolCallMeta{
			ToolName: "exec_command",
			IsShell:  true,
			Command:  command,
		},
	})
}

func containsInOrder(text string, parts ...string) bool {
	offset := 0
	for _, part := range parts {
		idx := strings.Index(text[offset:], part)
		if idx < 0 {
			return false
		}
		offset += idx + len(part)
	}
	return true
}
