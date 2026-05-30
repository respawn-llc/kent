package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func newBenchmarkTranscriptModel(entries []TranscriptEntry, opts ...Option) Model {
	model := NewModel(append([]Option{WithTheme("dark")}, opts...)...)
	next, _ := model.Update(SetViewportSizeMsg{Lines: 40, Width: 120})
	model = next.(Model)
	next, _ = model.Update(SetConversationMsg{Entries: entries})
	return next.(Model)
}

func newBenchmarkDetailModel(entries []TranscriptEntry, opts ...Option) Model {
	model := newBenchmarkTranscriptModel(entries, opts...)
	next, _ := model.Update(ToggleModeMsg{})
	return next.(Model)
}

func BenchmarkToggleModeFirstDetailSnapshot(b *testing.B) {
	entries := benchmarkDetailEntries(600)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		model := newBenchmarkTranscriptModel(entries)
		b.StartTimer()
		next, _ := model.Update(ToggleModeMsg{})
		model = next.(Model)
		_ = model.View()
	}
}

func BenchmarkCompactToggleModeLargeTranscript(b *testing.B) {
	entries := benchmarkDetailEntries(1200)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		model := newBenchmarkTranscriptModel(entries, WithCompactDetail())
		b.StartTimer()
		next, _ := model.Update(ToggleModeMsg{})
		model = next.(Model)
		_ = model.View()
	}
}

func BenchmarkToggleModeReopenDetailSnapshot(b *testing.B) {
	entries := benchmarkDetailEntries(600)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		model := newBenchmarkDetailModel(entries)
		next, _ := model.Update(ToggleModeMsg{})
		model = next.(Model)
		next, _ = model.Update(ToggleModeMsg{})
		model = next.(Model)
		b.StartTimer()
		next, _ = model.Update(ToggleModeMsg{})
		model = next.(Model)
		_ = model.View()
	}
}

func BenchmarkDetailFirstScrollFromLazyEntry(b *testing.B) {
	entries := benchmarkDetailEntries(600)
	model := newBenchmarkDetailModel(entries)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		local := model
		next, _ := local.Update(tea.KeyMsg{Type: tea.KeyUp})
		local = next.(Model)
		_ = local.View()
	}
}

func BenchmarkDetailScrollStep(b *testing.B) {
	entries := benchmarkDetailEntries(600)
	model := newBenchmarkDetailModel(entries)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
		model = next.(Model)
		_ = model.View()
	}
}

func BenchmarkDetailSelectionFocusStep(b *testing.B) {
	entries := benchmarkDetailEntries(600)
	model := newBenchmarkDetailModel(entries)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entryIndex := i % len(entries)
		next, _ := model.Update(SetSelectedTranscriptEntryMsg{EntryIndex: entryIndex, Active: true, RefreshDetailSnapshot: false})
		model = next.(Model)
		next, _ = model.Update(FocusTranscriptEntryMsg{EntryIndex: entryIndex, Center: true})
		model = next.(Model)
		_ = model.View()
	}
}

func BenchmarkDetailSelectionFocusStepWithRefresh(b *testing.B) {
	entries := benchmarkDetailEntries(600)
	model := newBenchmarkDetailModel(entries)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entryIndex := i % len(entries)
		next, _ := model.Update(SetSelectedTranscriptEntryMsg{EntryIndex: entryIndex, Active: true, RefreshDetailSnapshot: true})
		model = next.(Model)
		next, _ = model.Update(FocusTranscriptEntryMsg{EntryIndex: entryIndex, Center: true})
		model = next.(Model)
		_ = model.View()
	}
}

func BenchmarkOngoingStreamingUpdateLargeHistory(b *testing.B) {
	entries := benchmarkDetailEntries(600)
	base := newBenchmarkTranscriptModel(entries)
	_ = base.View()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		local := base
		next, _ := local.Update(StreamAssistantMsg{Delta: "x"})
		local = next.(Model)
		_ = local.View()
	}
}
