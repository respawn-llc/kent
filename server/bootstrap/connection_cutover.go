package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"core/server/auth"
	"core/shared/config"
)

// ConvertProviderConfiguration runs only while startup owns the root lease.
// Config must be replaced before discarding the old authentication selection.
func ConvertProviderConfiguration(ctx context.Context, root string) error {
	authPath := config.GlobalAuthConfigPath(config.App{PersistenceRoot: root})
	store := auth.NewFileStore(authPath)
	selection, legacy, err := store.LegacyConnectionSelection(ctx)
	if err != nil {
		return err
	}
	path, err := config.ResolveSettingsFilePathInRoot(root)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		changed, err := config.ConvertGlobalConnections(path, selection)
		if err != nil {
			return err
		}
		if changed {
			slog.InfoContext(ctx, "converted provider configuration", "path", path)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect %s: %w", path, err)
	}
	if legacy {
		if err := store.Save(ctx, auth.EmptyState()); err != nil {
			return fmt.Errorf("convert %s: %w", authPath, err)
		}
		slog.InfoContext(ctx, "converted credential store; subscription connections require sign-in", "path", authPath)
	}
	return nil
}
