package client

import (
	"errors"
	"reflect"
	"testing"

	"core/shared/clientui"
	projectpb "core/shared/protoapi/gen/kent/api/project"
	"core/shared/serverapi"
)

func TestProjectAttachmentUnavailableError(t *testing.T) {
	details := &projectpb.ProjectUnavailableDetails{
		ProjectId: "project", RootPath: "/project",
		Availability: projectpb.ProjectAvailability_PROJECT_AVAILABILITY_INACCESSIBLE,
	}
	err := connectionAttachmentGeneratedError("project_unavailable", nil, nil, details, nil, nil)
	var unavailable serverapi.ProjectUnavailableError
	if !errors.Is(err, serverapi.ErrProjectUnavailable) || !errors.As(err, &unavailable) {
		t.Fatalf("attachment error = %v, want typed unavailable project", err)
	}
	if unavailable.ProjectID != details.ProjectId || unavailable.RootPath != details.RootPath ||
		unavailable.Availability != clientui.ProjectAvailabilityInaccessible {
		t.Fatalf("unavailable project = %#v", unavailable)
	}
}

func TestProjectErrorRejectsMissingDetails(t *testing.T) {
	for _, tt := range []struct {
		name     string
		decode   func() error
		sentinel error
	}{
		{"ambiguous binding", func() error { return workspaceBindingAmbiguousError(nil) }, serverapi.ErrWorkspaceBindingAmbiguous},
		{"unavailable project", func() error { return projectUnavailableError(nil) }, serverapi.ErrProjectUnavailable},
		{"attachment workspace", func() error {
			return connectionAttachmentGeneratedError("workspace_not_registered", nil, nil, nil, nil, nil)
		}, serverapi.ErrWorkspaceNotRegistered},
		{"mutation", func() error {
			return projectWorkspaceMutationGeneratedError("workspace_mutation_failed", nil, nil, nil, nil, nil, nil)
		}, serverapi.ErrWorkspaceMutationFailed},
		{"detach conflict", func() error {
			return projectWorkspaceMutationGeneratedError("workspace_detach_conflict", nil, nil, nil, nil, nil, nil)
		}, serverapi.ErrWorkspaceDetachConflict},
		{"path identity", func() error {
			return projectWorkspaceMutationGeneratedError("workspace_path_identity", nil, nil, nil, nil, nil, nil)
		}, serverapi.ErrWorkspacePathIdentity},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.decode(); err == nil || errors.Is(err, tt.sentinel) {
				t.Fatalf("missing details decoded as domain error: %v", err)
			}
		})
	}
}

func TestAmbiguousProjectErrorOwnsProjectIDs(t *testing.T) {
	details := &projectpb.WorkspaceBindingAmbiguousDetails{
		CanonicalRoot: "/workspace", ProjectIds: []string{"first", "second"},
	}
	err := workspaceBindingAmbiguousError(details)
	var ambiguous serverapi.WorkspaceBindingAmbiguousError
	if !errors.Is(err, serverapi.ErrWorkspaceBindingAmbiguous) || !errors.As(err, &ambiguous) {
		t.Fatalf("binding error = %v, want typed ambiguous binding", err)
	}
	details.ProjectIds[0] = "changed"
	if ambiguous.CanonicalRoot != "/workspace" || !reflect.DeepEqual(ambiguous.ProjectIDs, []string{"first", "second"}) {
		t.Fatalf("ambiguous binding = %#v", ambiguous)
	}
}
