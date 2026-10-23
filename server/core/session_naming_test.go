package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	modelstub "core/internal/testharness/pty/blackbox"
	"core/internal/testharness/requestfixture"
	"core/internal/testharness/testsetup"
	"core/server/authmanager"
	serverbootstrap "core/server/bootstrap"
	"core/server/metadata"
	"core/shared/config"
	"core/shared/protoapi"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	workflowpb "core/shared/protoapi/gen/kent/api/workflow_definition"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
	"core/shared/serverapi"
	"core/shared/textutil"
	"github.com/google/uuid"
)

func TestNamingContinuesAfterTurnAndCallerEnd(t *testing.T) {
	testNamingAfterExecutionTargetChange(t, false)
}

func TestNamingOptOutAfterExecutionTargetChange(t *testing.T) {
	testNamingAfterExecutionTargetChange(t, true)
}

func testNamingAfterExecutionTargetChange(t *testing.T, optOut bool) {
	t.Helper()
	nanoStarted := make(chan struct{})
	releaseNano := make(chan struct{})
	requests := make(chan string, 10)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		requests <- request.Model
		if request.Model == "gpt-6-luna" {
			close(nanoStarted)
			select {
			case <-releaseNano:
				modelstub.WriteCompletedResponseStream(w, "Fix invoice totals", 10, 4)
			case <-r.Context().Done():
				t.Error("naming was canceled by the completed Turn/caller")
			}
		} else {
			modelstub.WriteCompletedResponseStream(w, "Fixed", 10, 4)
		}
	}))
	defer endpoint.Close()
	root, workspace := t.TempDir(), t.TempDir()
	body := fmt.Sprintf("model = \"gpt-6-sol\"\nconnection = \"local\"\n[connections.local]\nprotocol = \"responses\"\nendpoint = %q\n[nano]\nconnection = \"local\"\nmodel = \"gpt-6-sol\"\n[nano.features]\nsession_naming = %t\n", endpoint.URL, optOut)
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspace, LoadOptions: config.LoadOptions{ConfigRoot: root}})
	if err != nil {
		t.Fatal(err)
	}
	meta := testsetup.OpenStore(t, root)
	binding, err := meta.RegisterWorkspaceBinding(t.Context(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	worktree, worktreeID := filepath.Join(root, "worktrees", "naming"), uuid.NewString()
	if err := meta.UpsertWorktreeRecord(t.Context(), metadata.WorktreeRecord{
		ID: worktreeID, WorkspaceID: binding.WorkspaceID, CanonicalRoot: worktree, Managed: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(worktree, ".kent"), 0o755); err != nil {
		t.Fatal(err)
	}
	nanoConfig := fmt.Sprintf("[nano]\nmodel = \"gpt-6-luna\"\n[nano.features]\nsession_naming = %t\n", !optOut)
	if err := os.WriteFile(filepath.Join(worktree, ".kent", "config.toml"), []byte(nanoConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	app := newCoreTestAppWithOptions(t, resolved.Config, auth.EmptyState(), Options{MetadataStore: meta})
	store := createCoreSettingsSession(t, app, resolved.Config, binding.ProjectID)
	id := store.Identity().ID.String()
	subscription, err := app.ChatSettingsClient().SubscribeChatSettings(t.Context(), &chatsettingspb.SubscribeRequest{SessionId: id})
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	catalogChanges, err := app.WorkflowClient().SubscribeWorkflowProject(t.Context(), &workflowpb.ProjectSubscribeRequest{ProjectId: &binding.ProjectID})
	if err != nil {
		t.Fatal(err)
	}
	defer catalogChanges.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	wireSettings, err := protoapi.SessionSettingsToProto(resolved.Config.Settings)
	if err != nil {
		t.Fatal(err)
	}
	clientSettings, err := protoapi.SessionSettingsFromProto(wireSettings)
	if err != nil {
		t.Fatal(err)
	}
	err = app.SessionRuntimeClient().ActivateSessionRuntime(t.Context(), serverapi.SessionRuntimeActivateRequest{
		SessionID: requestfixture.SessionID(t, id),
		SessionRuntimePreparationOptions: serverapi.SessionRuntimePreparationOptions{
			ActiveSettings: clientSettings, Source: resolved.Config.Source,
			QuestionsEnabled: textutil.Value(true), AutoCompactionEnabled: textutil.Value(false),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := meta.UpdateSessionExecutionTarget(t.Context(), metadata.SessionExecutionTargetUpdate{
		SessionID: id, Workspace: &metadata.SessionExecutionTargetUpdateWorkspace{ID: binding.WorkspaceID},
		Worktree: &metadata.SessionExecutionTargetUpdateWorktree{ID: worktreeID},
	}); err != nil {
		t.Fatal(err)
	}
	target, err := meta.ResolveSessionExecutionTarget(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.bundles.Runtime.runtimeAuthority.SyncExecutionTarget(t.Context(), id, target, nil); err != nil {
		t.Fatal(err)
	}
	input, err := protoapi.UserTurnInputToProto(runtimeinput.Text("Fix invoice totals"))
	if err != nil {
		t.Fatal(err)
	}
	caller, endCaller := context.WithCancel(ctx)
	if _, err := app.RuntimeControlClient().SubmitUserTurn(caller, &runtimepb.SubmitUserTurnRequest{
		QueueItemId: runtimeids.NewQueueItemID().String(), SessionId: id, Input: input,
	}); err != nil {
		t.Fatal(err)
	}
	if optOut {
		defer endCaller()
		close(releaseNano)
		if first := <-requests; first != "gpt-6-sol" {
			t.Fatalf("main model = %s", first)
		}
		select {
		case model := <-requests:
			t.Fatalf("opted-out naming requested model %s", model)
		case <-time.After(250 * time.Millisecond):
		}
		record, err := meta.ResolvePersistedSession(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if record.Meta.Name != nil {
			t.Fatal("opted-out Session was named")
		}
		return
	}
	select {
	case <-nanoStarted:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	endCaller()
	close(releaseNano)
	record, err := meta.ResolvePersistedSession(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if preview := record.Meta.FirstPromptPreview; preview != "Fix invoice totals" {
		t.Fatalf("initial user preview = %q", preview)
	}
	for {
		snapshot, err := subscription.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.SessionName != nil {
			if *snapshot.SessionName != "Fix invoice totals" {
				t.Fatalf("saved title = %q", *snapshot.SessionName)
			}
			break
		}
	}
	change, err := catalogChanges.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if change.Resource != workflowpb.ProjectEventResource_WORKFLOW_PROJECT_EVENT_RESOURCE_SESSION_CATALOG {
		t.Fatalf("saved name did not invalidate Session catalog: %v", change.Resource)
	}
}
