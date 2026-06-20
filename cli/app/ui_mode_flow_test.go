package app

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"core/cli/tui"
	"core/shared/clientui"

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

func TestCtrlTTogglesTranscriptModeLikeShiftTab(t *testing.T) {
	m := setTestUITerminalSize(newProjectedStaticUIModel(), 100, 16)
	m.layout().syncViewport()

	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
	if m.view.Mode() != tui.ModeDetail {
		t.Fatalf("expected ctrl+t to enter detail mode, got %q", m.view.Mode())
	}

	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
	if m.view.Mode() != tui.ModeOngoing {
		t.Fatalf("expected ctrl+t to return to ongoing mode, got %q", m.view.Mode())
	}
}

func TestDetailEdgePagingWaitsForFirstNavigationToResolveMetrics(t *testing.T) {
	client := &recordingTranscriptRuntimeClient{
		loadPage: clientui.TranscriptPage{SessionID: "session-1"},
	}
	m := setTestUITerminalSize(newProjectedClosedUIModel(client), 100, 12)
	m.layout().syncViewport()

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
	m := setTestUITerminalSize(newProjectedClosedUIModel(client), 100, 12)
	_ = m.runtimeAdapter().applyRuntimeTranscriptPageWithRecovery(clientui.TranscriptPageRequest{Window: clientui.TranscriptWindowOngoingTail}, seed, clientui.TranscriptRecoveryCauseNone)
	m.layout().syncViewport()

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
	m := setTestUITerminalSize(newProjectedClosedUIModel(client), 100, 12)
	_ = m.runtimeAdapter().applyRuntimeTranscriptPageWithRecovery(clientui.TranscriptPageRequest{Window: clientui.TranscriptWindowOngoingTail}, seed, clientui.TranscriptRecoveryCauseNone)
	m.layout().syncViewport()

	next, enterCmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	detail := next.(*uiModel)
	_ = collectCmdMessages(t, enterCmd)
	beforeMetricsResolved := detail.view.DetailMetricsResolved()
	if beforeMetricsResolved {
		t.Fatal("expected duplicate-seeded detail entry to start with lazy metrics")
	}

	next, refreshCmd := detail.Update(detailTranscriptLoadMsg{})
	updated := next.(*uiModel)
	if refreshCmd != nil {
		t.Fatalf("expected duplicate seeded detail load to be skipped, got %T", refreshCmd())
	}
	afterMetricsResolved := updated.view.DetailMetricsResolved()
	if afterMetricsResolved {
		t.Fatal("expected duplicate deferred detail refresh to keep detail metrics lazy")
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
	m := setTestUITerminalSize(newProjectedClosedUIModel(client), 100, 12)
	_ = m.runtimeAdapter().applyRuntimeTranscriptPageWithRecovery(clientui.TranscriptPageRequest{Window: clientui.TranscriptWindowOngoingTail}, seed, clientui.TranscriptRecoveryCauseNone)
	m.layout().syncViewport()

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
	m := setTestUITerminalSize(newProjectedClosedUIModel(client), 100, 12)
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPageWithRecovery(clientui.TranscriptPageRequest{Window: clientui.TranscriptWindowOngoingTail}, seed, clientui.TranscriptRecoveryCauseNone); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}
	m.layout().syncViewport()
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

func TestMainUIStartsInNormalBuffer(t *testing.T) {
	m := newProjectedStaticUIModel()
	if m.altScreenActive {
		t.Fatal("expected main UI to start in normal buffer")
	}
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
