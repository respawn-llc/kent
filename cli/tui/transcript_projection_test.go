package tui

import (
	"reflect"
	"strings"
	"testing"

	"core/shared/clientui"
	"core/shared/transcript"
)

func defaultProjectionViewState() TranscriptProjectionViewState {
	return TranscriptProjectionViewState{ViewportWidth: 80, ViewportLines: 20, Theme: "dark"}
}

func TestCommittedOngoingProjectorPreservesBaseOffset(t *testing.T) {
	var projector CommittedOngoingProjector
	entries := []TranscriptEntry{{Role: "user", Text: "prompt"}, {Role: "assistant", Text: "answer"}}
	projection := projector.Project(entries, CommittedOngoingProjectionKey{
		Revision:   3,
		Width:      80,
		BaseOffset: 42,
		EntryCount: len(entries),
	})

	if len(projection.Blocks) != 2 {
		t.Fatalf("expected two projection blocks, got %#v", projection.Blocks)
	}
	if projection.Blocks[0].EntryIndex != 42 || projection.Blocks[1].EntryIndex != 43 {
		t.Fatalf("expected absolute entry indices from base offset, got %#v", projection.Blocks)
	}
}

func TestRenderAssistantMarkdownProjectionMatchesCommittedAssistantEntry(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		phase clientui.MessagePhase
		width int
	}{
		{
			name:  "prose",
			text:  "Long **markdown** prose wraps with the same assistant prefix and styling.",
			phase: clientui.MessagePhaseFinal,
			width: 32,
		},
		{
			name:  "table",
			text:  "| Name | Value |\n| --- | --- |\n| alpha | beta |\n",
			phase: clientui.MessagePhaseFinal,
			width: 48,
		},
		{
			name:  "commentary",
			text:  "Decision: keep `commentary` visually aligned with final answers.",
			phase: clientui.MessagePhaseCommentary,
			width: 40,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			entries := []TranscriptEntry{{
				Role:  TranscriptRoleAssistant,
				Text:  tt.text,
				Phase: tt.phase,
			}}
			committed := ProjectCommittedOngoingTranscript(entries, "dark", tt.width).Lines(TranscriptDivider)
			streamed := RenderAssistantMarkdownProjection(tt.text, "dark", tt.width)

			if !reflect.DeepEqual(streamed, committed) {
				t.Fatalf("streaming projection mismatch\nstreamed=%#v\ncommitted=%#v", streamed, committed)
			}
		})
	}
}

func TestTranscriptProjectionSharedPrefixBlockCountStopsAtFirstDivergence(t *testing.T) {
	previous := TranscriptProjection{Blocks: []TranscriptProjectionBlock{
		{Role: "user", DividerGroup: "user", Lines: []string{"❯ prompt"}},
		{Role: "assistant", DividerGroup: "assistant", Lines: []string{"❮ before"}},
		{Role: "user", DividerGroup: "user", Lines: []string{"❯ later"}},
	}}
	current := TranscriptProjection{Blocks: []TranscriptProjectionBlock{
		{Role: "user", DividerGroup: "user", Lines: []string{"❯ prompt"}},
		{Role: "assistant", DividerGroup: "assistant", Lines: []string{"❮ after"}},
		{Role: "user", DividerGroup: "user", Lines: []string{"❯ later"}},
	}}

	if got := current.SharedPrefixBlockCount(previous); got != 1 {
		t.Fatalf("expected shared prefix to stop before divergent assistant block, got %d", got)
	}
}

func TestTranscriptProjectionSharedPrefixBlockCountUsesShorterProjectionLength(t *testing.T) {
	previous := TranscriptProjection{Blocks: []TranscriptProjectionBlock{
		{Role: "assistant", DividerGroup: "assistant", Lines: []string{"❮ one"}},
		{Role: "assistant", DividerGroup: "assistant", Lines: []string{"❮ two"}},
	}}
	current := TranscriptProjection{Blocks: []TranscriptProjectionBlock{
		{Role: "assistant", DividerGroup: "assistant", Lines: []string{"❮ one"}},
		{Role: "assistant", DividerGroup: "assistant", Lines: []string{"❮ two"}},
		{Role: "assistant", DividerGroup: "assistant", Lines: []string{"❮ three"}},
	}}

	if got := current.SharedPrefixBlockCount(previous); got != 2 {
		t.Fatalf("expected shared prefix to include all shorter matching blocks, got %d", got)
	}
}

