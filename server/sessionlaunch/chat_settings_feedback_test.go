package sessionlaunch

import (
	"testing"

	"core/shared/protoapi"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"

	"google.golang.org/protobuf/proto"
)

func TestSessionSettingFeedbackUsesAuthoritativeResult(t *testing.T) {
	settings := &chatsettingspb.Settings{
		SelectedAgent:  &chatsettingspb.AgentSummary{Thinking: " medium "},
		Supervisor:     &chatsettingspb.Supervisor{Value: chatsettingspb.SupervisorValue_SUPERVISOR_VALUE_AFTER_EDITS},
		Fast:           &chatsettingspb.Fast{Value: false},
		Questions:      &chatsettingspb.Questions{Enabled: false},
		AutoCompaction: &chatsettingspb.AutoCompaction{Stored: false, Effective: true},
	}
	for _, test := range []struct {
		name      string
		operation *chatsettingspb.MutationOperation
		want      *transcriptpb.SessionSettingFeedback
	}{
		{
			name:      "thinking",
			operation: &chatsettingspb.MutationOperation{Operation: &chatsettingspb.MutationOperation_Thinking{Thinking: "high"}},
			want:      &transcriptpb.SessionSettingFeedback{Value: &transcriptpb.SessionSettingFeedback_Thinking{Thinking: "medium"}},
		},
		{
			name:      "supervisor",
			operation: &chatsettingspb.MutationOperation{Operation: &chatsettingspb.MutationOperation_Supervisor{Supervisor: chatsettingspb.SupervisorValue_SUPERVISOR_VALUE_ALWAYS}},
			want:      &transcriptpb.SessionSettingFeedback{Value: &transcriptpb.SessionSettingFeedback_Supervisor{Supervisor: "edits"}},
		},
		{
			name:      "fast",
			operation: &chatsettingspb.MutationOperation{Operation: &chatsettingspb.MutationOperation_FastEnabled{FastEnabled: true}},
			want:      &transcriptpb.SessionSettingFeedback{Value: &transcriptpb.SessionSettingFeedback_FastMode{FastMode: false}},
		},
		{
			name:      "questions",
			operation: &chatsettingspb.MutationOperation{Operation: &chatsettingspb.MutationOperation_QuestionsEnabled{QuestionsEnabled: true}},
			want:      &transcriptpb.SessionSettingFeedback{Value: &transcriptpb.SessionSettingFeedback_Questions{Questions: false}},
		},
		{
			name:      "auto_compaction",
			operation: &chatsettingspb.MutationOperation{Operation: &chatsettingspb.MutationOperation_AutoCompactionEnabled{AutoCompactionEnabled: true}},
			want:      &transcriptpb.SessionSettingFeedback{Value: &transcriptpb.SessionSettingFeedback_AutoCompaction{AutoCompaction: false}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, changed := range []bool{false, true} {
				feedback, publish, err := transcriptSessionSettingFeedback(test.operation, changed, settings)
				if err != nil {
					t.Fatal(err)
				}
				test.want.Changed = changed
				if !publish || !proto.Equal(feedback, test.want) {
					t.Fatalf("feedback = %v, publish = %v, want %v", feedback, publish, test.want)
				}
				if err := protoapi.Validate(feedback); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestSessionSettingFeedbackRejectsUnavailableFastValue(t *testing.T) {
	feedback, publish, err := transcriptSessionSettingFeedback(
		&chatsettingspb.MutationOperation{Operation: &chatsettingspb.MutationOperation_FastEnabled{FastEnabled: true}},
		true,
		&chatsettingspb.Settings{},
	)
	if err == nil || publish || feedback != nil {
		t.Fatalf("unavailable Fast feedback = %v, publish = %v, err = %v", feedback, publish, err)
	}
}
