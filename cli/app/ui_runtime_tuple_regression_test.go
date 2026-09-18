package app

import (
	"io"
	"reflect"
	"testing"

	"core/cli/tui/ongoing"
	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"

	"google.golang.org/protobuf/proto"
)

func TestRuntimeMainViewRefreshCoalescesAndDrainsAfterCompletion(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "success"},
		{name: "connection failure", err: io.EOF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			initial := runtimeTupleTestView(9, runtimeTupleTestIdleActivity())
			latest := runtimeTupleTestView(10, runtimeTupleTestRunningActivity())
			reads := &flakySessionViewClient{
				responses: []*sessionpb.MainViewSuccess{{MainView: initial}, {MainView: latest}},
				errs:      []error{tc.err},
			}
			client := newTestSessionRuntimeClient(reads, newUnavailableRuntimeControlService())
			client.storeMainView(initial)
			m := newProjectedTestUIModel(client)
			first := m.startRuntimeMainViewRefresh(nil)
			for range 3 {
				if cmd := m.startRuntimeMainViewRefresh(nil); cmd != nil {
					t.Fatal("refresh started another read before the current completion")
				}
			}
			firstResult := first().(runtimeMainViewRefreshedMsg)
			if reads.count != 1 {
				t.Fatalf("reads before completion = %d, want 1", reads.count)
			}
			next := m.handleRuntimeMainViewRefreshed(firstResult)
			if m.runtimeDisconnectStatusVisible() != (tc.err != nil) {
				t.Fatal("completion did not project connection availability")
			}
			if next == nil {
				t.Fatal("completion did not schedule the pending refresh")
			}
			lastResult := next().(runtimeMainViewRefreshedMsg)
			if cmd := m.handleRuntimeMainViewRefreshed(lastResult); cmd != nil {
				t.Fatal("coalesced requests scheduled more than one follow-up")
			}
			if reads.count != 2 {
				t.Fatalf("total reads = %d, want 2", reads.count)
			}
			assertRuntimeTupleView(t, client.MainView(), latest)
			if !m.runtimeActivityBlocksInput() || m.runtimeDisconnectStatusVisible() {
				t.Fatal("successful follow-up did not project current activity and connection")
			}
		})
	}
}

func TestInterruptRefreshRestoresOnlyCapturedSubmissionAfterCoalescing(t *testing.T) {
	for _, tc := range []struct {
		name   string
		queued bool
		newer  bool
	}{
		{name: "immediate"},
		{name: "coalesced", queued: true},
		{name: "newer submission", queued: true, newer: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := runtimeTupleTestView(10, runtimeTupleTestIdleActivity())
			m := newProjectedClosedUIModel(&runtimeControlFakeClient{mainView: view})
			m.sessionID = view.Session.SessionId
			m.activeSubmit = activeSubmitState{token: 1, text: "interrupted"}
			token := m.activeSubmit.token
			if tc.queued {
				inFlight := m.startRuntimeMainViewRefresh(nil)
				if cmd := m.startRuntimeMainViewRefresh(&token); cmd != nil {
					t.Fatal("interrupt recovery started a concurrent read")
				}
				m.startRuntimeMainViewRefresh(nil)
				next := m.handleRuntimeMainViewRefreshed(inFlight().(runtimeMainViewRefreshedMsg))
				if m.activeSubmit.token != 1 {
					t.Fatal("earlier read restored the interrupted submission")
				}
				if tc.newer {
					m.activeSubmit = activeSubmitState{token: 2, text: "new"}
				}
				m.handleRuntimeMainViewRefreshed(next().(runtimeMainViewRefreshedMsg))
			} else {
				cmd := m.startRuntimeMainViewRefresh(&token)
				m.handleRuntimeMainViewRefreshed(cmd().(runtimeMainViewRefreshedMsg))
			}
			if tc.newer {
				if m.activeSubmit.token != 2 {
					t.Fatal("interrupt recovery cleared the newer submission")
				}
			} else if m.hasLocalDispatchPending() {
				t.Fatal("idle refresh left the interrupted submission pending")
			}
		})
	}
}

