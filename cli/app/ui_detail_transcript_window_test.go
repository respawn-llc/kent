package app

import (
	"core/cli/tui"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/textutil"
	"errors"
	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/protobuf/proto"
	"strconv"
	"testing"
)

func TestDetailModeTransitionLoadsServerBackedTranscriptPage(t *testing.T) {
	sessionViews := &countingSessionViewClient{page: &transcriptpb.Page{ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED, SessionId: detailTestSessionID,
		Entries: []*transcriptpb.CommittedRow{detailTestUserRow("first page")}}}
	runtimeClient := &runtimeControlFakeClient{sessionView: &runtimepb.SessionView{SessionId: detailTestSessionID}}
	model := newProjectedClosedUIModel(
		runtimeClient,
		WithUISessionID(detailTestSessionID),
		WithUIStatusConfig(uiStatusConfig{SessionViews: sessionViews}),
	)
	model.view = tui.NewModel()
	model.view = mustUpdateTUIModel(t, model.view, tui.SetModeMsg{Mode: tui.ModeDetail})
	cmd := model.detailLoadCmdForModeTransition(tui.ModeOngoing, tui.ModeDetail)
	if cmd == nil {
		t.Fatal("detail transition did not create a transcript page load command")
	}
	if model.pendingDetailTranscript == nil || !model.pendingDetailTranscript.detailMode {
		t.Fatalf("detail transition request = %#v, want Detail Mode provenance", model.pendingDetailTranscript)
	}

	for _, msg := range collectCmdMessages(t, cmd) {
		if load, ok := msg.(detailTranscriptLoadMsg); ok {
			model = updateUIModel(t, model, load)
		}
	}

	if !model.detailTranscript.loaded {
		t.Fatal("detail transcript window was not loaded")
	}
	got := model.detailTranscript.page()
	want := []*transcriptpb.CommittedRow{detailTestUserRow("first page")}
	if !detailTestRowsEqual(got.Entries, want) {
		t.Fatalf("detail transcript entries = %#v, want %#v", got.Entries, want)
	}
}

func TestDetailModeReentryShowsCachedPageWhileRefreshingNewestPage(t *testing.T) {
	sessionViews := newControlledTranscriptPageClient()
	cached := &transcriptpb.Page{ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED, SessionId: detailTestSessionID,
		Entries: []*transcriptpb.CommittedRow{
			detailTestAssistantRow("cached detail page\nsecond line")}}
	cached.Entries[0].Visibility = transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING
	model := newProjectedClosedUIModel(
		&runtimeControlFakeClient{},
		WithUISessionID(detailTestSessionID),
		WithUIStatusConfig(uiStatusConfig{SessionViews: sessionViews}),
	)
	model.detailTranscript.replace(cached)
	model.view = tui.NewModel()
	model.forwardToView(tui.SetViewportSizeMsg{Lines: 1, Width: 80})
	model.forwardToView(tui.SetDetailTranscriptPageMsg{Page: cached})

	cmd := model.transitionTranscriptModeWithOptions(transcriptModeTransitionOptions{
		target:            tui.ModeDetail,
		suppressAltScreen: true,
		preserveSurface:   true,
	})
	if cmd == nil {
		t.Fatal("detail reentry did not request a fresh newest page")
	}
	if got := model.detailTranscript.page().Entries; !detailTestRowsEqual(got, cached.Entries) {
		t.Fatalf("detail reentry synchronously replaced cached membership: %#v", got)
	}
	done := runDetailTranscriptCommand(cmd)
	request := waitForDetailTranscriptRequest(t, sessionViews)
	if request.Direction != nil {
		t.Fatalf("detail reentry request = %#v, want newest-page request without cursors", request)
	}
	sessionViews.results <- controlledTranscriptPageResult{err: errors.New("stop test refresh")}
	_ = waitForDetailTranscriptCompletion(t, done)
}

