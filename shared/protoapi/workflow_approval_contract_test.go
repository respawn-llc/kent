package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
)

func TestWorkflowApprovalIdentityBoundary(t *testing.T) {
	for _, test := range []struct {
		id       string
		accepted bool
	}{
		{id: "11111111-1111-4111-8111-111111111111", accepted: true},
		{id: ""},
		{id: "approval-1"},
		{id: " 11111111-1111-4111-8111-111111111111"},
		{id: "11111111-1111-4111-8111-111111111111\t"},
		{id: "11111111-1111-1111-8111-111111111111"},
	} {
		t.Run(test.id, func(t *testing.T) {
			encoded, err := protoapi.Marshal(&taskpb.ApproveRequest{ApprovalId: test.id})
			if err != nil {
				t.Fatal(err)
			}
			err = protoapi.Decode(encoded, &taskpb.ApproveRequest{})
			if (err == nil) != test.accepted {
				t.Errorf("approval identity acceptance = %v, want %t", err, test.accepted)
			}
		})
	}
}