func TestCommittedOngoingEntriesDoNotTruncateAfterEmptyToolResult(t *testing.T) {
	entries := []TranscriptEntry{
		{Role: "user", Text: "prompt"},
		{Role: "tool_call", Text: "apply patch", ToolCallID: "call_patch", ToolCall: &transcript.ToolCallMeta{ToolName: "patch"}},
		{Role: "tool_result_ok", Text: "", ToolCallID: "call_patch"},
		{Role: "assistant", Text: "continued after empty result"},
	}

	committed := CommittedOngoingEntries(entries)
	if len(committed) != 4 {
		t.Fatalf("expected empty tool result marker preserved through committed frontier, got %#v", committed)
	}
	if committed[2].Role != "tool_result_ok" || committed[2].ToolCallID != "call_patch" {
		t.Fatalf("expected committed entries to keep empty tool result as structural status marker, got %#v", committed)
	}
	if committed[3].Role != "assistant" || committed[3].Text != "continued after empty result" {
		t.Fatalf("expected committed entries to include content after empty tool result, got %#v", committed)
	}

	pending := PendingOngoingEntries(entries)
	if len(pending) != 0 {
		t.Fatalf("expected no pending entries after empty tool result resolution, got %#v", pending)
	}
}

func TestCommittedOngoingPrefixEndExcludesToolCallWhenMatchingResultIsTransient(t *testing.T) {
	entries := []TranscriptEntry{
		{Role: "user", Text: "prompt"},
		{Role: "tool_call", Text: "pwd", ToolCallID: "call_1", ToolCall: &transcript.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "pwd"}},
		{Role: "tool_result_ok", Text: "/tmp", ToolCallID: "call_1", Transient: true},
	}

	if got := committedOngoingPrefixEnd(entries); got != 1 {
		t.Fatalf("committedOngoingPrefixEnd = %d, want 1", got)
	}
	committed := CommittedOngoingEntries(entries)
	if len(committed) != 1 || committed[0].Role != "user" {
		t.Fatalf("expected committed entries to exclude unresolved tool pair, got %#v", committed)
	}
}

func TestCommittedOngoingPrefixEndKeepsResolvedToolPairBeforeLaterTransientEntry(t *testing.T) {
	entries := []TranscriptEntry{
		{Role: "user", Text: "prompt"},
		{Role: "tool_call", Text: "pwd", ToolCallID: "call_1", ToolCall: &transcript.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "pwd"}},
		{Role: "tool_result_ok", Text: "/tmp", ToolCallID: "call_1"},
		{Role: "assistant", Text: "streaming", Transient: true},
	}

	if got := committedOngoingPrefixEnd(entries); got != 3 {
		t.Fatalf("committedOngoingPrefixEnd = %d, want 3", got)
	}
	committed := CommittedOngoingEntries(entries)
	if len(committed) != 3 {
		t.Fatalf("expected committed entries to keep resolved tool pair, got %#v", committed)
	}
	if committed[1].Role != "tool_call" || committed[2].Role != "tool_result_ok" {
		t.Fatalf("expected committed tool pair before transient tail, got %#v", committed)
	}
}

