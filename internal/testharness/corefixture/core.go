package corefixture

import (
	"testing"

	"core/server/authmanager"
	serverbootstrap "core/server/bootstrap"
	"core/server/core"
	"core/server/metadata"
	"core/shared/config"
)

func Open(t testing.TB, cfg config.App, store *metadata.Store, options core.Options) (*core.Core, serverbootstrap.AuthSupport) {
	t.Helper()
	if store == nil {
		t.Fatal("Core fixture requires an in-memory metadata store")
	}
	environment, err := serverbootstrap.LoadEnvironment(cfg.PersistenceRoot)
	if err != nil {
		t.Fatal(err)
	}
	authSupport, err := serverbootstrap.BuildAuthSupport(t.Context(), cfg.PersistenceRoot, auth.NewFileStore(config.GlobalAuthConfigPath(cfg)), environment, nil)
	if err != nil {
		t.Fatal(err)
	}
	background, err := serverbootstrap.BuildShellManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := background.Close(); err != nil {
			t.Errorf("close fixture shell manager: %v", err)
		}
	})
	appCore, err := core.NewWithContextOptions(t.Context(), cfg, authSupport, background, core.Options{
		MetadataStore: store, RuntimeClientFactory: options.RuntimeClientFactory,
		RootLease: options.RootLease, WorkspaceConfigLoadOptions: options.WorkspaceConfigLoadOptions,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := appCore.Close(); err != nil {
			t.Errorf("close fixture Core: %v", err)
		}
	})
	return appCore, authSupport
}
