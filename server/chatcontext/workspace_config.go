package chatcontext

import (
	"context"
	"core/server/llm"
	"core/server/metadata"
	"database/sql"
	"errors"
	"strings"

	"core/shared/config"
)

// FixedRootWorkspaceResolver reloads an exact workspace while retaining the
// config root and explicit settings overrides selected at server startup.
type FixedRootWorkspaceResolver struct {
	metadata             *metadata.Store
	startupWorkspaceRoot string
	startupLoadOptions   config.LoadOptions
}

func NewFixedRootWorkspaceResolver(store *metadata.Store, startupWorkspaceRoot string, startupLoadOptions config.LoadOptions) FixedRootWorkspaceResolver {
	configRoot := store.PersistenceRoot()
	startupLoadOptions.ConfigRoot = configRoot
	return FixedRootWorkspaceResolver{
		metadata:             store,
		startupWorkspaceRoot: strings.TrimSpace(startupWorkspaceRoot),
		startupLoadOptions:   startupLoadOptions,
	}
}

func (r FixedRootWorkspaceResolver) Resolve(workspaceRoot string) (config.App, error) {
	configRoot := strings.TrimSpace(r.startupLoadOptions.ConfigRoot)
	if configRoot == "" {
		return config.App{}, errors.New("fixed config root is required")
	}
	loadOptions := config.LoadOptions{ConfigRoot: configRoot}
	if r.startupWorkspaceRoot != "" {
		requestedRoot, err := config.CanonicalWorkspaceRoot(workspaceRoot)
		if err != nil {
			return config.App{}, err
		}
		startupRoot, err := config.CanonicalWorkspaceRoot(r.startupWorkspaceRoot)
		if err != nil {
			return config.App{}, err
		}
		if requestedRoot == startupRoot {
			loadOptions = r.startupLoadOptions
		}
	}
	mainRoot, err := resolveMainWorkspaceRoot(r.metadata, workspaceRoot)
	if err != nil {
		return config.App{}, err
	}
	app, err := config.Load(workspaceRoot, mainRoot, loadOptions)
	if err == nil {
		_, err = llm.ResolveNanoTargets(app.Settings)
	}
	return app, err
}

// ResolveMainWorkspaceRoot uses workspace/worktree bindings, without registering
// an ordinary unbound workspace or inferring ownership from its parent folders.
func ResolveMainWorkspaceRoot(persistenceRoot, workspaceRoot string) (string, error) {
	exists, err := metadata.DatabaseExists(persistenceRoot)
	if err != nil {
		return "", err
	}
	if !exists {
		return config.CanonicalWorkspaceRoot(workspaceRoot)
	}
	store, err := metadata.Open(persistenceRoot)
	if err != nil {
		return "", err
	}
	root, resolveErr := resolveMainWorkspaceRoot(store, workspaceRoot)
	return root, errors.Join(resolveErr, store.Close())
}

func resolveMainWorkspaceRoot(store *metadata.Store, workspaceRoot string) (string, error) {
	canonicalRoot, err := config.CanonicalWorkspaceRoot(workspaceRoot)
	if err != nil {
		return "", err
	}
	// Configuration depends on a managed worktree's main Workspace, not on
	// which Project owns an ordinary directory shared by several Projects.
	worktree, err := store.GetWorktreeRecordByCanonicalRoot(context.Background(), canonicalRoot)
	if errors.Is(err, sql.ErrNoRows) {
		return canonicalRoot, nil
	}
	if err != nil {
		return "", err
	}
	binding, err := store.LookupWorkspaceBindingByID(context.Background(), worktree.WorkspaceID)
	if err != nil {
		return "", err
	}
	return binding.CanonicalRoot, nil
}
