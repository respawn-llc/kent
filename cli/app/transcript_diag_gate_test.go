package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTranscriptDiagnosticGateUsesSharedHelper(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("filepath.Abs repo root: %v", err)
	}
	checks := []struct {
		path           string
		mustContain    string
		mustNotContain string
	}{
		{
			path:           filepath.Join(root, "cli", "app", "session_activity_channel.go"),
			mustNotContain: "EnabledFromEnv(",
		},
		{
			path:           filepath.Join(root, "cli", "app", "ui_runtime_client.go"),
			mustContain:    "transcriptdiag.Enabled(false, os.Getenv)",
			mustNotContain: "EnabledFromEnv(",
		},
	}
	for _, check := range checks {
		data, err := os.ReadFile(check.path)
		if err != nil {
			t.Fatalf("ReadFile %s: %v", check.path, err)
		}
		body := string(data)
		if check.mustContain != "" && !strings.Contains(body, check.mustContain) {
			t.Fatalf("expected %s to contain %q", check.path, check.mustContain)
		}
		if check.mustNotContain != "" && strings.Contains(body, check.mustNotContain) {
			t.Fatalf("expected %s to avoid %q", check.path, check.mustNotContain)
		}
	}
}
