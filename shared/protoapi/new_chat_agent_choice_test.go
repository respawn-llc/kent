package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/chat_settings"
	configurationpb "core/shared/protoapi/gen/kent/api/configuration"
	"google.golang.org/protobuf/proto"
)

func TestNewChatAgentChoiceRequiresCompleteAgent(t *testing.T) {
	for _, field := range []string{"complete", "model", "thinking"} {
		t.Run(field, func(t *testing.T) {
			agent := &pb.AgentChoice{Role: "default", Model: proto.String("model"), Thinking: proto.String("medium")}
			switch field {
			case "model":
				agent.Model = nil
			case "thinking":
				agent.Thinking = nil
			}
			if err := protoapi.Validate(agent); err != nil {
				t.Fatalf("shared Agent choice rejected optional selection: %v", err)
			}
			choice := &pb.NewChatAgentChoice{
				Defaults: testSessionSettings(t),
				Source:   &configurationpb.SourceReport{},
				Agent:    agent,
				Baseline: &pb.InitialChatSettings{
					AgentRole: "default", Supervisor: pb.SupervisorValue_SUPERVISOR_VALUE_OFF,
					QuestionsEnabled: proto.Bool(true), AutoCompactionEnabled: proto.Bool(true),
				},
				Supervisor: &pb.Supervisor{
					Value: pb.SupervisorValue_SUPERVISOR_VALUE_OFF, Baseline: pb.SupervisorValue_SUPERVISOR_VALUE_OFF,
					Editability: pb.Editability_EDITABILITY_EDITABLE,
				},
				Questions: &pb.Questions{Capable: true, Enabled: true, Editability: pb.Editability_EDITABILITY_EDITABLE},
				AutoCompaction: &pb.AutoCompaction{
					Policy: pb.AutoCompactionPolicy_AUTO_COMPACTION_POLICY_OPTIONAL, Stored: true, Effective: true,
					Editability: pb.Editability_EDITABILITY_EDITABLE,
				},
			}
			_, err := protoapi.Encode(choice)
			if (err == nil) != (field == "complete") {
				t.Fatalf("New Chat Agent missing %s: %v", field, err)
			}
		})
	}
}