func TestDetailModeNewestRefreshPreservesCachedNavigationWhenSelectedRowSurvives(t *testing.T) {
	sessionViews := newControlledTranscriptPageClient()
	selected := detailTestUserRow("selected full detail")
	selected.Visibility = transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING
	selectedCondensed := "selected"
	selected.GetUser().CondensedText = &selectedCondensed
	cached := &transcriptpb.Page{ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED, SessionId: detailTestSessionID,
		Entries: []*transcriptpb.CommittedRow{
			selected,
			detailTestUserRow("cached newest")}}
	model := newProjectedClosedUIModel(
		&runtimeControlFakeClient{},
		WithUISessionID(detailTestSessionID),
		WithUIStatusConfig(uiStatusConfig{SessionViews: sessionViews}),
	)
	model.detailTranscript.replace(cached)
	model.view = tui.NewModel()
	model.forwardToView(tui.SetViewportSizeMsg{Lines: 1, Width: 80})
	model.forwardToView(tui.SetDetailTranscriptPageMsg{
		Page:   cached,
		Anchor: tui.DetailTranscriptAnchorBottom,
	})

	cmd := model.transitionTranscriptModeWithOptions(transcriptModeTransitionOptions{
		target:            tui.ModeDetail,
		suppressAltScreen: true,
		preserveSurface:   true,
	})
	done := runDetailTranscriptCommand(cmd)
	waitForDetailTranscriptRequest(t, sessionViews)

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = next.(*uiModel)
	if action := model.view.DetailSelectionAction(); action != tui.DetailSelectionActionNone {
		t.Fatalf("cached navigation selected action = %v, want no action for complete surviving row", action)
	}

	sessionViews.results <- controlledTranscriptPageResult{response: &transcriptpb.PageSuccess{
		Transcript: &transcriptpb.Page{ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED, SessionId: detailTestSessionID,
			Entries: []*transcriptpb.CommittedRow{
				detailTestAssistantRow("fresh older boundary"),
				selected,
				detailTestUserRow("fresh newest")}}}}
	model = updateUIModel(t, model, waitForDetailTranscriptCompletion(t, done))
	if action := model.view.DetailSelectionAction(); action != tui.DetailSelectionActionNone {
		t.Fatalf("newest refresh selected action = %v, want no action for complete surviving row", action)
	}
}

func TestDetailModeNewestRefreshAnchorsAtEndWhenCachedWindowHasNewerContent(t *testing.T) {
	selected := detailTestUserRow("selected full detail")
	selected.Visibility = transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING
	selectedCondensed := "selected"
	selected.GetUser().CondensedText = &selectedCondensed
	cached := &transcriptpb.Page{ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED, SessionId: detailTestSessionID,
		NewerCursor:  appInt64Ptr(75),
		HasMoreBelow: true,
		Entries: []*transcriptpb.CommittedRow{
			selected,
			detailTestUserRow("cached page end")}}
	model := newProjectedClosedUIModel(
		&runtimeControlFakeClient{},
		WithUISessionID(detailTestSessionID))
	model.detailTranscript.replace(cached)
	model.view = tui.NewModel()
	model.forwardToView(tui.SetViewportSizeMsg{Lines: 1, Width: 80})
	model.forwardToView(tui.SetDetailTranscriptPageMsg{
		Page:   cached,
		Anchor: tui.DetailTranscriptAnchorBottom,
	})
	model.forwardToView(tui.SetModeMsg{Mode: tui.ModeDetail})
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = next.(*uiModel)
	if action := model.view.DetailSelectionAction(); action != tui.DetailSelectionActionNone {
		t.Fatalf("cached older-window selected action = %v, want no action for complete row", action)
	}

	model.applyDetailTranscriptLoad(detailTestSessionID, &transcriptpb.PageRequest{}, &transcriptpb.Page{ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED, SessionId: detailTestSessionID,
		Entries: []*transcriptpb.CommittedRow{
			detailTestAssistantRow("fresh older boundary"),
			selected,
			detailTestUserRow("fresh newest")}})

	if action := model.view.DetailSelectionAction(); action != tui.DetailSelectionActionNone {
		t.Fatalf("older-window refresh selected action = %v, want fresh page end", action)
	}
}

