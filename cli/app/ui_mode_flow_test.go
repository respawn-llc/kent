package app

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"builder/cli/tui"
	"builder/server/llm"
	"builder/server/runtime"
	"builder/server/session"
	"builder/server/tools"
	"builder/shared/cachewarn"
	"builder/shared/clientui"

	tea "github.com/charmbracelet/bubbletea"
)

type recordingTranscriptRuntimeClient struct {
	runtimeControlFakeClient
	loadRequests []clientui.TranscriptPageRequest
	loadPage     clientui.TranscriptPage
}

func (c *recordingTranscriptRuntimeClient) LoadTranscriptPage(req clientui.TranscriptPageRequest) (clientui.TranscriptPage, error) {
	c.loadRequests = append(c.loadRequests, req)
	if c.loadPage.SessionID != "" || len(c.loadPage.Entries) > 0 || c.loadPage.TotalEntries > 0 {
		return c.loadPage, nil
	}
	return c.Transcript(), nil
}

func (c *recordingTranscriptRuntimeClient) RefreshTranscriptPage(req clientui.TranscriptPageRequest) (clientui.TranscriptPage, error) {
	return c.LoadTranscriptPage(req)
}

func TestScenarioDetailWhileAgentWorksReturnsToLatestOngoingTail(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 100
	m.termHeight = 18
	m.input = "/"
	m.refreshSlashCommandFilterFromInput()
	m.syncViewport()

	for i := 1; i <= 20; i++ {
		m = updateUIModel(t, m, tui.AppendTranscriptMsg{Role: "assistant", Text: fmt.Sprintf("line %02d", i)})
	}

	detail := updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if detail.view.Mode() != tui.ModeDetail {
		t.Fatalf("expected detail mode, got %q", detail.view.Mode())
	}

	for i := 21; i <= 30; i++ {
		detail = updateUIModel(t, detail, tui.AppendTranscriptMsg{Role: "assistant", Text: fmt.Sprintf("line %02d", i)})
	}
	detail = updateUIModel(t, detail, tea.KeyMsg{Type: tea.KeyPgUp})

	ongoing := updateUIModel(t, detail, tea.KeyMsg{Type: tea.KeyShiftTab})
	if ongoing.view.Mode() != tui.ModeOngoing {
		t.Fatalf("expected ongoing mode, got %q", ongoing.view.Mode())
	}
	ongoing.termWidth = 100
	ongoing.termHeight = 18
	ongoing.windowSizeKnown = true
	ongoing.syncViewport()

	view := stripANSIAndTrimRight(ongoing.view.OngoingSnapshot())
	if !containsAny(view, "line 30", "line 29", "line 28") {
		t.Fatalf("expected latest content visible after returning from detail, got %q", view)
	}
	compact := stripANSIAndTrimRight(ongoing.View())
	if !strings.Contains(compact, "/new") {
		t.Fatalf("expected slash picker to remain visible, got %q", compact)
	}
}

func TestCtrlTTogglesTranscriptModeLikeShiftTab(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 100
	m.termHeight = 16
	m.syncViewport()

	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
	if m.view.Mode() != tui.ModeDetail {
		t.Fatalf("expected ctrl+t to enter detail mode, got %q", m.view.Mode())
	}

	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
	if m.view.Mode() != tui.ModeOngoing {
		t.Fatalf("expected ctrl+t to return to ongoing mode, got %q", m.view.Mode())
	}
}

