package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"core/prompts"
	"core/server/auth"
	"core/server/chatcontext"
	"core/server/launch"
	shelltool "core/server/tools/shell"
	"core/server/tools/shell/postprocess"
	"core/shared/config"
	"core/shared/textutil"
)

type Request struct {
	WorkspaceRoot         string
	WorkspaceRootExplicit bool
	SessionID             string
	OpenAIBaseURL         string
	OpenAIBaseURLExplicit bool
	LoadOptions           config.LoadOptions
	LookupEnv             func(string) string
	Now                   func() time.Time
}

type ConfigPlan struct {
	Config config.App
	Client config.ClientSettings
}

func ValidateSessionExists(persistenceRoot string, sessionID string) error {
	return launch.ValidateSessionExists(persistenceRoot, sessionID)
}

type AuthSupport struct {
	OAuthOptions auth.OpenAIOAuthOptions
	AuthManager  *auth.Manager
}

type RuntimeSupport struct {
	Background *shelltool.Manager
	Generated  prompts.GeneratedSyncResult
}

func ResolveConfig(req Request) (ConfigPlan, error) {
	persistenceRoot, err := config.ResolvePersistenceRoot(req.LoadOptions.ConfigRoot)
	if err != nil {
		return ConfigPlan{}, err
	}
	bootstrapPlan, err := launch.ResolveBootstrapPlan(persistenceRoot, launch.BootstrapRequest{
		WorkspaceRoot:         strings.TrimSpace(req.WorkspaceRoot),
		WorkspaceRootExplicit: req.WorkspaceRootExplicit,
		SessionID:             strings.TrimSpace(req.SessionID),
		OpenAIBaseURL:         strings.TrimSpace(req.OpenAIBaseURL),
		OpenAIBaseURLExplicit: req.OpenAIBaseURLExplicit,
	})
	if err != nil {
		return ConfigPlan{}, err
	}
	return loadConfig(req.LoadOptions, persistenceRoot, bootstrapPlan)
}

func BuildAuthSupport(store auth.Store, lookupEnv func(string) string, now func() time.Time) (AuthSupport, error) {
	if store == nil {
		return AuthSupport{}, errors.New("auth store is required")
	}
	if lookupEnv == nil {
		lookupEnv = os.Getenv
	}
	if now == nil {
		now = time.Now
	}
	oauthOpts := auth.OpenAIOAuthOptions{
		Issuer:   auth.DefaultOpenAIIssuer,
		ClientID: textutil.FirstNonEmpty(strings.TrimSpace(lookupEnv("KENT_OAUTH_CLIENT_ID")), auth.DefaultOpenAIClientID),
	}
	return AuthSupport{
		OAuthOptions: oauthOpts,
		AuthManager: auth.NewManager(
			store,
			auth.NewOpenAIOAuthRefresher(oauthOpts, now, 5*time.Minute),
			now,
		),
	}, nil
}

func BuildRuntimeSupport(cfg config.App) (RuntimeSupport, error) {
	runner, err := postprocess.NewRunner(postprocess.Settings{
		Mode:     cfg.Settings.Shell.PostprocessingMode,
		HookPath: cfg.Settings.Shell.PostprocessHook,
	})
	if err != nil {
		return RuntimeSupport{}, fmt.Errorf("compile shell postprocessor: %w", err)
	}
	background, err := shelltool.NewManager(
		shelltool.WithMinimumExecToBgTime(time.Duration(cfg.Settings.MinimumExecToBgSeconds)*time.Second),
		shelltool.WithPostprocessor(runner),
	)
	if err != nil {
		return RuntimeSupport{}, err
	}
	return RuntimeSupport{
		Background: background,
	}, nil
}

func BuildGeneratedSupport(ctx context.Context, persistenceRoot string) (prompts.GeneratedSyncResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	return prompts.GeneratedSync(ctx, prompts.GeneratedSyncOptions{ConfigRoot: strings.TrimSpace(persistenceRoot)})
}

func loadConfig(loadOpts config.LoadOptions, persistenceRoot string, plan launch.BootstrapPlan) (ConfigPlan, error) {
	if plan.UseOpenAIBaseURL {
		loadOpts.OpenAIBaseURL = plan.OpenAIBaseURL
	} else {
		loadOpts.OpenAIBaseURL = ""
	}
	if strings.TrimSpace(plan.WorkspaceRoot) == "" {
		app, err := config.LoadGlobal(loadOpts)
		return ConfigPlan{Config: app}, err
	}
	mainRoot := plan.MainWorkspaceRoot
	if mainRoot == nil {
		root, err := chatcontext.ResolveMainWorkspaceRoot(persistenceRoot, plan.WorkspaceRoot)
		if err != nil {
			return ConfigPlan{}, err
		}
		mainRoot = &root
	}
	app, client, err := config.LoadInteractive(plan.WorkspaceRoot, *mainRoot, loadOpts)
	return ConfigPlan{Config: app, Client: client}, err
}