func TestDetailModeReentryReloadsNewestPageAndSelectsItsEnd(t *testing.T) {
	stale := &transcriptpb.Page{ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED, SessionId: detailTestSessionID,
		Entries: []*transcriptpb.CommittedRow{
			detailTestAssistantRow("stale detail page")}}
	fresh := &transcriptpb.Page{ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED, SessionId: detailTestSessionID,
		Entries: []*transcriptpb.CommittedRow{
			detailTestAssistantRow("fresh expandable\nline two\nline three\nline four"),
			detailTestUserRow("fresh newest")}}
	sessionViews := &countingSessionViewClient{page: fresh}
	model := newProjectedClosedUIModel(
		&runtimeControlFakeClient{},
		WithUISessionID(detailTestSessionID),
		WithUIStatusConfig(uiStatusConfig{SessionViews: sessionViews}),
	)
	model.detailTranscript.replace(stale)
	model.view = tui.NewModel()
	model.forwardToView(tui.SetViewportSizeMsg{Lines: 1, Width: 80})
	model.forwardToView(tui.SetDetailTranscriptPageMsg{Page: stale})

	cmd := model.transitionTranscriptModeWithOptions(transcriptModeTransitionOptions{
		target:            tui.ModeDetail,
		suppressAltScreen: true,
		preserveSurface:   true,
	})
	if cmd == nil {
		t.Fatal("detail reentry did not request a fresh newest page")
	}
	for _, msg := range collectCmdMessages(t, cmd) {
		if load, ok := msg.(detailTranscriptLoadMsg); ok {
			model = updateUIModel(t, model, load)
		}
	}

	if sessionViews.lastPageReq.Direction != nil {
		t.Fatalf("detail reentry request = %#v, want newest-page request without cursors", sessionViews.lastPageReq)
	}
	if got := model.detailTranscript.page().Entries; !detailTestRowsEqual(got, fresh.Entries) {
		t.Fatalf("detail reentry entries = %#v, want fresh newest page %#v", got, fresh.Entries)
	}
	if action := model.view.DetailSelectionAction(); action != tui.DetailSelectionActionNone {
		t.Fatalf("detail reentry selected action = %v, want non-expandable newest row at page end", action)
	}
}

func TestDetailTranscriptLoadMergesAdjacentCursorPages(t *testing.T) {
	model := newProjectedClosedUIModel(&runtimeControlFakeClient{})
	newest := &transcriptpb.Page{ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED, SessionId: detailTestSessionID,
		OlderCursor:  appInt64Ptr(40),
		HasMoreAbove: true,
		NewerCursor:  appInt64Ptr(80),
		HasMoreBelow: false,
		Entries:      []*transcriptpb.CommittedRow{detailTestAssistantRow("newer")}}
	older := &transcriptpb.Page{ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED, SessionId: detailTestSessionID,
		OlderCursor:  appInt64Ptr(20),
		HasMoreAbove: false,
		NewerCursor:  appInt64Ptr(40),
		HasMoreBelow: true,
		Entries:      []*transcriptpb.CommittedRow{detailTestUserRow("older")}}

	model.applyDetailTranscriptLoad("", &transcriptpb.PageRequest{}, newest)
	model.applyDetailTranscriptLoad("", &transcriptpb.PageRequest{Direction: &transcriptpb.PageRequest_Cursor{Cursor: *appInt64Ptr(40)}}, older)

	got := model.detailTranscript.page()
	wantEntries := []*transcriptpb.CommittedRow{
		detailTestUserRow("older"),
		detailTestAssistantRow("newer")}
	if !detailTestRowsEqual(got.Entries, wantEntries) {
		t.Fatalf("merged detail entries = %#v, want %#v", got.Entries, wantEntries)
	}
	if got.OlderCursor == nil || *got.OlderCursor != 20 || got.HasMoreAbove || got.NewerCursor == nil || *got.NewerCursor != 80 || got.HasMoreBelow {
		t.Fatalf("merged cursors = older:%v above:%t newer:%v below:%t", got.OlderCursor, got.HasMoreAbove, got.NewerCursor, got.HasMoreBelow)
	}
}

