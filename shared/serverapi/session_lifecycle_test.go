package serverapi

import (
	"testing"

	"core/shared/worktreecontract"
)

func TestSessionRetargetWorkspaceResponseAcceptsScheduledAcknowledgement(t *testing.T) {
	response := SessionRetargetWorkspaceResponse{
		Scheduled: &SessionWorkspaceRetargetScheduledAcknowledgement{OperationID: worktreecontract.NewOperationID()},
	}
	if err := response.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}
