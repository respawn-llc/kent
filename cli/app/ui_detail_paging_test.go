package app

import (
	"core/cli/tui"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	tea "github.com/charmbracelet/bubbletea"
	"testing"
)

func TestDetailModeKeyForwardingReturnsPageRequestCommand(t *testing.T) {
	m := newProjectedStaticUIModel()
	olderCursor := int64(64)
	m.forwardToView(tui.SetDetailTranscriptPageMsg{Page: &transcriptpb.Page{
		OlderCursor:  &olderCursor,
		HasMoreAbove: true,
		Entries:      []*transcriptpb.CommittedRow{detailTestAssistantRow("current")}, ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_FRESH}})
	m.forwardToView(tui.SetModeMsg{Mode: tui.ModeDetail})

	_, cmd := m.inputController().handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if cmd == nil {
		t.Fatal("expected detail page request command")
	}
	msg := cmd()
	request, ok := msg.(tui.RequestDetailTranscriptPageMsg)
	if !ok {
		t.Fatalf("command message = %T, want detail page request", msg)
	}
	if request.Direction != tui.DetailTranscriptPageOlder {
		t.Fatalf("page request direction = %v, want older", request.Direction)
	}
}

func TestDetailTabForwardingReturnsWarmupPageLoadCommand(t *testing.T) {
	m := newProjectedStaticUIModel(WithUISessionID(detailTestSessionID))
	m.activeSurface = uiSurfaceTranscriptDetail
	m.statusConfig.SessionViews = &countingSessionViewClient{page: &transcriptpb.Page{SessionId: detailTestSessionID, ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_FRESH}}

	cmd := m.forwardToView(tea.KeyMsg{Type: tea.KeyTab})
	if cmd == nil {
		t.Fatal("expected detail warmup command")
	}
	for _, msg := range collectCmdMessages(t, cmd) {
		if _, ok := msg.(detailTranscriptLoadMsg); ok {
			return
		}
	}
	t.Fatal("detail warmup command did not emit detail transcript load")
}