func TestDetailTranscriptLoadIgnoresDuplicateAdjacentCursorResponse(t *testing.T) {
	model := newProjectedClosedUIModel(&runtimeControlFakeClient{})
	newest := &transcriptpb.Page{ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED, SessionId: detailTestSessionID,
		OlderCursor:  appInt64Ptr(40),
		HasMoreAbove: true,
		Entries:      []*transcriptpb.CommittedRow{detailTestAssistantRow("newer")},
		HasMoreBelow: false}
	older := &transcriptpb.Page{ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED, SessionId: detailTestSessionID,
		OlderCursor:  appInt64Ptr(20),
		HasMoreAbove: false,
		NewerCursor:  appInt64Ptr(40),
		HasMoreBelow: true,
		Entries:      []*transcriptpb.CommittedRow{detailTestUserRow("older")}}
	request := &transcriptpb.PageRequest{Direction: &transcriptpb.PageRequest_Cursor{Cursor: *appInt64Ptr(40)}}

	model.applyDetailTranscriptLoad("", &transcriptpb.PageRequest{}, newest)
	model.applyDetailTranscriptLoad("", request, older)
	model.applyDetailTranscriptLoad("", request, older)

	got := model.detailTranscript.page().Entries
	want := []*transcriptpb.CommittedRow{
		detailTestUserRow("older"),
		detailTestAssistantRow("newer")}
	if !detailTestRowsEqual(got, want) {
		t.Fatalf("deduplicated detail entries = %#v, want %#v", got, want)
	}
}

func TestDetailTranscriptNewestRefreshReplacesResidentWindow(t *testing.T) {
	model := newProjectedClosedUIModel(&runtimeControlFakeClient{})
	newest := &transcriptpb.Page{ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED, SessionId: detailTestSessionID,
		OlderCursor:  appInt64Ptr(40),
		HasMoreAbove: true,
		NewerCursor:  appInt64Ptr(80),
		HasMoreBelow: false,
		Entries:      []*transcriptpb.CommittedRow{detailTestAssistantRow("newer")}}
	older := &transcriptpb.Page{ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED, SessionId: detailTestSessionID,
		OlderCursor:  appInt64Ptr(20),
		HasMoreAbove: false,
		NewerCursor:  appInt64Ptr(40),
		HasMoreBelow: true,
		Entries:      []*transcriptpb.CommittedRow{detailTestUserRow("older")}}
	refreshedNewest := newest
	refreshedNewest.Entries = []*transcriptpb.CommittedRow{
		detailTestAssistantRow("newer refreshed\nline two\nline three\nline four")}

	model.applyDetailTranscriptLoad("", &transcriptpb.PageRequest{}, newest)
	model.applyDetailTranscriptLoad("", &transcriptpb.PageRequest{Direction: &transcriptpb.PageRequest_Cursor{Cursor: *appInt64Ptr(40)}}, older)
	model.applyDetailTranscriptLoad("", &transcriptpb.PageRequest{}, refreshedNewest)

	got := model.detailTranscript.page().Entries
	want := refreshedNewest.Entries
	if !detailTestRowsEqual(got, want) {
		t.Fatalf("detail entries after newest refresh = %#v, want refreshed newest page %#v", got, want)
	}
	if len(model.detailTranscript.segments) != 1 {
		t.Fatalf("resident segment count = %d, want refreshed newest segment only", len(model.detailTranscript.segments))
	}
}

