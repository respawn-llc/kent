package runtimewire

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"core/server/auth"
	"core/server/llm"
	"core/server/runtime"
	"core/server/session"
	"core/server/tools"
	askquestion "core/server/tools"
	patchtool "core/server/tools/patch"
	shelltool "core/server/tools/shell"
	"core/shared/config"
	"core/shared/toolspec"
)

func TestBuildToolRegistryAllowsHostedWebSearchWithoutLocalRuntimeBuilder(t *testing.T) {
	workspace := t.TempDir()

	registry, _ := newRuntimeWireToolRegistry(t, workspace, toolspec.ToolExecCommand, toolspec.ToolWebSearch)

	defs := registry.Definitions()
	if len(defs) != 1 {
		t.Fatalf("expected only local runtime tools in registry, got %d", len(defs))
	}
	if defs[0].ID != toolspec.ToolExecCommand {
		t.Fatalf("expected exec_command runtime tool definition, got %+v", defs[0])
	}
}

func TestBuildToolRegistryViewImageApprovedOutsidePathIsLogged(t *testing.T) {
	workspace := t.TempDir()
	outsideFile := filepath.Join(outsideNonTempDir(t), "doc.pdf")
	pdfBytes := []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\ntrailer\n<<>>\n%%EOF\n")
	if err := os.WriteFile(outsideFile, pdfBytes, 0o644); err != nil {
		t.Fatalf("write outside pdf: %v", err)
	}

	logger := &testLogger{}
	registry, broker := newRuntimeWireLoggedToolRegistry(
		t,
		workspace,
		logger,
		toolspec.ToolViewImage,
	)
	broker.SetAskHandler(func(req askquestion.AskQuestionRequest) (askquestion.AskQuestionResponse, error) {
		if !strings.Contains(req.Question, "Allow reading") {
			t.Fatalf("expected read-focused approval question, got %q", req.Question)
		}
		return askquestion.AskQuestionResponse{Approval: &askquestion.AskQuestionApprovalPayload{Decision: askquestion.AskQuestionApprovalDecisionAllowOnce}}, nil
	})

	viewImageHandler, ok := registry.Get(toolspec.ToolViewImage)
	if !ok {
		t.Fatal("expected view_image handler")
	}
	input, err := json.Marshal(map[string]any{"path": outsideFile})
	if err != nil {
		t.Fatalf("marshal view_image input: %v", err)
	}
	result, err := viewImageHandler.Call(context.Background(), tools.Call{ID: "call-1", Name: toolspec.ToolViewImage, Input: input})
	if err != nil {
		t.Fatalf("view_image call: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success result, got %s", string(result.Output))
	}
	if !strings.Contains(logger.String(), "tool.view_image.outside_workspace.approved") {
		t.Fatalf("expected outside-workspace approval audit line, got %q", logger.String())
	}
	if !strings.Contains(logger.String(), "reason=allow_once") {
		t.Fatalf("expected allow_once reason in audit line, got %q", logger.String())
	}
}

func TestBuildToolRegistryMissingWorkspaceRootSuggestsRebind(t *testing.T) {
	tests := []struct {
		name string
		tool toolspec.ID
	}{
		{name: "patch", tool: toolspec.ToolPatch},
		{name: "view_image", tool: toolspec.ToolViewImage},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			missingWorkspace := filepath.Join(t.TempDir(), "workspace-removed")
			newWorkspace := t.TempDir()
			t.Chdir(newWorkspace)
			sessionID := "session-1"

			_, _, _, err := BuildToolRegistry(
				missingWorkspace,
				sessionID,
				[]toolspec.ID{tt.tool},
				15*time.Second,
				16_000,
				false,
				true,
				nil,
				nil,
				nil,
				nil,
			)
			if err == nil {
				t.Fatal("expected build tool registry error for missing workspace root")
			}
			if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("expected os.ErrNotExist, got %v", err)
			}
			var retarget sessionWorkspaceRetargetError
			if !errors.As(err, &retarget) {
				t.Fatalf("expected sessionWorkspaceRetargetError, got %v", err)
			}
			if retarget.sessionID != sessionID {
				t.Fatalf("retarget sessionID = %q, want %q", retarget.sessionID, sessionID)
			}
			if retarget.workspaceRoot != missingWorkspace {
				t.Fatalf("retarget workspaceRoot = %q, want %q", retarget.workspaceRoot, missingWorkspace)
			}
			if retarget.newRoot != newWorkspace {
				t.Fatalf("retarget newRoot = %q, want %q", retarget.newRoot, newWorkspace)
			}
		})
	}
}

