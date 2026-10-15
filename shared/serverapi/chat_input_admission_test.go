package serverapi_test

import (
	"testing"

	"core/shared/protoapi"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
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

func TestChatInputAcceptanceValidatesDisposition(t *testing.T) {
	for _, disposition := range []chatpb.InputAdmissionDisposition{
		chatpb.InputAdmissionDisposition_INPUT_ADMISSION_DISPOSITION_STARTED_TURN,
		chatpb.InputAdmissionDisposition_INPUT_ADMISSION_DISPOSITION_PENDING_STEER,
		chatpb.InputAdmissionDisposition_INPUT_ADMISSION_DISPOSITION_PENDING_QUEUE,
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
