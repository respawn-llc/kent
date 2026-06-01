package main

import (
	"context"
	"errors"

	"builder/shared/serverapi"
)

func (bindingCommandTimeoutProjectViewStub) GetProjectEdit(context.Context, serverapi.ProjectEditGetRequest) (serverapi.ProjectEditGetResponse, error) {
	return serverapi.ProjectEditGetResponse{}, errors.New("unexpected GetProjectEdit call")
}

func (bindingCommandTimeoutProjectViewStub) UpdateProject(context.Context, serverapi.ProjectUpdateRequest) (serverapi.ProjectUpdateResponse, error) {
	return serverapi.ProjectUpdateResponse{}, errors.New("unexpected UpdateProject call")
}

func (bindingCommandTimeoutProjectViewStub) SetDefaultWorkspace(context.Context, serverapi.ProjectDefaultWorkspaceSetRequest) (serverapi.ProjectDefaultWorkspaceSetResponse, error) {
	return serverapi.ProjectDefaultWorkspaceSetResponse{}, errors.New("unexpected SetDefaultWorkspace call")
}

func (bindingCommandTimeoutProjectViewStub) UnlinkWorkspaceFromProject(context.Context, serverapi.ProjectWorkspaceUnlinkRequest) (serverapi.ProjectWorkspaceUnlinkResponse, error) {
	return serverapi.ProjectWorkspaceUnlinkResponse{}, errors.New("unexpected UnlinkWorkspaceFromProject call")
}

func (bindingCommandTimeoutProjectViewStub) DeleteProject(context.Context, serverapi.ProjectDeleteRequest) (serverapi.ProjectDeleteResponse, error) {
	return serverapi.ProjectDeleteResponse{}, errors.New("unexpected DeleteProject call")
}