func TestLocalToolRegistryBindingRebindUpdatesExecCommandRoot(t *testing.T) {
	rootA := filepath.Join(t.TempDir(), "workspace-a")
	rootB := filepath.Join(t.TempDir(), "workspace-b")
	if err := os.MkdirAll(rootA, 0o755); err != nil {
		t.Fatalf("mkdir rootA: %v", err)
	}
	if err := os.MkdirAll(rootB, 0o755); err != nil {
		t.Fatalf("mkdir rootB: %v", err)
	}
	binding := newRuntimeWireBinding(t, rootA, toolspec.ToolExecCommand)
	if got := shellPwdOutput(t, binding.Registry()); got != canonicalPathForTest(t, rootA) {
		t.Fatalf("pwd before rebind = %q, want %q", got, canonicalPathForTest(t, rootA))
	}
	if err := binding.Rebind(rootB); err != nil {
		t.Fatalf("rebind: %v", err)
	}
	if got := shellPwdOutput(t, binding.Registry()); got != canonicalPathForTest(t, rootB) {
		t.Fatalf("pwd after rebind = %q, want %q", got, canonicalPathForTest(t, rootB))
	}
}

func TestNewLocalToolRegistryBindingRejectsEmptyWorkspaceRoot(t *testing.T) {
	_, _, _, err := NewLocalToolRegistryBinding(
		"   ",
		"",
		[]toolspec.ID{toolspec.ToolExecCommand},
		15*time.Second,
		16_000,
		false,
		true,
		nil,
		nil,
		nil,
		nil,
	)
	if !errors.Is(err, errWorkspaceRootRequired) {
		t.Fatalf("new local tool registry binding error = %v, want errWorkspaceRootRequired", err)
	}
}

func TestLocalToolRegistryBindingRebindRejectsEmptyWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	binding := newRuntimeWireBinding(t, root, toolspec.ToolExecCommand)
	if err := binding.Rebind("   "); !errors.Is(err, errWorkspaceRootRequired) {
		t.Fatalf("rebind error = %v, want errWorkspaceRootRequired", err)
	}
}

func TestBackgroundEventRouterSkipsDeveloperNoticeForOrphanedShells(t *testing.T) {
	root := t.TempDir()
	storeA := newRuntimeWireSession(t, root, "ws-a")
	storeB := newRuntimeWireSession(t, root, "ws-b")
	clientA := &busyToggleFakeClient{responses: []llm.Response{{Assistant: llm.Message{Role: llm.RoleAssistant, Content: "a", Phase: llm.MessagePhaseFinal}, Usage: llm.Usage{WindowTokens: 200_000}}}}
	clientB := &busyToggleFakeClient{responses: []llm.Response{{Assistant: llm.Message{Role: llm.RoleAssistant, Content: "b", Phase: llm.MessagePhaseFinal}, Usage: llm.Usage{WindowTokens: 200_000}}}}
	var mu sync.Mutex
	backgroundUpdates := 0
	_ = newRuntimeWireEngine(t, storeA, clientA)
	engB := newRuntimeWireEngine(t, storeB, clientB, runtime.Config{Model: "gpt-5", OnEvent: func(evt runtime.Event) {
		if evt.Kind == runtime.EventBackgroundUpdated {
			mu.Lock()
			backgroundUpdates++
			mu.Unlock()
		}
	}})

	router := &BackgroundEventRouter{}
	router.SetActiveSession(storeB.Meta().SessionID, engB)
	router.Handle(shelltool.Event{Snapshot: shelltool.Snapshot{ID: "1000", OwnerSessionID: storeA.Meta().SessionID, State: "completed", Command: "kent run", Workdir: root, LogPath: filepath.Join(root, "1000.log")}, Type: shelltool.EventCompleted, Preview: "done"})

	time.Sleep(150 * time.Millisecond)
	if got := clientB.CallCount(); got != 0 {
		t.Fatalf("expected orphaned completion to skip model notice for active session, got %d client calls", got)
	}
	mu.Lock()
	updates := backgroundUpdates
	mu.Unlock()
	if updates != 0 {
		t.Fatalf("expected orphaned completion to stay isolated from foreign active sessions, got %d background updates", updates)
	}
	if got := clientA.CallCount(); got != 0 {
		t.Fatalf("did not expect inactive owner engine to be called, got %d", got)
	}
}

