package bootstrap

import (
	"errors"
	"os"
	"strings"
	"time"

	"core/server/auth"
	"core/server/chatcontext"
	"core/server/launch"
	shelltool "core/server/tools/shell"
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

func ResolveConfig(req Request) (ConfigPlan, error) {
	return resolveConfig(req, loadConfig)
}

// ResolveConnectionConfig preserves continuation discovery without resolving
// Main Workspace private ownership before server attachment.
func ResolveConnectionConfig(req Request) (ConfigPlan, error) {
	return resolveConfig(req, func(opts config.LoadOptions, _ string, plan launch.BootstrapPlan) (ConfigPlan, error) {
		app, client, err := config.LoadInteractiveConnectionDiscovery(plan.WorkspaceRoot, opts)
		return ConfigPlan{Config: app, Client: client}, err
	})
}

func resolveConfig(req Request, load func(config.LoadOptions, string, launch.BootstrapPlan) (ConfigPlan, error)) (ConfigPlan, error) {
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
	opts := req.LoadOptions
	if bootstrapPlan.UseOpenAIBaseURL {
		opts.OpenAIBaseURL = bootstrapPlan.OpenAIBaseURL
	} else {
		opts.OpenAIBaseURL = ""
	}
	return load(opts, persistenceRoot, bootstrapPlan)
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
		),
	}, nil
}

func BuildShellManager(cfg config.App) (*shelltool.Manager, error) {
	return shelltool.NewManager(
		shelltool.WithMaxConcurrent(cfg.Settings.Shell.MaxConcurrent),
		shelltool.WithMinimumExecToBgTime(time.Duration(cfg.Settings.MinimumExecToBgSeconds)*time.Second),
	)
}

func loadConfig(loadOpts config.LoadOptions, persistenceRoot string, plan launch.BootstrapPlan) (ConfigPlan, error) {
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
