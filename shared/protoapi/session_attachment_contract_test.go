package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	connectionpb "core/shared/protoapi/gen/kent/api/connection"
	"core/shared/runtimeids"
)

func TestSessionAttachmentCapabilityBoundary(t *testing.T) {
	for _, test := range []struct {
		capability string
		accepted   bool
	}{
		{capability: "signed-capability", accepted: true},
		{capability: ""},
		{capability: " signed-capability"},
		{capability: "signed-capability\t"},
		{capability: "\u0085signed-capability"},
		{capability: " \t"},
	} {
		t.Run(test.capability, func(t *testing.T) {
			result := &connectionpb.AttachSessionResult{
				Outcome: &connectionpb.AttachSessionResult_Success{
					Success: &connectionpb.AttachmentSuccess{
						Attachment: &connectionpb.AttachmentSuccess_Session{
							Session: &connectionpb.SessionAttachment{
								ProjectId:          "project-1",
								WorkspaceId:        "workspace-1",
								WorkspaceRoot:      "/workspace",
								SessionId:          runtimeids.NewSessionID().String(),
								ReattachCapability: test.capability,
							},
						},
					},
				},
			}
			encoded, err := protoapi.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			err = protoapi.Decode(encoded, &connectionpb.AttachSessionResult{})
			if (err == nil) != test.accepted {
				t.Errorf("capability acceptance = %v, want %t", err, test.accepted)
			}
		})
	}
}

func TestSessionAttachmentProjectIdentityBoundary(t *testing.T) {
	for _, test := range []struct {
		projectID string
		accepted  bool
	}{
		{projectID: "project-1", accepted: true},
		{projectID: " project-1 ", accepted: true},
		{projectID: ""},
		{projectID: " \t"},
		{projectID: "\u0085"},
	} {
		t.Run(test.projectID, func(t *testing.T) {
			attachment := &connectionpb.SessionAttachment{
				ProjectId:          test.projectID,
				WorkspaceId:        "workspace-1",
				WorkspaceRoot:      "/workspace",
				SessionId:          runtimeids.NewSessionID().String(),
				ReattachCapability: "signed-capability",
			}
			encoded, err := protoapi.Marshal(attachment)
			if err != nil {
				t.Fatal(err)
			}
			err = protoapi.Decode(encoded, &connectionpb.SessionAttachment{})
			if (err == nil) != test.accepted {
				t.Errorf("Project identity acceptance = %v, want %t", err, test.accepted)
			}
		})
	}
}