func TestDetailTranscriptNewestRefreshReplacesChangedSelectedRow(t *testing.T) {
	model := newProjectedClosedUIModel(&runtimeControlFakeClient{})
	model.view = tui.NewModel()
	model.view = mustUpdateTUIModel(t, model.view, tui.SetViewportSizeMsg{Lines: 2, Width: 80})
	model.view = mustUpdateTUIModel(t, model.view, tui.SetModeMsg{Mode: tui.ModeDetail})
	newest := &transcriptpb.Page{ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED, SessionId: detailTestSessionID,
		OlderCursor:  appInt64Ptr(40),
		HasMoreAbove: true,
		NewerCursor:  appInt64Ptr(80),
		HasMoreBelow: false,
		Entries: []*transcriptpb.CommittedRow{
			detailTestAssistantRow("newer\nline two\nline three\nline four")}}
	older := &transcriptpb.Page{ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED, SessionId: detailTestSessionID,
		OlderCursor:  appInt64Ptr(20),
		HasMoreAbove: false,
		NewerCursor:  appInt64Ptr(40),
		HasMoreBelow: true,
		Entries:      []*transcriptpb.CommittedRow{detailTestUserRow("older")}}
	refreshedNewest := newest
	refreshedNewest.Entries = []*transcriptpb.CommittedRow{
		detailTestAssistantRow("newer refreshed\nline two\nline three\nline four")}

	model.applyDetailTranscriptLoad("", &transcriptpb.PageRequest{}, newest)
	model.applyDetailTranscriptLoad("", &transcriptpb.PageRequest{Direction: &transcriptpb.PageRequest_Cursor{Cursor: *appInt64Ptr(40)}}, older)
	model.view = mustUpdateTUIModel(t, model.view, tea.KeyMsg{Type: tea.KeyEnter})
	if got := model.view.DetailSelectionAction(); got != tui.DetailSelectionActionNone {
		t.Fatalf("detail selection action after Enter = %v, want no action for complete row", got)
	}

	model.applyDetailTranscriptLoad("", &transcriptpb.PageRequest{}, refreshedNewest)

	if got := model.view.DetailSelectionAction(); got != tui.DetailSelectionActionNone {
		t.Fatalf("detail selection action after newest refresh = %v, want no action for complete row", got)
	}
	got := model.detailTranscript.page().Entries
	want := refreshedNewest.Entries
	if !detailTestRowsEqual(got, want) {
		t.Fatalf("detail entries after newest refresh = %#v, want refreshed newest page %#v", got, want)
	}
}

func TestDetailTranscriptWindowTrimsFarResidentSegmentsAfterAppend(t *testing.T) {
	var window uiDetailTranscriptWindow
	window.replace(detailTestPage(detailTestSessionID, 0, 599, appInt64Ptr(600), nil))
	window.appendCursorPage(detailTestPage(detailTestSessionID, 600, 1199, appInt64Ptr(1200), appInt64Ptr(600)))
	window.appendCursorPage(detailTestPage(detailTestSessionID, 1200, 1799, nil, appInt64Ptr(1200)))

	if len(window.segments) != uiDetailTranscriptMinResidentSegments {
		t.Fatalf("resident segment count = %d, want %d", len(window.segments), uiDetailTranscriptMinResidentSegments)
	}
	if len(window.entries) != 1200 {
		t.Fatalf("resident entry count = %d, want two retained 600-entry segments", len(window.entries))
	}
	if got := window.entries[0].GetAssistant().Text; got != "entry-600" {
		t.Fatalf("first retained entry = %q, want appended window to unload oldest segment", got)
	}
}

func TestDetailTranscriptWindowTrimsFarResidentSegmentsAfterPrepend(t *testing.T) {
	var window uiDetailTranscriptWindow
	window.replace(detailTestPage(detailTestSessionID, 1200, 1799, nil, appInt64Ptr(1200)))
	window.prependCursorPage(detailTestPage(detailTestSessionID, 600, 1199, appInt64Ptr(600), appInt64Ptr(1200)))
	window.prependCursorPage(detailTestPage(detailTestSessionID, 0, 599, appInt64Ptr(1), appInt64Ptr(600)))

	if len(window.segments) != uiDetailTranscriptMinResidentSegments {
		t.Fatalf("resident segment count = %d, want %d", len(window.segments), uiDetailTranscriptMinResidentSegments)
	}
	if len(window.entries) != 1200 {
		t.Fatalf("resident entry count = %d, want two retained 600-entry segments", len(window.entries))
	}
	if got := window.entries[len(window.entries)-1].GetAssistant().Text; got != "entry-1199" {
		t.Fatalf("last retained entry = %q, want prepended window to unload newest far segment", got)
	}
}

