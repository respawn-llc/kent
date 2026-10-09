package app

import (
	"context"
	"errors"

	"core/shared/config"
	"core/shared/protoapi"
	authpb "core/shared/protoapi/gen/kent/api/auth"
)

const (
	connectionStepTemplate           onboardingStepID = "connection_template"
	connectionStepID                 onboardingStepID = "connection_id"
	connectionStepEndpoint           onboardingStepID = "connection_endpoint"
	connectionStepEnvironment        onboardingStepID = "connection_environment"
	connectionStepBrowserAuth        onboardingStepID = "connection_browser_auth"
	connectionStepAuthMethod         onboardingStepID = "connection_auth_method"
	connectionEnvironmentExplanation                  = "Don't paste your API key here. This is the name of the **environment variable** Kent will read **at the server's location** to get the api key from. Alternatively, place it in a ~/.kent/.env file."
)

type connectionTemplate string

const (
	connectionTemplateSubscription       connectionTemplate = "subscription"
	connectionTemplateSubscriptionDevice connectionTemplate = "subscription_device"
	connectionTemplateAPI                connectionTemplate = "api"
	connectionTemplateAnonymous          connectionTemplate = "anonymous"
	connectionTemplateOpenAIAPI          connectionTemplate = "openai_api"
	connectionTemplateGrokSubscription   connectionTemplate = "grok_subscription"
	connectionTemplateGrokAPI            connectionTemplate = "grok_api"
)

type connectionForm struct {
	template   connectionTemplate
	id         config.ConnectionID
	definition config.ProviderConnection
	catalog    map[config.ConnectionID]config.ProviderConnection
}

func newConnectionForm(catalog *authpb.ConnectionCatalog) (*connectionForm, error) {
	form := &connectionForm{catalog: map[config.ConnectionID]config.ProviderConnection{}}
	for _, value := range catalog.Connections {
		id, definition, err := protoapi.ConnectionFromProto(value)
		if err != nil {
			return nil, err
		}
		form.catalog[id] = definition
	}
	form.selectTemplate(connectionTemplateSubscription)
	if catalog.PendingSetup != nil {
		id, definition, err := protoapi.ConnectionFromProto(catalog.PendingSetup)
		if err != nil {
			return nil, err
		}
		form.id, form.definition = id, definition
		form.template = connectionTemplateFor(definition)
	}
	return form, nil
}

func connectionTemplateFor(definition config.ProviderConnection) connectionTemplate {
	switch definition.Protocol {
	case config.ConnectionGrokCLIProxy, config.ConnectionGrokOAuthAPI:
		return connectionTemplateGrokSubscription
	case config.ConnectionGrokAPIKey:
		return connectionTemplateGrokAPI
	}
	if definition.Protocol.IsSubscription() {
		return connectionTemplateSubscription
	}
	if definition.EnvironmentVariable != nil {
		if definition.Endpoint != nil && *definition.Endpoint == config.DefaultOpenAIResponsesEndpoint {
			return connectionTemplateOpenAIAPI
		}
		return connectionTemplateAPI
	}
	return connectionTemplateAnonymous
}

func (f *connectionForm) selectTemplate(template connectionTemplate) {
	if f.template == template {
		return
	}
	f.template = template
	switch template {
	case connectionTemplateSubscription, connectionTemplateSubscriptionDevice:
		f.definition = config.ProviderConnection{Protocol: config.ConnectionChatGPT}
	case connectionTemplateGrokSubscription:
		f.definition = config.ProviderConnection{Protocol: config.ConnectionGrokCLIProxy}
	case connectionTemplateGrokAPI:
		f.definition = config.ProviderConnection{Protocol: config.ConnectionGrokAPIKey}
	case connectionTemplateOpenAIAPI:
		endpoint := config.DefaultOpenAIResponsesEndpoint
		f.definition = config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: &endpoint}
	default:
		f.definition = config.ProviderConnection{Protocol: config.ConnectionResponses}
	}
	f.id = config.SuggestConnectionID(f.definition.Protocol, f.catalog)
}

