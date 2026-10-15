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
	"core/server/auth"
	serverbootstrap "core/server/bootstrap"
	"core/shared/config"
	"core/shared/protoapi"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	workflowpb "core/shared/protoapi/gen/kent/api/workflow_definition"
	"core/shared/runtimeinput"
	"core/shared/serverapi"
	"core/shared/textutil"
)

func TestNamingContinuesAfterTurnAndCallerEnd(t *testing.T) {
	nanoStarted := make(chan struct{})
	releaseNano := make(chan struct{})
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
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
	body := fmt.Sprintf("model = \"gpt-6-sol\"\nconnection = \"local\"\n[connections.local]\nprotocol = \"responses\"\nendpoint = %q\n[nano]\nconnection = \"local\"\nmodel = \"gpt-6-luna\"\n", endpoint.URL)
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
	_, err = app.SessionRuntimeClient().ActivateSessionRuntime(t.Context(), serverapi.SessionRuntimeActivateRequest{
		SessionID: requestfixture.SessionID(t, id), OwnerID: "naming-test", ActiveSettings: clientSettings, Source: resolved.Config.Source,
		QuestionsEnabled: textutil.Value(true), AutoCompactionEnabled: textutil.Value(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	input, err := protoapi.UserTurnInputToProto(runtimeinput.Text("Fix invoice totals"))
	if err != nil {
		t.Fatal(err)
	}
	caller, endCaller := context.WithCancel(ctx)
	if _, err := app.RuntimeControlClient().SubmitUserTurn(caller, &runtimepb.SubmitUserTurnRequest{
		SessionId: id, Input: input,
	}); err != nil {
		t.Fatal(err)
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
