package runtimewire

import (
	"context"
	"fmt"

	"core/server/llm"
	"core/server/runtime"
	"core/server/session"
	"core/shared/config"
)

func connectionMaterializer(
	store *session.Store,
	active config.Settings,
	opts RuntimeWiringOptions,
	newClient func(config.Settings, RuntimeClientPurpose, RuntimeClientFactory, llm.ProviderCapabilities) (llm.Client, error),
) runtime.ConnectionResolver {
	return func(_ context.Context, request runtime.ConnectionSelectionRequest) (*runtime.PreparedConnection, error) {
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
		id, err := rotation.SelectReplacement(active, opts.ConnectionScope, request.Visited)
		if err != nil {
			return nil, err
		}
		settings := active
		settings.Connection = config.SingleConnection(id)
		capabilities, err := llm.ResolveEffectiveProviderCapabilities(store.Meta().Locked, settings)
		if err != nil {
			return nil, err
		}
		definition, err := settings.SelectedConnection()
		if err != nil {
			return nil, err
		}
		transportCapabilities, err := llm.ResolveConnectionCapabilities(definition)
		if err != nil {
			return nil, err
		}
		estimator, err := llm.ResolveConnectionTokenEstimator(definition)
		if err != nil {
			return nil, err
		}
		variant, err := llm.ResolveConnectionVariant(definition)
		if err != nil {
			return nil, err
		}
		client, err := newClient(settings, RuntimeClientPurposeMain, opts.ClientFactory, capabilities)
		if err != nil {
			return nil, err
		}
		return &runtime.PreparedConnection{
			ID: id, Client: client, TokenEstimator: estimator,
			RequestCapabilities: capabilities, TransportCapabilities: transportCapabilities,
			RawImageOriginalDetail: variant.Variant.Capabilities.IsOpenAIFirstParty,
		}, nil
	}
}
