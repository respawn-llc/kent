package registry

import (
	"path/filepath"
	"testing"

	testharness "core/internal/testharness/testsetup"
	"core/server/agentruntime"
	"core/server/runtimewire"
	askquestion "core/server/tools"
	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
)

func TestStepLifecyclePublishesRegistryLiveActivity(t *testing.T) {
	for kind, wantKind := range map[runtime.ActiveKind]runtimepb.ActivityActiveKind{
		runtime.ActiveKindGoalLoop:           runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_GOAL_LOOP,
		runtime.ActiveKindUserShell:          runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_USER_SHELL,
		runtime.ActiveKindRuntimeMaintenance: runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_RUNTIME_MAINTENANCE,
	} {
		t.Run(string(kind), func(t *testing.T) {
			registry := NewRuntimeRegistry(nil)
			store := newRegistryTestSession(t, t.TempDir(), "workspace", t.TempDir())
			metadata := testharness.OpenStore(t, filepath.Dir(store.Dir()))
			testharness.ImportSessionProjectWorkspaceContext(t, metadata, store)
			eventLog, err := store.MaterializeEventLog()
			if err != nil {
				t.Fatal(err)
			}
			engine, err := runtime.New(store, eventLog, registryRuntimeFakeClient{}, askquestion.NewRegistry(), runtime.Config{
				Model:                         "gpt-6-sol",
				ThinkingLevel:                 "medium",
				ProjectWorkspaceContextReader: metadata,
				StepLifecycle:                 runtimewire.NewStepLifecycleSink(store.Identity().ID.String(), registry),
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = engine.Close() })
			registerReady(t, registry, engine.SessionID(), engine)
			t.Cleanup(func() { closeRuntime(registry, engine.SessionID(), engine) })

			var runningVersion *runtimepb.ReadModelVersion
			if err := engine.RunWhenIdle(t.Context(), kind, func() error {
				view, ok := registry.RuntimeMainViewSnapshot(engine.SessionID())
				if !ok || view.Activity.State != runtimepb.ActivityState_RUNTIME_ACTIVITY_RUNNING ||
					view.Activity.ActiveStep == nil || view.Activity.ActiveStep.ActiveKind != wantKind {
					t.Fatalf("live activity = %+v, want running %s", view, kind)
				}
				if err := protoapi.Validate(view.Activity); err != nil {
					t.Fatalf("invalid running activity: %v", err)
				}
				runningVersion = view.Version
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			view, ok := registry.RuntimeMainViewSnapshot(engine.SessionID())
			if !ok || view.Activity.State != runtimepb.ActivityState_RUNTIME_ACTIVITY_REGISTERED_IDLE {
				t.Fatalf("terminal activity = %+v, want registered idle", view)
			}
			if !protoapi.ReadModelVersionNewerThan(view.Version, runningVersion) {
				t.Fatalf("terminal version = %+v must follow running version %+v", view.Version, runningVersion)
			}
			if err := protoapi.Validate(view.Activity); err != nil {
				t.Fatalf("invalid terminal activity: %v", err)
			}
		})
	}
}
