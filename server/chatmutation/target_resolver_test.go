package chatmutation

import (
	"context"
	"path/filepath"
	"testing"

	"core/server/launch"
	"core/server/metadata"
	"core/server/promptcommands"
	"core/server/session"
	"core/server/sessionlaunch"
	"core/shared/config"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeids"
	"core/shared/sessioncontract"
)

type placementActivity struct {
	state runtimepb.ActivityState
}

func (a placementActivity) RuntimeReadModelFeedSnapshot(context.Context, string) (*runtimepb.ReadModelUpdate, error) {
	return &runtimepb.ReadModelUpdate{Activity: &runtimepb.Activity{State: a.state}}, nil
}

func TestFreshPlacementRetainsIdleFreshSession(t *testing.T) {
	_, store, source, planner := newPlacementFixture(t)
	resolver := NewTargetResolver(store, nil, func(context.Context, runtimeids.SessionID) (PersistedSessionPlanner, error) {
		return planner, nil
	}, placementActivity{state: runtimepb.ActivityState_RUNTIME_ACTIVITY_REGISTERED_IDLE})
	id, err := runtimeids.ParseSessionID(source.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	target, err := resolver.SelectPlacement(t.Context(), ResolvedTarget{SessionID: id}, promptcommands.PlacementFresh)
	if err != nil {
		t.Fatal(err)
	}
	if target.SessionID != id || target.Created {
		t.Fatalf("idle fresh target = %+v, want source %s", target, id)
	}
}

func TestFreshPlacementCreatesDurablePreviousSessionChild(t *testing.T) {
	for _, established := range []bool{false, true} {
		name := "busy fresh"
		if established {
			name = "idle established"
		}
		t.Run(name, func(t *testing.T) {
			_, store, source, planner := newPlacementFixture(t)
			state := runtimepb.ActivityState_RUNTIME_ACTIVITY_RUNNING
			if established {
				log, err := source.MaterializeEventLog()
				if err != nil {
					t.Fatal(err)
				}
				content := "earlier user turn"
				if _, _, err := log.AppendRecord(nil, session.MessageRecord{Role: session.MessageRoleUser, Content: &content}); err != nil {
					t.Fatal(err)
				}
				state = runtimepb.ActivityState_RUNTIME_ACTIVITY_REGISTERED_IDLE
			}
			resolver := NewTargetResolver(store, nil, func(context.Context, runtimeids.SessionID) (PersistedSessionPlanner, error) {
				return planner, nil
			}, placementActivity{state: state})
			id, err := runtimeids.ParseSessionID(source.Meta().SessionID)
			if err != nil {
				t.Fatal(err)
			}
			target, err := resolver.SelectPlacement(t.Context(), ResolvedTarget{SessionID: id}, promptcommands.PlacementFresh)
			if err != nil {
				t.Fatal(err)
			}
			if target.SessionID == id || !target.Created {
				t.Fatalf("busy target = %+v, want child of %s", target, id)
			}
			child, err := session.ResolvePersistedSessionRecord(t.Context(), store, target.SessionID.String())
			if err != nil {
				t.Fatal(err)
			}
			if child.Meta.PreviousSessionID == nil || *child.Meta.PreviousSessionID != id || child.Meta.ConversationEstablished {
				t.Fatalf("child lineage/freshness = %+v", child.Meta)
			}
			parent, err := session.ResolvePersistedSessionRecord(t.Context(), store, id.String())
			if err != nil {
				t.Fatal(err)
			}
			if parent.Meta.PreviousSessionID != nil || parent.Meta.ConversationEstablished != established {
				t.Fatalf("source was changed: %+v", parent.Meta)
			}
		})
	}
}

func newPlacementFixture(t *testing.T) (config.App, *metadata.Store, *session.Store, *sessionlaunch.Service) {
	t.Helper()
	t.Setenv(config.PersistenceRootEnvName, t.TempDir())
	workspace := t.TempDir()
	cfg, err := config.Load(workspace, workspace, config.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	store, err := metadata.Open(cfg.PersistenceRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	binding, err := store.RegisterWorkspaceBinding(t.Context(), cfg.WorkspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	container := filepath.Join(cfg.PersistenceRoot, "projects", binding.ProjectID, "sessions")
	source, err := session.Create(container, binding.ProjectID, cfg.WorkspaceRoot, sessioncontract.SessionCategoryMain, store.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatal(err)
	}
	planner := sessionlaunch.NewService(launch.Planner{
		Config: cfg, ContainerDir: container, StoreOptions: store.AuthoritativeSessionStoreOptions(), PersistedSessions: store,
		ExecutionTargets: store, SessionProjects: store, ManagedWorktreeRoots: store,
	})
	return cfg, store, source, planner
}
