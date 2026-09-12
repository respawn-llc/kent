package runtimeactivity

import (
	"testing"

	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
)

const (
	runtimeActivityTestRunID  = "11111111-1111-4111-8111-111111111111"
	runtimeActivityTestStepID = "22222222-2222-4222-8222-222222222222"
)

func TestResolveRuntimeActivityUsesOnlyLiveResolverInputs(t *testing.T) {
	tests := []struct {
		name     string
		snapshot ResolverSnapshot
		want     runtimepb.ActivityState
		wantKind runtimepb.ActivityActiveKind
		active   bool
	}{
		{name: "no runtime entry unavailable", want: runtimepb.ActivityState_RUNTIME_ACTIVITY_UNAVAILABLE},
		{
			name:     "registered idle",
			snapshot: ResolverSnapshot{Registry: RegistrySnapshot{Registered: true, QueueAccepting: true}},
			want:     runtimepb.ActivityState_RUNTIME_ACTIVITY_REGISTERED_IDLE,
		},
		{
			name: "running copies runtime-owned active kind",
			snapshot: ResolverSnapshot{
				Registry: RegistrySnapshot{Registered: true, QueueAccepting: true},
				Active:   &ActiveStepSnapshot{RunID: runtimeActivityTestRunID, StepID: runtimeActivityTestStepID, ActiveKind: runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_GOAL_LOOP},
			},
			want:     runtimepb.ActivityState_RUNTIME_ACTIVITY_RUNNING,
			wantKind: runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_GOAL_LOOP,
			active:   true,
		},
		{
			name:     "draining masks active details",
			snapshot: ResolverSnapshot{Registry: RegistrySnapshot{Registered: true, Draining: true}},
			want:     runtimepb.ActivityState_RUNTIME_ACTIVITY_DRAINING,
			active:   true,
		},
		{
			name:     "closing masks active details",
			snapshot: ResolverSnapshot{Registry: RegistrySnapshot{Registered: true, Closing: true}},
			want:     runtimepb.ActivityState_RUNTIME_ACTIVITY_CLOSING,
			active:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			activity, err := ResolveRuntimeActivity(test.snapshot)
			if err != nil {
				t.Fatalf("ResolveRuntimeActivity: %v", err)
			}
			if activity.State != test.want {
				t.Fatalf("state = %q, want %q", activity.State, test.want)
			}
			if got := runtimeActivityKind(activity); got != test.wantKind {
				t.Fatalf("active kind = %q, want %q", got, test.wantKind)
			}
			if got := protoapi.RuntimeActivityActiveForControl(activity); got != test.active {
				t.Fatalf("active = %t, want %t", got, test.active)
			}
		})
	}
}

func TestResolveRuntimeActivityPreservesReviewerPhase(t *testing.T) {
	for _, want := range []runtimepb.ReviewerActivity{runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_INVOKING, runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_ADDRESSING_FEEDBACK} {
		t.Run(want.String(), func(t *testing.T) {
			activity, err := ResolveRuntimeActivity(ResolverSnapshot{
				Registry: RegistrySnapshot{Registered: true, QueueAccepting: true},
				Reviewer: want,
			})
			if err != nil {
				t.Fatalf("ResolveRuntimeActivity: %v", err)
			}
			if activity.Reviewer != want {
				t.Fatalf("Reviewer activity = %q, want %q", activity.Reviewer, want)
			}
		})
	}
}

func TestResolveRuntimeActivityCopiesEveryRuntimeOwnedActiveKind(t *testing.T) {
	for _, kind := range []runtimepb.ActivityActiveKind{runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_USER_TURN, runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_WORKFLOW_TURN, runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_GOAL_LOOP, runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_COMPACTION, runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_BACKGROUND} {
		t.Run(kind.String(), func(t *testing.T) {
			activity, err := ResolveRuntimeActivity(ResolverSnapshot{
				Registry: RegistrySnapshot{Registered: true, QueueAccepting: true},
				Active:   &ActiveStepSnapshot{RunID: runtimeActivityTestRunID, StepID: runtimeActivityTestStepID, ActiveKind: kind},
			})
			if err != nil {
				t.Fatalf("ResolveRuntimeActivity: %v", err)
			}
			if activity.State != runtimepb.ActivityState_RUNTIME_ACTIVITY_RUNNING || runtimeActivityKind(activity) != kind {
				t.Fatalf("activity = %+v, want running %q", activity, kind)
			}
			if !protoapi.RuntimeActivityActiveForControl(activity) {
				t.Fatalf("activity must block runtime control while active: %+v", activity)
			}
		})
	}
}