// The resident detail window holds two pages regardless of their entry counts.
func TestDetailTranscriptWindowTrimsFarSegmentByCountAfterAppend(t *testing.T) {
	var window uiDetailTranscriptWindow
	window.replace(detailTestPage(detailTestSessionID, 0, 2, appInt64Ptr(3), nil))
	window.appendCursorPage(detailTestPage(detailTestSessionID, 3, 5, appInt64Ptr(6), appInt64Ptr(3)))
	window.appendCursorPage(detailTestPage(detailTestSessionID, 6, 8, nil, appInt64Ptr(6)))

	if len(window.segments) != uiDetailTranscriptMinResidentSegments {
		t.Fatalf("resident segment count = %d, want %d (segment-driven, not entry-count)", len(window.segments), uiDetailTranscriptMinResidentSegments)
	}
	if len(window.entries) != 6 {
		t.Fatalf("resident entry count = %d, want two retained 3-entry segments", len(window.entries))
	}
	if got := window.entries[0].GetAssistant().Text; got != "entry-3" {
		t.Fatalf("first retained entry = %q, want appended window to unload oldest segment", got)
	}
}

func TestDetailTranscriptWindowTrimsFarSegmentByCountAfterPrepend(t *testing.T) {
	var window uiDetailTranscriptWindow
	window.replace(detailTestPage(detailTestSessionID, 6, 8, nil, appInt64Ptr(6)))
	window.prependCursorPage(detailTestPage(detailTestSessionID, 3, 5, appInt64Ptr(3), appInt64Ptr(6)))
	window.prependCursorPage(detailTestPage(detailTestSessionID, 0, 2, appInt64Ptr(0), appInt64Ptr(3)))

	if len(window.segments) != uiDetailTranscriptMinResidentSegments {
		t.Fatalf("resident segment count = %d, want %d (segment-driven, not entry-count)", len(window.segments), uiDetailTranscriptMinResidentSegments)
	}
	if len(window.entries) != 6 {
		t.Fatalf("resident entry count = %d, want two retained 3-entry segments", len(window.entries))
	}
	if got := window.entries[len(window.entries)-1].GetAssistant().Text; got != "entry-5" {
		t.Fatalf("last retained entry = %q, want prepended window to unload newest far segment", got)
	}
}

func TestDetailTranscriptPageDeepClonesPatchPresentation(t *testing.T) {
	model := newProjectedClosedUIModel(&runtimeControlFakeClient{})
	sourcePatch := detailTestPatchPresentation(3)
	sourceFile := sourcePatch.GetChanges().Files[0]
	sourceFile.Operations = append([]*transcriptpb.PatchFileOperation{{
		Operation: &transcriptpb.PatchFileOperation_Update{Update: &transcriptpb.PatchUpdateOperation{
			Groups: []*transcriptpb.PatchChangeGroup{
				{Lines: []*transcriptpb.PatchChangedLine{
					{Kind: transcriptpb.PatchChangedLineKind_PATCH_CHANGED_LINE_KIND_REMOVED, Content: "old"}}}},
		}},
	}}, sourceFile.Operations...)
	sourceFile.Operations[1].GetDelete().Disposition.Removed = 2
	page := &transcriptpb.Page{ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED, SessionId: detailTestSessionID,
		Entries: []*transcriptpb.CommittedRow{
			detailTestToolRow(&transcriptpb.ToolRow{ToolCallId: proto.String(string("3f14b1c8-5fee-4c0e-a72e-f38bb6c3c389")),
				ToolName: proto.String("patch"),
				Presentation: &transcriptpb.ToolPresentation{
					Presentation:      transcriptpb.ToolPresentationKind_TOOL_PRESENTATION_KIND_DEFAULT,
					RenderBehavior:    transcriptpb.ToolPresentationKind_TOOL_PRESENTATION_KIND_DEFAULT,
					PatchPresentation: sourcePatch,
				}})}}

	model.applyDetailTranscriptLoad("", &transcriptpb.PageRequest{}, page)
	sourcePatch.GetChanges().Files[0].Operations[0].GetUpdate().Groups[0].Lines[0].Content = "source changed"
	firstRead := model.detailTranscript.page()
	if got := firstRead.Entries[0].GetTool().Presentation.PatchPresentation.GetChanges().Files[0].Operations[0].GetUpdate().Groups[0].Lines[0].Content; got != "old" {
		t.Fatalf("stored patch diff = %q, want source-isolated old diff", got)
	}
	sourcePatch.GetChanges().Files[0].Operations[1].GetDelete().Disposition.Removed = 8
	if got := firstRead.Entries[0].GetTool().Presentation.PatchPresentation.GetChanges().Files[0].Operations[1].GetDelete().Disposition.Removed; got != 2 {
		t.Fatalf("stored deletion count = %d, want source-isolated 2", got)
	}

	firstRead.Entries[0].GetTool().Presentation.PatchPresentation.GetChanges().Files[0].Operations[0].GetUpdate().Groups[0].Lines[0].Content = "read changed"
	firstRead.Entries[0].GetTool().Presentation.PatchPresentation.GetChanges().Files[0].Operations[1].GetDelete().Disposition.Removed = 9
	secondRead := model.detailTranscript.page()
	if got := secondRead.Entries[0].GetTool().Presentation.PatchPresentation.GetChanges().Files[0].Operations[0].GetUpdate().Groups[0].Lines[0].Content; got != "old" {
		t.Fatalf("page patch diff = %q, want page-isolated old diff", got)
	}
	if got := secondRead.Entries[0].GetTool().Presentation.PatchPresentation.GetChanges().Files[0].Operations[1].GetDelete().Disposition.Removed; got != 2 {
		t.Fatalf("page deletion count = %d, want page-isolated 2", got)
	}
}

