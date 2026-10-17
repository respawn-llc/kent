package protoapi

import (
	"errors"
	"reflect"
	"testing"

	"core/shared/clientui"
	projectpb "core/shared/protoapi/gen/kent/api/project"
	"core/shared/serverapi"

	"google.golang.org/protobuf/proto"
)

func TestProjectErrorDecoding(t *testing.T) {
	notRegistered := &projectpb.WorkspaceNotRegisteredDetails{WorkspaceId: proto.String("workspace")}
	ambiguous := &projectpb.WorkspaceBindingAmbiguousDetails{
		CanonicalRoot: "/project", ProjectIds: []string{"first", "second"},
	}
	ambiguousMutation := &projectpb.WorkspaceBindingAmbiguousMutationDetails{ProjectIds: []string{"first", "second"}}
	unavailable := &projectpb.ProjectUnavailableDetails{
		ProjectId: "project", RootPath: "/project",
		Availability: projectpb.ProjectAvailability_PROJECT_AVAILABILITY_MISSING,
	}
	identity := &projectpb.WorkspacePathIdentityDetails{WorkspaceRoot: "/workspace"}
	mutation := &projectpb.WorkspaceMutationDetails{ProjectId: "project", WorkspaceId: "workspace"}
	detach := &projectpb.WorkspaceDetachConflictDetails{
		ProjectId: "project", WorkspaceId: "workspace", Retryable: true,
	}

	for _, tt := range []struct {
		name     string
		details  proto.Message
		decode   func() error
		expected error
		sentinel error
	}{
		{
			"not registered", notRegistered,
			func() error { return WorkspaceNotRegisteredFromProto(notRegistered) },
			serverapi.ErrWorkspaceNotRegistered, serverapi.ErrWorkspaceNotRegistered,
		},
		{
			"ambiguous binding", ambiguous,
			func() error {
				return WorkspaceBindingAmbiguousFromProto(ambiguous)
			},
			serverapi.WorkspaceBindingAmbiguousError{CanonicalRoot: "/project", ProjectIDs: []string{"first", "second"}},
			serverapi.ErrWorkspaceBindingAmbiguous,
		},
		{
			"ambiguous mutation", ambiguousMutation,
			func() error { return WorkspaceBindingAmbiguousMutationFromProto(ambiguousMutation) },
			serverapi.WorkspaceBindingAmbiguousError{ProjectIDs: []string{"first", "second"}},
			serverapi.ErrWorkspaceBindingAmbiguous,
		},
		{
			"unavailable", unavailable,
			func() error {
				return ProjectUnavailableFromProto(unavailable)
			},
			serverapi.ProjectUnavailableError{
				ProjectID: "project", RootPath: "/project", Availability: clientui.ProjectAvailabilityMissing,
			},
			serverapi.ErrProjectUnavailable,
		},
		{
			"path identity", identity,
			func() error { return WorkspacePathIdentityFromProto(identity) },
			serverapi.WorkspacePathIdentityError{WorkspaceRoot: "/workspace"}, serverapi.ErrWorkspacePathIdentity,
		},
		{
			"mutation", mutation,
			func() error { return WorkspaceMutationFromProto(mutation) },
			&serverapi.WorkspaceMutationError{ProjectID: "project", WorkspaceID: "workspace"},
			serverapi.ErrWorkspaceMutationFailed,
		},
		{
			"detach conflict", detach,
			func() error { return WorkspaceDetachConflictFromProto(detach) },
			&serverapi.WorkspaceDetachConflictError{ProjectID: "project", WorkspaceID: "workspace"},
			serverapi.ErrWorkspaceDetachConflict,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := Validate(tt.details); err != nil {
				t.Fatal(err)
			}
			err := tt.decode()
			if !errors.Is(err, tt.sentinel) {
				t.Fatalf("decoded error = %v, want category %v", err, tt.sentinel)
			}
			if !reflect.DeepEqual(err, tt.expected) {
				t.Fatalf("decoded error = %#v, want %#v", err, tt.expected)
			}
			proto.Reset(tt.details)
			if err := Validate(tt.details); err == nil {
				t.Fatal("invalid details accepted at the contract boundary")
			}
		})
	}
}
