package protoapi

import (
	"testing"

	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
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

func TestSessionNameFeedbackRequiresMatchingMarker(t *testing.T) {
	for _, changed := range []bool{false, true} {
		feedback := &transcriptpb.SessionSettingFeedback{
			Kind:    transcriptpb.SessionSettingKind_SESSION_SETTING_KIND_SESSION_NAME,
			Changed: changed,
			Value:   &transcriptpb.SessionSettingFeedback_SessionName{SessionName: &emptypb.Empty{}},
		}
		if err := Validate(feedback); err != nil {
			t.Fatalf("rename feedback changed=%v: %v", changed, err)
		}
		feedback.Kind = transcriptpb.SessionSettingKind_SESSION_SETTING_KIND_FAST_MODE
		if err := Validate(feedback); err == nil {
			t.Fatal("rename marker accepted for a different setting")
		}
		feedback.Kind = transcriptpb.SessionSettingKind_SESSION_SETTING_KIND_SESSION_NAME
		feedback.Value = &transcriptpb.SessionSettingFeedback_FastMode{FastMode: false}
		if err := Validate(feedback); err == nil {
			t.Fatal("rename feedback accepted a different setting's value")
		}
		feedback.Value = nil
		if err := Validate(feedback); err == nil {
			t.Fatal("rename feedback accepted without marker")
		}
	}
}
