package startup

import (
	"context"
	"errors"
	"strings"

	"core/server/auth"
	"core/server/authservice"
	serverbootstrap "core/server/bootstrap"
	"core/server/core"
	"core/server/metadata"
	"core/shared/config"
)

type Request struct {
	WorkspaceRoot         string
	WorkspaceRootExplicit bool
	AllowUnauthenticated  bool
	SessionID             string
	OpenAIBaseURL         string
	OpenAIBaseURLExplicit bool
	LoadOptions           config.LoadOptions
}

func startCoreWithBootstrap(ctx context.Context, bootstrapReq serverbootstrap.Request, requireAuth bool) (*core.Core, error) {
	resolved, err := serverbootstrap.ResolveConfig(bootstrapReq)
	if err != nil {
		panicOnMetadataMigrationFailure(err)
		return nil, err
	}
	cfg := resolved.Config
	store := auth.NewFileStore(config.GlobalAuthConfigPath(cfg))
	authSupport, err := serverbootstrap.BuildAuthSupport(store, bootstrapReq.LookupEnv, bootstrapReq.Now)
	if err != nil {
		return nil, err
	}
	if requireAuth {
		state, err := authSupport.AuthManager.Load(ctx)
		if err != nil {
			return nil, err
		}
		if authservice.StartupAuthRequired(cfg.Settings) {
			if err := auth.EnsureStartupReady(state); err != nil {
				return nil, err
			}
		}
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
	if req.OpenAIBaseURLExplicit {
		loadOptions.OpenAIBaseURL = strings.TrimSpace(req.OpenAIBaseURL)
	} else {
		loadOptions.OpenAIBaseURL = ""
	}
	return core.Options{
		RootLease:                  rootLease,
		WorkspaceConfigLoadOptions: loadOptions,
	}
}

func buildRequest(req Request) serverbootstrap.Request {
	return serverbootstrap.Request{
		WorkspaceRoot:         req.WorkspaceRoot,
		WorkspaceRootExplicit: req.WorkspaceRootExplicit,
		SessionID:             req.SessionID,
		OpenAIBaseURL:         req.OpenAIBaseURL,
		OpenAIBaseURLExplicit: req.OpenAIBaseURLExplicit,
		LoadOptions:           req.LoadOptions,
	}
}
