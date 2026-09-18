package client

import (
	"context"
	"fmt"

	"core/shared/protoapi"
	projectpb "core/shared/protoapi/gen/kent/api/project"
	"core/shared/serverapi"

	"google.golang.org/protobuf/proto"
)

func (c *Remote) CreateProject(ctx context.Context, request *projectpb.CreateProjectRequest) (*projectpb.CreateProjectSuccess, error) {
	return callGeneratedBinary(c, ctx, projectCatalogMethod("Create"), request,
		&projectpb.CreateProjectResult{},
		projectCreateGeneratedError)
}

func (c *Remote) UpdateProject(ctx context.Context, request *projectpb.UpdateProjectRequest) (*projectpb.UpdateProjectSuccess, error) {
	return callGeneratedBinary(c, ctx, projectCatalogMethod("Update"), request,
		&projectpb.UpdateProjectResult{},
		projectUpdateGeneratedError)
}

func (c *Remote) SetDefaultWorkspace(ctx context.Context, request *projectpb.SetDefaultWorkspaceRequest) (*projectpb.SetDefaultWorkspaceSuccess, error) {
	return callGeneratedBinary(c, ctx, projectCatalogMethod("SetDefaultWorkspace"), request,
		&projectpb.SetDefaultWorkspaceResult{},
		func(failure *projectpb.SetDefaultWorkspaceError) error {
			return projectWorkspaceMutationGeneratedError(
				failure.Code,
				failure.GetProjectNotFound(),
				failure.GetWorkspaceNotRegistered(),
				failure.GetWorkspacePathIdentity(),
				nil,
				failure.GetWorkspaceMutationFailed(),
			)
		})
}

func (c *Remote) UnlinkWorkspaceFromProject(ctx context.Context, request *projectpb.UnlinkWorkspaceRequest) (*projectpb.UnlinkWorkspaceSuccess, error) {
	response, err := callGeneratedBinary(c, ctx, projectCatalogMethod("UnlinkWorkspace"), request,
		&projectpb.UnlinkWorkspaceResult{},
		func(failure *projectpb.UnlinkWorkspaceError) error {
			return projectWorkspaceMutationGeneratedError(
				failure.Code,
				failure.GetProjectNotFound(),
				failure.GetWorkspaceNotRegistered(),
				failure.GetWorkspacePathIdentity(),
				failure.GetWorkspaceDetachConflict(),
				failure.GetWorkspaceMutationFailed(),
			)
		})
	if err != nil {
		return nil, err
	}
	if response.ProjectId != request.ProjectId {
		return nil, fmt.Errorf(
			"Project Workspace unlink response project %q does not match request project %q",
			response.ProjectId,
			request.ProjectId,
		)
	}
	return response, nil
}

func (c *Remote) DeleteProject(ctx context.Context, request *projectpb.DeleteProjectRequest) (*projectpb.DeleteProjectSuccess, error) {
	response, err := callGeneratedBinary(c, ctx, projectCatalogMethod("Delete"), request,
		&projectpb.DeleteProjectResult{},
		func(failure *projectpb.DeleteProjectError) error {
			if failure.Code == "auth_required" {
				return serverapi.ErrServerAuthRequired
			}
			return projectNotFoundGeneratedError(
				failure.Code, failure.GetProjectNotFound())
		})
	if err != nil {
		return nil, err
	}
	if response.ProjectId != request.ProjectId {
		return nil, fmt.Errorf(
			"Project delete response project %q does not match request project %q",
			response.ProjectId,
			request.ProjectId,
		)
	}
	return response, nil
}

func (c *Remote) AttachWorkspaceToProject(ctx context.Context, request *projectpb.AttachWorkspaceRequest) (*projectpb.AttachWorkspaceSuccess, error) {
	response, err := callGeneratedBinary(c, ctx, projectCatalogMethod("AttachWorkspace"), request,
		&projectpb.AttachWorkspaceResult{},
		projectAttachGeneratedError)
	if err != nil {
		return nil, err
	}
	if response.Binding.ProjectId != request.ProjectId {
		return nil, fmt.Errorf(
			"Project Workspace attach response project %q does not match request project %q",
			response.Binding.ProjectId,
			request.ProjectId,
		)
	}
	return response, nil
}

