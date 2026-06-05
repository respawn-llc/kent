package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestDetectPathReferenceQuery(t *testing.T) {
	tests := []struct {
		name      string
		fixture   string
		wantOK    bool
		wantQuery string
	}{
		{name: "empty token", fixture: "@|", wantOK: false},
		{name: "single rune", fixture: "@s|", wantOK: false},
		{name: "ascii valid", fixture: "@sea|", wantOK: true, wantQuery: "sea"},
		{name: "double at", fixture: "@@|", wantOK: false},
		{name: "space after at", fixture: "@ |", wantOK: false},
		{name: "unicode valid", fixture: "@прив|", wantOK: true, wantQuery: "прив"},
		{name: "digits valid", fixture: "@12|", wantOK: true, wantQuery: "12"},
		{name: "nested path valid", fixture: "@cli/app/u|", wantOK: true, wantQuery: "cli/app/u"},
		{name: "hidden path valid", fixture: "@.gith|", wantOK: true, wantQuery: ".gith"},
		{name: "punctuation cancels", fixture: "@ab!|", wantOK: false},
		{name: "email style rejected", fixture: "mail@ab|", wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input, cursor := testPathReferenceFixture(tc.fixture)
			got := detectPathReferenceQuery(input, cursor)
			if got.Active != tc.wantOK {
				t.Fatalf("active = %v, want %v", got.Active, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if got.RawQuery != tc.wantQuery {
				t.Fatalf("query = %q, want %q", got.RawQuery, tc.wantQuery)
			}
		})
	}
}

func TestApplyPathReferenceCompletionReplacesMiddleSpan(t *testing.T) {
	input, cursor := testPathReferenceFixture("compare @cliap| with tests")
	query := detectPathReferenceQuery(input, cursor)
	updated, nextCursor, ok := applyPathReferenceCompletion(input, cursor, query, uiPathReferenceCandidate{Path: "cli/app/ui.go"})
	if !ok {
		t.Fatal("expected completion applied")
	}
	if updated != "compare @cli/app/ui.go with tests" {
		t.Fatalf("updated input = %q", updated)
	}
	if nextCursor != len([]rune("compare @cli/app/ui.go")) {
		t.Fatalf("cursor = %d", nextCursor)
	}
}

func TestApplyPathReferenceCompletionAddsTrailingSlashForDirectory(t *testing.T) {
	input, cursor := testPathReferenceFixture("inspect @cliap|")
	query := detectPathReferenceQuery(input, cursor)
	updated, _, ok := applyPathReferenceCompletion(input, cursor, query, uiPathReferenceCandidate{Path: "cli/app", Directory: true})
	if !ok {
		t.Fatal("expected completion applied")
	}
	if updated != "inspect @cli/app/" {
		t.Fatalf("updated input = %q", updated)
	}
}

func TestPathReferenceReactivatesAfterDirectoryCompletion(t *testing.T) {
	input, cursor := testPathReferenceFixture("inspect @cli/app/u|")
	query := detectPathReferenceQuery(input, cursor)
	if !query.Active {
		t.Fatal("expected nested path query active after directory completion")
	}
	if query.RawQuery != "cli/app/u" {
		t.Fatalf("query = %q", query.RawQuery)
	}
}

func TestPathReferenceSearchIgnoredWhileSlashPickerActive(t *testing.T) {
	search := newStubUIPathReferenceSearch()
	m := newProjectedStaticUIModel(WithUIPathReferenceSearch(search))
	m.replaceMainInput("/@ab", -1)

	if !m.slashCommandPicker().visible {
		t.Fatal("expected slash picker visible")
	}
	if m.pathReferencePicker().visible {
		t.Fatal("did not expect path picker visible while slash picker is active")
	}
	if len(search.requests) != 0 {
		t.Fatalf("did not expect path search requests, got %+v", search.requests)
	}
}

func TestPathReferenceStartupPrewarmQueuedForWorkspace(t *testing.T) {
	search := newStubUIPathReferenceSearch()
	m := newProjectedStaticUIModel(
		WithUIPathReferenceSearch(search),
		WithUIStatusConfig(uiStatusConfig{WorkspaceRoot: "/tmp/workspace"}),
	)

	if len(m.startupCmds) == 0 {
		t.Fatal("expected startup prewarm command")
	}
	for _, cmd := range m.startupCmds {
		if cmd != nil {
			_ = cmd()
		}
	}
	if len(search.prewarmRoots) != 1 || search.prewarmRoots[0] != "/tmp/workspace" {
		t.Fatalf("unexpected prewarm roots: %+v", search.prewarmRoots)
	}
}

func TestPathReferenceLoadingDelayDoesNotOverwriteFresherMatches(t *testing.T) {
	search := newStubUIPathReferenceSearch()
	m := newProjectedStaticUIModel(WithUIPathReferenceSearch(search), WithUIStatusConfig(uiStatusConfig{WorkspaceRoot: "/tmp/workspace"}))

	m.replaceMainInput("@ab", -1)
	firstToken := m.pathReference.queryToken
	firstDraft := m.pathReference.draftToken
	m.replaceMainInput("@abc", -1)
	secondToken := m.pathReference.queryToken
	secondDraft := m.pathReference.draftToken

	next, _ := m.Update(uiPathReferenceMatchResultMsg{
		WorkspaceRoot:    "/tmp/workspace",
		CorpusGeneration: 1,
		DraftToken:       secondDraft,
		QueryToken:       secondToken,
		NormalizedQuery:  "abc",
		Matches:          []uiPathReferenceCandidate{{Path: "cli/app/ui.go"}},
	})
	updated := next.(*uiModel)

	next, _ = updated.Update(uiPathReferenceLoadingDelayMsg{
		WorkspaceRoot:    "/tmp/workspace",
		CorpusGeneration: 1,
		DraftToken:       firstDraft,
		QueryToken:       firstToken,
		NormalizedQuery:  "ab",
	})
	updated = next.(*uiModel)
	if updated.pathReference.loading {
		t.Fatal("did not expect stale loading event to overwrite fresher matches")
	}
	if len(updated.pathReference.matches) != 1 || updated.pathReference.matches[0].Path != "cli/app/ui.go" {
		t.Fatalf("unexpected matches after stale loading event: %+v", updated.pathReference.matches)
	}
}

func TestPathReferenceDropsStaleCorpusGenerationEvents(t *testing.T) {
	search := newStubUIPathReferenceSearch()
	m := newProjectedStaticUIModel(WithUIPathReferenceSearch(search), WithUIStatusConfig(uiStatusConfig{WorkspaceRoot: "/tmp/workspace"}))
	m.replaceMainInput("@ab", -1)
	m.pathReference.corpusGeneration = 2

	next, _ := m.Update(uiPathReferenceCorpusReadyMsg{WorkspaceRoot: "/tmp/workspace", CorpusGeneration: 1})
	updated := next.(*uiModel)
	if updated.pathReference.corpusGeneration != 2 {
		t.Fatalf("stale corpus-ready changed generation to %d", updated.pathReference.corpusGeneration)
	}

	next, _ = updated.Update(uiPathReferenceMatchResultMsg{
		WorkspaceRoot:    "/tmp/workspace",
		CorpusGeneration: 1,
		DraftToken:       updated.pathReference.draftToken,
		QueryToken:       updated.pathReference.queryToken,
		NormalizedQuery:  updated.pathReference.normalizedQuery,
		Matches:          []uiPathReferenceCandidate{{Path: "stale.go"}},
	})
	updated = next.(*uiModel)
	if len(updated.pathReference.matches) != 0 {
		t.Fatalf("expected stale generation match dropped, got %+v", updated.pathReference.matches)
	}

	updated.pathReference.pending = true
	next, _ = updated.Update(uiPathReferenceLoadingDelayMsg{
		WorkspaceRoot:    "/tmp/workspace",
		CorpusGeneration: 1,
		DraftToken:       updated.pathReference.draftToken,
		QueryToken:       updated.pathReference.queryToken,
		NormalizedQuery:  updated.pathReference.normalizedQuery,
	})
	updated = next.(*uiModel)
	if updated.pathReference.loading {
		t.Fatal("expected stale generation loading event dropped")
	}
}

func TestPathReferenceWorkspaceSwitchDropsInFlightEvents(t *testing.T) {
	search := newStubUIPathReferenceSearch()
	m := newProjectedStaticUIModel(WithUIPathReferenceSearch(search), WithUIStatusConfig(uiStatusConfig{WorkspaceRoot: "/tmp/workspace-a"}))
	m.replaceMainInput("@ab", -1)
	staleDraft := m.pathReference.draftToken
	staleToken := m.pathReference.queryToken

	m.statusConfig.WorkspaceRoot = "/tmp/workspace-b"
	m.replaceMainInput("@ab", -1)

	next, _ := m.Update(uiPathReferenceMatchResultMsg{
		WorkspaceRoot:    "/tmp/workspace-a",
		CorpusGeneration: 1,
		DraftToken:       staleDraft,
		QueryToken:       staleToken,
		NormalizedQuery:  "ab",
		Matches:          []uiPathReferenceCandidate{{Path: "stale.go"}},
	})
	updated := next.(*uiModel)
	if len(updated.pathReference.matches) != 0 {
		t.Fatalf("expected stale workspace match dropped, got %+v", updated.pathReference.matches)
	}
	if updated.pathReference.workspaceRoot != "/tmp/workspace-b" {
		t.Fatalf("workspace root = %q", updated.pathReference.workspaceRoot)
	}
}

func TestPathReferenceUpDownNavigatesSelectionWithoutRewritingInput(t *testing.T) {
	search := newStubUIPathReferenceSearch()
	m := newProjectedStaticUIModel(WithUIPathReferenceSearch(search), WithUIStatusConfig(uiStatusConfig{WorkspaceRoot: "/tmp/workspace"}))
	m.promptHistory = []string{"older prompt"}
	m.replaceMainInput("@ab", -1)

	next, _ := m.Update(uiPathReferenceMatchResultMsg{
		WorkspaceRoot:    "/tmp/workspace",
		CorpusGeneration: 1,
		DraftToken:       m.pathReference.draftToken,
		QueryToken:       m.pathReference.queryToken,
		NormalizedQuery:  "ab",
		Matches: []uiPathReferenceCandidate{
			{Path: "cli/app"},
			{Path: "cli/app/ui.go"},
		},
	})
	updated := next.(*uiModel)

	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated = next.(*uiModel)
	if updated.pathReference.selection != 1 {
		t.Fatalf("selection = %d, want 1", updated.pathReference.selection)
	}
	if updated.input != "@ab" {
		t.Fatalf("input = %q, want unchanged draft", updated.input)
	}
	if updated.promptHistorySelection != -1 {
		t.Fatalf("did not expect prompt history navigation, got %d", updated.promptHistorySelection)
	}
}

func TestPathReferencePickerHighlightTracksAbsoluteSelectionAfterViewportScroll(t *testing.T) {
	withTrueColor(t)
	m := newProjectedStaticUIModel()
	m.pathReference.tracked = uiPathReferenceQuery{Active: true, Start: 0, End: 3, RawQuery: "ab", NormalizedQuery: "ab"}
	m.pathReference.matches = []uiPathReferenceCandidate{
		{Path: "match-00.go"},
		{Path: "match-01.go"},
		{Path: "match-02.go"},
		{Path: "match-03.go"},
		{Path: "match-04.go"},
		{Path: "match-05.go"},
		{Path: "match-06.go"},
		{Path: "match-07.go"},
		{Path: "match-08.go"},
	}
	m.pathReference.selection = 7

	state := m.pathReferencePicker()
	if state.start == 0 {
		t.Fatalf("expected path picker viewport to scroll, got %+v", state)
	}
	if len(state.rows) != len(m.pathReference.matches) {
		t.Fatalf("expected path picker rows to keep full absolute row list, got %d rows for %d matches", len(state.rows), len(m.pathReference.matches))
	}
	assertActivePickerHighlightedSelection(t, m, 80)
}

func TestPathReferencePickerSanitizesControlCharactersForDisplay(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.pathReference.tracked = uiPathReferenceQuery{Active: true, Start: 0, End: 3, RawQuery: "ab", NormalizedQuery: "ab"}
	m.pathReference.matches = []uiPathReferenceCandidate{{Path: "safe/]52;evilname.txt"}}

	state := m.pathReferencePicker()
	if !state.visible || len(state.rows) != 1 {
		t.Fatalf("unexpected picker state: %+v", state)
	}
	if state.rows[0].primary != "safe/name.txt" {
		t.Fatalf("display path = %q", state.rows[0].primary)
	}
	if m.pathReference.matches[0].Path != "safe/]52;evilname.txt" {
		t.Fatalf("expected underlying candidate path preserved, got %q", m.pathReference.matches[0].Path)
	}
}

func testPathReferenceFixture(fixture string) (string, int) {
	runes := []rune(fixture)
	idx := -1
	for i, r := range runes {
		if r == '|' {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fixture, -1
	}
	result := make([]rune, 0, len(runes)-1)
	for i, r := range runes {
		if i == idx {
			continue
		}
		result = append(result, r)
	}
	return string(result), idx
}

type stubUIPathReferenceSearch struct {
	events       chan uiPathReferenceSearchEvent
	prewarmRoots []string
	requests     []uiPathReferenceSearchRequest
}

func newStubUIPathReferenceSearch() *stubUIPathReferenceSearch {
	return &stubUIPathReferenceSearch{events: make(chan uiPathReferenceSearchEvent, 32)}
}

func (s *stubUIPathReferenceSearch) Events() <-chan uiPathReferenceSearchEvent {
	return s.events
}

func (s *stubUIPathReferenceSearch) StartPrewarm(workspaceRoot string) {
	s.prewarmRoots = append(s.prewarmRoots, workspaceRoot)
}

func (s *stubUIPathReferenceSearch) Search(req uiPathReferenceSearchRequest) {
	s.requests = append(s.requests, req)
}

func (s *stubUIPathReferenceSearch) Stop() {}
