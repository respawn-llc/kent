package startup

import (
	"testing"

	"core/shared/config"
)

func TestBuildRequestMapsStartupOptions(t *testing.T) {
	req := buildRequest(Request{
		WorkspaceRoot:         "/tmp/workspace",
		WorkspaceRootExplicit: true,
		SessionID:             "session-123",
		Model:                 "gpt-5",
		ProviderOverride:      "openai",
		ThinkingLevel:         "high",
		Theme:                 "dark",
		ModelTimeoutSeconds:   45,
		Tools:                 "shell,patch",
		OpenAIBaseURL:         "http://example.test/v1",
		OpenAIBaseURLExplicit: true,
	})

	if req.WorkspaceRoot != "/tmp/workspace" || !req.WorkspaceRootExplicit || req.SessionID != "session-123" {
		t.Fatalf("unexpected request mapping: %+v", req)
	}
	if req.OpenAIBaseURL != "http://example.test/v1" || !req.OpenAIBaseURLExplicit {
		t.Fatalf("unexpected base URL mapping: %+v", req)
	}
	if req.LoadOptions != (config.LoadOptions{
		Model:               "gpt-5",
		ProviderOverride:    "openai",
		ThinkingLevel:       "high",
		Theme:               "dark",
		ModelTimeoutSeconds: 45,
		Tools:               "shell,patch",
	}) {
		t.Fatalf("unexpected load options: %+v", req.LoadOptions)
	}
}
