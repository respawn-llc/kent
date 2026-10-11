package protoapi

import (
	"testing"

	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeids"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestSessionNameRequestRequiresAction(t *testing.T) {
	request := &runtimepb.SetSessionNameRequest{SessionId: runtimeids.NewSessionID().String()}
	if err := Validate(request); err == nil {
		t.Fatal("missing Session name action accepted")
	}
}

func TestSessionNameRequestAcceptsSetAndClear(t *testing.T) {
	for _, mutation := range []*runtimepb.SessionNameMutation{
		{Action: &runtimepb.SessionNameMutation_Set{Set: "  レビュー  \"release\"\nnotes  "}},
		{Action: &runtimepb.SessionNameMutation_Clear{Clear: &emptypb.Empty{}}},
	} {
		request := &runtimepb.SetSessionNameRequest{
			SessionId: runtimeids.NewSessionID().String(),
			Mutation:  mutation,
		}
		if err := Validate(request); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSessionNameRequestRejectsBlankSet(t *testing.T) {
	request := &runtimepb.SetSessionNameRequest{
		SessionId: runtimeids.NewSessionID().String(),
		Mutation: &runtimepb.SessionNameMutation{
			Action: &runtimepb.SessionNameMutation_Set{Set: " \n\t "},
		},
	}
	if err := Validate(request); err == nil {
		t.Fatal("blank Session name Set accepted")
	}
}
