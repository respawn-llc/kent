package app

import (
	"testing"

	"core/shared/clientui"
)

func TestDeferredContinuityRefreshPreservesRecoveryCauseAcrossBusyHydration(t *testing.T) {
	client := &refreshingRuntimeClient{
		transcripts: []clientui.TranscriptPage{{SessionID: "session-1", Entries: []clientui.ChatEntry{{Role: "assistant", Text: "authoritative after gap"}}, TotalEntries: 1}},
	}
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	m.startupCmds = nil
	m.runtimeTranscriptBusy = true
	m.runtimeTranscriptToken = 7

	if cmd := m.requestRuntimeTranscriptSyncForContinuityLoss(clientui.TranscriptRecoveryCauseStreamGap); cmd != nil {
		t.Fatalf("expected no command while hydration is already in flight, got %T", cmd)
	}
	if !m.runtimeTranscriptPendingSet {
		t.Fatal("expected pending hydrate follow-up after deferred continuity refresh")
	}
	if got := m.runtimeTranscriptPending.recoveryCause; got != clientui.TranscriptRecoveryCauseStreamGap {
		t.Fatalf("pending recovery cause = %q, want %q", got, clientui.TranscriptRecoveryCauseStreamGap)
	}

	next, followCmd := m.Update(runtimeTranscriptRefreshedMsg{token: 7, transcript: clientui.TranscriptPage{SessionID: "session-1"}})
	if followCmd == nil {
		t.Fatal("expected follow-up refresh after dirty hydrate completion")
	}
	followMsg, ok := followCmd().(runtimeTranscriptRefreshedMsg)
	if !ok {
		t.Fatalf("expected runtimeTranscriptRefreshedMsg, got %T", followCmd())
	}
	if followMsg.recoveryCause != clientui.TranscriptRecoveryCauseStreamGap {
		t.Fatalf("follow-up recovery cause = %q, want %q", followMsg.recoveryCause, clientui.TranscriptRecoveryCauseStreamGap)
	}
	if followMsg.syncCause != runtimeTranscriptSyncCauseContinuityRecovery {
		t.Fatalf("follow-up sync cause = %q, want %q", followMsg.syncCause, runtimeTranscriptSyncCauseContinuityRecovery)
	}
	updated := next.(*uiModel)
	if updated.runtimeTranscriptPendingSet {
		t.Fatal("expected pending hydrate cleared once follow-up request starts")
	}
}