func TestBackgroundEventRouterRoutesCompletionToMatchingActiveOwnerSession(t *testing.T) {
	root := t.TempDir()
	storeA := newRuntimeWireSession(t, root, "ws-a")
	storeB := newRuntimeWireSession(t, root, "ws-b")
	clientA := &busyToggleFakeClient{responses: []llm.Response{{Assistant: llm.Message{Role: llm.RoleAssistant, Content: "a", Phase: llm.MessagePhaseFinal}, Usage: llm.Usage{WindowTokens: 200_000}}}}
	clientB := &busyToggleFakeClient{responses: []llm.Response{{Assistant: llm.Message{Role: llm.RoleAssistant, Content: "b", Phase: llm.MessagePhaseFinal}, Usage: llm.Usage{WindowTokens: 200_000}}}}
	engA := newRuntimeWireEngine(t, storeA, clientA)
	engB := newRuntimeWireEngine(t, storeB, clientB)

	router := &BackgroundEventRouter{}
	router.SetActiveSession(storeA.Meta().SessionID, engA)
	router.SetActiveSession(storeB.Meta().SessionID, engB)
	router.Handle(shelltool.Event{Snapshot: shelltool.Snapshot{ID: "1002", OwnerSessionID: storeA.Meta().SessionID, State: "completed", Command: "kent run", Workdir: root, LogPath: filepath.Join(root, "1002.log")}, Type: shelltool.EventCompleted, Preview: "done"})

	deadline := time.Now().Add(2 * time.Second)
	for clientA.CallCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := clientA.CallCount(); got == 0 {
		t.Fatal("expected owner session completion to route to its active engine even when another session is also active")
	}
	if got := clientB.CallCount(); got != 0 {
		t.Fatalf("did not expect foreign active session to receive routed completion, got %d", got)
	}
}

func TestBackgroundEventRouterClearActiveSessionDropsOnlyThatOwner(t *testing.T) {
	root := t.TempDir()
	storeA := newRuntimeWireSession(t, root, "ws-a")
	storeB := newRuntimeWireSession(t, root, "ws-b")
	clientA := &busyToggleFakeClient{responses: []llm.Response{{Assistant: llm.Message{Role: llm.RoleAssistant, Content: "a", Phase: llm.MessagePhaseFinal}, Usage: llm.Usage{WindowTokens: 200_000}}}}
	clientB := &busyToggleFakeClient{responses: []llm.Response{{Assistant: llm.Message{Role: llm.RoleAssistant, Content: "b", Phase: llm.MessagePhaseFinal}, Usage: llm.Usage{WindowTokens: 200_000}}}}
	engA := newRuntimeWireEngine(t, storeA, clientA)
	engB := newRuntimeWireEngine(t, storeB, clientB)

	router := &BackgroundEventRouter{}
	router.SetActiveSession(storeA.Meta().SessionID, engA)
	router.SetActiveSession(storeB.Meta().SessionID, engB)
	router.ClearActiveSession(storeA.Meta().SessionID, engA)
	router.Handle(shelltool.Event{Snapshot: shelltool.Snapshot{ID: "1003", OwnerSessionID: storeA.Meta().SessionID, State: "completed", Command: "kent run", Workdir: root, LogPath: filepath.Join(root, "1003.log")}, Type: shelltool.EventCompleted, Preview: "done"})
	time.Sleep(150 * time.Millisecond)
	if got := clientA.CallCount(); got != 0 {
		t.Fatalf("expected cleared owner session to drop completions, got %d", got)
	}
	if got := clientB.CallCount(); got != 0 {
		t.Fatalf("did not expect foreign active session to receive cleared-owner completion, got %d", got)
	}

	router.Handle(shelltool.Event{Snapshot: shelltool.Snapshot{ID: "1004", OwnerSessionID: storeB.Meta().SessionID, State: "completed", Command: "kent run", Workdir: root, LogPath: filepath.Join(root, "1004.log")}, Type: shelltool.EventCompleted, Preview: "done"})
	deadline := time.Now().Add(2 * time.Second)
	for clientB.CallCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := clientB.CallCount(); got == 0 {
		t.Fatal("expected other active sessions to keep receiving their own completions after clearing a different owner")
	}
}

