package launch

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"core/internal/testharness/testsetup"
	"core/server/sessionstore"
	"core/shared/config"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
	"core/shared/textutil"
)

func TestPlanPreparedRetainedRoleConnectionFallback(t *testing.T) {
	for _, test := range []struct {
		name            string
		roleSettings    []string
		locked          bool
		wantReplacement *config.ConnectionID
	}{
		{name: "inherited set", wantReplacement: textutil.Value(config.ConnectionID("b"))},
		{name: "inherited set with locked model", locked: true, wantReplacement: textutil.Value(config.ConnectionID("b"))},
		{name: "explicit exhausted singleton", roleSettings: []string{`connection = "a"`}},
		{name: "explicit alternative singleton", roleSettings: []string{`connection = "c"`}, wantReplacement: textutil.Value(config.ConnectionID("c"))},
		{name: "explicit set", roleSettings: []string{`connection = ["a", "c"]`}, wantReplacement: textutil.Value(config.ConnectionID("c"))},
	} {
		t.Run(test.name, func(t *testing.T) {
			planner, container, persistence := newTypedIntentPlanner(t)
			lines := []string{
				`connection = ["a", "b"]`,
				`[connections.a]`, `protocol = "chatgpt-codex"`,
				`[connections.b]`, `protocol = "chatgpt-codex"`,
				`[connections.c]`, `protocol = "chatgpt-codex"`,
				`[subagents.worker]`, `model = "gpt-6-sol"`,
			}
			planner.Config = loadLaunchConfig(t, t.TempDir(), append(lines, test.roleSettings...)...)
			planner.Rotation = NewConnectionRotation(testsetup.OpenStore(t, t.TempDir()), time.Minute)
			store := createTypedIntentSession(t, container, sessioncontract.SessionCategoryMain, persistence)
			if err := store.SetContinuationContext(session.ContinuationContext{AgentRole: textutil.Value("worker")}); err != nil {
				t.Fatal(err)
			}
			if err := store.SetConnectionID("a"); err != nil {
				t.Fatal(err)
			}
			if test.locked {
				if err := store.MarkModelDispatchLocked(session.LockedContract{
					Model: "gpt-6-sol", HasEnabledTools: true, EnabledTools: []string{"exec_command"}, WebSearchMode: "native",
				}); err != nil {
					t.Fatal(err)
				}
			}
			meta := store.Meta()
			id := mustTypedIntentSessionID(t, meta.SessionID)
			plan, err := planner.PlanPreparedSession(t.Context(), SessionRequest{
				Mode: ModeHeadless, Intent: serverapi.OpenExistingSessionLaunchIntent(id),
			}, meta, PreparedExecutionContext{ProjectID: testProjectID})
			if err != nil {
				t.Fatal(err)
			}
			selected, err := plan.ActiveSettings.Connection.ConcreteID()
			if err != nil || selected == nil || *selected != "a" {
				t.Fatalf("retained dispatch connection = %v, %v; want a", selected, err)
			}
			planner.Rotation.RecordExhaustion("a")
			replacement, err := planner.Rotation.SelectReplacement(t.Context(), plan.ActiveSettings, plan.ConnectionScope, map[config.ConnectionID]struct{}{"a": {}})
			if test.wantReplacement == nil {
				var unavailable *ConnectionsUnavailableError
				if !errors.As(err, &unavailable) {
					t.Fatalf("exhausted singleton replacement error = %v", err)
				}
			} else if err != nil || replacement != *test.wantReplacement {
				t.Fatalf("role replacement = %q, %v; want %q", replacement, err, *test.wantReplacement)
			}
			record, err := persistence.ResolvePersistedSession(t.Context(), id.String())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(meta, *record.Meta) {
				t.Fatal("planning or selecting a replacement changed retained Session metadata")
			}
		})
	}
}