func TestProjectTranscriptViewsDerivesOngoingAndDetailFromSameCanonicalEntries(t *testing.T) {
	entries := []TranscriptEntry{
		{Role: "user", Text: "prompt"},
		{Role: "assistant", Text: "answer"},
	}

	projection := ProjectTranscriptViews(TranscriptProjectionInput{
		BaseOffset: 10,
		Entries:    entries,
	}, defaultProjectionViewState())

	if len(projection.Ongoing.Blocks) != 2 {
		t.Fatalf("expected ongoing projection from canonical entries, got %#v", projection.Ongoing.Blocks)
	}
	if len(projection.Detail.Blocks) != 2 {
		t.Fatalf("expected detail projection from canonical entries, got %#v", projection.Detail.Blocks)
	}
	if projection.Ongoing.Blocks[0].EntryIndex != 10 || projection.Detail.Blocks[0].EntryIndex != 10 {
		t.Fatalf("expected projections to preserve shared base offset, ongoing=%#v detail=%#v", projection.Ongoing.Blocks, projection.Detail.Blocks)
	}
	if projection.Ongoing.Blocks[1].EntryIndex != projection.Detail.Blocks[1].EntryIndex {
		t.Fatalf("expected ongoing/detail entry identity parity, ongoing=%#v detail=%#v", projection.Ongoing.Blocks, projection.Detail.Blocks)
	}
}

func TestDetailProjectionEmptySeparatorsRemainContentLines(t *testing.T) {
	projection := ProjectTranscriptViews(TranscriptProjectionInput{
		Entries: []TranscriptEntry{
			{Role: "user", Text: "prompt"},
			{Role: "assistant", Text: "answer"},
		},
	}, defaultProjectionViewState())

	if len(projection.DetailLines) < 3 {
		t.Fatalf("expected detail projection to include blank separator, got %#v", projection.DetailLines)
	}
	separator := projection.DetailLines[1]
	if separator.Text != "" {
		t.Fatalf("expected empty detail separator text, got %q in %#v", separator.Text, projection.DetailLines)
	}
	if separator.Kind != VisibleLineContent {
		t.Fatalf("expected empty detail separator to remain content, got %v", separator.Kind)
	}
}

func TestProjectTranscriptViewsMapsSelectionEntryToDetailLines(t *testing.T) {
	entries := []TranscriptEntry{
		{Role: "user", Text: "first"},
		{Role: "assistant", Text: "second line one\nsecond line two"},
		{Role: "user", Text: "third"},
	}

	state := defaultProjectionViewState()
	state.DetailSelectedEntry = 21
	state.DetailSelectedActive = true
	state.SelectedEntry = 21
	state.SelectedEntryIsActive = true
	projection := ProjectTranscriptViews(TranscriptProjectionInput{
		BaseOffset: 20,
		Entries:    entries,
	}, state)

	lineRange, ok := projection.DetailEntryLineRanges[21]
	if !ok {
		t.Fatalf("expected detail range for selected entry, got %#v", projection.DetailEntryLineRanges)
	}
	if lineRange.Start < 0 || lineRange.End < lineRange.Start {
		t.Fatalf("invalid selected entry range: %#v", lineRange)
	}
	for idx := lineRange.Start; idx <= lineRange.End; idx++ {
		if projection.DetailLineOwners[idx] != 21 {
			t.Fatalf("expected selected range line %d to be owned by entry 21, owners=%#v", idx, projection.DetailLineOwners)
		}
	}
}

func TestProjectTranscriptViewsDoesNotMutateInputModel(t *testing.T) {
	m := NewModel(WithTheme("dark"), WithPreviewLines(20), WithCompactDetail())
	m = updateModel(t, m, SetViewportSizeMsg{Lines: 20, Width: 80})
	m = updateModel(t, m, SetConversationMsg{Entries: []TranscriptEntry{
		{Role: "user", Text: "prompt"},
		{Role: "assistant", Text: "answer"},
	}})
	m.selectedTranscriptEntry = 0
	m.selectedTranscriptActive = true

	before := m
	_ = ProjectTranscriptViews(m.TranscriptProjectionInput(), m.TranscriptProjectionViewState())

	if before.detailBottomAnchor != m.detailBottomAnchor ||
		before.detailBottomOffset != m.detailBottomOffset {
		t.Fatalf("projection mutated model cache state before=%#v after=%#v", before, m)
	}
}

