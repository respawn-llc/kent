package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	projectpb "core/shared/protoapi/gen/kent/api/project"
)

func TestBoundProjectPlanRequiresNonblankProjectIdentity(t *testing.T) {
	plan := &projectpb.PlanWorkspaceBindingSuccess{
		Kind:             projectpb.WorkspaceBindingPlanKind_WORKSPACE_BINDING_PLAN_KIND_BOUND,
		CanonicalRoot:    "/workspace",
		PathAvailability: projectpb.ProjectAvailability_PROJECT_AVAILABILITY_AVAILABLE,
		Binding: &projectpb.ProjectBinding{
			ProjectId: "project", ProjectKey: "PR", ProjectName: "Project",
			WorkspaceId: "workspace", CanonicalRoot: "/workspace",
			WorkspaceStatus: projectpb.ProjectAvailability_PROJECT_AVAILABILITY_AVAILABLE,
		},
	}
	if err := protoapi.Validate(plan); err != nil {
		t.Fatal(err)
	}
	plan.Binding.ProjectId = " \t "
	if err := protoapi.Validate(plan); err == nil {
		t.Fatal("blank bound Project identity accepted")
	}
}
