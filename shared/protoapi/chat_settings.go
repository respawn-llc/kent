package protoapi

import (
	"fmt"

	pb "core/shared/protoapi/gen/kent/api/chat_settings"
	"core/shared/serverapi"
	"core/shared/textutil"
)

func InitialChatSettingsFromProto(value *pb.InitialChatSettings) serverapi.InitialChatSettings {
	return serverapi.InitialChatSettings{
		AgentRole: value.AgentRole, Supervisor: ChatSettingsSupervisorFromProto(value.Supervisor),
		Thinking: textutil.Pointer(value.Thinking), Fast: textutil.Pointer(value.Fast),
		QuestionsEnabled: *value.QuestionsEnabled, AutoCompactionEnabled: *value.AutoCompactionEnabled,
	}
}

func ChatSettingsSupervisorToProto(value string) (pb.SupervisorValue, error) {
	switch value {
	case "off":
		return pb.SupervisorValue_SUPERVISOR_VALUE_OFF, nil
	case "edits":
		return pb.SupervisorValue_SUPERVISOR_VALUE_AFTER_EDITS, nil
	case "all":
		return pb.SupervisorValue_SUPERVISOR_VALUE_ALWAYS, nil
	default:
		return pb.SupervisorValue_SUPERVISOR_VALUE_UNSPECIFIED, fmt.Errorf("invalid Chat Supervisor %q", value)
	}
}

func ChatSettingsSupervisorFromProto(value pb.SupervisorValue) string {
	return [...]string{"off", "edits", "all"}[value-pb.SupervisorValue_SUPERVISOR_VALUE_OFF]
}