func TestBackgroundEventRouterStaleClearKeepsReplacementForSameSession(t *testing.T) {
	root := t.TempDir()
	store := newRuntimeWireSession(t, root, "ws")
	clientA := &busyToggleFakeClient{responses: []llm.Response{{Assistant: llm.Message{Role: llm.RoleAssistant, Content: "a", Phase: llm.MessagePhaseFinal}, Usage: llm.Usage{WindowTokens: 200_000}}}}
	clientB := &busyToggleFakeClient{responses: []llm.Response{{Assistant: llm.Message{Role: llm.RoleAssistant, Content: "b", Phase: llm.MessagePhaseFinal}, Usage: llm.Usage{WindowTokens: 200_000}}}}
	engA := newRuntimeWireEngine(t, store, clientA)
	engB := newRuntimeWireEngine(t, store, clientB)

	router := &BackgroundEventRouter{}
	router.SetActiveSession(store.Meta().SessionID, engA)
	router.SetActiveSession(store.Meta().SessionID, engB)
	router.ClearActiveSession(store.Meta().SessionID, engA)
	router.Handle(shelltool.Event{Snapshot: shelltool.Snapshot{ID: "1005", OwnerSessionID: store.Meta().SessionID, State: "completed", Command: "kent run", Workdir: root, LogPath: filepath.Join(root, "1005.log")}, Type: shelltool.EventCompleted, Preview: "done"})

	deadline := time.Now().Add(2 * time.Second)
	for clientB.CallCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := clientB.CallCount(); got == 0 {
		t.Fatal("expected replacement active session to receive completion after stale clear")
	}
	if got := clientA.CallCount(); got != 0 {
		t.Fatalf("did not expect stale active session to receive completion, got %d", got)
	}
}

func TestBackgroundEventRouterQueuesNoticeForActiveOwnerSession(t *testing.T) {
	root := t.TempDir()
	store := newRuntimeWireSession(t, root, "ws")
	client := &busyToggleFakeClient{responses: []llm.Response{{Assistant: llm.Message{Role: llm.RoleAssistant, Content: "notice handled", Phase: llm.MessagePhaseFinal}, Usage: llm.Usage{WindowTokens: 200_000}}}}
	eng := newRuntimeWireEngine(t, store, client)

	router := &BackgroundEventRouter{}
	router.SetActiveSession(store.Meta().SessionID, eng)
	router.Handle(shelltool.Event{Snapshot: shelltool.Snapshot{ID: "1001", OwnerSessionID: store.Meta().SessionID, State: "completed", Command: "kent run", Workdir: root, LogPath: filepath.Join(root, "1001.log")}, Type: shelltool.EventCompleted, Preview: "done"})

	deadline := time.Now().Add(2 * time.Second)
	for client.CallCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := client.CallCount(); got == 0 {
		t.Fatal("expected active owner completion to queue a model notice")
	}
}

func TestNewRuntimeWiringRejectsEmptyModelAfterBypassingConfigDefaults(t *testing.T) {
	root := t.TempDir()
	store, err := session.Create(root, "ws", root)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	_, err = NewRuntimeWiringWithBackground(
		store,
		config.Settings{
			Model:              "",
			ProviderOverride:   "openai",
			OpenAIBaseURL:      "http://example.test/v1",
			ModelContextWindow: 272_000,
			Timeouts: config.Timeouts{
				ModelRequestSeconds: 1,
			},
		},
		[]toolspec.ID{toolspec.ToolExecCommand},
		root,
		auth.NewManager(auth.NewMemoryStore(auth.EmptyState()), nil, nil),
		nil,
		nil,
		RuntimeWiringOptions{},
	)
	if !errors.Is(err, runtime.ErrModelRequired) {
		t.Fatalf("expected runtime.ErrModelRequired, got %v", err)
	}
}

