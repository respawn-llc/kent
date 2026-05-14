package serverbridge

// Package serverbridge is the documented CLI composition bridge for local
// embedded/server process wiring. UI and command packages depend on shared
// contracts plus these narrow functions instead of importing server packages.

import (
	"context"

	"builder/server/auth"
	"builder/server/authflow"
	serverbootstrap "builder/server/bootstrap"
	serverembedded "builder/server/embedded"
	"builder/server/generated"
	"builder/server/llm"
	serveronboarding "builder/server/onboarding"
	"builder/server/runtime"
	"builder/server/serve"
	serverstartup "builder/server/startup"
	"builder/shared/config"
)

type AuthStore = auth.Store
type AuthManager = auth.Manager
type InteractionRequest = authflow.InteractionRequest
type InteractionOutcome = authflow.InteractionOutcome
type OAuthOptions = auth.OpenAIOAuthOptions
type DeviceCode = auth.DeviceCode
type BrowserCallback = auth.BrowserCallback
type BrowserAuthSession = auth.BrowserAuthSession
type DeviceAuthorizationGrant = auth.DeviceAuthorizationGrant
type OAuthCallbackListener = auth.OAuthCallbackListener
type Server = serverembedded.Server
type StartupRequest = serverstartup.Request
type StartupAuthHandler = serverstartup.AuthHandler
type StartupOnboardingHandler = serverstartup.OnboardingHandler
type StartupOnboardingRequest = serverstartup.OnboardingRequest
type ServeServer = serve.Server
type SkillInspection = runtime.SkillInspection
type OnboardingResult = serveronboarding.Result
type BootstrapRequest = serverbootstrap.Request
type BootstrapConfigPlan = serverbootstrap.ConfigPlan

func WrapStoreWithEnvAPIKeyOverride(base auth.Store, lookupEnv func(string) string) auth.Store {
	return authflow.WrapStoreWithEnvAPIKeyOverride(base, lookupEnv)
}

func EnsureEmptyStartupReady() error {
	return auth.EnsureStartupReady(auth.EmptyState())
}

func OpenBrowser(url string) error {
	return auth.OpenBrowser(url)
}

func StartOAuthCallbackListener() (*auth.OAuthCallbackListener, error) {
	return auth.StartOAuthCallbackListener()
}

func RunOpenAIDeviceCodeFlow(ctx context.Context, opts auth.OpenAIOAuthOptions, onCode func(auth.DeviceCode)) (auth.Method, error) {
	return auth.RunOpenAIDeviceCodeFlow(ctx, opts, onCode)
}

func BeginOpenAIBrowserFlow(opts auth.OpenAIOAuthOptions, redirectURI string) (auth.BrowserAuthSession, error) {
	return auth.BeginOpenAIBrowserFlow(opts, redirectURI)
}

func CompleteOpenAIBrowserFlow(ctx context.Context, opts auth.OpenAIOAuthOptions, session auth.BrowserAuthSession, callbackInput string) (auth.Method, error) {
	return auth.CompleteOpenAIBrowserFlow(ctx, opts, session, callbackInput)
}

func ParseOAuthCallbackInput(callbackInput string) (auth.BrowserCallback, error) {
	return auth.ParseOAuthCallbackInput(callbackInput)
}

func CollectOpenAIDeviceAuthorizationGrant(ctx context.Context, opts auth.OpenAIOAuthOptions, onCode func(auth.DeviceCode)) (auth.DeviceAuthorizationGrant, error) {
	return auth.CollectOpenAIDeviceAuthorizationGrant(ctx, opts, onCode)
}

func ProviderCapabilitiesForSettings(authState auth.State, settings config.Settings) (llm.ProviderCapabilities, error) {
	return llm.ProviderCapabilitiesForSettings(authState, settings)
}

func LookupModelMetadata(model string) (llm.ModelMetadata, bool) {
	return llm.LookupModelMetadata(model)
}

func SupportsLargeContextWindowModel(model string) bool {
	return llm.SupportsLargeContextWindowModel(model)
}

func SupportsReasoningEffortModel(model string) bool {
	return llm.SupportsReasoningEffortModel(model)
}

func SupportedThinkingLevelsModel(model string) []string {
	return llm.SupportedThinkingLevelsModel(model)
}

func SupportsVerbosityModel(model string) bool {
	return llm.SupportsVerbosityModel(model)
}

func SupportedVerbosityLevelsModel(model string) []string {
	return llm.SupportedVerbosityLevelsModel(model)
}

func ApplyDerivedModelContextBudget(settings *config.Settings, model string, baselineWindow int, baselineThreshold int) {
	llm.ApplyDerivedModelContextBudget(settings, model, baselineWindow, baselineThreshold)
}

func ModelDisplayLabel(model string, thinkingLevel string) string {
	return llm.ModelDisplayLabel(model, thinkingLevel)
}

func ParseSkillMetadata(path string) (runtime.SkillMetadata, bool) {
	return runtime.ParseSkillMetadata(path)
}

func InspectSkills(workspaceRoot string, disabledSkills map[string]bool) ([]runtime.SkillInspection, error) {
	return runtime.InspectSkills(workspaceRoot, disabledSkills)
}

func InstalledAgentsPaths(workspaceRoot string) ([]string, error) {
	return runtime.InstalledAgentsPaths(workspaceRoot)
}

func RecoveredRootNonEmpty() (bool, error) {
	return generated.RecoveredRootNonEmpty()
}

func RecoveredWarning() string {
	return generated.RecoveredWarning()
}

func ResolveConfig(req BootstrapRequest) (BootstrapConfigPlan, error) {
	return serverbootstrap.ResolveConfig(req)
}

func StartEmbedded(ctx context.Context, req StartupRequest, authHandler StartupAuthHandler, onboardingHandler StartupOnboardingHandler) (*Server, error) {
	return serverstartup.Start(ctx, req, authHandler, onboardingHandler)
}

func NewHeadlessHandlers(lookupEnv func(string) string) (StartupAuthHandler, StartupOnboardingHandler) {
	return serverstartup.NewHeadlessHandlers(lookupEnv)
}

func StartServe(ctx context.Context, req StartupRequest, authHandler StartupAuthHandler, onboardingHandler StartupOnboardingHandler) (*ServeServer, error) {
	return serve.Start(ctx, req, authHandler, onboardingHandler)
}

func ReleaseServeReservation(cfg config.App) {
	serve.ReleaseTestListenReservation(config.ServerListenAddress(cfg))
}

func EnsureOnboardingReady(ctx context.Context, cfg config.App, mgr *auth.Manager, interactive bool, reloadConfig func() (config.App, error), runner serveronboarding.InteractiveRunner) (config.App, bool, error) {
	return serveronboarding.EnsureReady(ctx, cfg, mgr, interactive, reloadConfig, runner)
}