func TestModelViewDoesNotMutateCanonicalProjectionState(t *testing.T) {
	m := NewModel(WithTheme("dark"), WithPreviewLines(5), WithCompactDetail())
	m = updateModel(t, m, SetViewportSizeMsg{Lines: 5, Width: 80})
	m = updateModel(t, m, SetConversationMsg{Entries: []TranscriptEntry{
		{Role: "user", Text: "prompt"},
		{Role: "assistant", Text: "answer"},
	}})
	m = updateModel(t, m, ToggleModeMsg{})

	beforeInput := m.TranscriptProjectionInput()
	beforeState := m.TranscriptProjectionViewState()
	beforeScroll := m.DetailScroll()
	beforeOngoingScroll := m.OngoingScroll()

	_ = m.View()
	_ = m.VisibleLineKinds()

	afterInput := m.TranscriptProjectionInput()
	afterState := m.TranscriptProjectionViewState()
	if !reflect.DeepEqual(beforeInput, afterInput) ||
		!reflect.DeepEqual(beforeState, afterState) ||
		beforeScroll != m.DetailScroll() ||
		beforeOngoingScroll != m.OngoingScroll() {
		t.Fatalf("rendering mutated canonical/view state before input=%+v state=%+v after input=%+v state=%+v", beforeInput, beforeState, afterInput, afterState)
	}
}

func TestModelDoesNotStoreLegacyRenderCacheGraph(t *testing.T) {
	forbidden := map[string]struct{}{
		"transcript":                {},
		"transcriptBaseOffset":      {},
		"transcriptTotalEntries":    {},
		"transcriptRevision":        {},
		"transcriptEntriesRevision": {},
		"ongoing":                   {},
		"streamingReasoning":        {},
		"ongoingSnapshot":           {},
		"ongoingLineCache":          {},
		"ongoingLineKinds":          {},
		"ongoingBaseLines":          {},
		"ongoingBaseLineKinds":      {},
		"ongoingBaseLastGroup":      {},
		"ongoingStreamingLines":     {},
		"ongoingStreamingKinds":     {},
		"ongoingStreamingDivider":   {},
		"ongoingBaseDirty":          {},
		"ongoingDirty":              {},
		"detailSnapshot":            {},
		"detailLines":               {},
		"detailLineKinds":           {},
		"detailLineEntryIndices":    {},
		"detailEntryLineRanges":     {},
		"detailBlockLineRanges":     {},
		"detailEntryRangeOffset":    {},
		"detailBlocks":              {},
		"detailBlockLines":          {},
		"detailTotalLineCount":      {},
		"detailMetricsResolved":     {},
		"detailDirty":               {},
		"detailStale":               {},
		"detailRebuildCount":        {},
	}
	modelType := reflect.TypeOf(Model{})
	if _, ok := modelType.FieldByName("transcriptInput"); !ok {
		t.Fatal("Model must store transcript data through one canonical transcriptInput field")
	}
	for idx := 0; idx < modelType.NumField(); idx++ {
		field := modelType.Field(idx)
		if _, ok := forbidden[field.Name]; ok {
			t.Fatalf("Model still stores legacy render cache field %q", field.Name)
		}
	}
}

func TestProjectTranscriptViewsExpansionChangesDetailProjectionOnly(t *testing.T) {
	entries := []TranscriptEntry{
		{Role: "tool_call", Text: "printf long", ToolCallID: "call_1", ToolCall: &transcript.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "printf long"}},
		{Role: "tool_result_ok", Text: strings.Repeat("line\n", 8), ToolCallID: "call_1"},
	}
	input := TranscriptProjectionInput{Entries: entries}
	collapsed := ProjectTranscriptViews(input, TranscriptProjectionViewState{ViewportWidth: 80, ViewportLines: 20, Theme: "dark", CompactDetail: true})
	expanded := ProjectTranscriptViews(input, TranscriptProjectionViewState{
		ViewportWidth:         80,
		ViewportLines:         20,
		Theme:                 "dark",
		CompactDetail:         true,
		DetailExpandedEntries: map[int]struct{}{0: {}},
	})

	if len(expanded.DetailLines) <= len(collapsed.DetailLines) {
		t.Fatalf("expected expanded detail projection to add visible detail lines, collapsed=%d expanded=%d", len(collapsed.DetailLines), len(expanded.DetailLines))
	}
	if collapsed.Ongoing.Render(TranscriptDivider) != expanded.Ongoing.Render(TranscriptDivider) {
		t.Fatalf("expected detail expansion to leave ongoing projection unchanged")
	}
}