func TestReviewerProviderSettingsFallbacks(t *testing.T) {
	resolved := config.ResolveReviewerProviderSettings(config.Settings{
		ProviderOverride: "openai",
		OpenAIBaseURL:    "http://127.0.0.1:8080/v1",
	})
	if resolved.ProviderOverride != "openai" || resolved.OpenAIBaseURL != "http://127.0.0.1:8080/v1" {
		t.Fatalf("expected main provider settings fallback, got %+v", resolved)
	}

	resolved = config.ResolveReviewerProviderSettings(config.Settings{
		OpenAIBaseURL: "http://127.0.0.1:8080/v1",
		Reviewer: config.ReviewerSettings{
			ProviderOverride: "openai",
		},
	})
	if resolved.ProviderOverride != "openai" || resolved.OpenAIBaseURL != "http://127.0.0.1:8080/v1" {
		t.Fatalf("expected explicit reviewer openai provider to inherit main base URL, got %+v", resolved)
	}

	resolved = config.ResolveReviewerProviderSettings(config.Settings{
		OpenAIBaseURL: "http://127.0.0.1:8080/v1",
		Reviewer: config.ReviewerSettings{
			ProviderOverride: "openai",
			OpenAIBaseURL:    "http://localhost:11434/v1",
		},
	})
	if resolved.ProviderOverride != "openai" || resolved.OpenAIBaseURL != "http://localhost:11434/v1" {
		t.Fatalf("expected explicit reviewer provider settings, got %+v", resolved)
	}
}

func TestReviewerModelCapabilitiesHonorExplicitFalseSources(t *testing.T) {
	locked := lockedModelCapabilitiesForConfig(
		"gpt-5",
		config.ModelCapabilitiesOverride{SupportsReasoningEffort: false},
		map[string]string{"reviewer.model_capabilities.supports_reasoning_effort": "file"},
		"reviewer.model_capabilities.supports_reasoning_effort",
		"reviewer.model_capabilities.supports_vision_inputs",
	)

	if locked.SupportsReasoningEffort {
		t.Fatalf("expected explicit reviewer reasoning false override to beat model contract, got %+v", locked)
	}
	if !locked.SupportsVisionInputs {
		t.Fatalf("expected default reviewer vision capability to come from model contract, got %+v", locked)
	}
}

func TestReviewerModelCapabilitiesHonorInheritedExplicitFalseSources(t *testing.T) {
	locked := lockedModelCapabilitiesForConfig(
		"gpt-5",
		config.ModelCapabilitiesOverride{SupportsReasoningEffort: false},
		map[string]string{"model_capabilities.supports_reasoning_effort": "file"},
		"reviewer.model_capabilities.supports_reasoning_effort",
		"reviewer.model_capabilities.supports_vision_inputs",
	)

	if locked.SupportsReasoningEffort {
		t.Fatalf("expected inherited explicit reviewer reasoning false override to beat model contract, got %+v", locked)
	}
	if !locked.SupportsVisionInputs {
		t.Fatalf("expected default reviewer vision capability to come from model contract, got %+v", locked)
	}
}

func TestRuntimeProviderClientUsesProviderCapabilitiesOverride(t *testing.T) {
	client, err := llm.NewProviderClient(llm.ProviderClientOptions{
		Model:         "local-reviewer",
		Provider:      llm.Provider("openai"),
		OpenAIBaseURL: "http://127.0.0.1:11434/v1",
		Auth:          nil,
		ProviderCapabilitiesOverride: &llm.ProviderCapabilities{
			ProviderID:             "local-reviewer",
			SupportsResponsesAPI:   true,
			SupportsPromptCacheKey: true,
		},
	})
	if err != nil {
		t.Fatalf("new runtime provider client: %v", err)
	}
	provider, ok := client.(llm.ProviderCapabilitiesClient)
	if !ok {
		t.Fatalf("expected provider capabilities client, got %T", client)
	}
	caps, err := provider.ProviderCapabilities(context.Background())
	if err != nil {
		t.Fatalf("resolve provider capabilities: %v", err)
	}
	if caps.ProviderID != "local-reviewer" || !caps.SupportsResponsesAPI || !caps.SupportsPromptCacheKey {
		t.Fatalf("unexpected reviewer provider capabilities: %+v", caps)
	}
}

