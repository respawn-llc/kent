package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"builder/prompts"
	"builder/server/metadata"
	"builder/server/session"
	"builder/shared/client"
	"builder/shared/config"
	"builder/shared/serverapi"
	"builder/shared/toolspec"
)

type recordingGoalRemote struct {
	showReq          []serverapi.RuntimeGoalShowRequest
	setReq           []serverapi.RuntimeGoalSetRequest
	completeReq      []serverapi.RuntimeGoalStatusRequest
	goal             *serverapi.RuntimeGoal
	setErr           error
	showDeadline     time.Time
	completeDeadline time.Time
}

func (r *recordingGoalRemote) Close() error { return nil }

func (r *recordingGoalRemote) ShowGoal(ctx context.Context, req serverapi.RuntimeGoalShowRequest) (serverapi.RuntimeGoalShowResponse, error) {
	r.showReq = append(r.showReq, req)
	if deadline, ok := ctx.Deadline(); ok {
		r.showDeadline = deadline
	}
	return serverapi.RuntimeGoalShowResponse{Goal: r.goal}, nil
}

func (r *recordingGoalRemote) SetGoal(_ context.Context, req serverapi.RuntimeGoalSetRequest) (serverapi.RuntimeGoalShowResponse, error) {
	r.setReq = append(r.setReq, req)
	if r.setErr != nil {
		return serverapi.RuntimeGoalShowResponse{}, r.setErr
	}
	return serverapi.RuntimeGoalShowResponse{Goal: r.goal}, nil
}

func (r *recordingGoalRemote) PauseGoal(context.Context, serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
	return serverapi.RuntimeGoalShowResponse{}, nil
}

func (r *recordingGoalRemote) ResumeGoal(context.Context, serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
	return serverapi.RuntimeGoalShowResponse{}, nil
}

func (r *recordingGoalRemote) CompleteGoal(ctx context.Context, req serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
	r.completeReq = append(r.completeReq, req)
	if deadline, ok := ctx.Deadline(); ok {
		r.completeDeadline = deadline
	}
	return serverapi.RuntimeGoalShowResponse{Goal: r.goal}, nil
}

func (r *recordingGoalRemote) ClearGoal(context.Context, serverapi.RuntimeGoalClearRequest) (serverapi.RuntimeGoalShowResponse, error) {
	return serverapi.RuntimeGoalShowResponse{}, nil
}

func TestGoalShowUsesBuilderSessionID(t *testing.T) {
	t.Setenv("BUILDER_SESSION_ID", "session-1")
	remote := &recordingGoalRemote{goal: &serverapi.RuntimeGoal{ID: "goal-1", Objective: "ship goal mode", Status: "active"}}
	restore := replaceGoalCommandRemoteOpener(t, remote)
	defer restore()

	stdout := new(strings.Builder)
	stderr := new(strings.Builder)
	if code := goalSubcommand([]string{"show"}, stdout, stderr); code != 0 {
		t.Fatalf("goal show exit = %d stderr=%q", code, stderr.String())
	}
	if len(remote.showReq) != 1 || remote.showReq[0].SessionID != "session-1" {
		t.Fatalf("show requests = %+v", remote.showReq)
	}
	if !strings.Contains(stdout.String(), "ship goal mode") || !strings.Contains(stdout.String(), "active") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if strings.Contains(stdout.String(), "goal-1") || strings.Contains(stdout.String(), "ID:") {
		t.Fatalf("plain goal show leaked goal id: %q", stdout.String())
	}
}

