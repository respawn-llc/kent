package app

import (
	"fmt"
	"testing"

	"core/shared/clientui"
)

func TestDetailTranscriptWindowMergeExpandsOverlappingPages(t *testing.T) {
	window := uiDetailTranscriptWindow{}
	window.replace(testTranscriptPage(100, 3, 110))
	window.merge(testTranscriptPage(103, 2, 110))

	if !window.loaded {
		t.Fatal("expected merged window to stay loaded")
	}
	if window.offset != 100 {
		t.Fatalf("offset = %d, want 100", window.offset)
	}
	if got := len(window.entries); got != 5 {
		t.Fatalf("entry count = %d, want 5", got)
	}
	if window.totalEntries != 110 {
		t.Fatalf("total entries = %d, want 110", window.totalEntries)
	}
	if window.entries[0].Text != "line 100" || window.entries[4].Text != "line 104" {
		t.Fatalf("unexpected merged entries: first=%q last=%q", window.entries[0].Text, window.entries[4].Text)
	}
	if want := (clientui.TranscriptPageRequest{Offset: 103, Limit: 2}); !pageRequestEqual(window.lastRequest, want) {
		t.Fatalf("last request = %+v, want %+v", window.lastRequest, want)
	}
}

func TestDetailTranscriptWindowTrimAroundKeepsAnchorRegionBounded(t *testing.T) {
	window := uiDetailTranscriptWindow{}
	page := testTranscriptPage(0, uiDetailTranscriptMaxEntries+200, uiDetailTranscriptMaxEntries+200)
	window.replace(page)

	if got := len(window.entries); got != uiDetailTranscriptMaxEntries {
		t.Fatalf("entry count after trim = %d, want %d", got, uiDetailTranscriptMaxEntries)
	}
	if window.offset != 0 {
		t.Fatalf("offset after tail replace trim = %d, want 0", window.offset)
	}

	window = uiDetailTranscriptWindow{}
	window.replace(testTranscriptPage(0, uiDetailTranscriptMaxEntries, uiDetailTranscriptMaxEntries+200))
	window.merge(testTranscriptPage(uiDetailTranscriptMaxEntries, 200, uiDetailTranscriptMaxEntries+200))

	if got := len(window.entries); got != uiDetailTranscriptMaxEntries {
		t.Fatalf("entry count after anchor trim = %d, want %d", got, uiDetailTranscriptMaxEntries)
	}
	if window.offset <= 0 {
		t.Fatalf("expected anchor trim to advance offset, got %d", window.offset)
	}
	last := window.offset + len(window.entries) - 1
	if last != uiDetailTranscriptMaxEntries+199 {
		t.Fatalf("trimmed last entry = %d, want %d", last, uiDetailTranscriptMaxEntries+199)
	}
}

func TestDetailTranscriptWindowPageRequests(t *testing.T) {
	window := uiDetailTranscriptWindow{}
	window.replace(testTranscriptPage(250, 250, 800))

	before, ok := window.pageBefore()
	if !ok {
		t.Fatal("expected pageBefore request")
	}
	if want := (clientui.TranscriptPageRequest{Offset: 0, Limit: 250}); !pageRequestEqual(before, want) {
		t.Fatalf("pageBefore = %+v, want %+v", before, want)
	}

	after, ok := window.pageAfter()
	if !ok {
		t.Fatal("expected pageAfter request")
	}
	if want := (clientui.TranscriptPageRequest{Offset: 500, Limit: 250}); !pageRequestEqual(after, want) {
		t.Fatalf("pageAfter = %+v, want %+v", after, want)
	}

	requested := window.requestedPageForDetailEntry()
	if want := (clientui.TranscriptPageRequest{Offset: 250, Limit: 250}); !pageRequestEqual(requested, want) {
		t.Fatalf("requestedPageForDetailEntry = %+v, want %+v", requested, want)
	}
	window.reset()
	if requested := window.requestedPageForDetailEntry(); !pageRequestEqual(requested, (clientui.TranscriptPageRequest{Offset: 0, Limit: uiDetailTranscriptPageSize})) {
		t.Fatalf("default requested page = %+v", requested)
	}
}

func TestDetailTranscriptWindowResetClearsState(t *testing.T) {
	window := uiDetailTranscriptWindow{}
	window.replace(testTranscriptPage(10, 5, 25))
	window.reset()

	if window.loaded {
		t.Fatal("expected reset window to be unloaded")
	}
	if window.sessionID != "" || window.offset != 0 || window.totalEntries != 0 {
		t.Fatalf("expected zeroed window metadata after reset, got %+v", window)
	}
	if len(window.entries) != 0 {
		t.Fatalf("expected no entries after reset, got %d", len(window.entries))
	}
	if window.lastRequest != (clientui.TranscriptPageRequest{}) {
		t.Fatalf("expected zero lastRequest after reset, got %+v", window.lastRequest)
	}
}

func testTranscriptPage(offset, count, total int) clientui.TranscriptPage {
	page := clientui.TranscriptPage{SessionID: "session-1", Offset: offset, TotalEntries: total}
	for i := 0; i < count; i++ {
		index := offset + i
		page.Entries = append(page.Entries, clientui.ChatEntry{Role: "assistant", Text: fmt.Sprintf("line %03d", index)})
	}
	return page
}
