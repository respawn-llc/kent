package sessionview

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"core/server/session"
	"core/server/sessionruntime"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/rollbacktarget"

	"google.golang.org/protobuf/proto"
)

func TestServiceDormantTranscriptPagesPreserveRollbackLocatorAcrossCandidateFreeCompactions(t *testing.T) {
	const (
		userStepID    = "11111111-1111-4111-8111-111111111111"
		compactStepID = "22222222-2222-4222-8222-222222222222"
	)
	dir := t.TempDir()
	store, _ := newSessionViewParentAgentChild(t, dir, "ws", dir)
	appended := appendSessionViewRecordWithCursor(t, store, userStepID, session.MessageRecord{
		Role:    session.MessageRoleUser,
		Content: sessionViewStringPointer("candidate before dormant compactions"),
	})
	if appended.EndByteCursor == nil {
		t.Fatal("rollback candidate append did not return a page cursor")
	}
	locator := rollbacktarget.CandidateLocator{
		UserMessageSeq:       appended.Record.Seq(),
		CandidatePageEndByte: *appended.EndByteCursor,
	}
	wantLocator := &transcriptpb.RollbackCandidate{
		UserMessageSeq:       locator.UserMessageSeq,
		CandidatePageEndByte: locator.CandidatePageEndByte,
	}
	for index := 0; index < 3; index++ {
		appendSessionViewHistoryReplacement(t, store, compactStepID, session.HistoryReplacementRecord{
			Engine:                  "local",
			Mode:                    session.CompactionModeAuto,
			LatestRollbackCandidate: &locator,
		})
	}
	dormant := NewService(newTestSessionResolver(store), nil, nil)

	dormantNewest := mustTranscriptPage(t, dormant, store.Meta().SessionID, nil, nil)
	if !proto.Equal(dormantNewest.LatestRollbackCandidate, wantLocator) {
		t.Fatalf("dormant newest locator = %#v, want %#v", dormantNewest.LatestRollbackCandidate, locator)
	}

	cursor := locator.CandidatePageEndByte
	dormantCandidate := mustTranscriptPage(t, dormant, store.Meta().SessionID, &cursor, nil)
	if !proto.Equal(dormantCandidate.LatestRollbackCandidate, wantLocator) {
		t.Fatalf("dormant candidate-page locator = %#v, want %#v", dormantCandidate.LatestRollbackCandidate, locator)
	}
	wantTarget := rollbacktarget.EncodeUserMessageSeq(locator.UserMessageSeq)
	found := false
	for _, row := range dormantCandidate.Entries {
		if user := row.GetUser(); user != nil && user.RollbackTargetId != nil && *user.RollbackTargetId == wantTarget {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("dormant direct candidate page did not contain rollback target %q", wantTarget)
	}

	dormantNewer := mustTranscriptPage(t, dormant, store.Meta().SessionID, nil, &cursor)
	if !proto.Equal(dormantNewer.LatestRollbackCandidate, wantLocator) {
		t.Fatalf("dormant newer-page locator = %#v, want %#v", dormantNewer.LatestRollbackCandidate, locator)
	}
}

func TestServiceTranscriptReadsHonorCanceledContext(t *testing.T) {
	store := newSessionViewStore(t, t.TempDir(), "ws", t.TempDir())
	service := NewService(newTestSessionResolver(store), nil, nil)
	cursor := int64(1)
	requests := map[string]*transcriptpb.PageRequest{
		"newest page": {SessionId: store.Meta().SessionID},
		"older page":  {SessionId: store.Meta().SessionID, Direction: &transcriptpb.PageRequest_Cursor{Cursor: cursor}},
		"newer page":  {SessionId: store.Meta().SessionID, Direction: &transcriptpb.PageRequest_NewerCursor{NewerCursor: cursor}},
	}
	for name, request := range requests {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if _, err := service.GetSessionTranscriptPage(ctx, request); !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want context canceled", err)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := service.SessionTranscriptTailEntries(ctx, store.Meta().SessionID); !errors.Is(err, context.Canceled) {
		t.Fatalf("tail error = %v, want context canceled", err)
	}
}

func TestRuntimeMainViewSnapshotDoesNotRequirePersistedSessionResolution(t *testing.T) {
	store, fixture, release, handle := startBlockingRuntimeRun(t)
	live := NewService(nil, fixture.activity, nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := live.GetSessionMainView(ctx, &sessionpb.MainViewRequest{SessionId: store.Meta().SessionID}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled live main-view error = %v, want context canceled", err)
	}
	liveMain := mustMainView(t, live, store.Meta().SessionID)
	if liveMain.Activity.State == runtimepb.ActivityState_RUNTIME_ACTIVITY_UNAVAILABLE {
		t.Fatalf("expected completed Runtime Main View, got %+v", liveMain.Activity)
	}

	release()
	if _, err := handle.Wait(context.Background()); err != nil {
		t.Fatalf("submit user message: %v", err)
	}
}

func startBlockingRuntimeRun(t *testing.T) (*session.Store, sessionViewRuntimeFixture, func(), sessionruntime.ExecutionHandle) {
	t.Helper()
	dir := t.TempDir()
	store := newSessionViewStore(t, dir, "ws", dir)
	started := make(chan struct{})
	releaseModel := make(chan struct{})
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() { close(releaseModel) })
	}
	fixture := newSessionViewRuntimeFixture(t, store, &serviceBlockingLLM{started: started, release: releaseModel})
	t.Cleanup(release)
	handle := fixture.startUserTurn(t, "run tools")
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for active run")
	}
	return store, fixture, release, handle
}

func mustMainView(t *testing.T, svc *Service, sessionID string) *runtimepb.MainView {
	t.Helper()
	resp, err := svc.GetSessionMainView(context.Background(), &sessionpb.MainViewRequest{SessionId: sessionID})
	if err != nil {
		t.Fatalf("get main view: %v", err)
	}
	return resp.MainView
}

func mustTranscriptPage(t *testing.T, svc *Service, sessionID string, cursor, newerCursor *int64) *transcriptpb.Page {
	t.Helper()
	request := &transcriptpb.PageRequest{SessionId: sessionID}
	if cursor != nil {
		request.Direction = &transcriptpb.PageRequest_Cursor{Cursor: *cursor}
	}
	if newerCursor != nil {
		request.Direction = &transcriptpb.PageRequest_NewerCursor{NewerCursor: *newerCursor}
	}
	response, err := svc.GetSessionTranscriptPage(context.Background(), request)
	if err != nil {
		t.Fatalf("get transcript page: %v", err)
	}
	return response.Transcript
}
