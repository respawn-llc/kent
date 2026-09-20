package runtimewire

import (
	"context"
	"errors"
	"fmt"

	"core/server/metadata/sqlitegen"
	"core/server/tools"
	"core/shared/invariant"

	sqlitedriver "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

type WorkspaceMembership interface {
	FindContainingProjectWorkspace(context.Context, string, string) (*sqlitegen.Workspace, error)
}

func workspaceRootLookup(metadata WorkspaceMembership, debug bool) tools.WorkspaceRootLookup {
	return func(ctx context.Context, projectID, path string) (*tools.FilesystemRoot, error) {
		if metadata == nil {
			return nil, errors.New("Workspace membership authority is required")
		}
		workspace, err := metadata.FindContainingProjectWorkspace(sqlitegen.WithQueryFailureDiagnostics(ctx), projectID, path)
		if err != nil {
			var driverError *sqlitedriver.Error
			contention := errors.As(err, &driverError) &&
				(driverError.Code()&0xff == sqlite3.SQLITE_BUSY || driverError.Code()&0xff == sqlite3.SQLITE_LOCKED)
			if !contention && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				invariant.OperationalPolicy(debug).Check(false, invariant.FailureDiagnostic(
					invariant.ScopeSessionPersistence, "resolve attached Workspace permission", err,
				))
			}
			return nil, fmt.Errorf("resolve attached Workspace permission: %w", err)
		}
		if workspace == nil {
			return nil, nil
		}
		root, err := trustedRootForPath(workspace.CanonicalRootPath)
		if err != nil {
			return nil, err
		}
		return &root, nil
	}
}