func (c *Remote) RebindWorkspace(ctx context.Context, request *projectpb.RebindWorkspaceRequest) (*projectpb.RebindWorkspaceSuccess, error) {
	return callGeneratedBinary(c, ctx, projectCatalogMethod("RebindWorkspace"), request,
		&projectpb.RebindWorkspaceResult{},
		func(failure *projectpb.RebindWorkspaceError) error {
			switch failure.Code {
			case "auth_required":
				return serverapi.ErrServerAuthRequired
			case "workspace_not_registered":
				return protoapi.WorkspaceNotRegisteredFromProto(failure.GetWorkspaceNotRegistered())
			case "workspace_binding_ambiguous":
				return protoapi.WorkspaceBindingAmbiguousMutationFromProto(failure.GetWorkspaceBindingAmbiguous())
			case "workspace_already_bound":
				return validateEmptyProjectMutationDetail(
					failure.GetWorkspaceAlreadyBound(), serverapi.ErrWorkspaceAlreadyBound)
			case "workspace_path_missing":
				return validateEmptyProjectMutationDetail(
					failure.GetWorkspacePathMissing(), serverapi.ErrWorkspacePathMissing)
			default:
				return generatedOperationFailure(failure.Code)
			}
		})
}

func projectCreateGeneratedError(failure *projectpb.CreateProjectError) error {
	switch failure.Code {
	case "auth_required":
		return serverapi.ErrServerAuthRequired
	case "project_key_conflict":
		return projectKeyConflictError(failure.GetProjectKeyConflict())
	case "workspace_already_bound":
		return validateEmptyProjectMutationDetail(failure.GetWorkspaceAlreadyBound(), serverapi.ErrWorkspaceAlreadyBound)
	case "workspace_path_missing":
		return validateEmptyProjectMutationDetail(failure.GetWorkspacePathMissing(), serverapi.ErrWorkspacePathMissing)
	default:
		return generatedOperationFailure(failure.Code)
	}
}

func projectUpdateGeneratedError(failure *projectpb.UpdateProjectError) error {
	switch failure.Code {
	case "auth_required":
		return serverapi.ErrServerAuthRequired
	case "project_not_found":
		return projectNotFoundError(failure.GetProjectNotFound())
	case "project_key_conflict":
		return projectKeyConflictError(failure.GetProjectKeyConflict())
	default:
		return generatedOperationFailure(failure.Code)
	}
}

func projectAttachGeneratedError(failure *projectpb.AttachWorkspaceError) error {
	switch failure.Code {
	case "auth_required":
		return serverapi.ErrServerAuthRequired
	case "project_not_found":
		return projectNotFoundError(failure.GetProjectNotFound())
	case "workspace_already_bound":
		return validateEmptyProjectMutationDetail(failure.GetWorkspaceAlreadyBound(), serverapi.ErrWorkspaceAlreadyBound)
	case "workspace_path_missing":
		return validateEmptyProjectMutationDetail(failure.GetWorkspacePathMissing(), serverapi.ErrWorkspacePathMissing)
	default:
		return generatedOperationFailure(failure.Code)
	}
}

func projectWorkspaceMutationGeneratedError(
	code string,
	notFound *projectpb.ProjectNotFoundDetails,
	notRegistered *projectpb.WorkspaceNotRegisteredDetails,
	pathIdentity *projectpb.WorkspacePathIdentityDetails,
	detachConflict *projectpb.WorkspaceDetachConflictDetails,
	mutation *projectpb.WorkspaceMutationDetails,
) error {
	switch code {
	case "auth_required":
		return serverapi.ErrServerAuthRequired
	case "project_not_found":
		return projectNotFoundError(notFound)
	case "workspace_not_registered":
		return protoapi.WorkspaceNotRegisteredFromProto(notRegistered)
	case "workspace_path_identity":
		return protoapi.WorkspacePathIdentityFromProto(pathIdentity)
	case "workspace_detach_conflict":
		return protoapi.WorkspaceDetachConflictFromProto(detachConflict)
	case "workspace_mutation_failed":
		return protoapi.WorkspaceMutationFromProto(mutation)
	default:
		return generatedOperationFailure(code)
	}
}

func workspaceBindingAmbiguousError(details *projectpb.WorkspaceBindingAmbiguousDetails) error {
	value, err := protoapi.WorkspaceBindingAmbiguousFromProto(details)
	if err != nil {
		return err
	}
	return value
}

func projectUnavailableError(details *projectpb.ProjectUnavailableDetails) error {
	value, err := protoapi.ProjectUnavailableFromProto(details)
	if err != nil {
		return err
	}
	return value
}

func projectKeyConflictError(details *projectpb.ProjectKeyConflictDetails) error {
	if err := protoapi.Validate(details); err != nil {
		return err
	}
	return serverapi.ProjectKeyConflictError{ProjectKey: details.ProjectKey}
}

func validateEmptyProjectMutationDetail(detail proto.Message, sentinel error) error {
	if err := protoapi.Validate(detail); err != nil {
		return err
	}
	return sentinel
}
