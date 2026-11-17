package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestTaskPRURLRequestValidation(t *testing.T) {
	for _, tc := range []struct {
		url   string
		valid bool
	}{
		{"https://git.example/repo/pull/1", true},
		{"http://localhost:8080/team/repo/merge_requests/42", true},
		{"HTTPS://git.example/pull/1", true},
		{"https:///path", true}, // The approved contract uses library URI semantics.
		{"", false},
		{"/repo/pull/1", false},
		{"git.example/pull/1", false},
		{"ftp://git.example/pull/1", false},
		{"javascript:alert(1)", false},
		{"https://bad host/pull/1", false},
		{"https://git.example/%zz", false},
	} {
		t.Run(tc.url, func(t *testing.T) {
			req := &taskpb.UpdateRequest{TaskId: "task-1", PrUrlChange: &taskpb.UpdateRequest_PrUrl{PrUrl: tc.url}}
			if err := protoapi.Validate(req); (err == nil) != tc.valid {
				t.Fatalf("valid=%v: %v", tc.valid, err)
			}
		})
	}
	for _, req := range []*taskpb.UpdateRequest{
		{TaskId: "task-1"},
		{TaskId: "task-1", PrUrlChange: &taskpb.UpdateRequest_ClearPrUrl{ClearPrUrl: &emptypb.Empty{}}},
	} {
		if err := protoapi.Validate(req); err != nil {
			t.Fatal(err)
		}
	}
}