func TestCtrlTShowsLatestDetailTailWithoutResolvingMetricsEagerly(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 100
	m.termHeight = 12
	m.syncViewport()

	for i := 0; i < 24; i++ {
		m = updateUIModel(t, m, tui.AppendTranscriptMsg{Role: "assistant", Text: fmt.Sprintf("line %02d", i)})
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	_ = cmd
	detail := next.(*uiModel)
	if detail.view.Mode() != tui.ModeDetail {
		t.Fatalf("expected ctrl+t to enter detail mode, got %q", detail.view.Mode())
	}
	if detail.view.DetailMetricsResolved() {
		t.Fatal("expected detail entry to stay lazily bottom-anchored until first navigation")
	}
	view := stripANSIAndTrimRight(detail.view.View())
	if !containsAny(view, "line 23", "line 22", "line 21") {
		t.Fatalf("expected newest transcript tail immediately visible in detail, got %q", view)
	}
	if strings.Contains(view, "line 00") {
		t.Fatalf("did not expect eager full-history detail rendering on ctrl+t, got %q", view)
	}
	if pageCmd := detail.maybeRequestDetailTranscriptPage(); pageCmd != nil {
		t.Fatal("did not expect edge transcript paging before detail metrics are resolved")
	}
}

func TestDetailEdgePagingWaitsForFirstNavigationToResolveMetrics(t *testing.T) {
	client := &recordingTranscriptRuntimeClient{
		loadPage: clientui.TranscriptPage{SessionID: "session-1"},
	}
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	m.termWidth = 100
	m.termHeight = 12
	m.syncViewport()

	page := clientui.TranscriptPage{SessionID: "session-1", Offset: 100, TotalEntries: 500}
	for i := 0; i < 250; i++ {
		page.Entries = append(page.Entries, clientui.ChatEntry{Role: "assistant", Text: fmt.Sprintf("line %03d", 100+i)})
	}
	entries := transcriptEntriesFromPage(page)
	m.detailTranscript.replace(page)
	m.forwardToView(tui.SetConversationMsg{BaseOffset: page.Offset, TotalEntries: page.TotalEntries, Entries: entries})

	next, enterCmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	_ = enterCmd
	detail := next.(*uiModel)
	if detail.view.Mode() != tui.ModeDetail {
		t.Fatalf("expected detail mode after ctrl+t, got %q", detail.view.Mode())
	}
	if detail.view.DetailMetricsResolved() {
		t.Fatal("expected detail entry to defer metric resolution until navigation")
	}
	if len(client.loadRequests) != 0 {
		t.Fatalf("expected no transcript page loads on detail entry, got %d", len(client.loadRequests))
	}

	next, navCmd := detail.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated := next.(*uiModel)
	if updated.view.DetailMetricsResolved() {
		t.Fatal("expected first detail navigation to stay lazy")
	}
	if navCmd == nil {
		t.Fatal("expected edge-triggered transcript paging after first navigation at the bottom edge")
	}
	msg, ok := navCmd().(runtimeTranscriptRefreshedMsg)
	if !ok {
		t.Fatalf("expected runtimeTranscriptRefreshedMsg, got %T", navCmd())
	}
	if got, want := len(client.loadRequests), 1; got != want {
		t.Fatalf("load request count = %d, want %d", got, want)
	}
	expectedReq := clientui.TranscriptPageRequest{Offset: 350, Limit: 150}
	if msg.req != expectedReq {
		t.Fatalf("paging request = %+v, want %+v", msg.req, expectedReq)
	}
	if client.loadRequests[0] != expectedReq {
		t.Fatalf("client paging request = %+v, want %+v", client.loadRequests[0], expectedReq)
	}
}

func TestCtrlTDeferredDetailLoadSkipsDuplicateSeededPageRequest(t *testing.T) {
	seed := clientui.TranscriptPage{SessionID: "session-1", Offset: 300, TotalEntries: 500}
	for i := 0; i < 200; i++ {
		seed.Entries = append(seed.Entries, clientui.ChatEntry{Role: "assistant", Text: fmt.Sprintf("line %03d", 300+i)})
	}
	client := &recordingTranscriptRuntimeClient{loadPage: seed}
	m := newProjectedTestUIModel(
		client,
		closedProjectedRuntimeEvents(),
		closedAskEvents(),
	)
	m.termWidth = 100
	m.termHeight = 12
	_ = m.runtimeAdapter().applyRuntimeTranscriptPage(clientui.TranscriptPageRequest{Window: clientui.TranscriptWindowOngoingTail}, seed)
	m.syncViewport()

	next, enterCmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	detail := next.(*uiModel)
	if detail.view.Mode() != tui.ModeDetail {
		t.Fatalf("expected detail mode after ctrl+t, got %q", detail.view.Mode())
	}
	if got := len(client.loadRequests); got != 0 {
		t.Fatalf("expected no transcript load before deferred detail tick, got %d", got)
	}

	msgs := collectCmdMessages(t, enterCmd)
	foundDeferredLoad := false
	for _, msg := range msgs {
		if _, ok := msg.(detailTranscriptLoadMsg); ok {
			foundDeferredLoad = true
			break
		}
	}
	if !foundDeferredLoad {
		t.Fatalf("expected deferred detail transcript load message, got %#v", msgs)
	}

	next, refreshCmd := detail.Update(detailTranscriptLoadMsg{})
	updated := next.(*uiModel)
	if refreshCmd != nil {
		t.Fatalf("expected duplicate seeded detail load to be skipped, got %T", refreshCmd())
	}
	if got := len(client.loadRequests); got != 0 {
		t.Fatalf("load request count = %d, want 0", got)
	}
	if updated.runtimeTranscriptBusy {
		t.Fatal("expected duplicate seeded detail load to leave transcript sync idle")
	}
}

func TestCtrlTDeferredDetailLoadSkippedKeepsDetailMetricsLazyEndToEnd(t *testing.T) {
	seed := clientui.TranscriptPage{SessionID: "session-1", Offset: 300, TotalEntries: 500}
	for i := 0; i < 200; i++ {
		seed.Entries = append(seed.Entries, clientui.ChatEntry{Role: "assistant", Text: fmt.Sprintf("line %03d", 300+i)})
	}
	client := &recordingTranscriptRuntimeClient{loadPage: seed}
	m := newProjectedTestUIModel(
		client,
		closedProjectedRuntimeEvents(),
		closedAskEvents(),
	)
	m.termWidth = 100
	m.termHeight = 12
	_ = m.runtimeAdapter().applyRuntimeTranscriptPage(clientui.TranscriptPageRequest{Window: clientui.TranscriptWindowOngoingTail}, seed)
	m.syncViewport()

	next, enterCmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	detail := next.(*uiModel)
	_ = collectCmdMessages(t, enterCmd)
	beforeMetricsResolved := detail.view.DetailMetricsResolved()

	next, refreshCmd := detail.Update(detailTranscriptLoadMsg{})
	updated := next.(*uiModel)
	if refreshCmd != nil {
		t.Fatalf("expected duplicate seeded detail load to be skipped, got %T", refreshCmd())
	}
	afterMetricsResolved := updated.view.DetailMetricsResolved()
	if afterMetricsResolved != beforeMetricsResolved {
		t.Fatalf("duplicate deferred detail refresh changed detail metric resolution %v -> %v", beforeMetricsResolved, afterMetricsResolved)
	}
	if updated.view.Mode() != tui.ModeDetail {
		t.Fatalf("expected detail mode to remain active, got %q", updated.view.Mode())
	}
	if updated.runtimeTranscriptBusy {
		t.Fatal("expected transcript sync to clear busy state after duplicate refresh result")
	}
}

func TestDeferredDetailLoadRefreshesWhenTranscriptDirty(t *testing.T) {
	seed := clientui.TranscriptPage{SessionID: "session-1", Offset: 300, TotalEntries: 500}
	for i := 0; i < 200; i++ {
		seed.Entries = append(seed.Entries, clientui.ChatEntry{Role: "assistant", Text: fmt.Sprintf("line %03d", 300+i)})
	}
	client := &recordingTranscriptRuntimeClient{loadPage: seed}
	m := newProjectedTestUIModel(
		client,
		closedProjectedRuntimeEvents(),
		closedAskEvents(),
	)
	m.termWidth = 100
	m.termHeight = 12
	_ = m.runtimeAdapter().applyRuntimeTranscriptPage(clientui.TranscriptPageRequest{Window: clientui.TranscriptWindowOngoingTail}, seed)
	m.syncViewport()

	next, enterCmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	detail := next.(*uiModel)
	_ = collectCmdMessages(t, enterCmd)
	detail.transcriptLiveDirty = true

	next, refreshCmd := detail.Update(detailTranscriptLoadMsg{})
	updated := next.(*uiModel)
	if refreshCmd == nil {
		t.Fatal("expected dirty detail transcript to refresh even when request matches seeded page")
	}
	refreshed, ok := refreshCmd().(runtimeTranscriptRefreshedMsg)
	if !ok {
		t.Fatalf("expected runtimeTranscriptRefreshedMsg, got %T", refreshCmd())
	}
	if refreshed.syncCause != runtimeTranscriptSyncCauseManualTranscriptRefresh {
		t.Fatalf("detail refresh sync cause = %q, want %q", refreshed.syncCause, runtimeTranscriptSyncCauseManualTranscriptRefresh)
	}
	expectedReq := clientui.TranscriptPageRequest{Offset: seed.Offset, Limit: len(seed.Entries)}
	if refreshed.req != expectedReq {
		t.Fatalf("dirty deferred detail request = %+v, want %+v", refreshed.req, expectedReq)
	}
	if got := len(client.loadRequests); got != 1 {
		t.Fatalf("load request count = %d, want 1", got)
	}
	if client.loadRequests[0] != expectedReq {
		t.Fatalf("client load request = %+v, want %+v", client.loadRequests[0], expectedReq)
	}
	if !updated.runtimeTranscriptBusy {
		t.Fatal("expected transcript sync to remain busy until dirty refresh result is applied")
	}
}

func TestCtrlTDeferredDetailLoadDoesNotMutateNativeHistoryState(t *testing.T) {
	seed := clientui.TranscriptPage{SessionID: "session-1", Offset: 300, TotalEntries: 500}
	for i := 0; i < 200; i++ {
		seed.Entries = append(seed.Entries, clientui.ChatEntry{Role: "assistant", Text: fmt.Sprintf("tail %03d", 300+i)})
	}
	detailPage := clientui.TranscriptPage{SessionID: "session-1", Offset: 0, TotalEntries: 500}
	for i := 0; i < 250; i++ {
		detailPage.Entries = append(detailPage.Entries, clientui.ChatEntry{Role: "assistant", Text: fmt.Sprintf("history %03d", i)})
	}
	client := &recordingTranscriptRuntimeClient{loadPage: detailPage}
	m := newProjectedTestUIModel(
		client,
		closedProjectedRuntimeEvents(),
		closedAskEvents(),
	)
	m.termWidth = 100
	m.termHeight = 12
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPage(clientui.TranscriptPageRequest{Window: clientui.TranscriptWindowOngoingTail}, seed); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}
	m.syncViewport()
	baselineProjection := m.nativeProjection
	baselineRenderedProjection := m.nativeRenderedProjection
	baselineRenderedSnapshot := m.nativeRenderedSnapshot
	baselineFlushedEntryCount := m.nativeFlushedEntryCount

	next, enterCmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	detail := next.(*uiModel)
	_ = collectCmdMessages(t, enterCmd)
	detail.transcriptLiveDirty = true
	next, refreshCmd := detail.Update(detailTranscriptLoadMsg{})
	detail = next.(*uiModel)
	if refreshCmd == nil {
		t.Fatal("expected deferred detail load to refresh when transcript is dirty")
	}
	refreshed, ok := refreshCmd().(runtimeTranscriptRefreshedMsg)
	if !ok {
		t.Fatalf("expected runtimeTranscriptRefreshedMsg, got %T", refreshCmd())
	}
	next, followUp := detail.Update(refreshed)
	if followUp != nil {
		_ = collectCmdMessages(t, followUp)
	}
	updated := next.(*uiModel)
	if !reflect.DeepEqual(updated.nativeProjection, baselineProjection) {
		t.Fatal("deferred detail load changed native projection state")
	}
	if !reflect.DeepEqual(updated.nativeRenderedProjection, baselineRenderedProjection) {
		t.Fatal("deferred detail load changed rendered native projection state")
	}
	if updated.nativeRenderedSnapshot != baselineRenderedSnapshot {
		t.Fatalf("deferred detail load changed rendered native snapshot: %q -> %q", baselineRenderedSnapshot, updated.nativeRenderedSnapshot)
	}
	if updated.nativeFlushedEntryCount != baselineFlushedEntryCount {
		t.Fatalf("deferred detail load changed native flushed entry count: %d -> %d", baselineFlushedEntryCount, updated.nativeFlushedEntryCount)
	}
}

