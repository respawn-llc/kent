package protoapi

import (
	"fmt"

	pb "core/shared/protoapi/gen/kent/api/chat_settings"
	"core/shared/serverapi"
	"core/shared/textutil"
)

func InitialChatSettingsFromProto(value *pb.InitialChatSettings) (serverapi.InitialChatSettings, error) {
	if err := Validate(value); err != nil {
		return serverapi.InitialChatSettings{}, err
	}
	supervisor, err := ChatSettingsSupervisorFromProto(value.Supervisor)
	if err != nil {
		return serverapi.InitialChatSettings{}, err
	}
	result := serverapi.InitialChatSettings{
		AgentRole: value.AgentRole, Supervisor: supervisor,
		Thinking: textutil.Pointer(value.Thinking), Fast: textutil.Pointer(value.Fast),
		QuestionsEnabled: *value.QuestionsEnabled, AutoCompactionEnabled: *value.AutoCompactionEnabled,
	}
	return result, result.Validate()
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

func ChatSettingsSupervisorFromProto(value pb.SupervisorValue) (string, error) {
	switch value {
	case pb.SupervisorValue_SUPERVISOR_VALUE_OFF:
		return "off", nil
	case pb.SupervisorValue_SUPERVISOR_VALUE_AFTER_EDITS:
		return "edits", nil
	case pb.SupervisorValue_SUPERVISOR_VALUE_ALWAYS:
		return "all", nil
	default:
		return "", fmt.Errorf("invalid Chat Supervisor %v", value)
	}
}
