package client

import (
	"context"
	"errors"

	"builder/shared/serverapi"
)

type ProjectViewClient interface {
	ListProjects(ctx context.Context, req serverapi.ProjectListRequest) (serverapi.ProjectListResponse, error)
	ResolveProjectPath(ctx context.Context, req serverapi.ProjectResolvePathRequest) (serverapi.ProjectResolvePathResponse, error)
	PlanWorkspaceBinding(ctx context.Context, req serverapi.ProjectBindingPlanRequest) (serverapi.ProjectBindingPlanResponse, error)
	CreateProject(ctx context.Context, req serverapi.ProjectCreateRequest) (serverapi.ProjectCreateResponse, error)
	AttachWorkspaceToProject(ctx context.Context, req serverapi.ProjectAttachWorkspaceRequest) (serverapi.ProjectAttachWorkspaceResponse, error)
	RebindWorkspace(ctx context.Context, req serverapi.ProjectRebindWorkspaceRequest) (serverapi.ProjectRebindWorkspaceResponse, error)
	GetProjectOverview(ctx context.Context, req serverapi.ProjectGetOverviewRequest) (serverapi.ProjectGetOverviewResponse, error)
	ListSessionsByProject(ctx context.Context, req serverapi.SessionListByProjectRequest) (serverapi.SessionListByProjectResponse, error)
}

type loopbackProjectViewClient struct {
	service serverapi.ProjectViewService
}

func NewLoopbackProjectViewClient(service serverapi.ProjectViewService) ProjectViewClient {
	return &loopbackProjectViewClient{service: service}
}

func (c *loopbackProjectViewClient) ListProjects(ctx context.Context, req serverapi.ProjectListRequest) (serverapi.ProjectListResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.ProjectListResponse{}, errors.New("project view service is required")
	}
	return c.service.ListProjects(ctx, req)
}

func (c *loopbackProjectViewClient) ResolveProjectPath(ctx context.Context, req serverapi.ProjectResolvePathRequest) (serverapi.ProjectResolvePathResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.ProjectResolvePathResponse{}, errors.New("project view service is required")
	}
	return c.service.ResolveProjectPath(ctx, req)
}

func (c *loopbackProjectViewClient) PlanWorkspaceBinding(ctx context.Context, req serverapi.ProjectBindingPlanRequest) (serverapi.ProjectBindingPlanResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.ProjectBindingPlanResponse{}, errors.New("project view service is required")
	}
	return c.service.PlanWorkspaceBinding(ctx, req)
}

func (c *loopbackProjectViewClient) CreateProject(ctx context.Context, req serverapi.ProjectCreateRequest) (serverapi.ProjectCreateResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.ProjectCreateResponse{}, errors.New("project view service is required")
	}
	return c.service.CreateProject(ctx, req)
}

func (c *loopbackProjectViewClient) AttachWorkspaceToProject(ctx context.Context, req serverapi.ProjectAttachWorkspaceRequest) (serverapi.ProjectAttachWorkspaceResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.ProjectAttachWorkspaceResponse{}, errors.New("project view service is required")
	}
	return c.service.AttachWorkspaceToProject(ctx, req)
}

func (c *loopbackProjectViewClient) RebindWorkspace(ctx context.Context, req serverapi.ProjectRebindWorkspaceRequest) (serverapi.ProjectRebindWorkspaceResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.ProjectRebindWorkspaceResponse{}, errors.New("project view service is required")
	}
	return c.service.RebindWorkspace(ctx, req)
}

func (c *loopbackProjectViewClient) GetProjectOverview(ctx context.Context, req serverapi.ProjectGetOverviewRequest) (serverapi.ProjectGetOverviewResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.ProjectGetOverviewResponse{}, errors.New("project view service is required")
	}
	return c.service.GetProjectOverview(ctx, req)
}

func (c *loopbackProjectViewClient) ListSessionsByProject(ctx context.Context, req serverapi.SessionListByProjectRequest) (serverapi.SessionListByProjectResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.SessionListByProjectResponse{}, errors.New("project view service is required")
	}
	return c.service.ListSessionsByProject(ctx, req)
}
