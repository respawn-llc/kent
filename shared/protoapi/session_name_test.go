package protoapi

import (
	"testing"

	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
	"google.golang.org/protobuf/proto"
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

func TestSessionSettingFeedbackRequiresTypedValue(t *testing.T) {
	for _, changed := range []bool{false, true} {
		for _, feedback := range []*transcriptpb.SessionSettingFeedback{
			{Value: &transcriptpb.SessionSettingFeedback_SessionName{SessionName: &emptypb.Empty{}}},
			{Value: &transcriptpb.SessionSettingFeedback_Thinking{Thinking: "medium"}},
			{Value: &transcriptpb.SessionSettingFeedback_FastMode{FastMode: false}},
			{Value: &transcriptpb.SessionSettingFeedback_Supervisor{Supervisor: "off"}},
			{Value: &transcriptpb.SessionSettingFeedback_Questions{Questions: false}},
			{Value: &transcriptpb.SessionSettingFeedback_AutoCompaction{AutoCompaction: false}},
		} {
			feedback.Changed = changed
			if err := Validate(feedback); err != nil {
				t.Fatalf("feedback %T changed=%v: %v", feedback.Value, changed, err)
			}
		}
		for _, feedback := range []*transcriptpb.SessionSettingFeedback{
			{},
			{Value: &transcriptpb.SessionSettingFeedback_Thinking{Thinking: ""}},
			{Value: &transcriptpb.SessionSettingFeedback_Thinking{Thinking: " \n\t "}},
			{Value: &transcriptpb.SessionSettingFeedback_Thinking{Thinking: " medium "}},
			{Value: &transcriptpb.SessionSettingFeedback_Supervisor{Supervisor: ""}},
			{Value: &transcriptpb.SessionSettingFeedback_Supervisor{Supervisor: "unknown"}},
		} {
			feedback.Changed = changed
			if err := Validate(feedback); err == nil {
				t.Fatalf("invalid feedback %T accepted changed=%v", feedback.Value, changed)
			}
		}
	}
}

func TestSessionSettingFeedbackRejectsUnknownValue(t *testing.T) {
	feedback := &transcriptpb.SessionSettingFeedback{}
	// A future unknown oneof field must not satisfy the required typed value.
	if err := proto.Unmarshal([]byte{0x48, 0x01}, feedback); err != nil {
		t.Fatal(err)
	}
	if err := Validate(feedback); err == nil {
		t.Fatal("feedback with only an unknown value was accepted")
	}
}