func TestScenarioHarnessRestartAndSessionResumeKeepsTranscriptVisible(t *testing.T) {
	workspace := t.TempDir()
	store, err := session.Create(workspace, "ws", workspace)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	appendTranscriptMessage(t, store, llm.RoleUser, "u1")
	appendTranscriptMessage(t, store, llm.RoleAssistant, "a1")
	appendTranscriptMessage(t, store, llm.RoleUser, "u2")
	appendTranscriptMessage(t, store, llm.RoleAssistant, "a2 tail")

	eng, err := runtime.New(store, statusLineFakeClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	m := newProjectedEngineUIModel(eng)
	m.termWidth = 90
	m.termHeight = 16
	m.syncViewport()

	first := stripANSIAndTrimRight(m.view.OngoingSnapshot())
	if !strings.Contains(first, "a2 tail") {
		t.Fatalf("expected resumed tail in ongoing mode, got %q", first)
	}

	eng.AppendLocalEntry("assistant", "post-resume live update")
	_ = m.runtimeAdapter().handleRuntimeEvent(runtime.Event{Kind: runtime.EventLocalEntryAdded, LocalEntry: &runtime.ChatEntry{Role: "assistant", Text: "post-resume live update"}})
	live := stripANSIAndTrimRight(m.view.OngoingSnapshot())
	if !strings.Contains(live, "post-resume live update") {
		t.Fatalf("expected live update after local entry event, got %q", live)
	}

	reopened, err := session.Open(store.Dir())
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	eng2, err := runtime.New(reopened, statusLineFakeClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("new engine after restart: %v", err)
	}
	m2 := newProjectedEngineUIModel(eng2)
	m2.termWidth = 90
	m2.termHeight = 16
	m2.syncViewport()

	afterRestart := stripANSIAndTrimRight(m2.view.OngoingSnapshot())
	if !strings.Contains(afterRestart, "a2 tail") {
		t.Fatalf("expected resumed transcript after harness restart, got %q", afterRestart)
	}
	if strings.Contains(afterRestart, "post-resume live update") {
		t.Fatalf("did not expect non-persisted local update to survive restart, got %q", afterRestart)
	}

	m2 = updateUIModel(t, m2, tea.KeyMsg{Type: tea.KeyShiftTab})
	m2 = updateUIModel(t, m2, tea.KeyMsg{Type: tea.KeyShiftTab})
	backToOngoing := stripANSIAndTrimRight(m2.view.OngoingSnapshot())
	if !strings.Contains(backToOngoing, "a2 tail") {
		t.Fatalf("expected transcript preserved across detail roundtrip after restart, got %q", backToOngoing)
	}
}

func TestScenarioSessionResumeNormalizesLegacyReviewerEntriesInOngoingMode(t *testing.T) {
	workspace := t.TempDir()
	store, err := session.Create(workspace, "ws", workspace)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if _, err := store.AppendEvent("legacy-step", "local_entry", map[string]any{
		"role":         "reviewer_suggestions",
		"text":         "Supervisor suggested:\n1. Add final verification notes.",
		"ongoing_text": "Supervisor made 1 suggestion.",
	}); err != nil {
		t.Fatalf("append legacy reviewer_suggestions: %v", err)
	}
	if _, err := store.AppendEvent("legacy-step", "local_entry", map[string]any{
		"role": "reviewer_status",
		"text": "Supervisor ran, applied 1 suggestion:\n1. Add final verification notes.",
	}); err != nil {
		t.Fatalf("append legacy reviewer_status: %v", err)
	}

	reopened, err := session.Open(store.Dir())
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	eng, err := runtime.New(reopened, statusLineFakeClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("new engine after restart: %v", err)
	}
	m := newProjectedEngineUIModel(eng)
	m.termWidth = 90
	m.termHeight = 16
	m.syncViewport()

	ongoing := stripANSIAndTrimRight(m.view.OngoingSnapshot())
	if !containsInOrder(ongoing, "Supervisor made 1 suggestion.", "Supervisor ran, applied 1 suggestion:") {
		t.Fatalf("expected stored reviewer entries after session resume, got %q", ongoing)
	}
	if strings.Contains(ongoing, "Supervisor suggested") {
		t.Fatalf("did not expect legacy reviewer suggestion header after resume, got %q", ongoing)
	}
	if strings.Contains(ongoing, "Supervisor ran: 1 suggestion, applied.") {
		t.Fatalf("did not expect legacy reviewer status summary after resume, got %q", ongoing)
	}
}

func TestScenarioTeleportBetweenSessionsResetsVisibleConversation(t *testing.T) {
	workspace := t.TempDir()
	storeA, err := session.Create(workspace, "ws", workspace)
	if err != nil {
		t.Fatalf("create store A: %v", err)
	}
	appendTranscriptMessage(t, storeA, llm.RoleUser, "session-a-user")
	appendTranscriptMessage(t, storeA, llm.RoleAssistant, "session-a-tail")

	storeB, err := session.Create(workspace, "ws", workspace)
	if err != nil {
		t.Fatalf("create store B: %v", err)
	}
	appendTranscriptMessage(t, storeB, llm.RoleUser, "session-b-user")
	appendTranscriptMessage(t, storeB, llm.RoleAssistant, "session-b-tail")

	engA, err := runtime.New(storeA, statusLineFakeClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("new engine A: %v", err)
	}
	modelA := newProjectedEngineUIModel(engA)
	modelA.termWidth = 80
	modelA.termHeight = 14
	modelA.syncViewport()
	viewA := stripANSIAndTrimRight(modelA.view.OngoingSnapshot())
	if !strings.Contains(viewA, "session-a-tail") {
		t.Fatalf("expected session A tail, got %q", viewA)
	}

	engB, err := runtime.New(storeB, statusLineFakeClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("new engine B: %v", err)
	}
	modelB := newProjectedEngineUIModel(engB)
	modelB.termWidth = 80
	modelB.termHeight = 14
	modelB.syncViewport()
	viewB := stripANSIAndTrimRight(modelB.view.OngoingSnapshot())
	if !strings.Contains(viewB, "session-b-tail") || strings.Contains(viewB, "session-a-tail") {
		t.Fatalf("expected teleported session B view only, got %q", viewB)
	}

	reopenA, err := session.Open(storeA.Dir())
	if err != nil {
		t.Fatalf("reopen A: %v", err)
	}
	engA2, err := runtime.New(reopenA, statusLineFakeClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("new engine A2: %v", err)
	}
	modelA2 := newProjectedEngineUIModel(engA2)
	modelA2.termWidth = 80
	modelA2.termHeight = 14
	modelA2.syncViewport()
	viewA2 := stripANSIAndTrimRight(modelA2.view.OngoingSnapshot())
	if !strings.Contains(viewA2, "session-a-tail") || strings.Contains(viewA2, "session-b-tail") {
		t.Fatalf("expected teleported-back session A view only, got %q", viewA2)
	}
}

func TestScenarioScrollAttemptsAcrossModesAfterLongDetailStay(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 80
	m.termHeight = 10
	m.syncViewport()

	for i := 1; i <= 40; i++ {
		m = updateUIModel(t, m, tui.AppendTranscriptMsg{Role: "assistant", Text: fmt.Sprintf("line %02d", i)})
	}
	start := m.view.OngoingScroll()
	if start == 0 {
		t.Fatal("expected ongoing transcript to be scrollable")
	}

	updated := updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	if got := updated.view.OngoingScroll(); got != start {
		t.Fatalf("expected pgup not to mutate ongoing scroll, got %d from %d", got, start)
	}

	detail := updateUIModel(t, updated, tea.KeyMsg{Type: tea.KeyShiftTab})
	if detail.view.Mode() != tui.ModeDetail {
		t.Fatalf("expected detail mode, got %q", detail.view.Mode())
	}
	for i := 41; i <= 45; i++ {
		detail = updateUIModel(t, detail, tui.AppendTranscriptMsg{Role: "assistant", Text: fmt.Sprintf("line %02d", i)})
	}
	detail = updateUIModel(t, detail, tea.KeyMsg{Type: tea.KeyPgUp})
	detail = updateUIModel(t, detail, tea.KeyMsg{Type: tea.KeyPgDown})

	ongoing := updateUIModel(t, detail, tea.KeyMsg{Type: tea.KeyShiftTab})
	plain := stripANSIAndTrimRight(ongoing.view.OngoingSnapshot())
	if !containsAny(plain, "line 45", "line 44", "line 43") {
		t.Fatalf("expected latest line visible after returning from detail, got %q", plain)
	}

	afterUp := ongoing.view.OngoingScroll()
	ongoing = updateUIModel(t, ongoing, tea.KeyMsg{Type: tea.KeyPgDown})
	if ongoing.view.OngoingScroll() != afterUp {
		t.Fatalf("expected pgdown not to mutate ongoing scroll, got %d from %d", ongoing.view.OngoingScroll(), afterUp)
	}
}

func TestMainUIStartsInNormalBuffer(t *testing.T) {
	m := newProjectedStaticUIModel()
	if m.altScreenActive {
		t.Fatal("expected main UI to start in normal buffer")
	}
}

func TestStartupHydrationKeepsCompactionSummaryDetailOnly(t *testing.T) {
	client := &startupTranscriptRuntimeClient{
		view: clientui.RuntimeMainView{Session: clientui.RuntimeSessionView{SessionID: "session-1", SessionName: "incident triage"}},
		page: clientui.TranscriptPage{
			SessionID:    "session-1",
			Offset:       0,
			TotalEntries: 2,
			Entries: []clientui.ChatEntry{
				{Role: "compaction_notice", Text: "context compacted for the 1st time"},
				{Role: "compaction_summary", Text: "summary line one\nsummary line two"},
			},
		},
	}
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())

	next, startupCmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	updated := next.(*uiModel)
	if startupCmd == nil {
		t.Fatal("expected startup native history replay command")
	}
	seededOngoing := stripANSIAndTrimRight(updated.view.OngoingSnapshot())
	if !strings.Contains(seededOngoing, "context compacted for the 1st time") {
		t.Fatalf("expected compaction notice visible in ongoing startup seed, got %q", seededOngoing)
	}
	if strings.Contains(seededOngoing, "summary line one") || strings.Contains(seededOngoing, "summary line two") {
		t.Fatalf("expected compaction summary hidden in ongoing startup seed, got %q", seededOngoing)
	}
	hydrationMsg, ok := startupRuntimeTranscriptRefreshedMsg(updated.startupCmds)
	if !ok {
		t.Fatalf("expected runtimeTranscriptRefreshedMsg from startup hydration, got %d command(s)", len(updated.startupCmds))
	}
	hydrated := updateUIModel(t, updated, hydrationMsg)
	hydratedOngoing := stripANSIAndTrimRight(hydrated.view.OngoingSnapshot())
	if !strings.Contains(hydratedOngoing, "context compacted for the 1st time") {
		t.Fatalf("expected compaction notice visible after hydration, got %q", hydratedOngoing)
	}
	if strings.Contains(hydratedOngoing, "summary line one") || strings.Contains(hydratedOngoing, "summary line two") {
		t.Fatalf("expected compaction summary hidden after hydration, got %q", hydratedOngoing)
	}

	detail := updateUIModel(t, hydrated, tea.KeyMsg{Type: tea.KeyCtrlT})
	detail = updateUIModel(t, detail, tea.KeyMsg{Type: tea.KeyEnter})
	if detail.view.Mode() != tui.ModeDetail {
		t.Fatalf("expected detail mode after toggle, got %q", detail.view.Mode())
	}
	detailView := stripANSIAndTrimRight(detail.view.View())
	if !containsInOrder(detailView, "context compacted for the 1st time", "summary line one", "summary line two") {
		t.Fatalf("expected compaction notice plus full summary in detail after hydration, got %q", detailView)
	}
	if got, want := len(client.loadRequests), 1; got != want {
		t.Fatalf("load request count = %d, want %d", got, want)
	}
	if client.loadRequests[0].Window != clientui.TranscriptWindowOngoingTail {
		t.Fatalf("startup hydration window = %q, want ongoing_tail", client.loadRequests[0].Window)
	}
}

