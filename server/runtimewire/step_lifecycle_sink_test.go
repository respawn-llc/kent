package runtimewire

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"core/server/agentruntime"
	"core/server/runtimeactivity"
	"core/shared/invariant"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
)

type recordingRuntimeReadModelPublisher struct {
	snapshots []*runtimepb.ReadModelUpdate
	registry  runtimeactivity.RegistrySnapshot
	feed      func() (*runtimepb.ReadModelUpdate, error)
}

func (p *recordingRuntimeReadModelPublisher) PublishRuntimeReadModelUpdate(_ string, snapshot *runtimepb.ReadModelUpdate) {
	p.snapshots = append(p.snapshots, snapshot)
}

func (p *recordingRuntimeReadModelPublisher) RuntimeActivityRegistrySnapshot(string) runtimeactivity.RegistrySnapshot {
	return p.registry
}

func (p *recordingRuntimeReadModelPublisher) RuntimeReadModelFeedSnapshot(context.Context, string) (*runtimepb.ReadModelUpdate, error) {
	return p.feed()
}

func lifecycleFeedSnapshot(t *testing.T, resolver runtimeactivity.ResolverSnapshot) *runtimepb.ReadModelUpdate {
	t.Helper()
	update, err := runtimeactivity.BuildFeedSnapshot(runtimeactivity.NextReadModelVersion(t.Name()), resolver)
	if err != nil {
		t.Fatal(err)
	}
	return update
}

func TestStepLifecycleSinkPublishesAuthoritativeFeed(t *testing.T) {
	for name, resolver := range map[string]runtimeactivity.ResolverSnapshot{
		"running": {
			Registry: runtimeactivity.RegistrySnapshot{Registered: true, QueueAccepting: true},
			Active: &runtimeactivity.ActiveStepSnapshot{
				StepID:     "22222222-2222-4222-8222-222222222222",
				ActiveKind: runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_USER_TURN,
			},
		},
		"idle":     {Registry: runtimeactivity.RegistrySnapshot{Registered: true, QueueAccepting: true}},
		"draining": {Registry: runtimeactivity.RegistrySnapshot{Registered: true, Draining: true}},
	} {
		t.Run(name, func(t *testing.T) {
			update := lifecycleFeedSnapshot(t, resolver)
			publisher := &recordingRuntimeReadModelPublisher{
				feed: func() (*runtimepb.ReadModelUpdate, error) { return update, nil },
			}
			sink := NewStepLifecycleSink("session-feed", publisher)
			if err := sink.StepBegan(t.Context(), runtime.StepLifecycleSnapshot{}); err != nil {
				t.Fatal(err)
			}
			if err := sink.StepEnded(t.Context(), runtime.StepLifecycleSnapshot{}); err != nil {
				t.Fatal(err)
			}
			if len(publisher.snapshots) != 2 || publisher.snapshots[0] != update || publisher.snapshots[1] != update {
				t.Fatalf("published snapshots = %+v, want authoritative feed for both transitions", publisher.snapshots)
			}
		})
	}
}

func TestStepLifecycleSinkNormalizesUnavailableFeed(t *testing.T) {
	update := lifecycleFeedSnapshot(t, runtimeactivity.ResolverSnapshot{})
	publisher := &recordingRuntimeReadModelPublisher{
		feed: func() (*runtimepb.ReadModelUpdate, error) { return update, nil },
	}
	if err := NewStepLifecycleSink("session-unavailable", publisher).StepEnded(t.Context(), runtime.StepLifecycleSnapshot{}); err != nil {
		t.Fatal(err)
	}
	if len(publisher.snapshots) != 1 || publisher.snapshots[0].Activity.State != runtimepb.ActivityState_RUNTIME_ACTIVITY_REGISTERED_IDLE ||
		!publisher.snapshots[0].Activity.QueueAccepting {
		t.Fatalf("published snapshots = %+v, want registered idle", publisher.snapshots)
	}
}

