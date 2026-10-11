package promptcontrol

import (
	"errors"
	"testing"
	"time"

	"core/server/registry"
	"core/server/tools"
	"core/shared/protoapi"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

func TestPendingQuestionProjectsAssignedSessionNumber(t *testing.T) {
	store, target, _ := promptControlAgentSessions(t)
	identity := target.Identity()
	view := NewAskViewService(&stubAskPendingPromptSource{items: []registry.PendingPromptSnapshot{{
		Request:   tools.AskQuestionRequest{ToolCallID: "ask", StepID: promptViewStepID, Question: "continue?"},
		CreatedAt: time.Now(),
	}}}, store)
	result, err := view.ListPendingAsksBySession(t.Context(), &promptpb.ListPendingRequest{SessionId: protoapi.SessionSelectorToProto(*identity.ShortID)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Questions) != 1 || result.Questions[0].SessionId != identity.ID.String() ||
		result.Questions[0].ShortId == nil || *result.Questions[0].ShortId != identity.ShortID.Int64() {
		t.Fatalf("pending Question identity = %v, want %v", result, identity)
	}
}

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
