package core

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"builder/server/auth"
	serverbootstrap "builder/server/bootstrap"
	"builder/shared/config"
)

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
	cfg := config.App{PersistenceRoot: t.TempDir()}
	runtimeSupport, err := serverbootstrap.BuildRuntimeSupport(cfg)
	if err != nil {
		t.Fatalf("BuildRuntimeSupport: %v", err)
	}
	t.Cleanup(func() { _ = runtimeSupport.Background.Close() })

	_, err = NewWithContext(t.Context(), cfg, serverbootstrap.AuthSupport{}, runtimeSupport)
	if err == nil {
		t.Fatal("expected NewWithContext error")
	}
	if !strings.Contains(err.Error(), "auth bundle") || !strings.Contains(err.Error(), "auth manager") {
		t.Fatalf("error = %q, want auth bundle/resource name", err.Error())
	}
}

func TestNewWithContextNamesMissingRuntimeBundleResource(t *testing.T) {
	cfg := config.App{PersistenceRoot: t.TempDir()}
	authSupport, err := serverbootstrap.BuildAuthSupport(auth.NewMemoryStore(auth.EmptyState()), nil, nil)
	if err != nil {
		t.Fatalf("BuildAuthSupport: %v", err)
	}

	_, err = NewWithContext(t.Context(), cfg, authSupport, serverbootstrap.RuntimeSupport{})
	if err == nil {
		t.Fatal("expected NewWithContext error")
	}
	if !strings.Contains(err.Error(), "runtime bundle") || !strings.Contains(err.Error(), "background manager") {
		t.Fatalf("error = %q, want runtime bundle/resource name", err.Error())
	}
}

func TestNewWithContextCleansPersistenceOnAuthBundleFailure(t *testing.T) {
	cfg := config.App{PersistenceRoot: t.TempDir()}
	runtimeSupport, err := serverbootstrap.BuildRuntimeSupport(cfg)
	if err != nil {
		t.Fatalf("BuildRuntimeSupport first: %v", err)
	}
	t.Cleanup(func() { _ = runtimeSupport.Background.Close() })

	_, err = NewWithContext(t.Context(), cfg, serverbootstrap.AuthSupport{}, runtimeSupport)
	if err == nil {
		t.Fatal("expected first NewWithContext error")
	}

	authSupport, err := serverbootstrap.BuildAuthSupport(auth.NewMemoryStore(auth.EmptyState()), nil, nil)
	if err != nil {
		t.Fatalf("BuildAuthSupport: %v", err)
	}
	runtimeSupportSecond, err := serverbootstrap.BuildRuntimeSupport(cfg)
	if err != nil {
		t.Fatalf("BuildRuntimeSupport second: %v", err)
	}
	appCore, err := NewWithContext(t.Context(), cfg, authSupport, runtimeSupportSecond)
	if err != nil {
		t.Fatalf("NewWithContext after failed construction: %v", err)
	}
	t.Cleanup(func() { _ = appCore.Close() })
}
