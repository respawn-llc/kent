package launch

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"core/internal/testharness/testsetup"
	"core/server/sessionstore"
	"core/shared/config"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
	"core/shared/textutil"
)

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
	planner.Rotation = new(ConnectionRotation)
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
	next, err := planner.Rotation.Prepare(planner.Config.Settings, true)
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

func TestHeadlessNameUsesCommittedNumberAndPreservesAuthoredName(t *testing.T) {
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
	if err := EnsureSubagentSessionName(store); err != nil {
		t.Fatal(err)
	}
	if store.Meta().Name == nil || *store.Meta().Name != store.Identity().String()+" "+SubagentSessionSuffix || store.Identity().ShortID == nil {
		t.Fatalf("generated name did not use committed number: %v", store.Meta().Name)
	}
	if err := store.SetName(textutil.Value("authored name")); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSubagentSessionName(store); err != nil {
		t.Fatal(err)
	}
	if store.Meta().Name == nil || *store.Meta().Name != "authored name" {
		t.Fatal("existing name was replaced")
	}
}