func TestStepLifecycleSinkFeedFailureDiagnosticsAndTerminalRecovery(t *testing.T) {
	failure := errors.New("feed unavailable")
	recoveryFailure := errors.New("recovery unavailable")
	lifecycle := runtime.StepLifecycleSnapshot{SessionID: "session-failure", StepID: "22222222-2222-4222-8222-222222222222", ActiveKind: runtime.ActiveKindGoalLoop}
	for _, terminal := range []bool{false, true} {
		for _, recoveryFails := range []bool{false, true} {
			t.Run(fmt.Sprintf("terminal=%t/recoveryFails=%t", terminal, recoveryFails), func(t *testing.T) {
				recovery := lifecycleFeedSnapshot(t, runtimeactivity.ResolverSnapshot{Registry: runtimeactivity.RegistrySnapshot{Registered: true, Draining: true}})
				calls := 0
				publisher := &recordingRuntimeReadModelPublisher{
					registry: runtimeactivity.RegistrySnapshot{Registered: true, Draining: true},
					feed: func() (*runtimepb.ReadModelUpdate, error) {
						calls++
						if calls == 1 {
							return nil, failure
						}
						if recoveryFails {
							return nil, recoveryFailure
						}
						return recovery, nil
					},
				}
				var diagnostics []invariant.Diagnostic
				sink := NewStepLifecycleSinkWithInvariantPolicy(lifecycle.SessionID, publisher, invariant.NewPolicy(
					invariant.WithMode(invariant.ModeDiagnostic),
					invariant.WithSink(invariant.SinkFunc(func(d invariant.Diagnostic) { diagnostics = append(diagnostics, d) })),
				))
				var err error
				if terminal {
					err = sink.StepEnded(t.Context(), lifecycle)
				} else {
					err = sink.StepBegan(t.Context(), lifecycle)
				}
				if terminal && recoveryFails {
					if !errors.Is(err, recoveryFailure) {
						t.Fatalf("recovery error = %v, want %v", err, recoveryFailure)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				if len(diagnostics) != 1 {
					t.Fatalf("diagnostics = %+v, want one publication failure", diagnostics)
				}
				d := diagnostics[0]
				if d.Scope != invariant.ScopeReadModelPublication || d.Fields[invariant.FieldSessionID] != lifecycle.SessionID ||
					d.Fields[invariant.FieldResolverInputs] != fmt.Sprintf("%+v", lifecycle) ||
					d.Fields[invariant.FieldOwnerSnapshots] != fmt.Sprintf("%+v", publisher.registry) ||
					d.Fields[invariant.FieldProviderError] != failure.Error() {
					t.Fatalf("publication diagnostic lost failure context: %+v", d)
				}
				wantCalls, wantPublished := 1, 0
				if terminal {
					wantCalls = 2
					if !recoveryFails {
						wantPublished = 1
					}
				}
				if calls != wantCalls || len(publisher.snapshots) != wantPublished {
					t.Fatalf("feed reads = %d, publications = %d; want %d, %d", calls, len(publisher.snapshots), wantCalls, wantPublished)
				}
				if wantPublished == 1 && publisher.snapshots[0] != recovery {
					t.Fatalf("published snapshot = %+v, want authoritative recovery", publisher.snapshots[0])
				}
			})
		}
	}
}

func TestStepLifecycleSinkPanicsOnPublicationInvariantFailureInPanicMode(t *testing.T) {
	publisher := &recordingRuntimeReadModelPublisher{
		feed: func() (*runtimepb.ReadModelUpdate, error) { return nil, errors.New("broken feed") },
	}
	sink := NewStepLifecycleSinkWithInvariantPolicy("session-panic", publisher, invariant.NewPolicy(invariant.WithMode(invariant.ModePanic)))
	defer func() {
		diagnostic, ok := recover().(invariant.Diagnostic)
		if !ok || diagnostic.Scope != invariant.ScopeReadModelPublication {
			t.Fatalf("expected publication invariant panic, got %+v", diagnostic)
		}
		if len(publisher.snapshots) != 0 {
			t.Fatalf("published snapshots after panic = %+v, want none", publisher.snapshots)
		}
	}()
	_ = sink.StepEnded(t.Context(), runtime.StepLifecycleSnapshot{})
}
