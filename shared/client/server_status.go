package client

import (
	"context"
	"errors"

	"builder/shared/serverapi"
	"builder/shared/servicecontract"
)

type ServerStatusClient interface {
	GetServerReadiness(ctx context.Context, req serverapi.ServerReadinessRequest) (serverapi.ServerReadinessResponse, error)
	GetServerCapabilities(ctx context.Context, req serverapi.ServerCapabilitiesRequest) (serverapi.ServerCapabilitiesResponse, error)
}

type loopbackServerStatusClient struct {
	service servicecontract.ServerStatusService
}

func NewLoopbackServerStatusClient(service servicecontract.ServerStatusService) ServerStatusClient {
	return &loopbackServerStatusClient{service: service}
}

func (c *loopbackServerStatusClient) GetServerReadiness(ctx context.Context, req serverapi.ServerReadinessRequest) (serverapi.ServerReadinessResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.ServerReadinessResponse{}, errors.New("server status service is required")
	}
	return c.service.GetServerReadiness(ctx, req)
}

func (c *loopbackServerStatusClient) GetServerCapabilities(ctx context.Context, req serverapi.ServerCapabilitiesRequest) (serverapi.ServerCapabilitiesResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.ServerCapabilitiesResponse{}, errors.New("server status service is required")
	}
	return c.service.GetServerCapabilities(ctx, req)
}
