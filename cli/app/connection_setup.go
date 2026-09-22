package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"core/shared/config"
	"core/shared/protoapi"
	authpb "core/shared/protoapi/gen/kent/api/auth"
)

const (
	connectionStepTemplate           onboardingStepID = "connection_template"
	connectionStepID                 onboardingStepID = "connection_id"
	connectionStepEndpoint           onboardingStepID = "connection_endpoint"
	connectionStepEnvironment        onboardingStepID = "connection_environment"
	connectionEnvironmentExplanation                  = "Don't paste your API key here. This is the name of the **environment variable** Kent will read **at the server's location** to get the api key from."
)

type connectionTemplate string

const (
	connectionTemplateSubscription connectionTemplate = "subscription"
	connectionTemplateAPI          connectionTemplate = "api"
	connectionTemplateAnonymous    connectionTemplate = "anonymous"
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
	if definition.Protocol == config.ConnectionChatGPT {
		return connectionTemplateSubscription
	}
	if definition.EnvironmentVariable != nil {
		return connectionTemplateAPI
	}
	return connectionTemplateAnonymous
}

func (f *connectionForm) selectTemplate(template connectionTemplate) {
	if f.template == template {
		return
	}
	f.template = template
	f.definition = config.ProviderConnection{Protocol: config.ConnectionChatGPT}
	if template != connectionTemplateSubscription {
		f.definition.Protocol = config.ConnectionResponses
	}
	if template == connectionTemplateAPI {
		endpoint := config.DefaultOpenAIResponsesEndpoint
		f.definition.Endpoint = &endpoint
	}
	f.id = config.SuggestConnectionID(f.definition.Protocol, f.catalog)
}

func (f *connectionForm) steps() []onboardingStepDefinition {
	finish := func(state *onboardingFlowState) error {
		if err := f.definition.Validate(); err != nil {
			return errors.New("Check the connection address and environment variable name.")
		}
		state.pendingAction = onboardingPendingActionConnectionComplete
		return nil
	}
	return []onboardingStepDefinition{
		{id: connectionStepTemplate, build: func(*onboardingFlowState) onboardingScreen {
			return onboardingScreen{ID: connectionStepTemplate, Kind: onboardingScreenChoice, Title: "Choose a provider connection",
				DefaultOptionID: string(f.template), Options: []onboardingOption{
					{ID: string(connectionTemplateSubscription), Title: "ChatGPT subscription"},
					{ID: string(connectionTemplateAPI), Title: "API-key Responses-compatible"},
					{ID: string(connectionTemplateAnonymous), Title: "Auth-less Responses-compatible"},
				}}
		}, apply: func(_ *onboardingFlowState, value string) error {
			switch template := connectionTemplate(value); template {
			case connectionTemplateSubscription, connectionTemplateAPI, connectionTemplateAnonymous:
				f.selectTemplate(template)
				return nil
			default:
				return errors.New("choose a connection template")
			}
		}},
		{id: connectionStepID, build: func(*onboardingFlowState) onboardingScreen {
			return onboardingScreen{ID: connectionStepID, Kind: onboardingScreenInput, Title: "Name this connection", InputValue: string(f.id),
				Helper: "Start with a lowercase letter. Use lowercase letters, numbers, hyphens, or underscores."}
		}, apply: func(state *onboardingFlowState, value string) error {
			id, err := config.ParseConnectionID(value)
			if err != nil {
				return errors.New("Start the ID with a lowercase letter and use only lowercase letters, numbers, hyphens, or underscores.")
			}
			if _, exists := f.catalog[id]; exists {
				return fmt.Errorf("Connection %q already exists.", id)
			}
			f.id = id
			if f.template == connectionTemplateSubscription {
				return finish(state)
			}
			return nil
		}},
		{id: connectionStepEndpoint, visible: func(*onboardingFlowState) bool { return f.template != connectionTemplateSubscription },
			build: func(*onboardingFlowState) onboardingScreen {
				value := ""
				if f.definition.Endpoint != nil {
					value = *f.definition.Endpoint
				}
				return onboardingScreen{ID: connectionStepEndpoint, Kind: onboardingScreenInput, Title: "Responses endpoint", InputValue: value, Helper: "Enter the full HTTP or HTTPS base URL, including /v1 if your server requires it."}
			}, apply: func(state *onboardingFlowState, value string) error {
				definition := f.definition
				definition.Endpoint = &value
				if err := definition.Validate(); err != nil {
					return errors.New("Enter an absolute HTTP or HTTPS endpoint.")
				}
				f.definition = definition
				if f.template == connectionTemplateAnonymous {
					return finish(state)
				}
				return nil
			}},
		{id: connectionStepEnvironment, visible: func(*onboardingFlowState) bool { return f.template == connectionTemplateAPI },
			build: func(state *onboardingFlowState) onboardingScreen {
				value := ""
				if f.definition.EnvironmentVariable != nil {
					value = *f.definition.EnvironmentVariable
				}
				return onboardingScreen{ID: connectionStepEnvironment, Kind: onboardingScreenInput, Title: "API-key environment variable", InputValue: value,
					Body: newStartupMarkdownRendererWithWordWrap(state.selections.themeValue()).Render(connectionEnvironmentExplanation, defaultPickerWidth)}
			}, apply: func(state *onboardingFlowState, value string) error {
				if strings.TrimSpace(value) == "" {
					return errors.New("Enter the environment variable name.")
				}
				f.definition.EnvironmentVariable = &value
				return finish(state)
			}},
	}
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
		if err == nil || errors.Is(err, ErrAuthCanceledByUser) || ctx.Err() != nil {
			return err
		}
		model.errorText = connectionOperationErrorText(err)
		model.syncScreen(false)
	}
}
