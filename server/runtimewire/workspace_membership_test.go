package runtimewire

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"core/internal/testharness/testsetup"
	"core/server/tools"

	sqlitedriver "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

func TestWorkspaceAuthorityFailureUsesDiagnosticPolicy(t *testing.T) {
	for _, debug := range []bool{false, true} {
		t.Run(map[bool]string{false: "production", true: "debug"}[debug], func(t *testing.T) {
			store := testsetup.OpenStore(t, t.TempDir())
			binding, err := store.RegisterWorkspaceBinding(t.Context(), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			filesystem, err := NewFilesystemContext(binding.CanonicalRoot, binding.CanonicalRoot, binding.ProjectID)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			policy, err := tools.NewFileAccessPolicy(tools.FileAccessPolicyConfig{
				Context: filesystem, Mode: tools.FileAccessMutation,
				Approver:    NewOutsideWorkspaceApprover(tools.NewAskQuestionBroker()),
				Permissions: tools.NewWorkspacePermissions(workspaceRootLookup(store, debug)),
			})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(outsideNonTempDir(t), "file.txt")
			defer func() {
				panicValue := recover()
				if debug != (panicValue != nil) {
					t.Errorf("authority failure panic = %v, debug = %t", panicValue, debug)
				}
			}()
			outcome := policy.BeginCall().Authorize(t.Context(), path, path)
			if outcome.Kind != tools.FileAccessPolicyFailed || outcome.Cause == nil {
				t.Fatalf("authority failure = %+v", outcome)
			}
		})
	}
}

func TestWorkspaceAuthorityContentionExhaustionIsOperationFailure(t *testing.T) {
	store := testsetup.OpenStore(t, t.TempDir())
	binding, err := store.RegisterWorkspaceBinding(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	filesystem, err := NewFilesystemContext(binding.CanonicalRoot, binding.CanonicalRoot, binding.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(t.Context(), "PRAGMA journal_mode=DELETE"); err != nil {
		t.Fatal(err)
	}
	lock, err := store.DB().Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if _, err := lock.ExecContext(t.Context(), "BEGIN EXCLUSIVE"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := lock.ExecContext(context.Background(), "ROLLBACK"); err != nil {
			t.Error(err)
		}
	}()
	policy, err := tools.NewFileAccessPolicy(tools.FileAccessPolicyConfig{
		Context: filesystem, Mode: tools.FileAccessMutation,
		Approver:    NewOutsideWorkspaceApprover(tools.NewAskQuestionBroker()),
		Permissions: tools.NewWorkspacePermissions(workspaceRootLookup(store, true)),
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(outsideNonTempDir(t), "file.txt")
	outcome := policy.BeginCall().Authorize(t.Context(), path, path)
	var driverError *sqlitedriver.Error
	if outcome.Kind != tools.FileAccessPolicyFailed || !errors.As(outcome.Cause, &driverError) ||
		(driverError.Code()&0xff != sqlite3.SQLITE_BUSY && driverError.Code()&0xff != sqlite3.SQLITE_LOCKED) {
		t.Fatalf("contention outcome = %+v", outcome)
	}
}

func TestWorkspaceAuthorityPoolWaitCanBeCanceledWithoutApproval(t *testing.T) {
	store := testsetup.OpenStore(t, t.TempDir())
	binding, err := store.RegisterWorkspaceBinding(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	filesystem, err := NewFilesystemContext(binding.CanonicalRoot, binding.CanonicalRoot, binding.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	store.DB().SetMaxOpenConns(1)
	connection, err := store.DB().Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	policy, err := tools.NewFileAccessPolicy(tools.FileAccessPolicyConfig{
		Context: filesystem, Mode: tools.FileAccessMutation,
		Approver:    NewOutsideWorkspaceApprover(tools.NewAskQuestionBroker()),
		Permissions: tools.NewWorkspacePermissions(workspaceRootLookup(store, true)),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	path := filepath.Join(outsideNonTempDir(t), "file.txt")
	outcome := policy.BeginCall().Authorize(ctx, path, path)
	if outcome.Kind != tools.FileAccessPolicyFailed || !errors.Is(outcome.Cause, context.Canceled) {
		t.Fatalf("canceled membership outcome = %+v", outcome)
	}
}
