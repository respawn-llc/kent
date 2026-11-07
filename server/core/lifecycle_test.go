package core

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"core/internal/testharness/testsetup"
	"core/server/authmanager"
	serverbootstrap "core/server/bootstrap"
	"core/server/operation"
	"core/shared/config"
)

func TestCoreShutdownJoinsServerOperationsBeforeClosingPersistence(t *testing.T) {
	cfg := config.App{
		PersistenceRoot: t.TempDir(),
		Settings: config.Settings{
			Shell: config.ShellSettings{MaxConcurrent: config.DefaultMaxConcurrentShells, PostprocessingMode: config.ShellPostprocessingModeBuiltin},
		},
	}
	store := testsetup.OpenStore(t, cfg.PersistenceRoot)
	background, err := serverbootstrap.BuildShellManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = background.Close() })
	owner := operation.NewOperationOwner()
	appCore := &Core{bundles: composeBundles(bundleCompositionInput{
		cfg: cfg, metadataStore: store, background: background, operationOwner: owner,
	})}
	started := make(chan struct{})
	canceled := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	work, err := owner.Start(t.Context(), func(scope operation.OperationScope) error {
		close(started)
		<-scope.Context().Done()
		close(canceled)
		<-release
		_, err := store.ListProjects(t.Context())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	closed := make(chan error, 1)
	go func() { closed <- appCore.Close() }()
	<-canceled
	if _, err := owner.Start(t.Context(), func(operation.OperationScope) error { return nil }); !errors.Is(err, operation.ErrOperationOwnerClosed) {
		t.Fatalf("new operation during shutdown: %v", err)
	}
	select {
	case err := <-closed:
		t.Fatalf("Core closed while work was still running: %v", err)
	default:
	}
	if _, err := store.ListProjects(t.Context()); err != nil {
		t.Fatalf("persistence closed before accepted work joined: %v", err)
	}
	// Release without closing the channel twice if an earlier assertion fails.
	release <- struct{}{}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if err := work.Await(t.Context()); err != nil {
		t.Fatalf("operation lost persistence during shutdown: %v", err)
	}
	if _, err := store.ListProjects(context.Background()); err == nil {
		t.Fatal("Core did not close persistence after joining work")
	}
}

func TestCoreCloseClosesResourcesOnceInReverseRegistrationOrder(t *testing.T) {
	var calls []string
	appCore := &Core{
		bundles: &Bundles{
			cleanup: []lifecycleResource{
				{name: "root lock", close: func() error {
					calls = append(calls, "root lock")
					return nil
				}},
				{name: "metadata store", close: func() error {
					calls = append(calls, "metadata store")
					return nil
				}},
				{name: "background manager", close: func() error {
					calls = append(calls, "background manager")
					return nil
				}},
			},
		},
	}

	if err := appCore.Close(); err != nil {
		t.Fatalf("Close first: %v", err)
	}
	if err := appCore.Close(); err != nil {
		t.Fatalf("Close second: %v", err)
	}
	want := []string{"background manager", "metadata store", "root lock"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("close calls = %v, want %v", calls, want)
	}
}

func TestComposeBundlesClosesWorkflowExecutionBeforeAuthorityAndPersistence(t *testing.T) {
	bundles := composeBundles(bundleCompositionInput{})
	registrationIndex := make(map[string]int, len(bundles.cleanup))
	for index, resource := range bundles.cleanup {
		registrationIndex[resource.name] = index
	}

	metadataIndex, hasMetadata := registrationIndex["metadata store"]
	authorityIndex, hasAuthority := registrationIndex["session runtime authority"]
	starterIndex, hasStarter := registrationIndex["workflow runtime starter"]
	controllerIndex, hasController := registrationIndex["workflow execution controller"]
	if !hasMetadata || !hasAuthority || !hasStarter || !hasController {
		t.Fatalf("cleanup resources = %+v, want metadata, authority, starter, and controller", registrationIndex)
	}
	if !(metadataIndex < authorityIndex && authorityIndex < starterIndex && starterIndex < controllerIndex) {
		t.Fatalf(
			"cleanup registration = %+v, want reverse close order controller, starter, authority, metadata",
			registrationIndex,
		)
	}
}

func TestCoreCloseNamesFailedResources(t *testing.T) {
	wantErr := errors.New("boom")
	appCore := &Core{
		bundles: &Bundles{
			cleanup: []lifecycleResource{
				{name: "metadata store", close: func() error {
					return wantErr
				}},
			},
		},
	}

	err := appCore.Close()
	if err == nil {
		t.Fatal("expected close error")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("Close error = %v, want wrapped %v", err, wantErr)
	}
	if got := err.Error(); got != "metadata store: boom" {
		t.Fatalf("Close error text = %q, want resource name", got)
	}
}

func TestNewWithContextNamesMissingAuthBundleResource(t *testing.T) {
	cfg := config.App{
		PersistenceRoot: t.TempDir(),
		Settings: config.Settings{
			Shell: config.ShellSettings{MaxConcurrent: config.DefaultMaxConcurrentShells, PostprocessingMode: config.ShellPostprocessingModeBuiltin},
		},
	}
	metadataStore := testsetup.OpenStore(t, cfg.PersistenceRoot)
	background, err := serverbootstrap.BuildShellManager(cfg)
	if err != nil {
		t.Fatalf("BuildShellManager: %v", err)
	}
	t.Cleanup(func() { _ = background.Close() })

	_, err = NewWithContextOptions(t.Context(), cfg, serverbootstrap.AuthSupport{}, background, Options{
		MetadataStore: metadataStore,
	})
	if err == nil {
		t.Fatal("expected NewWithContext error")
	}
	var missing BundleResourceRequiredError
	if !errors.As(err, &missing) || missing.BundleName != "auth" || missing.ResourceName != "auth manager" {
		t.Fatalf("error = %v, want auth bundle/resource name", err)
	}
}

func TestNewWithContextNamesMissingRuntimeBundleResource(t *testing.T) {
	cfg := config.App{PersistenceRoot: t.TempDir()}
	metadataStore := testsetup.OpenStore(t, cfg.PersistenceRoot)
	authSupport, err := serverbootstrap.BuildAuthSupport(t.Context(), cfg.PersistenceRoot, auth.NewMemoryStore(auth.EmptyState()), func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatalf("BuildAuthSupport: %v", err)
	}

	_, err = NewWithContextOptions(t.Context(), cfg, authSupport, nil, Options{
		MetadataStore: metadataStore,
	})
	if err == nil {
		t.Fatal("expected NewWithContext error")
	}
	var missing BundleResourceRequiredError
	if !errors.As(err, &missing) || missing.BundleName != "runtime" || missing.ResourceName != "background manager" {
		t.Fatalf("error = %v, want runtime bundle/resource name", err)
	}
}

func TestNewWithContextCleansPersistenceOnAuthBundleFailure(t *testing.T) {
	cfg := config.App{
		PersistenceRoot: t.TempDir(),
		Settings: config.Settings{
			NanoConcurrency: 5,
			Shell:           config.ShellSettings{MaxConcurrent: config.DefaultMaxConcurrentShells, PostprocessingMode: config.ShellPostprocessingModeBuiltin},
			Workflow:        config.WorkflowSettings{Concurrency: 1},
		},
	}
	metadataStore := testsetup.OpenStore(t, cfg.PersistenceRoot)
	background, err := serverbootstrap.BuildShellManager(cfg)
	if err != nil {
		t.Fatalf("BuildShellManager first: %v", err)
	}
	t.Cleanup(func() { _ = background.Close() })

	_, err = NewWithContextOptions(t.Context(), cfg, serverbootstrap.AuthSupport{}, background, Options{
		MetadataStore: metadataStore,
	})
	if err == nil {
		t.Fatal("expected first NewWithContext error")
	}

	authSupport, err := serverbootstrap.BuildAuthSupport(t.Context(), cfg.PersistenceRoot, auth.NewMemoryStore(auth.EmptyState()), func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatalf("BuildAuthSupport: %v", err)
	}
	backgroundSecond, err := serverbootstrap.BuildShellManager(cfg)
	if err != nil {
		t.Fatalf("BuildShellManager second: %v", err)
	}
	appCore, err := NewWithContextOptions(t.Context(), cfg, authSupport, backgroundSecond, Options{
		MetadataStore: testsetup.OpenStore(t, cfg.PersistenceRoot),
	})
	if err != nil {
		t.Fatalf("NewWithContext after failed construction: %v", err)
	}
	t.Cleanup(func() { _ = appCore.Close() })
}
