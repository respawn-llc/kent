package core

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"core/server/auth"
	"core/shared/apicontract"
	"core/shared/config"
	"core/shared/protoapi"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/serverapi"
	"core/shared/textutil"
)

func TestConnectionRotationSharedAcrossConcurrentProjectRoleLaunches(t *testing.T) {
	root, workspaceA, workspaceB := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(`
connection = ["a", "b"]
model = "operator-model"
[connections.a]
protocol = "chatgpt-codex"
[connections.b]
protocol = "chatgpt-codex"
[reviewer]
frequency = "off"
[subagents.worker]
connection = ["b", "a"]
model = "operator-model"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(workspaceA, workspaceA, config.LoadOptions{ConfigRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	app := newCoreTestApp(t, cfg, auth.EmptyState())
	clients := make([]apicontract.SessionLaunchService, 0, 2)
	for _, workspace := range []string{workspaceA, workspaceB} {
		binding, err := app.MetadataStore().RegisterWorkspaceBinding(t.Context(), workspace)
		if err != nil {
			t.Fatal(err)
		}
		client, err := app.SessionLaunchClientForProjectWorkspace(t.Context(), binding.ProjectID, workspace)
		if err != nil {
			t.Fatal(err)
		}
		clients = append(clients, client)
	}
	intent, err := protoapi.SessionLaunchIntentToProto(serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()))
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan string, 10)
	create := func(client apicontract.SessionLaunchService, role *string) {
		result, err := client.PlanSession(t.Context(), &sessionlaunchpb.SessionPlanRequest{
			Mode:   sessionlaunchpb.SessionLaunchMode_SESSION_LAUNCH_MODE_INTERACTIVE,
			Intent: intent, Overrides: &sessionlaunchpb.RunPromptOverrides{AgentRole: role},
		})
		if err != nil {
			t.Error(err)
			return
		}
		if result.Plan.ActiveSettings.Model != "operator-model" {
			t.Errorf("authored model changed: %v", result.Plan.ActiveSettings.Model)
		}
		results <- result.Plan.ActiveSettings.GetConnection()
	}
	create(clients[0], nil)
	var group sync.WaitGroup
	for range 3 {
		group.Go(func() { create(clients[0], nil) })
		group.Go(func() { create(clients[0], textutil.Value("worker")) })
		group.Go(func() { create(clients[1], textutil.Value("worker")) })
	}
	group.Wait()
	close(results)
	counts := map[string]int{}
	for id := range results {
		counts[id]++
	}
	if counts["a"] != 5 || counts["b"] != 5 {
		t.Fatalf("shared project/role assignments = %v, want five each", counts)
	}
}
