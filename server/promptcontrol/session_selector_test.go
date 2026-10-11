package promptcontrol

import (
	"errors"
	"testing"

	"core/shared/protoapi"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

func TestQuestionSelectorRejectsCanonicalSelf(t *testing.T) {
	store, target, _ := promptControlAgentSessions(t)
	service := NewPromptControlService(&stubPromptResponder{}, store, store)
	identity := target.Identity()
	caller := identity.ID.String()
	for _, selector := range []runtimeids.SessionSelector{identity.ID, *identity.ShortID} {
		request := promptAnswerBatchRequest(t)
		request.SessionId = protoapi.SessionSelectorToProto(selector)
		request.InvokingSessionId = &caller
		_, err := service.AnswerPromptBatch(t.Context(), request)
		var self *serverapi.SessionSelfTargetError
		if !errors.As(err, &self) || self.SessionID != identity.ID {
			t.Fatalf("self-target %s: %v", selector, err)
		}
		view := NewAskViewService(&stubAskPendingPromptSource{}, store)
		_, err = view.ListPendingAsksBySession(t.Context(), &promptpb.ListPendingRequest{
			SessionId: request.SessionId, CallerSessionId: &caller,
		})
		if !errors.As(err, &self) {
			t.Fatalf("self-target read %s: %v", selector, err)
		}
	}
}
