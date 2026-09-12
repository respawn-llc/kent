package runtimewire

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"core/internal/testharness/scriptedllm"
	"core/server/session"
	"core/server/tools"
	"core/shared/config"
	"core/shared/toolspec"
)

func TestCompactionEnablesAstraImageHandlerInExistingRuntime(t *testing.T) {
	root := t.TempDir()
	store := newRuntimeWireSession(t, root, "astra-vision")
	if err := store.MarkModelDispatchLocked(session.LockedContract{
		Model:             "gpt-6-astra",
		ModelCapabilities: session.LockedModelCapabilities{SupportsReasoningEffort: true},
		EnabledTools:      []string{string(toolspec.ToolViewImage)}, HasEnabledTools: true,
	}); err != nil {
		t.Fatal(err)
	}
	client := scriptedllm.NewClient(scriptedllm.Script{
		Steps: []scriptedllm.Step{scriptedllm.FinalAnswer("seed"), scriptedllm.FinalAnswer("summary"), scriptedllm.FinalAnswer("next")},
	})
	active := runtimeWireShellSettings(config.ShellPostprocessingModeNone, nil)
	active.Model = "gpt-6-astra"
	active.ThinkingLevel = "medium"
	active.CompactionMode = "local"
	wiring, err := NewRuntimeWiringWithBackground(
		store, materializedRuntimeWireEventLog(t, store), active,
		[]toolspec.ID{toolspec.ToolViewImage}, nil, nil, nil,
		requiredRuntimeWireTestOptions(RuntimeWiringOptions{
			FilesystemContext: runtimeWireFilesystemContext(t, root), Client: client,
			GlobalConfigDir: t.TempDir(),
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := wiring.Close(); err != nil {
			t.Error(err)
		}
	})
	path := filepath.Join(root, "image.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\ntrailer\n<<>>\n%%EOF\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		t.Fatal(err)
	}
	handler, ok := wiring.LocalTools.Registry().Get(toolspec.ToolViewImage)
	if !ok {
		t.Fatal("view_image handler is missing")
	}
	call := tools.Call{ID: "image", Name: toolspec.ToolViewImage, Input: input}
	before, err := handler.Call(t.Context(), call)
	if err != nil || !before.IsError {
		t.Fatalf("old context image result = %+v, error=%v", before, err)
	}
	if _, err := wiring.Engine.SubmitUserMessage(t.Context(), "seed"); err != nil {
		t.Fatal(err)
	}
	if err := wiring.Engine.CompactContextForWorkflowContinuation(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := wiring.Engine.SubmitUserMessage(t.Context(), "after compaction"); err != nil {
		t.Fatal(err)
	}
	after, err := handler.Call(t.Context(), call)
	if err != nil || after.IsError {
		t.Fatalf("fresh context image result = %+v, error=%v", after, err)
	}
	requests := client.Requests()
	if len(requests) != 3 || len(requests[0].Tools) != 0 {
		t.Fatalf("requests did not preserve the outgoing tool contract: %+v", requests)
	}
	if tools := requests[2].Tools; len(tools) != 1 || tools[0].Name != string(toolspec.ToolViewImage) {
		t.Fatalf("fresh request tools = %+v, want view_image", tools)
	}
}