func TestGoalAgentEnvAllowsSetWithAgentActor(t *testing.T) {
	t.Setenv("BUILDER_SESSION_ID", "session-1")
	remote := &recordingGoalRemote{goal: &serverapi.RuntimeGoal{ID: "goal-1", Objective: "new goal", Status: "active"}}
	restore := replaceGoalCommandRemoteOpener(t, remote)
	defer restore()

	stdout := new(strings.Builder)
	stderr := new(strings.Builder)
	if code := goalSubcommand([]string{"set", "new goal"}, stdout, stderr); code != 0 {
		t.Fatalf("goal set exit = %d stderr=%q", code, stderr.String())
	}
	if len(remote.setReq) != 1 {
		t.Fatalf("set requests = %+v", remote.setReq)
	}
	if remote.setReq[0].SessionID != "session-1" || remote.setReq[0].Actor != "agent" || remote.setReq[0].Objective != "new goal" {
		t.Fatalf("set request = %+v", remote.setReq[0])
	}
	if !strings.Contains(stdout.String(), "Goal: new goal") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestGoalAgentEnvSetOverwritePrintsDeniedPrompt(t *testing.T) {
	t.Setenv("BUILDER_SESSION_ID", "session-1")
	existing := &serverapi.RuntimeGoal{ID: "goal-1", Objective: "existing goal", Status: "active"}
	remote := &recordingGoalRemote{
		goal:   existing,
		setErr: errors.New(strings.TrimSpace(prompts.GoalAgentCommandDeniedPrompt)),
	}
	restore := replaceGoalCommandRemoteOpener(t, remote)
	defer restore()

	stdout := new(strings.Builder)
	stderr := new(strings.Builder)
	if code := goalSubcommand([]string{"set", "replacement goal"}, stdout, stderr); code == 0 {
		t.Fatalf("goal set overwrite exit = 0")
	}
	if !strings.Contains(stderr.String(), strings.TrimSpace(prompts.GoalAgentCommandDeniedPrompt)) {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if len(remote.setReq) != 1 {
		t.Fatalf("set requests = %+v", remote.setReq)
	}
	if remote.goal != existing || remote.goal.Objective != "existing goal" {
		t.Fatalf("remote goal mutated = %+v", remote.goal)
	}
}

func TestGoalAgentEnvDeniesNonSetMutationWithoutDialing(t *testing.T) {
	t.Setenv("BUILDER_SESSION_ID", "session-1")
	remote := &recordingGoalRemote{}
	restore := replaceGoalCommandRemoteOpener(t, remote)
	defer restore()

	stderr := new(strings.Builder)
	if code := goalSubcommand([]string{"pause"}, new(strings.Builder), stderr); code == 0 {
		t.Fatalf("goal pause exit = 0")
	}
	if !strings.Contains(stderr.String(), prompts.GoalAgentCommandDeniedPrompt) {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if len(remote.showReq) != 0 || len(remote.completeReq) != 0 || len(remote.setReq) != 0 {
		t.Fatalf("remote was called: %+v", remote)
	}
}

func TestGoalSetRejectsEmptyObjectiveBeforeDialing(t *testing.T) {
	t.Setenv("BUILDER_SESSION_ID", "")
	remote := &recordingGoalRemote{}
	restore := replaceGoalCommandRemoteOpener(t, remote)
	defer restore()

	stderr := new(strings.Builder)
	if code := goalSubcommand([]string{"set", "--session", "session-1", "   "}, new(strings.Builder), stderr); code != 2 {
		t.Fatalf("goal set empty exit = %d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "goal set requires an objective") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if len(remote.setReq) != 0 {
		t.Fatalf("set called for empty objective: %+v", remote.setReq)
	}
}

func TestGoalAgentCompleteRequiresConfirmTripwire(t *testing.T) {
	t.Setenv("BUILDER_SESSION_ID", "session-1")
	remote := &recordingGoalRemote{goal: &serverapi.RuntimeGoal{ID: "goal-1", Objective: "ship goal mode", Status: "active"}}
	restore := replaceGoalCommandRemoteOpener(t, remote)
	defer restore()

	stderr := new(strings.Builder)
	if code := goalSubcommand([]string{"complete"}, new(strings.Builder), stderr); code == 0 {
		t.Fatalf("goal complete without confirm exit = 0")
	}
	if !strings.Contains(stderr.String(), prompts.GoalCompleteConfirmRequiredPrompt) {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if len(remote.completeReq) != 0 {
		t.Fatalf("complete called before confirm: %+v", remote.completeReq)
	}

	stdout := new(strings.Builder)
	stderr.Reset()
	if code := goalSubcommand([]string{"complete", "--confirm"}, stdout, stderr); code != 0 {
		t.Fatalf("goal complete --confirm exit = %d stderr=%q", code, stderr.String())
	}
	if len(remote.completeReq) != 1 {
		t.Fatalf("complete requests = %+v", remote.completeReq)
	}
	if remote.completeReq[0].SessionID != "session-1" || remote.completeReq[0].Actor != "agent" {
		t.Fatalf("complete req = %+v", remote.completeReq[0])
	}
}

func TestGoalCompleteAlreadyCompletePrintsAlreadyCompletePrompt(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
	}{
		{name: "without confirm", args: []string{"complete"}},
		{name: "with confirm", args: []string{"complete", "--confirm"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("BUILDER_SESSION_ID", "session-1")
			remote := &recordingGoalRemote{goal: &serverapi.RuntimeGoal{ID: "goal-1", Objective: "ship goal mode", Status: "complete"}}
			restore := replaceGoalCommandRemoteOpener(t, remote)
			defer restore()

			stdout := new(strings.Builder)
			stderr := new(strings.Builder)
			if code := goalSubcommand(tt.args, stdout, stderr); code != 0 {
				t.Fatalf("goal complete already-complete exit = %d stderr=%q", code, stderr.String())
			}
			if got, want := strings.TrimSpace(stdout.String()), prompts.RenderGoalAlreadyCompletePrompt("ship goal mode"); got != want {
				t.Fatalf("stdout = %q, want %q", got, want)
			}
			if stderr.String() != "" {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
			if len(remote.completeReq) != 0 {
				t.Fatalf("complete called for already-complete goal: %+v", remote.completeReq)
			}
			if len(remote.showReq) != 1 || remote.showReq[0].SessionID != "session-1" {
				t.Fatalf("show requests = %+v", remote.showReq)
			}
		})
	}
}

func TestGoalCompleteUsesFreshTimeoutForCompletionRPC(t *testing.T) {
	t.Setenv("BUILDER_SESSION_ID", "session-1")
	remote := &recordingGoalRemote{goal: &serverapi.RuntimeGoal{ID: "goal-1", Objective: "ship goal mode", Status: "active"}}
	restore := replaceGoalCommandRemoteOpener(t, remote)
	defer restore()

	stdout := new(strings.Builder)
	stderr := new(strings.Builder)
	if code := goalSubcommand([]string{"complete", "--confirm"}, stdout, stderr); code != 0 {
		t.Fatalf("goal complete --confirm exit = %d stderr=%q", code, stderr.String())
	}
	if remote.showDeadline.IsZero() || remote.completeDeadline.IsZero() {
		t.Fatalf("deadlines missing: show=%v complete=%v", remote.showDeadline, remote.completeDeadline)
	}
	if !remote.completeDeadline.After(remote.showDeadline) {
		t.Fatalf("complete deadline = %v, want fresh deadline after show deadline %v", remote.completeDeadline, remote.showDeadline)
	}
}

func TestGoalCompleteHelpDoesNotExposeConfirmTripwire(t *testing.T) {
	stderr := new(strings.Builder)
	if code := goalSubcommand([]string{"complete", "--help"}, new(strings.Builder), stderr); code != 0 {
		t.Fatalf("goal complete --help exit = %d", code)
	}
	if strings.Contains(stderr.String(), "--confirm") {
		t.Fatalf("goal complete help leaked hidden confirm flag: %q", stderr.String())
	}
}

func TestGoalCommandSubprocessTargetsLiveSessionFromUnboundWorktree(t *testing.T) {
	builderPath := filepath.Join(t.TempDir(), "builder")
	buildCmd := exec.Command("go", "build", "-o", builderPath, ".")
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("build subprocess builder: %v\n%s", err, output)
	}

	home := t.TempDir()
	workspace := t.TempDir()
	unboundWorktree := t.TempDir()
	t.Setenv("HOME", home)
	configureBindingCommandTestServerPort(t)

	cfg, err := config.Load(workspace, config.LoadOptions{})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	metadataStore, err := metadata.Open(cfg.PersistenceRoot)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	defer func() { _ = metadataStore.Close() }()
	binding, err := metadataStore.RegisterWorkspaceBinding(context.Background(), cfg.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	store, err := session.Create(
		config.ProjectSessionsRoot(cfg, binding.ProjectID),
		filepath.Base(cfg.WorkspaceRoot),
		cfg.WorkspaceRoot,
		metadataStore.AuthoritativeSessionStoreOptions()...,
	)
	if err != nil {
		t.Fatalf("session.Create: %v", err)
	}
	if _, err := store.SetGoal("exercise live goal CLI", session.GoalActorUser); err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	if err := store.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable: %v", err)
	}
	record, err := metadataStore.ResolvePersistedSession(context.Background(), store.Meta().SessionID)
	if err != nil {
		t.Fatalf("ResolvePersistedSession: %v", err)
	}
	if record.Meta == nil || record.Meta.Goal == nil {
		t.Fatalf("persisted goal metadata missing: %+v", record.Meta)
	}

	cleanup := startBindingCommandServer(t, unboundWorktree)
	defer cleanup()
	remote, err := client.DialConfiguredRemoteForProjectWorkspace(context.Background(), cfg, binding.ProjectID, cfg.WorkspaceRoot)
	if err != nil {
		t.Fatalf("DialConfiguredRemoteForProjectWorkspace: %v", err)
	}
	defer func() { _ = remote.Close() }()
	settings := cfg.Settings
	settings.Model = "gpt-5"
	settings.ProviderOverride = "openai"
	activateResp, err := remote.ActivateSessionRuntime(context.Background(), serverapi.SessionRuntimeActivateRequest{
		ClientRequestID: "activate-goal-cli-e2e",
		SessionID:       store.Meta().SessionID,
		ActiveSettings:  settings,
		EnabledToolIDs:  toolIDsAsStrings(config.EnabledToolIDs(settings)),
		Source:          cfg.Source,
	})
	if err != nil {
		t.Fatalf("ActivateSessionRuntime: %v", err)
	}
	defer func() {
		_, _ = remote.ReleaseSessionRuntime(context.Background(), serverapi.SessionRuntimeReleaseRequest{
			ClientRequestID: "release-goal-cli-e2e",
			SessionID:       store.Meta().SessionID,
			LeaseID:         activateResp.LeaseID,
		})
	}()

	t.Setenv("BUILDER_SESSION_ID", store.Meta().SessionID)
	showOutput, showErr := runGoalCommandSubprocess(t, builderPath, unboundWorktree, store.Meta().SessionID, "show", "--json")
	if showErr != "" {
		t.Fatalf("goal show stderr = %q", showErr)
	}
	var show serverapi.RuntimeGoalShowResponse
	if err := json.Unmarshal([]byte(showOutput), &show); err != nil {
		t.Fatalf("decode show json: %v output=%q", err, showOutput)
	}
	if show.Goal == nil || show.Goal.Status != "active" || show.Goal.Objective != "exercise live goal CLI" {
		t.Fatalf("show goal = %+v", show.Goal)
	}

	completeOutput, completeErr := runGoalCommandSubprocess(t, builderPath, unboundWorktree, store.Meta().SessionID, "complete", "--confirm")
	if completeErr != "" {
		t.Fatalf("goal complete stderr = %q", completeErr)
	}
	if !strings.Contains(completeOutput, "Status: complete") {
		t.Fatalf("complete stdout = %q", completeOutput)
	}
}

func TestGoalCommandSubprocessSetPersistsWhilePrimaryRunActive(t *testing.T) {
	builderPath := filepath.Join(t.TempDir(), "builder")
	buildCmd := exec.Command("go", "build", "-o", builderPath, ".")
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("build subprocess builder: %v\n%s", err, output)
	}

	home := t.TempDir()
	workspace := t.TempDir()
	unboundWorktree := t.TempDir()
	t.Setenv("HOME", home)
	configureBindingCommandTestServerPort(t)

	cfg, err := config.Load(workspace, config.LoadOptions{})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	metadataStore, err := metadata.Open(cfg.PersistenceRoot)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	defer func() { _ = metadataStore.Close() }()
	binding, err := metadataStore.RegisterWorkspaceBinding(context.Background(), cfg.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	store, err := session.Create(
		config.ProjectSessionsRoot(cfg, binding.ProjectID),
		filepath.Base(cfg.WorkspaceRoot),
		cfg.WorkspaceRoot,
		metadataStore.AuthoritativeSessionStoreOptions()...,
	)
	if err != nil {
		t.Fatalf("session.Create: %v", err)
	}
	if err := store.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable: %v", err)
	}

	modelRequestStarted := make(chan struct{}, 1)
	releaseModelRequest := make(chan struct{})
	var releaseModelOnce sync.Once
	releaseModel := func() {
		releaseModelOnce.Do(func() {
			close(releaseModelRequest)
		})
	}
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case modelRequestStarted <- struct{}{}:
		default:
		}
		select {
		case <-releaseModelRequest:
			http.Error(w, "released", http.StatusInternalServerError)
		case <-r.Context().Done():
		}
	}))
	defer modelServer.Close()
	defer releaseModel()

	cleanup := startBindingCommandServer(t, unboundWorktree)
	defer cleanup()
	remote, err := client.DialConfiguredRemoteForProjectWorkspace(context.Background(), cfg, binding.ProjectID, cfg.WorkspaceRoot)
	if err != nil {
		t.Fatalf("DialConfiguredRemoteForProjectWorkspace: %v", err)
	}
	defer func() { _ = remote.Close() }()
	settings := cfg.Settings
	settings.Model = "gpt-5"
	settings.ProviderOverride = "openai"
	settings.OpenAIBaseURL = modelServer.URL + "/v1"
	settings.Timeouts.ModelRequestSeconds = 30
	activateResp, err := remote.ActivateSessionRuntime(context.Background(), serverapi.SessionRuntimeActivateRequest{
		ClientRequestID: "activate-goal-cli-busy-e2e",
		SessionID:       store.Meta().SessionID,
		ActiveSettings:  settings,
		EnabledToolIDs:  toolIDsAsStrings(config.EnabledToolIDs(settings)),
		Source:          cfg.Source,
	})
	if err != nil {
		t.Fatalf("ActivateSessionRuntime: %v", err)
	}
	defer func() {
		_, _ = remote.ReleaseSessionRuntime(context.Background(), serverapi.SessionRuntimeReleaseRequest{
			ClientRequestID: "release-goal-cli-busy-e2e",
			SessionID:       store.Meta().SessionID,
			LeaseID:         activateResp.LeaseID,
		})
	}()

	submitCtx, cancelSubmit := context.WithCancel(context.Background())
	defer cancelSubmit()
	submitDone := make(chan error, 1)
	go func() {
		_, err := remote.SubmitUserMessage(submitCtx, serverapi.RuntimeSubmitUserMessageRequest{
			ClientRequestID:   "submit-hanging-run",
			SessionID:         store.Meta().SessionID,
			ControllerLeaseID: activateResp.LeaseID,
			Text:              "hold the primary run",
		})
		submitDone <- err
	}()
	select {
	case <-modelRequestStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for active model request")
	}

	stdout, stderr, err := runGoalCommandSubprocessRaw(t, builderPath, unboundWorktree, "", "set", "--session", store.Meta().SessionID, "new goal while busy")
	if err != nil {
		t.Fatalf("goal set failed during active primary run: %v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("goal set stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "Goal: new goal while busy") || !strings.Contains(stdout, "Status: active") {
		t.Fatalf("goal set stdout = %q", stdout)
	}
	record, err := metadataStore.ResolvePersistedSession(context.Background(), store.Meta().SessionID)
	if err != nil {
		t.Fatalf("ResolvePersistedSession: %v", err)
	}
	if record.Meta == nil {
		t.Fatal("persisted session metadata missing")
	}
	if goal := record.Meta.Goal; goal == nil || goal.Objective != "new goal while busy" || goal.Status != session.GoalStatusActive {
		t.Fatalf("persisted goal = %+v", goal)
	}
	events, err := store.ReadEvents()
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	foundGoalSet := false
	for _, event := range events {
		if event.Kind == "goal_set" {
			foundGoalSet = true
		}
	}
	if !foundGoalSet {
		t.Fatalf("goal_set event not persisted after busy subprocess set")
	}

	cancelSubmit()
	releaseModel()
	select {
	case <-submitDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for active model request to stop")
	}
}