func TestRuntimeMainViewRefreshIgnoresObsoleteCompletion(t *testing.T) {
	initial := runtimeTupleTestView(9, runtimeTupleTestIdleActivity())
	latest := runtimeTupleTestView(10, runtimeTupleTestRunningActivity())
	reads := &countingSessionViewClient{view: initial}
	client := newTestSessionRuntimeClient(reads, newUnavailableRuntimeControlService())
	client.storeMainView(initial)
	m := newProjectedTestUIModel(client)
	first := m.startRuntimeMainViewRefresh(nil)
	oldResult := first().(runtimeMainViewRefreshedMsg)
	m.handleRuntimeMainViewRefreshed(oldResult)

	reads.view = latest
	current := m.startRuntimeMainViewRefresh(nil)
	m.startRuntimeMainViewRefresh(nil)
	for _, err := range []error{nil, io.EOF} {
		oldResult.err = err
		if cmd := m.handleRuntimeMainViewRefreshed(oldResult); cmd != nil {
			t.Fatal("obsolete completion released a pending refresh")
		}
	}
	if m.runtimeDisconnectStatusVisible() {
		t.Fatal("obsolete failure changed connection availability")
	}
	assertRuntimeTupleView(t, client.MainView(), initial)
	if cmd := m.startRuntimeMainViewRefresh(nil); cmd != nil {
		t.Fatal("obsolete completion released the current read")
	}
	next := m.handleRuntimeMainViewRefreshed(current().(runtimeMainViewRefreshedMsg))
	if next == nil {
		t.Fatal("obsolete completion lost the pending refresh")
	}
	assertRuntimeTupleView(t, client.MainView(), latest)
	if cmd := m.handleRuntimeMainViewRefreshed(next().(runtimeMainViewRefreshedMsg)); cmd != nil {
		t.Fatal("pending refresh was not drained")
	}
	if reads.mainViewCount.Load() != 3 {
		t.Fatalf("total reads = %d, want 3", reads.mainViewCount.Load())
	}
}

func TestDelayedTranscriptRuntimeTupleCannotRollBackNewerUnaryState(t *testing.T) {
	controls := newUnavailableRuntimeControlService()
	runtimeClient := newTestSessionRuntimeClient(&countingSessionViewClient{}, controls)
	v9 := runtimeTupleTestView(9, runtimeTupleTestIdleActivity())
	runtimeClient.storeMainView(v9)
	m := newProjectedTestUIModel(runtimeClient)
	controller := newOngoingTranscriptController(
		&ongoingSurfaceSpy{},
		m.ongoingFrameInput,
		runtimeClient.admitTranscriptMessageState,
		m.applyAdmittedTranscriptMessageState,
	)
	if _, _, err := controller.Accept(runtimeTupleTestHydration(9, runtimeTupleTestIdleActivity())); err != nil {
		t.Fatalf("accept initial hydration: %v", err)
	}

	v11 := runtimeTupleTestView(11, runtimeTupleTestIdleActivity())
	canonical := runtimeClient.storeMainView(v11)
	m.applyRuntimeMainViewState(canonical)
	delayed := runtimeTupleTestUpdateMessage(2, 10, runtimeTupleTestRunningActivity())
	if _, cmd, err := controller.Accept(delayed); err != nil {
		t.Fatalf("accept delayed runtime update: %v", err)
	} else if cmd != nil {
		t.Fatal("lower-sequence runtime update scheduled an unexpected refresh")
	}

	assertRuntimeTupleView(t, runtimeClient.MainView(), v11)
	if m.runtimeActivityBusy() || m.runtimeActivityBlocksInput() {
		t.Fatalf("delayed running state blocked input: projection=%+v lifecycle=%+v", m.runtimeActivityProjection, m.runtimeLifecycle.Run)
	}
	if m.currentRunID != "" || m.currentStepID != "" {
		t.Fatalf("delayed running state restored active identity run=%q step=%q", m.currentRunID, m.currentStepID)
	}
}

func TestRuntimeTupleEqualityIncludesReviewerActivity(t *testing.T) {
	inactive := runtimeTupleTestIdleActivity()
	running := proto.Clone(inactive).(*runtimepb.Activity)
	running.Reviewer = runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_INVOKING

	if runtimeActivitiesEqual(inactive, running) {
		t.Fatal("runtime activities with different Reviewer state compared equal")
	}
	addressing := proto.Clone(inactive).(*runtimepb.Activity)
	addressing.Reviewer = runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_ADDRESSING_FEEDBACK
	if err := protoapi.Validate(addressing); err != nil {
		t.Fatalf("addressing-feedback Reviewer activity failed validation: %v", err)
	}
	if runtimeActivitiesEqual(running, addressing) {
		t.Fatal("runtime activities with different Reviewer phases compared equal")
	}
}