func TestProjectTranscriptViewsToolCallRenderingUsesSameEntryMapping(t *testing.T) {
	entries := []TranscriptEntry{
		{Role: "tool_call", Text: "pwd", ToolCallID: "call_1", ToolCall: &transcript.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "pwd"}},
		{Role: "tool_result_ok", Text: "/tmp/project", ToolCallID: "call_1"},
	}

	projection := ProjectTranscriptViews(TranscriptProjectionInput{BaseOffset: 30, Entries: entries}, defaultProjectionViewState())

	if len(projection.Ongoing.Blocks) != 1 || projection.Ongoing.Blocks[0].EntryIndex != 30 || projection.Ongoing.Blocks[0].EntryEnd != 31 {
		t.Fatalf("expected ongoing tool projection to merge call/result entry range, got %#v", projection.Ongoing.Blocks)
	}
	if len(projection.Detail.Blocks) != 1 || projection.Detail.Blocks[0].EntryIndex != 30 || projection.Detail.Blocks[0].EntryEnd != 31 {
		t.Fatalf("expected detail tool projection to merge call/result entry range, got %#v", projection.Detail.Blocks)
	}
}

func TestProjectTranscriptViewsViewportResizeChangesProjectionWrapping(t *testing.T) {
	input := TranscriptProjectionInput{Entries: []TranscriptEntry{{Role: "assistant", Text: strings.Repeat("x", 90)}}}
	wide := ProjectTranscriptViews(input, TranscriptProjectionViewState{ViewportWidth: 120, ViewportLines: 20, Theme: "dark"})
	narrow := ProjectTranscriptViews(input, TranscriptProjectionViewState{ViewportWidth: 40, ViewportLines: 20, Theme: "dark"})

	if len(narrow.OngoingLines) <= len(wide.OngoingLines) {
		t.Fatalf("expected narrower viewport to increase wrapped ongoing line count, wide=%d narrow=%d", len(wide.OngoingLines), len(narrow.OngoingLines))
	}
	if len(narrow.DetailLines) <= len(wide.DetailLines) {
		t.Fatalf("expected narrower viewport to increase wrapped detail line count, wide=%d narrow=%d", len(wide.DetailLines), len(narrow.DetailLines))
	}
}

func TestProjectTranscriptViewsLargeTranscriptKeepsLastEntryRange(t *testing.T) {
	entries := benchmarkDetailEntries(500)
	baseOffset := 1000
	lastEntry := baseOffset + len(entries) - 1

	projection := ProjectTranscriptViews(TranscriptProjectionInput{BaseOffset: baseOffset, Entries: entries}, TranscriptProjectionViewState{ViewportWidth: 100, ViewportLines: 30, Theme: "dark"})

	if len(projection.Detail.Blocks) == 0 || len(projection.Ongoing.Blocks) == 0 {
		t.Fatalf("expected large transcript projections to contain blocks")
	}
	lineRange, ok := projection.DetailEntryLineRanges[lastEntry]
	if !ok {
		t.Fatalf("expected detail range for last entry %d", lastEntry)
	}
	if lineRange.End < lineRange.Start || lineRange.Start < 0 {
		t.Fatalf("expected valid last entry line range, got %#v", lineRange)
	}
}

