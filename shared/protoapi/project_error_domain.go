package protoapi

import (
	"fmt"

	"core/shared/clientui"
	projectpb "core/shared/protoapi/gen/kent/api/project"
	"core/shared/serverapi"
)

func WorkspaceBindingAmbiguousToProto(value serverapi.WorkspaceBindingAmbiguousError) (*projectpb.WorkspaceBindingAmbiguousDetails, error) {
	details := &projectpb.WorkspaceBindingAmbiguousDetails{
		CanonicalRoot: value.CanonicalRoot,
		ProjectIds:    append([]string(nil), value.ProjectIDs...),
	}
	if err := Validate(details); err != nil {
		return nil, fmt.Errorf("convert ambiguous workspace binding to protobuf: %w", err)
	}
	return details, nil
}

func WorkspaceBindingAmbiguousFromProto(details *projectpb.WorkspaceBindingAmbiguousDetails) serverapi.WorkspaceBindingAmbiguousError {
	return serverapi.WorkspaceBindingAmbiguousError{
		CanonicalRoot: details.CanonicalRoot,
		ProjectIDs:    append([]string(nil), details.ProjectIds...),
	}
}

func ProjectUnavailableToProto(value serverapi.ProjectUnavailableError) (*projectpb.ProjectUnavailableDetails, error) {
	availability, err := ProjectAvailabilityToProto(value.Availability)
	if err != nil {
		return nil, err
	}
	details := &projectpb.ProjectUnavailableDetails{
		ProjectId:    value.ProjectID,
		RootPath:     value.RootPath,
		Availability: availability,
	}
	if err := Validate(details); err != nil {
		return nil, fmt.Errorf("convert unavailable project to protobuf: %w", err)
	}
	return details, nil
}

func ProjectUnavailableFromProto(details *projectpb.ProjectUnavailableDetails) serverapi.ProjectUnavailableError {
	availability := ProjectAvailabilityFromProto(details.Availability)
	return serverapi.ProjectUnavailableError{
		ProjectID:    details.ProjectId,
		RootPath:     details.RootPath,
		Availability: availability,
	}
}

func WorkspaceNotRegisteredFromProto(details *projectpb.WorkspaceNotRegisteredDetails) error {
	return serverapi.ErrWorkspaceNotRegistered
}

func WorkspacePathIdentityFromProto(details *projectpb.WorkspacePathIdentityDetails) error {
	return serverapi.WorkspacePathIdentityError{WorkspaceRoot: details.WorkspaceRoot}
}

func WorkspaceMutationFromProto(details *projectpb.WorkspaceMutationDetails) error {
	return &serverapi.WorkspaceMutationError{ProjectID: details.ProjectId, WorkspaceID: details.WorkspaceId}
}

func WorkspaceDetachConflictFromProto(details *projectpb.WorkspaceDetachConflictDetails) error {
	return &serverapi.WorkspaceDetachConflictError{ProjectID: details.ProjectId, WorkspaceID: details.WorkspaceId}
}

func WorkspaceBindingAmbiguousMutationFromProto(details *projectpb.WorkspaceBindingAmbiguousMutationDetails) error {
	return serverapi.WorkspaceBindingAmbiguousError{ProjectIDs: append([]string(nil), details.ProjectIds...)}
}

var projectAvailabilityValues = map[projectpb.ProjectAvailability]clientui.ProjectAvailability{
	projectpb.ProjectAvailability_PROJECT_AVAILABILITY_AVAILABLE:    clientui.ProjectAvailabilityAvailable,
	projectpb.ProjectAvailability_PROJECT_AVAILABILITY_MISSING:      clientui.ProjectAvailabilityMissing,
	projectpb.ProjectAvailability_PROJECT_AVAILABILITY_INACCESSIBLE: clientui.ProjectAvailabilityInaccessible,
	projectpb.ProjectAvailability_PROJECT_AVAILABILITY_UNLINKED:     clientui.ProjectAvailabilityUnlinked,
}

func ProjectAvailabilityToProto(value clientui.ProjectAvailability) (projectpb.ProjectAvailability, error) {
	for wire, native := range projectAvailabilityValues {
		if native == value {
			return wire, nil
		}
	}
	return projectpb.ProjectAvailability_PROJECT_AVAILABILITY_UNSPECIFIED, fmt.Errorf("project availability %q is unsupported", value)
}

func ProjectAvailabilityFromProto(value projectpb.ProjectAvailability) clientui.ProjectAvailability {
	return projectAvailabilityValues[value]
}
