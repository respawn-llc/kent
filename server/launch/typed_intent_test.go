package launch

import (
	"context"
	"path/filepath"
	"testing"

	"core/server/session"
	"core/server/session/sessiontest"
	"core/shared/config"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
	"core/shared/textutil"
)

func TestPlannerCreateNewIntentCreatesWithAndWithoutValidatedParent(t *testing.T) {
	t.Run("without parent", func(t *testing.T) {
		planner, containerDir, _ := newTypedIntentPlanner(t)
		plan, err := planner.PlanSession(context.Background(), SessionRequest{
			Mode:   ModeInteractive,
			Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
		})
		if err != nil {
			t.Fatalf("plan create-new session: %v", err)
		}
		meta := testStoreForPlannerPlan(t, planner, plan).Meta()
		if meta.Category == nil || *meta.Category != sessioncontract.SessionCategoryMain {
			t.Fatalf("created category = %v, want main", meta.Category)
		}
		if meta.PreviousSessionID != nil || meta.ParentAgentSessionID != nil {
			t.Fatalf("provenance = previous:%v parent-agent:%v, want absent", meta.PreviousSessionID, meta.ParentAgentSessionID)
		}
		if testStoreForPlannerPlan(t, planner, plan).Dir() == containerDir {
			t.Fatal("expected a session directory below the container")
		}
	})

	t.Run("with validated parent", func(t *testing.T) {
		planner, containerDir, persistence := newTypedIntentPlanner(t)
		parent := createTypedIntentSession(t, containerDir, sessioncontract.SessionCategoryMain, persistence)
		parentID := mustTypedIntentSessionID(t, parent.Meta().SessionID)

		plan, err := planner.PlanSession(context.Background(), SessionRequest{
			Mode:   ModeInteractive,
			Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.PreviousSessionCreateOrigin(parentID)),
		})
		if err != nil {
			t.Fatalf("plan child session: %v", err)
		}
		meta := testStoreForPlannerPlan(t, planner, plan).Meta()
		if meta.PreviousSessionID == nil || *meta.PreviousSessionID != parentID {
			t.Fatalf("previous session ID = %v, want %q", meta.PreviousSessionID, parent.Meta().SessionID)
		}
	})
}

func TestPlannerOpenExistingIntentOpensTheRequestedSession(t *testing.T) {
	planner, containerDir, persistence := newTypedIntentPlanner(t)
	target := createTypedIntentSession(t, containerDir, sessioncontract.SessionCategoryMain, persistence)
	targetID := mustTypedIntentSessionID(t, target.Meta().SessionID)

	plan, err := planner.PlanSession(context.Background(), SessionRequest{
		Mode:   ModeInteractive,
		Intent: serverapi.OpenExistingSessionLaunchIntent(targetID),
	})
	if err != nil {
		t.Fatalf("plan open-existing session: %v", err)
	}
	if plan.Descriptor.SessionID().String() != target.Meta().SessionID {
		t.Fatalf("opened session ID = %q, want %q", plan.Descriptor.SessionID(), target.Meta().SessionID)
	}
}

func TestNavigationOnlyCreationUsesFreshSettingsAndWorkspace(t *testing.T) {
	planner, container, persistence := newTypedIntentPlanner(t)
	source := createTypedIntentSession(t, container, sessioncontract.SessionCategoryMain, persistence)
	if err := source.MarkModelDispatchLocked(session.LockedContract{Model: "source-model"}); err != nil {
		t.Fatal(err)
	}
	id := mustTypedIntentSessionID(t, source.Meta().SessionID)
	planner.Config.WorkspaceRoot = "/tmp/fresh-workspace"
	plan, err := planner.PlanSession(t.Context(), SessionRequest{
		Mode:   ModeInteractive,
		Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.NavigationOnlySessionCreateOrigin(id)),
		InitialChat: &session.ChatDraftState{
			Agent: config.DefaultSubagentRole,
			Settings: &session.ChatSettingsOverrides{
				Thinking: textutil.Value("low"), Questions: textutil.Value(false),
				Supervisor: textutil.Value("off"), Fast: textutil.Value(false), AutoCompaction: textutil.Value(true),
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	meta := testStoreForPlannerPlan(t, planner, plan).Meta()
	if meta.PreviousSessionID == nil || *meta.PreviousSessionID != id {
		t.Fatalf("navigation origin = %v", meta.PreviousSessionID)
	}
	if meta.ParentAgentSessionID != nil || meta.Locked != nil || meta.Continuation != nil || meta.WorkflowSnapshot != nil || meta.WorktreeReminder != nil {
		t.Fatalf("navigation-only creation inherited context: %+v", meta)
	}
	if meta.WorkspaceRoot != planner.Config.WorkspaceRoot || plan.ActiveSettings.Model != planner.Config.Settings.Model {
		t.Fatalf("fresh target/settings were not retained: %+v", plan)
	}
	if plan.QuestionsEnabled || plan.ActiveSettings.ThinkingLevel != "low" {
		t.Fatalf("selected settings were not retained: %+v", plan)
	}
}

func TestTypedLaunchSourceRejectsEscapingIdentity(t *testing.T) {
	if _, err := runtimeids.ParseSessionID("../escape"); err == nil {
		t.Fatal("path-shaped session identity validated")
	}
}

func newTypedIntentPlanner(t *testing.T) (Planner, string, *sessiontest.Persistence) {
	t.Helper()
	root := t.TempDir()
	containerDir := filepath.Join(root, "sessions")
	persistence := sessiontest.NewPersistence()
	return newPersistenceBackedTestPlanner(config.App{
		WorkspaceRoot:   "/tmp/workspace-a",
		PersistenceRoot: root,
		Settings:        config.Settings{Model: "gpt-6-sol"},
	}, containerDir, persistence), containerDir, persistence
}

func createTypedIntentSession(t *testing.T, containerDir string, category sessioncontract.SessionCategory, persistence *sessiontest.Persistence) *session.Store {
	t.Helper()
	store, err := session.Create(containerDir, "workspace-a", "/tmp/workspace-a", category, persistence.Options()...)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable: %v", err)
	}
	return store
}

func mustTypedIntentSessionID(t *testing.T, raw string) runtimeids.SessionID {
	t.Helper()
	id, err := runtimeids.ParseSessionID(raw)
	if err != nil {
		t.Fatalf("parse session ID %q: %v", raw, err)
	}
	return id
}

func createNewTypedIntentWithPreviousSession(t *testing.T, raw string) serverapi.SessionLaunchIntent {
	t.Helper()
	parentID := mustTypedIntentSessionID(t, raw)
	return serverapi.CreateNewSessionLaunchIntent(serverapi.PreviousSessionCreateOrigin(parentID))
}