func TestStartupHydrationKeepsDefaultCacheWarningDetailOnly(t *testing.T) {
	warningText := cachewarn.Text(cachewarn.Warning{Scope: cachewarn.ScopeConversation, Reason: cachewarn.ReasonNonPostfix})
	client := &startupTranscriptRuntimeClient{
		view: clientui.RuntimeMainView{Session: clientui.RuntimeSessionView{SessionID: "session-1", SessionName: "incident triage"}},
		page: clientui.TranscriptPage{
			SessionID:    "session-1",
			Offset:       0,
			TotalEntries: 2,
			Entries: []clientui.ChatEntry{
				{Role: "assistant", Text: "latest answer"},
				{Role: "cache_warning", Text: warningText, Visibility: clientui.EntryVisibilityDetailOnly},
			},
		},
	}
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())

	next, startupCmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	updated := next.(*uiModel)
	if startupCmd == nil {
		t.Fatal("expected startup native history replay command")
	}
	seededOngoing := stripANSIAndTrimRight(updated.view.OngoingSnapshot())
	if !strings.Contains(seededOngoing, "latest answer") {
		t.Fatalf("expected assistant answer visible in ongoing startup seed, got %q", seededOngoing)
	}
	if strings.Contains(seededOngoing, warningText) {
		t.Fatalf("expected default cache warning hidden in ongoing startup seed, got %q", seededOngoing)
	}

	hydrationMsg, ok := startupRuntimeTranscriptRefreshedMsg(updated.startupCmds)
	if !ok {
		t.Fatalf("expected runtimeTranscriptRefreshedMsg from startup hydration, got %d command(s)", len(updated.startupCmds))
	}
	hydrated := updateUIModel(t, updated, hydrationMsg)
	hydratedOngoing := stripANSIAndTrimRight(hydrated.view.OngoingSnapshot())
	if !strings.Contains(hydratedOngoing, "latest answer") {
		t.Fatalf("expected assistant answer visible after hydration, got %q", hydratedOngoing)
	}
	if strings.Contains(hydratedOngoing, warningText) {
		t.Fatalf("expected default cache warning hidden after hydration, got %q", hydratedOngoing)
	}

	detail := updateUIModel(t, hydrated, tea.KeyMsg{Type: tea.KeyCtrlT})
	detailView := stripANSIAndTrimRight(detail.view.View())
	if !containsInOrder(detailView, "latest answer", warningText) {
		t.Fatalf("expected assistant answer plus cache warning in detail after hydration, got %q", detailView)
	}
}

