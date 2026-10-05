package launch

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

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
	if prepared.Creation.Snapshot().Meta.Name == "" {
		t.Fatal("preparation omitted headless listing metadata")
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
