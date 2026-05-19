package app

import (
	"context"
	"os"
	"testing"

	"builder/server/auth"
	"builder/server/metadata"
	"builder/shared/config"
)

func TestBootstrapAppIgnoresOAuthIssuerOverrideEnv(t *testing.T) {
	t.Setenv("BUILDER_OAUTH_ISSUER", "https://attacker.example")
	t.Setenv("BUILDER_OAUTH_CLIENT_ID", "client-test")
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	registerAppWorkspace(t, workspace)

	readyAuth := readyMemoryAuthHandler()
	readyAuth.lookupEnv = os.Getenv
	boot, err := startEmbeddedServer(context.Background(), Options{WorkspaceRoot: workspace}, readyAuth)
	if err != nil {
		t.Fatalf("bootstrap app: %v", err)
	}
	defer func() { _ = boot.Close() }()
	if got := boot.OAuthOptions().Issuer; got != auth.DefaultOpenAIIssuer {
		t.Fatalf("oauth issuer = %q, want %q", got, auth.DefaultOpenAIIssuer)
	}
	if got := boot.OAuthOptions().ClientID; got != "client-test" {
		t.Fatalf("oauth client id = %q", got)
	}
	binding, err := metadata.ResolveBinding(context.Background(), boot.Config().PersistenceRoot, boot.Config().WorkspaceRoot)
	if err != nil {
		t.Fatalf("resolve bootstrap binding: %v", err)
	}
	containerDir := config.ProjectSessionsRoot(boot.Config(), binding.ProjectID)
	if _, err := os.Stat(containerDir); err != nil {
		t.Fatalf("expected bootstrap container dir to exist: %v", err)
	}
}
