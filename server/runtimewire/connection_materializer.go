package runtimewire

import (
	"context"
	"fmt"

	"core/server/llm"
	"core/server/runtime"
	"core/server/session"
	"core/shared/config"
)

func materializeConnection(
	store *session.Store,
	settings config.Settings,
	opts RuntimeWiringOptions,
	newClient func(config.Settings, RuntimeClientPurpose, RuntimeClientFactory, llm.ProviderCapabilities) (llm.Client, error),
) (*runtime.PreparedConnection, error) {
	capabilities, err := llm.ResolveEffectiveProviderCapabilities(store.Meta().Locked, settings)
	if err != nil {
		return nil, err
	}
	if opts.ProviderCapabilitiesOverride != nil {
		capabilities = *opts.ProviderCapabilitiesOverride
	}
	definition, err := settings.SelectedConnection()
	if err != nil {
		return nil, err
	}
	id, err := settings.Connection.ConcreteID()
	if err != nil {
		return nil, err
	}
	transportCapabilities, err := llm.ResolveConnectionCapabilities(definition)
	if err != nil {
		return nil, err
	}
	capabilities = llm.EffectiveProviderCapabilities(&capabilities, transportCapabilities)
	estimator, err := llm.ResolveConnectionTokenEstimator(definition)
	if err != nil {
		return nil, err
	}
	variant, err := llm.ResolveConnectionVariant(definition)
	if err != nil {
		return nil, err
	}
	client := opts.Client
	if client == nil {
		client, err = newClient(settings, RuntimeClientPurposeMain, opts.ClientFactory, capabilities)
		if err != nil {
			return nil, err
		}
	}
	return &runtime.PreparedConnection{
		ID: id, Client: client, TokenEstimator: estimator,
		RequestCapabilities: capabilities, TransportCapabilities: transportCapabilities,
		RawImageOriginalDetail: variant.Variant.Capabilities.IsOpenAIFirstParty,
	}, nil
}

func connectionMaterializer(
	store *session.Store,
	active config.Settings,
	opts RuntimeWiringOptions,
	newClient func(config.Settings, RuntimeClientPurpose, RuntimeClientFactory, llm.ProviderCapabilities) (llm.Client, error),
) runtime.ConnectionResolver {
	return func(ctx context.Context, request runtime.ConnectionSelectionRequest) (*runtime.PreparedConnection, error) {
		rotation := opts.ConnectionRotation
		if !rotation.FallbackEnabled() {
			return nil, nil
		}
		switch request.Reason {
		case runtime.ConnectionBeforeDispatch:
			if !rotation.CoolingDown(request.Current) {
				return nil, nil
			}
		case runtime.ConnectionUsageExhausted:
			if request.Failed == nil {
				return nil, fmt.Errorf("exhausted connection identity is required")
			}
			rotation.RecordExhaustion(*request.Failed)
			_, visited := request.Visited[request.Current]
			if *request.Failed != request.Current && !visited && !rotation.CoolingDown(request.Current) {
				return nil, nil
			}
		default:
			return nil, fmt.Errorf("invalid connection selection reason %d", request.Reason)
		}
		id, err := rotation.SelectReplacement(ctx, active, opts.ConnectionScope, request.Visited)
		if err != nil {
			return nil, err
		}
		if id == request.Current {
			return nil, nil
		}
		settings := active
		settings.Connection = config.SingleConnection(id)
		replacementOptions := opts
		replacementOptions.Client = nil
		return materializeConnection(store, settings, replacementOptions, newClient)
	}
}