func TestDetailTranscriptPagePreservesKnownZeroDeletionCount(t *testing.T) {
	model := newProjectedClosedUIModel(&runtimeControlFakeClient{})
	page := &transcriptpb.Page{ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED, SessionId: detailTestSessionID,
		Entries: []*transcriptpb.CommittedRow{
			detailTestToolRow(&transcriptpb.ToolRow{ToolCallId: proto.String(string("da35dc7a-02e8-4993-aa45-5cfba6eb4546")),
				ToolName: proto.String(string("patch")),
				Presentation: &transcriptpb.ToolPresentation{
					Presentation:      transcriptpb.ToolPresentationKind_TOOL_PRESENTATION_KIND_DEFAULT,
					RenderBehavior:    transcriptpb.ToolPresentationKind_TOOL_PRESENTATION_KIND_DEFAULT,
					PatchPresentation: detailTestPatchPresentation(0),
				}})}}

	model.applyDetailTranscriptLoad("", &transcriptpb.PageRequest{}, page)
	stored := model.detailTranscript.page()
	file := stored.Entries[0].GetTool().Presentation.PatchPresentation.GetChanges().Files[0]
	if removed := file.Removed; removed == nil || *removed != 0 {
		t.Fatalf("detail client removed count = %v, want present zero", removed)
	}
}

func detailTestPatchPresentation(removed int32) *transcriptpb.PatchPresentation {
	return &transcriptpb.PatchPresentation{Presentation: &transcriptpb.PatchPresentation_Changes{
		Changes: &transcriptpb.PatchChanges{Files: []*transcriptpb.PatchFileChange{{
			Path:    &transcriptpb.PatchPath{Absolute: "/workspace/file.txt", Relative: "file.txt"},
			Removed: &removed,
			Operations: []*transcriptpb.PatchFileOperation{{
				Operation: &transcriptpb.PatchFileOperation_Delete{Delete: &transcriptpb.PatchDeletionOperation{
					Id: &transcriptpb.PatchDeletionOperationID{HunkOrdinal: 0},
					Disposition: &transcriptpb.PatchDeletionDisposition{
						PhysicalGroup: &transcriptpb.PatchDeletionGroupID{FirstOperation: &transcriptpb.PatchDeletionOperationID{HunkOrdinal: 0}},
						Removed:       removed,
					},
				}},
			}},
		}}},
	}}
}

func mustUpdateTUIModel(t *testing.T, model tui.Model, msg tea.Msg) tui.Model {
	t.Helper()
	next, _ := model.Update(msg)
	updated, ok := next.(tui.Model)
	if !ok {
		t.Fatalf("updated TUI model type = %T, want tui.Model", next)
	}
	return updated
}

func detailTestPage(sessionID string, first int, last int, olderCursor *int64, newerCursor *int64) *transcriptpb.Page {
	return &transcriptpb.Page{ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED, SessionId: sessionID,
		OlderCursor:  olderCursor,
		HasMoreAbove: olderCursor != nil,
		NewerCursor:  newerCursor,
		HasMoreBelow: newerCursor != nil,
		Entries:      detailTestEntries(first, last),
		SessionName:  textutil.Value(sessionID)}
}