func startupRuntimeTranscriptRefreshedMsg(cmds []tea.Cmd) (runtimeTranscriptRefreshedMsg, bool) {
	return startupCmdMessage[runtimeTranscriptRefreshedMsg](cmds)
}

func startupCmdMessage[T tea.Msg](cmds []tea.Cmd) (T, bool) {
	var zero T
	for _, cmd := range cmds {
		if cmd == nil {
			continue
		}
		msg, ok := cmd().(T)
		if ok {
			return msg, true
		}
	}
	return zero, false
}

func appendTranscriptMessage(t *testing.T, store *session.Store, role llm.Role, text string) {
	t.Helper()
	if _, err := store.AppendEvent("s1", "message", llm.Message{Role: role, Content: text}); err != nil {
		t.Fatalf("append %s message: %v", role, err)
	}
}

func updateUIModel(t *testing.T, m *uiModel, msg tea.Msg) *uiModel {
	t.Helper()
	next, _ := m.Update(msg)
	updated, ok := next.(*uiModel)
	if !ok {
		t.Fatalf("unexpected model type %T", next)
	}
	return updated
}

func collectCmdMessages(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	msgs := make([]tea.Msg, 0)
	var runMsg func(tea.Msg)
	var runCmd func(tea.Cmd)
	runCmd = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		runMsg(cmd())
	}
	runMsg = func(msg tea.Msg) {
		if msg == nil {
			return
		}
		switch typed := msg.(type) {
		case tea.BatchMsg:
			for _, child := range typed {
				runCmd(child)
			}
			return
		}
		value := reflect.ValueOf(msg)
		if value.IsValid() && value.Kind() == reflect.Slice {
			for i := 0; i < value.Len(); i++ {
				child, ok := value.Index(i).Interface().(tea.Cmd)
				if !ok {
					msgs = append(msgs, msg)
					return
				}
				runCmd(child)
			}
			return
		}
		msgs = append(msgs, msg)
	}
	runCmd(cmd)
	return msgs
}

func containsAny(text string, parts ...string) bool {
	for _, part := range parts {
		if strings.Contains(text, part) {
			return true
		}
	}
	return false
}
