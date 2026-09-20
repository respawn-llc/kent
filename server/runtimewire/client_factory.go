package runtimewire

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	"core/server/authservice"
	"core/server/llm"
	"core/shared/config"
	"core/shared/toolspec"
)

var ErrRuntimeClientFactoryConflict = errors.New("runtime client factory cannot be combined with direct client override")

type RuntimeClientPurpose int

const (
	RuntimeClientPurposeMain RuntimeClientPurpose = iota + 1
	RuntimeClientPurposeReviewer
	RuntimeClientPurposeWorkflow
)

type RuntimeClientRequest struct {
	Purpose        RuntimeClientPurpose
	SessionID      string
	ActiveSettings config.Settings
	EnabledTools   []toolspec.ID
	Sources        map[string]config.Origin
	Connection     authservice.ResolvedConnection
}

type RuntimeClientFactory interface {
	NewRuntimeClient(context.Context, RuntimeClientRequest) (llm.Client, error)
}

type RuntimeClientFactoryFunc func(context.Context, RuntimeClientRequest) (llm.Client, error)

func (f RuntimeClientFactoryFunc) NewRuntimeClient(ctx context.Context, req RuntimeClientRequest) (llm.Client, error) {
	return f(ctx, req)
}

// NewRuntimeClient keeps the same resolved connection on both construction
// paths, including Workflow and Supervisor clients.
func NewRuntimeClient(ctx context.Context, factory RuntimeClientFactory, request RuntimeClientRequest) (llm.Client, error) {
	request.EnabledTools = append([]toolspec.ID(nil), request.EnabledTools...)
	request.Sources = maps.Clone(request.Sources)
	if factory != nil {
		client, err := factory.NewRuntimeClient(ctx, request)
		if err != nil {
			return nil, err
		}
		if client == nil {
			return nil, fmt.Errorf("runtime client factory returned nil client for purpose %d", request.Purpose)
		}
		return client, nil
	}
	active := request.ActiveSettings
	connection := request.Connection
	endpoint := ""
	if connection.Definition.Endpoint != nil {
		endpoint = *connection.Definition.Endpoint
	}
	capabilities, err := llm.ResolveConnectionCapabilities(connection.Definition)
	if err != nil {
		return nil, err
	}
	return llm.NewProviderClient(llm.ProviderClientOptions{
		Provider: llm.ProviderOpenAI, Model: active.Model, Auth: connection.Auth,
		HTTPClient:    llm.NewProviderHTTPClient(endpoint, time.Duration(active.Timeouts.ModelRequestSeconds)*time.Second),
		OpenAIBaseURL: endpoint, ModelVerbosity: string(active.ModelVerbosity),
		ProviderIdentifier: &active.ProviderIdentifier, Store: active.Store,
		ContextWindowTokens: active.ModelContextWindow, ProviderCapabilitiesOverride: &capabilities,
	})
}

func cloneSources(sources map[string]config.Origin) map[string]config.Origin {
	return maps.Clone(sources)
}