func runGoalCommandSubprocess(t *testing.T, builderPath string, workdir string, sessionID string, args ...string) (stdout string, stderr string) {
	t.Helper()
	stdout, stderr, err := runGoalCommandSubprocessRaw(t, builderPath, workdir, sessionID, args...)
	if err != nil {
		t.Fatalf("%s goal %s failed: %v stdout=%q stderr=%q", builderPath, strings.Join(args, " "), err, stdout, stderr)
	}
	return stdout, stderr
}

func runGoalCommandSubprocessRaw(t *testing.T, builderPath string, workdir string, sessionID string, args ...string) (stdout string, stderr string, err error) {
	t.Helper()
	cmd := exec.Command(builderPath, append([]string{"goal"}, args...)...)
	cmd.Dir = workdir
	cmd.Env = goalCommandSubprocessEnv(sessionID)
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err = cmd.Run()
	return out.String(), errOut.String(), err
}

func goalCommandSubprocessEnv(sessionID string) []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, item := range os.Environ() {
		if strings.HasPrefix(item, "BUILDER_SESSION_ID=") {
			continue
		}
		env = append(env, item)
	}
	if strings.TrimSpace(sessionID) != "" {
		env = append(env, "BUILDER_SESSION_ID="+sessionID)
	}
	return env
}

func toolIDsAsStrings(ids []toolspec.ID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, string(id))
	}
	return out
}

func replaceGoalCommandRemoteOpener(t *testing.T, remote *recordingGoalRemote) func() {
	t.Helper()
	previous := goalCommandRemoteOpener
	goalCommandRemoteOpener = func(context.Context) (goalCommandRemote, error) {
		return remote, nil
	}
	return func() { goalCommandRemoteOpener = previous }
}