func TestReviewerAuthNoneDoesNotSendGlobalAuthToLocalEndpoint(t *testing.T) {
	authHeaders := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		authHeaders <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"resp_local_reviewer",
			"object":"response",
			"output":[{
				"type":"message",
				"id":"msg_local_reviewer",
				"role":"assistant",
				"status":"completed",
				"content":[{"type":"output_text","text":"{\"suggestions\":[]}"}]
			}],
			"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}
		}`))
	}))
	defer server.Close()

	client, err := llm.NewProviderClient(llm.ProviderClientOptions{
		Model:               "local-reviewer",
		Provider:            llm.Provider("openai"),
		OpenAIBaseURL:       server.URL + "/v1",
		Auth:                nil,
		HTTPClient:          server.Client(),
		ModelVerbosity:      string(config.ModelVerbosityLow),
		ContextWindowTokens: 64000,
	})
	if err != nil {
		t.Fatalf("new reviewer provider client: %v", err)
	}
	if _, err := client.Generate(context.Background(), llm.Request{
		Model:        "local-reviewer",
		Temperature:  1,
		SystemPrompt: "review",
		Items: []llm.ResponseItem{{
			Type:    llm.ResponseItemTypeMessage,
			Role:    llm.RoleUser,
			Content: "hi",
		}},
	}); err != nil {
		t.Fatalf("generate: %v", err)
	}
	select {
	case got := <-authHeaders:
		if strings.TrimSpace(got) != "" {
			t.Fatalf("expected no Authorization header, got %q", got)
		}
	default:
		t.Fatal("expected local reviewer server request")
	}
}

func TestReviewerProviderCapabilitiesOverrideControlsRuntimePromptCacheKey(t *testing.T) {
	root := t.TempDir()
	store := newRuntimeWireSession(t, root, "ws")
	mainClient := &runtimewireCaptureClient{
		caps: llm.ProviderCapabilities{ProviderID: "openai-compatible", SupportsResponsesAPI: true, SupportsPromptCacheKey: true},
		responses: []llm.Response{{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done", Phase: llm.MessagePhaseFinal},
			Usage:     llm.Usage{WindowTokens: 200_000},
		}},
	}
	reviewerClient := &runtimewireCaptureClient{
		caps: llm.ProviderCapabilities{ProviderID: "local-reviewer", SupportsResponsesAPI: true, SupportsPromptCacheKey: true},
		responses: []llm.Response{{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: `{"suggestions":[]}`},
			Usage:     llm.Usage{WindowTokens: 200_000},
		}},
	}
	reviewerOverride := runtimewireCapabilitiesOverrideClient{
		Client: reviewerClient,
		Capabilities: llm.ProviderCapabilities{
			ProviderID:             "local-reviewer",
			SupportsResponsesAPI:   true,
			SupportsPromptCacheKey: false,
		},
	}

	eng := newRuntimeWireEngine(t, store, mainClient, runtime.Config{
		Model: "gpt-5",
		Reviewer: runtime.ReviewerConfig{
			Frequency: "all",
			Model:     "local-reviewer",
			Client:    reviewerOverride,
		},
	})
	if _, err := eng.SubmitUserMessage(context.Background(), "review this"); err != nil {
		t.Fatalf("submit user message: %v", err)
	}
	if reviewerClient.CallCount() != 1 {
		t.Fatalf("expected one reviewer call, got %d", reviewerClient.CallCount())
	}
	reviewerReq := reviewerClient.LastRequest()
	if reviewerReq.PromptCacheKey != "" {
		t.Fatalf("expected reviewer capability override to suppress prompt cache key, got %q", reviewerReq.PromptCacheKey)
	}
}

type testLogger struct {
	lines []string
}

func (l *testLogger) Logf(format string, args ...any) {
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func (l *testLogger) String() string {
	return strings.Join(l.lines, "\n")
}

func outsideNonTempDir(t *testing.T) string {
	t.Helper()
	bases := make([]string, 0, 2)
	if wd, err := os.Getwd(); err == nil {
		bases = append(bases, wd)
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		bases = append(bases, home)
	}
	for _, base := range bases {
		dir, err := os.MkdirTemp(base, "kent-runtimewire-outside-*")
		if err != nil {
			continue
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			_ = os.RemoveAll(dir)
			continue
		}
		if patchtool.IsPathInTemporaryDir(abs) {
			_ = os.RemoveAll(dir)
			continue
		}
		t.Cleanup(func() {
			_ = os.RemoveAll(dir)
		})
		return abs
	}
	t.Skip("unable to create non-temporary outside directory for test")
	return ""
}

func newRuntimeWireSession(t *testing.T, root string, name string) *session.Store {
	t.Helper()
	store, err := session.Create(root, name, root)
	if err != nil {
		t.Fatalf("create store %s: %v", name, err)
	}
	return store
}

func newRuntimeWireToolRegistry(t *testing.T, workspace string, enabled ...toolspec.ID) (*tools.Registry, *askquestion.AskQuestionBroker) {
	t.Helper()
	return newRuntimeWireLoggedToolRegistry(t, workspace, nil, enabled...)
}

func newRuntimeWireLoggedToolRegistry(t *testing.T, workspace string, logger Logger, enabled ...toolspec.ID) (*tools.Registry, *askquestion.AskQuestionBroker) {
	t.Helper()
	registry, broker, _, err := BuildToolRegistry(workspace, "", enabled, 15*time.Second, 16_000, false, true, logger, nil, nil, nil)
	if err != nil {
		t.Fatalf("build tool registry: %v", err)
	}
	return registry, broker
}

func newRuntimeWireBinding(t *testing.T, workspace string, enabled ...toolspec.ID) *LocalToolRegistryBinding {
	t.Helper()
	binding, _, _, err := NewLocalToolRegistryBinding(workspace, "", enabled, 15*time.Second, 16_000, false, true, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("new local tool registry binding: %v", err)
	}
	return binding
}

func newRuntimeWireEngine(t *testing.T, store *session.Store, client llm.Client, cfg ...runtime.Config) *runtime.Engine {
	t.Helper()
	engineConfig := runtime.Config{Model: "gpt-5"}
	if len(cfg) > 0 {
		engineConfig = cfg[0]
	}
	eng, err := runtime.New(store, client, tools.NewRegistry(), engineConfig)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := eng.Close(); closeErr != nil {
			t.Fatalf("close runtime: %v", closeErr)
		}
	})
	return eng
}

func shellPwdOutput(t *testing.T, registry *tools.Registry) string {
	t.Helper()
	handler, ok := registry.Get(toolspec.ToolExecCommand)
	if !ok {
		t.Fatal("expected exec_command handler")
	}
	result, err := handler.Call(context.Background(), tools.Call{ID: "call-pwd", Name: toolspec.ToolExecCommand, Input: json.RawMessage(`{"command":"pwd"}`)})
	if err != nil {
		t.Fatalf("exec_command call: %v", err)
	}
	var payload string
	if err := json.Unmarshal(result.Output, &payload); err != nil {
		t.Fatalf("decode exec_command output: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(payload), "\n")
	if len(lines) == 0 {
		t.Fatalf("expected exec_command output, got %q", payload)
	}
	return canonicalPathForTest(t, strings.TrimSpace(lines[len(lines)-1]))
}

func canonicalPathForTest(t *testing.T, path string) string {
	t.Helper()
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("canonicalize path %q: %v", path, err)
	}
	return filepath.Clean(canonical)
}

type busyToggleFakeClient struct {
	mu        sync.Mutex
	responses []llm.Response
	calls     int
}

type runtimewireCaptureClient struct {
	mu        sync.Mutex
	caps      llm.ProviderCapabilities
	responses []llm.Response
	calls     []llm.Request
}

type runtimewireCapabilitiesOverrideClient struct {
	llm.Client
	Capabilities llm.ProviderCapabilities
}

func (c runtimewireCapabilitiesOverrideClient) ProviderCapabilities(context.Context) (llm.ProviderCapabilities, error) {
	return c.Capabilities, nil
}

func (f *runtimewireCaptureClient) Generate(ctx context.Context, req llm.Request) (llm.Response, error) {
	if err := ctx.Err(); err != nil {
		return llm.Response{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	if len(f.responses) == 0 {
		return llm.Response{}, errors.New("no fake response configured")
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}

func (f *runtimewireCaptureClient) ProviderCapabilities(context.Context) (llm.ProviderCapabilities, error) {
	return f.caps, nil
}

func (f *runtimewireCaptureClient) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *runtimewireCaptureClient) LastRequest() llm.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return llm.Request{}
	}
	return f.calls[len(f.calls)-1]
}

func (f *busyToggleFakeClient) Generate(ctx context.Context, _ llm.Request) (llm.Response, error) {
	if err := ctx.Err(); err != nil {
		return llm.Response{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if len(f.responses) == 0 {
		return llm.Response{}, errors.New("no fake response configured")
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}

func (f *busyToggleFakeClient) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}