func TestRuntimeMainViewRefreshCommitsOnlyWhenReducerHandlesCandidate(t *testing.T) {
	v10 := runtimeTupleTestView(10, runtimeTupleTestIdleActivity())
	v10.Status = &runtimepb.Status{
		ReviewerFrequency: "edits",
		ReviewerEnabled:   true,
		ThinkingLevel:     "high",
	}
	v10.Session.SessionName = proto.String("captured unary metadata")
	v10.Session.ConversationFreshness = runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED
	v10.Session.ExecutionTarget = &worktreepb.SessionExecutionTarget{
		WorkspaceId:      proto.String("workspace-1"),
		WorkspaceName:    "workspace",
		WorkspaceRoot:    "/workspace",
		EffectiveWorkdir: "/workspace",
	}
	reads := &countingSessionViewClient{view: v10}
	runtimeClient := newTestSessionRuntimeClient(reads, newUnavailableRuntimeControlService())
	v9 := runtimeTupleTestView(9, runtimeTupleTestIdleActivity())
	runtimeClient.storeMainView(v9)
	m := newProjectedTestUIModel(runtimeClient)
	controller := newOngoingTranscriptController(
		&ongoingSurfaceSpy{},
		m.ongoingFrameInput,
		runtimeClient.admitTranscriptMessageState,
		m.applyAdmittedTranscriptMessageState,
	)
	if _, _, err := controller.Accept(runtimeTupleTestHydration(9, v9.Activity)); err != nil {
		t.Fatalf("accept initial hydration: %v", err)
	}

	refresh := m.startRuntimeMainViewRefresh(nil)
	if refresh == nil {
		t.Fatal("refresh request did not return a command")
	}
	msg, ok := refresh().(runtimeMainViewRefreshedMsg)
	if !ok {
		t.Fatalf("refresh command returned %T, want runtimeMainViewRefreshedMsg", refresh())
	}
	assertRuntimeTupleView(t, runtimeClient.MainView(), v9)
	if !runtimeActivitiesEqual(m.runtimeActivityProjection, v9.Activity) {
		t.Fatalf("UI projection changed before reducer: %+v", m.runtimeActivityProjection)
	}

	v11 := runtimeTupleTestView(11, runtimeTupleTestIdleActivity())
	if _, _, err := controller.Accept(runtimeTupleTestUpdateMessage(2, 11, v11.Activity)); err != nil {
		t.Fatalf("accept newer transcript update: %v", err)
	}
	m.handleRuntimeMainViewRefreshed(msg)

	got := runtimeClient.MainView()
	assertRuntimeTupleView(t, got, v11)
	if got.Status.ReviewerFrequency != "edits" || !got.Status.ReviewerEnabled {
		t.Fatalf("unary status metadata was not projected: %+v", got.Status)
	}
	if got.Session.GetSessionName() != "captured unary metadata" || got.Session.ExecutionTarget.GetWorkspaceId() != "workspace-1" {
		t.Fatalf("unary session metadata was not projected: %+v", got.Session)
	}
	if m.reviewerMode != "edits" || !m.reviewerEnabled || m.sessionName != "captured unary metadata" {
		t.Fatalf("UI metadata was not projected: reviewer=%q enabled=%t session=%q", m.reviewerMode, m.reviewerEnabled, m.sessionName)
	}
}

func TestRuntimeMainViewRefreshPreservesMetadataChangedAfterRequestStarted(t *testing.T) {
	v10 := runtimeTupleTestView(10, runtimeTupleTestIdleActivity())
	v10.Status = &runtimepb.Status{
		ReviewerFrequency: "stale unary reviewer",
		ThinkingLevel:     "stale unary thinking",
	}
	reads := &countingSessionViewClient{view: v10}
	runtimeClient := newTestSessionRuntimeClient(reads, newUnavailableRuntimeControlService())
	v9 := runtimeTupleTestView(9, runtimeTupleTestIdleActivity())
	v9.Status = &runtimepb.Status{ReviewerFrequency: "initial", ThinkingLevel: "initial"}
	runtimeClient.storeMainView(v9)
	m := newProjectedTestUIModel(runtimeClient)

	refresh := m.startRuntimeMainViewRefresh(nil)
	msg, ok := refresh().(runtimeMainViewRefreshedMsg)
	if !ok {
		t.Fatal("refresh command returned an unexpected message")
	}
	statusMessage := &transcriptpb.Message{Event: &transcriptpb.Event{Payload: &transcriptpb.Event_SessionStatus{SessionStatus: &transcriptpb.SessionStatus{
		ReviewerFrequency: "fresh transcript reviewer",
		ThinkingLevel:     "fresh transcript thinking",
		CompactionMode:    "auto",
	}}}}

	admission, err := runtimeClient.admitTranscriptMessageState(statusMessage)
	if err != nil {
		t.Fatalf("admit transcript status: %v", err)
	}
	m.applyAdmittedTranscriptMessageState(statusMessage, admission)

	m.handleRuntimeMainViewRefreshed(msg)

	got := runtimeClient.MainView()
	assertRuntimeTupleView(t, got, v10)
	if got.Status.ReviewerFrequency != "fresh transcript reviewer" || got.Status.ThinkingLevel != "fresh transcript thinking" {
		t.Fatalf("stale unary response replaced newer metadata: %+v", got.Status)
	}
	if m.reviewerMode != "fresh transcript reviewer" || m.thinkingLevel != "fresh transcript thinking" {
		t.Fatalf("UI projected stale unary metadata: reviewer=%q thinking=%q", m.reviewerMode, m.thinkingLevel)
	}
}