func detailTestEntries(first int, last int) []*transcriptpb.CommittedRow {
	if last < first {
		return nil
	}
	entries := make([]*transcriptpb.CommittedRow, 0, last-first+1)
	for idx := first; idx <= last; idx++ {
		entries = append(entries, detailTestAssistantRow("entry-"+strconv.Itoa(idx)))
	}
	return entries
}

func TestDetailTranscriptLoadIgnoresStaleSessionResponse(t *testing.T) {
	model := newProjectedClosedUIModel(&runtimeControlFakeClient{}, WithUISessionID(detailTestSessionID))
	model.applyDetailTranscriptLoad(
		detailTestSessionID, &transcriptpb.PageRequest{}, &transcriptpb.Page{ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED, SessionId: detailTestSessionID,
			Entries: []*transcriptpb.CommittedRow{detailTestAssistantRow("current")}})

	model.applyDetailTranscriptLoad(
		detailTestStaleSessionID, &transcriptpb.PageRequest{}, &transcriptpb.Page{ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED, SessionId: detailTestStaleSessionID,
			Entries: []*transcriptpb.CommittedRow{detailTestAssistantRow("stale")}})

	got := model.detailTranscript.page().Entries
	want := []*transcriptpb.CommittedRow{detailTestAssistantRow("current")}
	if !detailTestRowsEqual(got, want) {
		t.Fatalf("detail entries after stale response = %#v, want %#v", got, want)
	}
}

func TestDetailTranscriptIgnoresRuntimeCommittedChangesUntilPageIsRequested(t *testing.T) {
	model := newProjectedClosedUIModel(&runtimeControlFakeClient{}, WithUISessionID(detailTestSessionID))
	model.applyDetailTranscriptLoad(
		detailTestSessionID, &transcriptpb.PageRequest{}, &transcriptpb.Page{ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED, SessionId: detailTestSessionID,
			Entries: []*transcriptpb.CommittedRow{detailTestAssistantRow("loaded page")}})

	got := model.detailTranscript.page().Entries
	want := []*transcriptpb.CommittedRow{detailTestAssistantRow("loaded page")}
	if !detailTestRowsEqual(got, want) {
		t.Fatalf("detail entries after committed runtime event = %#v, want stale loaded page %#v", got, want)
	}
}

func TestDetailEdgeRequestMessageLoadsAdjacentPage(t *testing.T) {
	sessionViews := &countingSessionViewClient{page: &transcriptpb.Page{ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED, SessionId: detailTestSessionID,
		Entries: []*transcriptpb.CommittedRow{detailTestUserRow("older page")}}}
	model := newProjectedClosedUIModel(
		&runtimeControlFakeClient{sessionView: &runtimepb.SessionView{SessionId: detailTestSessionID}},
		WithUISessionID(detailTestSessionID),
		WithUIStatusConfig(uiStatusConfig{SessionViews: sessionViews}))
	model.detailTranscript.replace(&transcriptpb.Page{ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED, SessionId: detailTestSessionID,
		OlderCursor:  appInt64Ptr(25),
		HasMoreAbove: true,
		Entries:      []*transcriptpb.CommittedRow{detailTestAssistantRow("current page")}})

	next, cmd := model.Update(tui.RequestDetailTranscriptPageMsg{Direction: tui.DetailTranscriptPageOlder})
	model = next.(*uiModel)
	for _, msg := range collectCmdMessages(t, cmd) {
		if load, ok := msg.(detailTranscriptLoadMsg); ok {
			model = updateUIModel(t, model, load)
		}
	}

	got := model.detailTranscript.page().Entries
	want := []*transcriptpb.CommittedRow{
		detailTestUserRow("older page"),
		detailTestAssistantRow("current page")}
	if !detailTestRowsEqual(got, want) {
		t.Fatalf("loaded adjacent entries = %#v, want %#v", got, want)
	}
}

var _ tea.Msg = detailTranscriptLoadMsg{}

func appInt64Ptr(value int64) *int64 {
	return &value
}
