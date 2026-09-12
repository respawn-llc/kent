package app

import (
	"testing"

	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"

	"google.golang.org/protobuf/proto"
)

func TestRuntimeMainViewCacheOwnsIndependentSnapshots(t *testing.T) {
	runtimeClient := newTestSessionRuntimeClientWithControls(&reconnectRetryRuntimeControlClient{})
	incoming := runtimeTupleTestView(10, runtimeTupleTestRunningActivity())
	incoming.Status.Goal = runtimeClientTestRuntimeGoal(runtimeClientTestGoal("goal-1", "ship", runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE), false)
	incoming.Session.SessionName = proto.String("session")
	expected := proto.Clone(incoming).(*runtimepb.MainView)
	stored := runtimeClient.storeMainView(incoming)

	incoming.Activity.ActiveStep.StepId = "changed-input"
	incoming.Status.Goal.Goal.Objective = "changed-input"
	stored.Session.SessionName = proto.String("changed-return")
	stored.Version.Sequence++
	if got := runtimeClient.MainView(); !proto.Equal(got, expected) {
		t.Fatalf("cache changed through the input or stored result: got %v, want %v", got, expected)
	}

	cached, ok := runtimeClient.CachedMainView()
	if !ok {
		t.Fatal("stored snapshot is unavailable")
	}
	cached.Activity.QueueAccepting = false
	cached.Status.Goal.Goal.Objective = "changed-cache-read"
	runtimeClient.MainView().Version.Sequence++
	runtimeClient.Status().Goal.Goal.Objective = "changed-status"
	runtimeClient.SessionView().SessionName = proto.String("changed-session")
	if got := runtimeClient.MainView(); !proto.Equal(got, expected) {
		t.Fatalf("cache changed through a read projection: got %v, want %v", got, expected)
	}
}

func TestRuntimeTuplePolicy(t *testing.T) {
	valid := func(epoch string, generation, sequence uint64) *runtimepb.ReadModelVersion {
		return &runtimepb.ReadModelVersion{Epoch: epoch, Generation: generation, Sequence: sequence}
	}
	tests := []struct {
		name     string
		current  *runtimepb.ReadModelVersion
		incoming *runtimepb.ReadModelVersion
		ingress  runtimeTupleIngress
		want     runtimeTupleDecision
	}{
		{name: "first incremental", incoming: valid("epoch-1", 1, 1), ingress: runtimeTupleIngressIncremental, want: runtimeTupleApply},
		{name: "higher same generation incremental", current: valid("epoch-1", 1, 1), incoming: valid("epoch-1", 1, 2), ingress: runtimeTupleIngressIncremental, want: runtimeTupleApply},
		{name: "equal incremental", current: valid("epoch-1", 1, 2), incoming: valid("epoch-1", 1, 2), ingress: runtimeTupleIngressIncremental, want: runtimeTupleIgnore},
		{name: "lower incremental", current: valid("epoch-1", 1, 2), incoming: valid("epoch-1", 1, 1), ingress: runtimeTupleIngressIncremental, want: runtimeTupleIgnore},
		{name: "older generation incremental", current: valid("epoch-1", 2, 1), incoming: valid("epoch-1", 1, 99), ingress: runtimeTupleIngressIncremental, want: runtimeTupleIgnore},
		{name: "forward generation incremental", current: valid("epoch-1", 1, 99), incoming: valid("epoch-1", 2, 1), ingress: runtimeTupleIngressIncremental, want: runtimeTupleRefresh},
		{name: "new epoch incremental", current: valid("epoch-1", 2, 99), incoming: valid("epoch-2", 1, 1), ingress: runtimeTupleIngressIncremental, want: runtimeTupleRefresh},
		{name: "forward generation unary", current: valid("epoch-1", 1, 99), incoming: valid("epoch-1", 2, 1), ingress: runtimeTupleIngressAuthoritativeSnapshot, want: runtimeTupleApply},
		{name: "new epoch unary", current: valid("epoch-1", 2, 99), incoming: valid("epoch-2", 1, 1), ingress: runtimeTupleIngressAuthoritativeSnapshot, want: runtimeTupleApply},
		{name: "forward generation hydration", current: valid("epoch-1", 1, 99), incoming: valid("epoch-1", 2, 1), ingress: runtimeTupleIngressHydration, want: runtimeTupleApply},
		{name: "new epoch hydration", current: valid("epoch-1", 2, 99), incoming: valid("epoch-2", 1, 1), ingress: runtimeTupleIngressHydration, want: runtimeTupleApply},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := decideRuntimeTuple(tt.current, tt.incoming, tt.ingress); got != tt.want {
				t.Fatalf("decision = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHydrationRuntimeTupleAdmissionMapping(t *testing.T) {
	current := runtimeTupleTestView(
		11,
		runtimeTupleTestIdleActivity(),
	)
	tests := []struct {
		name        string
		incoming    *runtimepb.ReadModelUpdate
		wantErr     bool
		wantProject bool
	}{
		{
			name: "exact current tuple is accepted",
			incoming: &runtimepb.ReadModelUpdate{
				Version:  current.Version,
				Activity: current.Activity,
			},
			wantProject: true,
		},
		{
			name: "lower sequence is developer error",
			incoming: &runtimepb.ReadModelUpdate{
				Version:  &runtimepb.ReadModelVersion{Epoch: current.Version.Epoch, Generation: current.Version.Generation, Sequence: 10},
				Activity: current.Activity,
			},
			wantErr: true,
		},
		{
			name: "older generation is developer error",
			incoming: &runtimepb.ReadModelUpdate{
				Version:  &runtimepb.ReadModelVersion{Epoch: current.Version.Epoch, Generation: current.Version.Generation - 1, Sequence: 99},
				Activity: current.Activity,
			},
			wantErr: true,
		},
		{
			name: "same version conflict is developer error",
			incoming: &runtimepb.ReadModelUpdate{
				Version:  current.Version,
				Activity: runtimeTupleTestRunningActivity(),
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &sessionRuntimeClient{
				sessionID:   "session-1",
				mainView:    current,
				hasMainView: true,
			}
			message := ongoingHydrationMessage(1)
			payload := message.Event.GetHydration()
			payload.RuntimeReadModelUpdate = tt.incoming
			message = &transcriptpb.Message{Sequence: 1, Event: &transcriptpb.Event{Payload: &transcriptpb.Event_Hydration{Hydration: payload}}}
			result, err := client.admitTranscriptMessageState(message)
			if (err != nil) != tt.wantErr {
				t.Fatalf("admission error = %v, wantErr=%t", err, tt.wantErr)
			}
			if result.project != tt.wantProject {
				t.Fatalf("project = %t, want %t", result.project, tt.wantProject)
			}
			if tt.wantErr {
				assertUnchanged(t, "cache after rejected hydration", client.mainView, current)
			}
		})
	}
}