func TestResolveRuntimeActivityProjectsPromptWaitFromExplicitFact(t *testing.T) {
	activity, err := ResolveRuntimeActivity(ResolverSnapshot{
		Registry:   RegistrySnapshot{Registered: true},
		Active:     &ActiveStepSnapshot{RunID: runtimeActivityTestRunID, StepID: runtimeActivityTestStepID, ActiveKind: runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_USER_TURN},
		PromptWait: true,
	})
	if err != nil {
		t.Fatalf("ResolveRuntimeActivity: %v", err)
	}
	if activity.State != runtimepb.ActivityState_RUNTIME_ACTIVITY_AWAITING_PROMPT || runtimeActivityKind(activity) != runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_USER_TURN {
		t.Fatalf("activity = %+v, want awaiting prompt on active runtime step", activity)
	}
}

func TestResolveRuntimeActivityKeepsPassiveQueuedFactsIdle(t *testing.T) {
	activity, err := ResolveRuntimeActivity(ResolverSnapshot{
		Registry: RegistrySnapshot{Registered: true, QueueAccepting: true},
	})
	if err != nil {
		t.Fatalf("ResolveRuntimeActivity: %v", err)
	}
	if activity.State != runtimepb.ActivityState_RUNTIME_ACTIVITY_REGISTERED_IDLE {
		t.Fatalf("passive queued facts must not become active activity, got %+v", activity)
	}

	activity, err = ResolveRuntimeActivity(ResolverSnapshot{
		Registry:            RegistrySnapshot{Registered: true},
		PendingContinuation: PendingContinuationSnapshot{Promoted: true},
	})
	if err != nil {
		t.Fatalf("ResolveRuntimeActivity promoted continuation: %v", err)
	}
	if activity.State != runtimepb.ActivityState_RUNTIME_ACTIVITY_STARTING {
		t.Fatalf("promoted continuation activity = %+v, want starting", activity)
	}
}

func TestResolveRuntimeActivityTreatsOpenLiveRunGroupAsBlocking(t *testing.T) {
	activity, err := ResolveRuntimeActivity(ResolverSnapshot{
		Registry:      RegistrySnapshot{Registered: true, QueueAccepting: true},
		LiveRunActive: true,
	})
	if err != nil {
		t.Fatalf("ResolveRuntimeActivity: %v", err)
	}
	if activity.State != runtimepb.ActivityState_RUNTIME_ACTIVITY_DRAINING || !protoapi.RuntimeActivityActiveForControl(activity) {
		t.Fatalf("activity = %+v, want blocking live-run group projection", activity)
	}
}

func TestReadModelVersionsPermitSequenceHoles(t *testing.T) {
	first, err := BuildFeedSnapshot(NextReadModelVersion("sequence-holes"), ResolverSnapshot{Registry: RegistrySnapshot{Registered: true}})
	if err != nil {
		t.Fatalf("first snapshot: %v", err)
	}
	hole := NextReadModelVersion("sequence-holes")
	second, err := BuildFeedSnapshot(NextReadModelVersion("sequence-holes"), ResolverSnapshot{Registry: RegistrySnapshot{Registered: true}})
	if err != nil {
		t.Fatalf("second snapshot: %v", err)
	}
	if !protoapi.ReadModelVersionNewerThan(hole, first.Version) || !protoapi.ReadModelVersionNewerThan(second.Version, hole) {
		t.Fatalf("versions did not preserve hole ordering: first=%+v hole=%+v second=%+v", first.Version, hole, second.Version)
	}
}

func TestReadModelVersionBuildsCanonicalFeedSnapshot(t *testing.T) {
	update, err := BuildFeedSnapshot(NextReadModelVersion("canonical-feed"), ResolverSnapshot{
		Registry: RegistrySnapshot{Registered: true, QueueAccepting: true},
	})
	if err != nil {
		t.Fatalf("build canonical feed snapshot: %v", err)
	}
	if err := protoapi.Validate(update); err != nil {
		t.Fatalf("validate canonical feed snapshot: %v", err)
	}
	if update.Activity.State != runtimepb.ActivityState_RUNTIME_ACTIVITY_REGISTERED_IDLE || !update.Activity.QueueAccepting {
		t.Fatalf("canonical activity = %+v", update.Activity)
	}
}

func runtimeActivityKind(activity *runtimepb.Activity) runtimepb.ActivityActiveKind {
	if activity.ActiveStep == nil {
		return runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_UNSPECIFIED
	}
	return activity.ActiveStep.ActiveKind
}