func TestTranscriptViewProjectionDetailViewportPreservesScrollAndOwners(t *testing.T) {
	projection := ProjectTranscriptViews(TranscriptProjectionInput{Entries: []TranscriptEntry{
		{Role: "user", Text: "first"},
		{Role: "assistant", Text: "second"},
		{Role: "user", Text: "third"},
	}}, defaultProjectionViewState())

	viewport := projection.DetailViewport(ProjectionViewportState{ViewportLines: 2, Scroll: 2})
	if viewport.Scroll != 2 {
		t.Fatalf("expected viewport scroll to stay at requested position, got %d", viewport.Scroll)
	}
	if len(viewport.Lines) != 2 || len(viewport.Owners) != 2 {
		t.Fatalf("expected two viewport lines with owners, got lines=%#v owners=%#v", viewport.Lines, viewport.Owners)
	}
	if viewport.Owners[0] < 0 {
		t.Fatalf("expected first visible line to retain an entry owner, owners=%#v", viewport.Owners)
	}
}

func TestTranscriptViewProjectionDetailViewportBottomAnchorUsesOffset(t *testing.T) {
	projection := ProjectTranscriptViews(TranscriptProjectionInput{Entries: benchmarkDetailEntries(4)}, TranscriptProjectionViewState{ViewportWidth: 80, ViewportLines: 3, Theme: "dark"})

	viewport := projection.DetailViewport(ProjectionViewportState{ViewportLines: 3, BottomAnchor: true, BottomOffset: 2})
	if viewport.Scroll != viewport.MaxScroll-2 {
		t.Fatalf("expected bottom offset to anchor viewport above bottom, scroll=%d max=%d", viewport.Scroll, viewport.MaxScroll)
	}
	if viewport.BottomOffset != 2 {
		t.Fatalf("expected bottom offset to be preserved, got %d", viewport.BottomOffset)
	}
}

func TestModelTranscriptProjectionInputRevisionAdvancesOnCanonicalInputChanges(t *testing.T) {
	m := NewModel(WithPreviewLines(20))
	initial := m.TranscriptProjectionInput().Revision

	m = updateModel(t, m, AppendTranscriptMsg{Role: "user", Text: "prompt"})
	afterTranscript := m.TranscriptProjectionInput()
	if afterTranscript.Revision <= initial || afterTranscript.EntriesRevision <= 0 {
		t.Fatalf("expected transcript append to advance projection revisions, got %+v initial=%d", afterTranscript, initial)
	}

	m = updateModel(t, m, StreamAssistantMsg{Delta: "stream"})
	afterStreaming := m.TranscriptProjectionInput()
	if afterStreaming.Revision <= afterTranscript.Revision {
		t.Fatalf("expected streaming assistant to advance projection revision, before=%d after=%d", afterTranscript.Revision, afterStreaming.Revision)
	}
	if afterStreaming.EntriesRevision != afterTranscript.EntriesRevision {
		t.Fatalf("expected streaming assistant to keep committed entries revision stable, before=%d after=%d", afterTranscript.EntriesRevision, afterStreaming.EntriesRevision)
	}

	m = updateModel(t, m, UpsertStreamingReasoningMsg{Key: "r1", Role: "reasoning", Text: "thinking"})
	afterReasoning := m.TranscriptProjectionInput().Revision
	if afterReasoning <= afterStreaming.Revision {
		t.Fatalf("expected streaming reasoning to advance projection revision, before=%d after=%d", afterStreaming.Revision, afterReasoning)
	}
}

func TestModelTranscriptProjectionInputRevisionDoesNotAdvanceOnViewStateOnlyChanges(t *testing.T) {
	m := NewModel(WithPreviewLines(20))
	m = updateModel(t, m, SetConversationMsg{Entries: []TranscriptEntry{{Role: "user", Text: "prompt"}}})
	before := m.TranscriptProjectionInput().Revision

	m = updateModel(t, m, SetSelectedTranscriptEntryMsg{EntryIndex: 0, Active: true, RefreshDetailSnapshot: true})
	m = updateModel(t, m, SetViewportSizeMsg{Lines: 10, Width: 100})

	after := m.TranscriptProjectionInput().Revision
	if after != before {
		t.Fatalf("expected view state changes to keep canonical projection revision stable, before=%d after=%d", before, after)
	}
}