func runtimeTupleTestView(
	sequence uint64,
	activity *runtimepb.Activity,
) *runtimepb.MainView {
	return &runtimepb.MainView{
		Version:  &runtimepb.ReadModelVersion{Epoch: "runtime-tuple-test", Generation: 1, Sequence: sequence},
		Session:  &runtimepb.SessionView{SessionId: "session-1"},
		Status:   &runtimepb.Status{},
		Activity: activity,
	}
}

func runtimeTupleTestHydration(
	sequence uint64,
	activity *runtimepb.Activity,
) *transcriptpb.Message {
	message := ongoingHydrationMessage(1)
	payload := message.Event.GetHydration()
	payload.RuntimeReadModelUpdate = &runtimepb.ReadModelUpdate{
		Version:  &runtimepb.ReadModelVersion{Epoch: "runtime-tuple-test", Generation: 1, Sequence: sequence},
		Activity: activity,
	}
	message = &transcriptpb.Message{Sequence: 1, Event: &transcriptpb.Event{Payload: &transcriptpb.Event_Hydration{Hydration: payload}}}
	return message
}

func runtimeTupleTestUpdateMessage(
	deliverySequence uint64,
	runtimeSequence uint64,
	activity *runtimepb.Activity,
) *transcriptpb.Message {
	return &transcriptpb.Message{Sequence: deliverySequence, Event: &transcriptpb.Event{Payload: &transcriptpb.Event_RuntimeReadModelUpdate{RuntimeReadModelUpdate: &runtimepb.ReadModelUpdate{
		Version:  &runtimepb.ReadModelVersion{Epoch: "runtime-tuple-test", Generation: 1, Sequence: runtimeSequence},
		Activity: activity,
	}}}}

}

func runtimeTupleTestIdleActivity() *runtimepb.Activity {
	return &runtimepb.Activity{
		State:          runtimepb.ActivityState_RUNTIME_ACTIVITY_REGISTERED_IDLE,
		Reviewer:       runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_INACTIVE,
		QueueAccepting: true,
	}
}

func runtimeTupleTestRunningActivity() *runtimepb.Activity {
	return &runtimepb.Activity{
		State:          runtimepb.ActivityState_RUNTIME_ACTIVITY_RUNNING,
		Reviewer:       runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_INACTIVE,
		QueueAccepting: true,
		ActiveStep: &runtimepb.ActiveStep{
			RunId:      ongoingTestRunID().String(),
			StepId:     ongoingTestStepID().String(),
			ActiveKind: runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_USER_TURN,
		},
	}
}

func assertRuntimeTupleView(t *testing.T, got, want *runtimepb.MainView) {
	t.Helper()
	if !protoapi.ReadModelVersionsEqual(got.Version, want.Version) || !runtimeActivitiesEqual(got.Activity, want.Activity) {
		t.Fatalf(
			"runtime tuple = version=%+v activity=%+v, want version=%+v activity=%+v",
			got.Version,
			got.Activity,
			want.Version,
			want.Activity,
		)
	}
}

func assertRuntimeTupleHydrationError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected stale/conflicting hydration developer error")
	}
	developerErr, ok := err.(ongoing.DeveloperError)
	if !ok {
		t.Fatalf("hydration error = %T, want ongoing.DeveloperError", err)
	}
	if _, ok := developerErr.Facts["current_version"]; !ok {
		t.Fatalf("hydration error lacks current version diagnostics: %+v", developerErr)
	}
	if _, ok := developerErr.Facts["incoming_version"]; !ok {
		t.Fatalf("hydration error lacks incoming version diagnostics: %+v", developerErr)
	}
}

func assertUnchanged[T any](t *testing.T, label string, got, want T) {
	t.Helper()
	if gotMessage, ok := any(got).(proto.Message); ok {
		if !proto.Equal(gotMessage, any(want).(proto.Message)) {
			t.Fatalf("%s changed: got %+v, want %+v", label, got, want)
		}
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s changed: got %+v, want %+v", label, got, want)
	}
}