func TestPreparePreviousSessionChildAllocatesFreshRoleConnection(t *testing.T) {
	planner, _, persistence := newTypedIntentPlanner(t)
	planner.Config = loadLaunchConfig(t, t.TempDir())
	selection := config.ConnectionSelection{"a", "b", "c"}
	planner.Config.Settings.Connection = &selection
	planner.Config.Settings.ConnectionOrder = []config.ConnectionID{"a", "b", "c"}
	definition := planner.Config.Settings.Connections["test"]
	planner.Config.Settings.Connections = map[config.ConnectionID]config.ProviderConnection{
		"a": definition, "b": definition, "c": definition,
	}
	roleSelection := config.ConnectionSelection{"a", "b"}
	planner.Config.Settings.Subagents = map[string]config.SubagentRole{
		"worker": {
			Settings: config.Settings{Connection: &roleSelection},
			Sources: map[string]config.Origin{
				"connection": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "connection"}},
			},
		},
	}
	planner.Rotation = NewConnectionRotation(testsetup.OpenStore(t, t.TempDir()), 0)
	parent, err := planner.PlanSession(t.Context(), SessionRequest{
		Mode: ModeInteractive, Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
	})
	if err != nil {
		t.Fatal(err)
	}
	source := testStoreForPlannerPlan(t, planner, parent)
	if err := source.SetContinuationContext(session.ContinuationContext{AgentRole: textutil.Value("worker")}); err != nil {
		t.Fatal(err)
	}
	parentID := parent.Descriptor.SessionID()
	prepared, err := planner.PrepareSession(t.Context(), SessionPreparationRequest{
		Request: SessionRequest{
			Mode: ModeHeadless, Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.PreviousSessionCreateOrigin(parentID)),
		},
		SessionID: runtimeids.NewSessionID(), ProjectID: testProjectID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(prepared.Plan.ActiveSettings.Connection, config.SingleConnection("b")) {
		t.Fatalf("child connection = %v; want b", prepared.Plan.ActiveSettings.Connection)
	}
	meta := prepared.Creation.Snapshot().Meta
	if meta.PreviousSessionID == nil || *meta.PreviousSessionID != parentID ||
		!reflect.DeepEqual(meta.Continuation, source.Meta().Continuation) || meta.ConnectionID != nil {
		t.Fatalf("prepared child context = %+v", meta)
	}
	record, err := persistence.ResolvePersistedSession(t.Context(), parentID.String())
	if err != nil || record.Meta.ConnectionID == nil || *record.Meta.ConnectionID != "a" {
		t.Fatalf("parent binding changed: %+v, %v", record.Meta, err)
	}
	next, err := planner.Rotation.Prepare(t.Context(), planner.Config.Settings, true)
	if err != nil || next != "c" {
		t.Fatalf("next connection = %s, %v; want c after one child allocation", next, err)
	}
}

func TestPrepareSessionDoesNotPublishIdentityOrArtifacts(t *testing.T) {
	planner, container, persistence := newTypedIntentPlanner(t)
	id := runtimeids.NewSessionID()
	target := &worktreepb.SessionExecutionTarget{WorkspaceId: textutil.Value("workspace"), WorkspaceRoot: planner.Config.WorkspaceRoot}
	prepared, err := planner.PrepareSession(t.Context(), SessionPreparationRequest{
		Request:   SessionRequest{Mode: ModeHeadless, Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin())},
		SessionID: id, ExecutionTarget: target,
		ProjectID: testProjectID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Plan.Descriptor.SessionID() != id || prepared.Creation.Descriptor().SessionID() != id {
		t.Fatal("preparation changed requested identity")
	}
	if _, err := persistence.ResolvePersistedSession(t.Context(), id.String()); !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("preparation published metadata: %v", err)
	}
	if _, err := os.Stat(filepath.Join(container, id.String())); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preparation published artifacts: %v", err)
	}
	if prepared.Creation.Snapshot().Meta.Name != nil {
		t.Fatal("private preparation assigned a name before the number was committed")
	}
}

func TestPlanPreparedRetainedSessionLeavesMetadataUnchanged(t *testing.T) {
	planner, container, persistence := newTypedIntentPlanner(t)
	store := createTypedIntentSession(t, container, sessioncontract.SessionCategoryMain, persistence)
	meta := store.Meta()
	id, err := runtimeids.ParseSessionID(meta.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = planner.PlanPreparedSession(t.Context(), SessionRequest{
		Mode: ModeHeadless, Intent: serverapi.OpenExistingSessionLaunchIntent(id),
	}, meta, PreparedExecutionContext{
		ProjectID: testProjectID,
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := persistence.ResolvePersistedSession(t.Context(), id.String())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(meta, *record.Meta) {
		t.Fatal("retained preparation mutated committed metadata")
	}
}

func TestSubagentCreationLeavesNameAbsentAndAllowsAuthoredName(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	persistence := testsetup.OpenStore(t, root)
	binding, err := persistence.RegisterWorkspaceBinding(t.Context(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	store, err := session.Create(
		filepath.Join(root, "projects", binding.ProjectID, "sessions"), binding.WorkspaceName,
		workspace, sessioncontract.SessionCategorySubagent, persistence.AuthoritativeSessionStoreOptions()...,
	)
	if err != nil {
		t.Fatal(err)
	}
	if store.Meta().Name != nil || store.Identity().ShortID == nil {
		t.Fatalf("new subagent name/identity = %v, %v", store.Meta().Name, store.Identity())
	}
	if err := store.SetName(textutil.Value("authored name")); err != nil {
		t.Fatal(err)
	}
	if store.Meta().Name == nil || *store.Meta().Name != "authored name" {
		t.Fatal("existing name was replaced")
	}
}