func (f *connectionForm) steps() []onboardingStepDefinition {
	finish := func(state *onboardingFlowState) error {
		state.pendingAction = onboardingPendingActionConnectionComplete
		return nil
	}
	return []onboardingStepDefinition{
		{id: connectionStepTemplate, build: func(*onboardingFlowState) onboardingScreen {
			return onboardingScreen{ID: connectionStepTemplate, Kind: onboardingScreenChoice, Title: "Choose the inference provider to add",
				DefaultOptionID: string(f.template), Options: []onboardingOption{
					{ID: string(connectionTemplateSubscription), Group: "OpenAI", Title: "Subscription — browser"},
					{ID: string(connectionTemplateSubscriptionDevice), Group: "OpenAI", Title: "Subscription — device code"},
					{ID: string(connectionTemplateOpenAIAPI), Group: "OpenAI", Title: "API Key"},
					{ID: string(connectionTemplateGrokSubscription), Group: "Grok", Title: "Subscription"},
					{ID: string(connectionTemplateGrokAPI), Group: "Grok", Title: "API Key"},
					{ID: string(connectionTemplateAPI), Group: "Generic", Title: "Responses-compatible API key"},
					{ID: string(connectionTemplateAnonymous), Group: "Generic", Title: "No auth"},
				}}
		}, apply: func(_ *onboardingFlowState, value string) error {
			switch template := connectionTemplate(value); template {
			case connectionTemplateSubscription, connectionTemplateSubscriptionDevice, connectionTemplateOpenAIAPI, connectionTemplateGrokSubscription, connectionTemplateGrokAPI, connectionTemplateAPI, connectionTemplateAnonymous:
				f.selectTemplate(template)
				return nil
			default:
				return errors.New("choose a connection template")
			}
		}},
		{id: connectionStepID, build: func(*onboardingFlowState) onboardingScreen {
			return onboardingScreen{ID: connectionStepID, Kind: onboardingScreenInput, Title: "Name this connection", InputValue: string(f.id),
				Helper: "Use lowercase letters, numbers, hyphens, or underscores."}
		}, apply: func(state *onboardingFlowState, value string) error {
			f.id = config.ConnectionID(value)
			if f.definition.Protocol.IsSubscription() {
				return finish(state)
			}
			return nil
		}},
		{id: connectionStepEndpoint, visible: func(*onboardingFlowState) bool {
			return f.template == connectionTemplateAPI || f.template == connectionTemplateAnonymous
		},
			build: func(*onboardingFlowState) onboardingScreen {
				value := ""
				if f.definition.Endpoint != nil {
					value = *f.definition.Endpoint
				}
				return onboardingScreen{ID: connectionStepEndpoint, Kind: onboardingScreenInput, Title: "Responses endpoint", InputValue: value, Helper: "Enter the full HTTP or HTTPS base URL, including /v1 if your server requires it."}
			}, apply: func(state *onboardingFlowState, value string) error {
				f.definition.Endpoint = &value
				if f.template == connectionTemplateAnonymous {
					return finish(state)
				}
				return nil
			}},
		{id: connectionStepEnvironment, visible: func(*onboardingFlowState) bool {
			return !f.definition.Protocol.IsSubscription() && f.template != connectionTemplateAnonymous
		},
			build: func(state *onboardingFlowState) onboardingScreen {
				value := ""
				if f.definition.EnvironmentVariable != nil {
					value = *f.definition.EnvironmentVariable
				}
				return onboardingScreen{ID: connectionStepEnvironment, Kind: onboardingScreenInput, Title: "API-key environment variable", InputValue: value,
					Body: newStartupMarkdownRendererWithWordWrap(state.selections.themeValue()).Render(connectionEnvironmentExplanation, defaultPickerWidth)}
			}, apply: func(state *onboardingFlowState, value string) error {
				if value == "" {
					return errors.New("Enter the environment-variable name that holds your API key, not the API key itself.")
				}
				f.definition.EnvironmentVariable = &value
				return finish(state)
			}},
	}
}

func (f *connectionForm) authenticationMethod() *authMethodChoice {
	var choice authMethodChoice
	switch f.template {
	case connectionTemplateSubscription:
		choice = authMethodChoiceBrowserAuto
	case connectionTemplateSubscriptionDevice, connectionTemplateGrokSubscription:
		choice = authMethodChoiceDevice
	default:
		return nil
	}
	return &choice
}

func newConnectionFormModel(selectedTheme string, catalog *authpb.ConnectionCatalog, includeTheme bool) (*connectionForm, *onboardingModel, error) {
	form, err := newConnectionForm(catalog)
	if err != nil {
		return nil, nil, err
	}
	theme, err := seedThemeSelection(selectedTheme)
	if err != nil {
		return nil, nil, err
	}
	state := onboardingFlowState{selections: onboardingSelections{theme: theme}, pendingAction: onboardingPendingActionNone}
	steps := form.steps()
	if includeTheme {
		themeStep := newOnboardingWorkflow(&state).steps[0]
		steps = append([]onboardingStepDefinition{themeStep}, steps...)
	}
	model := newOnboardingFormModel(state, onboardingWorkflow{steps: steps})
	return form, model, nil
}

func runConnectionForm(ctx context.Context, model *onboardingModel, submit func() error) error {
	for {
		model.state.pendingAction = onboardingPendingActionNone
		if _, err := runOnboardingProgram(ctx, model); err != nil {
			return err
		}
		if model.canceled {
			return ErrAuthCanceledByUser
		}
		if model.terminalErr != nil {
			return model.terminalErr
		}
		err := submit()
		if errors.Is(err, ErrAuthBack) && ctx.Err() == nil {
			if err := showConnectionSelection(model); err != nil {
				return err
			}
			continue
		}
		if err == nil || errors.Is(err, ErrAuthCanceledByUser) || ctx.Err() != nil {
			return err
		}
		model.errorText = connectionOperationErrorText(err)
		model.syncScreen(false)
	}
}

func showConnectionSelection(model *onboardingModel) error {
	for index, step := range model.workflow.visibleSteps(&model.state) {
		if step.id == connectionStepTemplate {
			model.stepIndex = index
			model.syncScreen(true)
			return model.terminalErr
		}
	}
	return errors.New("connection form has no provider selection step")
}
