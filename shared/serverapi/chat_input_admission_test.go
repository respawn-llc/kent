package serverapi_test

import (
	"testing"

	"core/shared/protoapi"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/runtimeids"
)

func TestChatInputAcceptanceRequiresDisposition(t *testing.T) {
	result := &chatpb.InputAccepted{
		QueueItem: &chatpb.QueueItemIdentity{Id: runtimeids.NewQueueItemID().String()},
	}
	if err := protoapi.Validate(result); err == nil {
		t.Fatal("accepted input without admission disposition validated")
	}
}

func TestChatShellRequiresExistingSession(t *testing.T) {
	id := runtimeids.NewSessionID().String()
	request := &chatpb.QueueRequest{
		Target: &chatpb.ChatTarget{Target: &chatpb.ChatTarget_Session{
			Session: &chatpb.ExistingSessionRequestTarget{
				SessionId: &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: id}},
			},
		}},
		Activation: &chatpb.Activation{Input: &chatpb.Activation_Shell{
			Shell: &chatpb.ShellCommand{Command: "echo ready"},
		}},
		QueueItemId: runtimeids.NewQueueItemID().String(),
	}
	if err := protoapi.Validate(request); err != nil {
		t.Fatal(err)
	}
	request.Activation.GetShell().Command = " \t"
	if err := protoapi.Validate(request); err == nil {
		t.Fatal("blank Shell accepted")
	}
	request.Activation.GetShell().Command = "echo ready"
	enabled := true
	request.Target = &chatpb.ChatTarget{Target: &chatpb.ChatTarget_NewChat{NewChat: &chatpb.NewChatTarget{
		ProjectId: "project", WorkspaceId: "workspace",
		InitialSettings: &chatsettingspb.InitialChatSettings{
			AgentRole: "default", Supervisor: chatsettingspb.SupervisorValue_SUPERVISOR_VALUE_OFF,
			QuestionsEnabled: &enabled, AutoCompactionEnabled: &enabled,
		},
	}}}
	if err := protoapi.Validate(request.Target); err != nil {
		t.Fatal(err)
	}
	if err := protoapi.Validate(request); err == nil {
		t.Fatal("New Chat Shell accepted")
	}
}

func TestChatInputAcceptanceValidatesDisposition(t *testing.T) {
	for _, disposition := range []chatpb.InputAdmissionDisposition{
		chatpb.InputAdmissionDisposition_INPUT_ADMISSION_DISPOSITION_STARTED_TURN,
		chatpb.InputAdmissionDisposition_INPUT_ADMISSION_DISPOSITION_PENDING_STEER,
		chatpb.InputAdmissionDisposition_INPUT_ADMISSION_DISPOSITION_PENDING_QUEUE,
		chatpb.InputAdmissionDisposition_INPUT_ADMISSION_DISPOSITION_STARTED_SHELL,
		chatpb.InputAdmissionDisposition(99),
	} {
		result := &chatpb.InputAccepted{
			QueueItem:   &chatpb.QueueItemIdentity{Id: runtimeids.NewQueueItemID().String()},
			Disposition: disposition,
		}
		err := protoapi.Validate(result)
		if (err == nil) != (disposition != 99) {
			t.Fatalf("disposition %v: validation = %v", disposition, err)
		}
	}
}
