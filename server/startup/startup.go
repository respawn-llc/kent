package startup

import (
	"context"
	"errors"

	"core/server/auth"
	serverbootstrap "core/server/bootstrap"
	"core/server/core"
	"core/server/metadata"
	"core/shared/config"
)

type Request struct {
	WorkspaceRoot         string
	WorkspaceRootExplicit bool
	SessionID             string
	Model                 string
	ThinkingLevel         string
	Theme                 string
	ModelTimeoutSeconds   int
	Tools                 string
	LoadOptions           config.LoadOptions
}

func startCoreWithBootstrap(ctx context.Context, bootstrapReq serverbootstrap.Request) (*core.Core, error) {
	resolved, err := serverbootstrap.ResolveConfig(bootstrapReq)
	if err != nil {
		panicOnMetadataMigrationFailure(err)
		return nil, err
	}
	cfg := resolved.Config
	store := auth.NewFileStore(config.GlobalAuthConfigPath(cfg))
	authSupport, err := serverbootstrap.BuildAuthSupport(store, bootstrapReq.Environment, bootstrapReq.Now)
	if err != nil {
		return nil, err
	}
	if !cfg.Source.SettingsFileExists() {
		return nil, ErrOnboardingRequired
	}
	background, err := serverbootstrap.BuildShellManager(cfg)
	if err != nil {
		return nil, err
	}
	appCore, err := core.NewWithContextOptions(
		ctx,
		cfg,
		authSupport,
		background,
		coreOptionsForBootstrap(bootstrapReq, nil),
	)
	if err != nil {
		_ = background.Close()
		panicOnMetadataMigrationFailure(err)
		return nil, err
	}
	return appCore, nil
}

func panicOnMetadataMigrationFailure(err error) {
	var migrationErr *metadata.WorkspaceChatDraftCutoverMigrationError
	if errors.As(err, &migrationErr) {
		panic(err)
	}
}

func coreOptionsForBootstrap(req serverbootstrap.Request, rootLease *core.RootLockLease) core.Options {
	loadOptions := req.LoadOptions
	return core.Options{
		RootLease:                  rootLease,
		WorkspaceConfigLoadOptions: loadOptions,
	}
}

func buildRequest(req Request) serverbootstrap.Request {
	loadOptions := req.LoadOptions
	if loadOptions == (config.LoadOptions{}) {
		loadOptions = config.LoadOptions{
			Model:               req.Model,
			ThinkingLevel:       req.ThinkingLevel,
			Theme:               req.Theme,
			ModelTimeoutSeconds: req.ModelTimeoutSeconds,
			Tools:               req.Tools,
		}
	}
	return serverbootstrap.Request{
		WorkspaceRoot:         req.WorkspaceRoot,
		WorkspaceRootExplicit: req.WorkspaceRootExplicit,
		SessionID:             req.SessionID,
		LoadOptions:           loadOptions,
	}
}
