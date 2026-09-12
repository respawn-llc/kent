package runtimewire

import (
	"context"
	"testing"
	"time"

	"core/server/runtime"
	"core/server/runtimeactivity"
	"core/shared/invariant"
	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
)

const (
	stepLifecycleTestRunID  = "11111111-1111-4111-8111-111111111111"
	stepLifecycleTestStepID = "22222222-2222-4222-8222-222222222222"
)

type recordingRuntimeReadModelPublisher struct {
	snapshots     []*runtimepb.ReadModelUpdate
	panicNext     bool
	panicMessage  string
	panicConsumed bool
}

func (p *recordingRuntimeReadModelPublisher) PublishRuntimeReadModelUpdate(_ string, snapshot *runtimepb.ReadModelUpdate) {
	if p.panicNext && !p.panicConsumed {
		p.panicConsumed = true
		panic(p.panicMessage)
	}
	p.snapshots = append(p.snapshots, snapshot)
}

type registrySnapshotRuntimeReadModelPublisher struct {
	recordingRuntimeReadModelPublisher
	registry runtimeactivity.RegistrySnapshot
}

func (p *registrySnapshotRuntimeReadModelPublisher) RuntimeActivityRegistrySnapshot(string) runtimeactivity.RegistrySnapshot {
	return p.registry
}

func TestStepLifecycleSinkPublishesVersionedRunningThenIdleActivity(t *testing.T) {
	publisher := &recordingRuntimeReadModelPublisher{}
	sink := NewStepLifecycleSink("session-1", publisher)
	if sink == nil {
		t.Fatal("expected step lifecycle sink")
	}
	startedAt := time.Now().UTC()
	if err := sink.StepBegan(context.Background(), runtime.StepLifecycleSnapshot{
		SessionID:  "session-1",
		RunID:      stepLifecycleTestRunID,
		StepID:     stepLifecycleTestStepID,
		ActiveKind: runtime.ActiveKindGoalLoop,
		StartedAt:  startedAt,
	}); err != nil {
		t.Fatalf("StepBegan: %v", err)
	}
	if err := sink.StepEnded(context.Background(), runtime.StepLifecycleSnapshot{
		SessionID:  "session-1",
		RunID:      stepLifecycleTestRunID,
		StepID:     stepLifecycleTestStepID,
		ActiveKind: runtime.ActiveKindGoalLoop,
		StartedAt:  startedAt,
		FinishedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("StepEnded: %v", err)
	}
	if len(publisher.snapshots) != 2 {
		t.Fatalf("snapshot count = %d, want 2", len(publisher.snapshots))
	}
	if publisher.snapshots[0].Activity.State != runtimepb.ActivityState_RUNTIME_ACTIVITY_RUNNING ||
		publisher.snapshots[0].Activity.ActiveStep == nil ||
		publisher.snapshots[0].Activity.ActiveStep.ActiveKind != runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_GOAL_LOOP {
		t.Fatalf("began snapshot = %+v, want running goal_loop", publisher.snapshots[0].Activity)
	}
	if publisher.snapshots[1].Activity.State != runtimepb.ActivityState_RUNTIME_ACTIVITY_REGISTERED_IDLE {
		t.Fatalf("ended snapshot = %+v, want registered idle", publisher.snapshots[1].Activity)
	}
	if !protoapi.ReadModelVersionNewerThan(publisher.snapshots[1].Version, publisher.snapshots[0].Version) {
		t.Fatalf("ended version must be newer than began: began=%+v ended=%+v", publisher.snapshots[0].Version, publisher.snapshots[1].Version)
	}
}

func TestStepLifecycleSinkPublishesAuxiliaryRuntimeActivityKinds(t *testing.T) {
	tests := map[runtime.ActiveKind]runtimepb.ActivityActiveKind{
		runtime.ActiveKindUserShell:          runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_USER_SHELL,
		runtime.ActiveKindRuntimeMaintenance: runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_RUNTIME_MAINTENANCE,
	}
	for kind, want := range tests {
		t.Run(string(kind), func(t *testing.T) {
			publisher := &recordingRuntimeReadModelPublisher{}
			sink := NewStepLifecycleSink("session-auxiliary", publisher)
			if err := sink.StepBegan(context.Background(), runtime.StepLifecycleSnapshot{
				SessionID:  "session-auxiliary",
				RunID:      stepLifecycleTestRunID,
				StepID:     stepLifecycleTestStepID,
				ActiveKind: kind,
				StartedAt:  time.Now().UTC(),
			}); err != nil {
				t.Fatalf("StepBegan: %v", err)
			}
			if len(publisher.snapshots) != 1 ||
				publisher.snapshots[0].Activity.ActiveStep == nil ||
				publisher.snapshots[0].Activity.ActiveStep.ActiveKind != want {
				t.Fatalf("published activity = %+v, want active kind %q", publisher.snapshots, want)
			}
		})
	}
}

func TestStepLifecycleSinkUsesPublisherRegistrySnapshotForTerminalActivity(t *testing.T) {
	publisher := &registrySnapshotRuntimeReadModelPublisher{registry: runtimeactivity.RegistrySnapshot{Registered: true, Draining: true}}
	sink := NewStepLifecycleSink("session-draining", publisher)

	if err := sink.StepEnded(context.Background(), runtime.StepLifecycleSnapshot{
		SessionID:  "session-draining",
		RunID:      stepLifecycleTestRunID,
		StepID:     stepLifecycleTestStepID,
		ActiveKind: runtime.ActiveKindUserTurn,
	}); err != nil {
		t.Fatalf("StepEnded: %v", err)
	}
	if len(publisher.snapshots) != 1 {
		t.Fatalf("snapshot count = %d, want 1", len(publisher.snapshots))
	}
	if publisher.snapshots[0].Activity.State != runtimepb.ActivityState_RUNTIME_ACTIVITY_DRAINING || publisher.snapshots[0].Activity.QueueAccepting {
		t.Fatalf("terminal activity = %+v, want draining and not queue accepting", publisher.snapshots[0].Activity)
	}
}

func TestStepLifecycleSinkPanicsOnPublicationInvariantFailureInPanicMode(t *testing.T) {
	publisher := &recordingRuntimeReadModelPublisher{panicNext: true, panicMessage: "broken publication"}
	sink := NewStepLifecycleSinkWithInvariantPolicy("session-panic", publisher, invariant.NewPolicy(invariant.WithMode(invariant.ModePanic)))

	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("StepEnded did not panic in panic invariant mode")
		}
		if len(publisher.snapshots) != 0 {
			t.Fatalf("published snapshots after panic = %+v, want none", publisher.snapshots)
		}
	}()
	_ = sink.StepEnded(context.Background(), runtime.StepLifecycleSnapshot{
		SessionID:  "session-panic",
		RunID:      stepLifecycleTestRunID,
		StepID:     stepLifecycleTestStepID,
		ActiveKind: runtime.ActiveKindGoalLoop,
	})
}
