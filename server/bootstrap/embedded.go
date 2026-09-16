package bootstrap

import (
	"errors"
	"os"
	"strings"
	"time"

	"core/server/auth"
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
	InitialConfig         *InitialConfigSnapshot
	LookupEnv             func(string) string
	Now                   func() time.Time
}

type InitialConfigSnapshot struct {
	Config           config.App
	WorkspaceRoot    string
	OpenAIBaseURL    string
	UseOpenAIBaseURL bool
}

type ConfigPlan struct {
	Config config.App
}

func ValidateSessionExists(persistenceRoot string, sessionID string) error {
	return launch.ValidateSessionExists(persistenceRoot, sessionID)
}

type AuthSupport struct {
	OAuthOptions auth.OpenAIOAuthOptions
	AuthManager  *auth.Manager
}

func ResolveConfig(req Request) (ConfigPlan, error) {
	bootstrapPlan := launch.BootstrapPlan{
		WorkspaceRoot:    strings.TrimSpace(req.WorkspaceRoot),
		OpenAIBaseURL:    strings.TrimSpace(req.OpenAIBaseURL),
		UseOpenAIBaseURL: req.OpenAIBaseURLExplicit,
	}
	var cfg config.App
	var err error
	if req.InitialConfig == nil {
		cfg, err = loadConfig(req.LoadOptions, bootstrapPlan.WorkspaceRoot, bootstrapPlan.OpenAIBaseURL, bootstrapPlan.UseOpenAIBaseURL)
		if err != nil {
			return ConfigPlan{}, err
		}
	} else {
		if req.InitialConfig.WorkspaceRoot != bootstrapPlan.WorkspaceRoot ||
			req.InitialConfig.OpenAIBaseURL != bootstrapPlan.OpenAIBaseURL ||
			req.InitialConfig.UseOpenAIBaseURL != bootstrapPlan.UseOpenAIBaseURL {
			return ConfigPlan{}, errors.New("initial config snapshot does not match bootstrap target")
		}
		cfg = req.InitialConfig.Config
	}
	bootstrapPlan, err = launch.ResolveBootstrapPlan(cfg.PersistenceRoot, launch.BootstrapRequest{
		WorkspaceRoot:         strings.TrimSpace(req.WorkspaceRoot),
		WorkspaceRootExplicit: req.WorkspaceRootExplicit,
		SessionID:             strings.TrimSpace(req.SessionID),
		OpenAIBaseURL:         strings.TrimSpace(req.OpenAIBaseURL),
		OpenAIBaseURLExplicit: req.OpenAIBaseURLExplicit,
	})
	if err != nil {
		return ConfigPlan{}, err
	}
	if req.InitialConfig != nil &&
		bootstrapPlan.WorkspaceRoot == strings.TrimSpace(req.WorkspaceRoot) &&
		bootstrapPlan.OpenAIBaseURL == strings.TrimSpace(req.OpenAIBaseURL) &&
		bootstrapPlan.UseOpenAIBaseURL == req.OpenAIBaseURLExplicit {
		return ConfigPlan{Config: cfg}, nil
	}
	cfg, err = loadConfig(req.LoadOptions, bootstrapPlan.WorkspaceRoot, bootstrapPlan.OpenAIBaseURL, bootstrapPlan.UseOpenAIBaseURL)
	if err != nil {
		return ConfigPlan{}, err
	}
	return ConfigPlan{Config: cfg}, nil
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
		shelltool.WithMinimumExecToBgTime(time.Duration(cfg.Settings.MinimumExecToBgSeconds) * time.Second),
	)
}

func loadConfig(loadOpts config.LoadOptions, workspaceRoot, openAIBaseURL string, useOpenAIBaseURL bool) (config.App, error) {
	if useOpenAIBaseURL {
		loadOpts.OpenAIBaseURL = openAIBaseURL
	} else {
		loadOpts.OpenAIBaseURL = ""
	}
	if strings.TrimSpace(workspaceRoot) == "" {
		return config.LoadGlobal(loadOpts)
	}
	return config.Load(workspaceRoot, loadOpts)
}
