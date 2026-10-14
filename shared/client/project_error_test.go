package client

import (
	"errors"
	"reflect"
	"testing"

	"core/shared/clientui"
	"core/shared/protoapi"
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

func TestAmbiguousProjectErrorOwnsProjectIDs(t *testing.T) {
	details := &projectpb.WorkspaceBindingAmbiguousDetails{
		CanonicalRoot: "/workspace", ProjectIds: []string{"first", "second"},
	}
	var err error = protoapi.WorkspaceBindingAmbiguousFromProto(details)
	var ambiguous serverapi.WorkspaceBindingAmbiguousError
	if !errors.Is(err, serverapi.ErrWorkspaceBindingAmbiguous) || !errors.As(err, &ambiguous) {
		t.Fatalf("binding error = %v, want typed ambiguous binding", err)
	}
	details.ProjectIds[0] = "changed"
	if ambiguous.CanonicalRoot != "/workspace" || !reflect.DeepEqual(ambiguous.ProjectIDs, []string{"first", "second"}) {
		t.Fatalf("ambiguous binding = %#v", ambiguous)
	}
}
