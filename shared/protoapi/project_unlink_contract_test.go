package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	projectpb "core/shared/protoapi/gen/kent/api/project"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
)

func TestProjectTaskCountsRequiresNonblankProject(t *testing.T) {
	for _, project := range []string{"project", " project ", " \t "} {
		err := protoapi.Validate(&taskpb.ProjectTaskGroupCountsRequest{ProjectId: project})
		if (err == nil) != (project != " \t ") {
			t.Fatalf("count Project identity acceptance = %v", err)
		}
	}
}

func TestWorkspaceUnlinkRequiresNonblankIdentity(t *testing.T) {
	for _, identity := range []struct {
		project   string
		workspace string
		valid     bool
	}{
		{"project", "workspace", true},
		{" project ", " workspace ", true},
		{" \t ", "workspace", false},
		{"project", " \t ", false},
	} {
		response := &projectpb.UnlinkWorkspaceSuccess{ProjectId: identity.project, WorkspaceId: identity.workspace}
		if err := protoapi.Validate(response); (err == nil) != identity.valid {
			t.Fatalf("unlink identity acceptance = %v, want valid=%t", err, identity.valid)
		}
	}
}
