package launch

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"core/server/metadata"
	"core/server/session"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
	"core/shared/textutil"
)

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
	persistence, err := metadata.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = persistence.Close() })
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
