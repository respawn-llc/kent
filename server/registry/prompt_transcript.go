package registry

import (
	"strings"

	"core/shared/protoapi"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
)

type pendingPromptEventType uint8

const (
	pendingPromptEventPending pendingPromptEventType = iota + 1
	pendingPromptEventResolved
)

func publishPendingPrompt(feed *sessionFeedSequencer, sessionID string, snapshot PendingPromptSnapshot, eventType pendingPromptEventType) {
	if feed == nil || strings.TrimSpace(snapshot.Request.ToolCallID) == "" {
		return
	}
	prompt, err := transcriptPendingPromptFromSnapshot(sessionID, snapshot, eventType)
	if err != nil {
		panic(err)
	}
	feed.Publish([]*transcriptpb.Event{{Payload: &transcriptpb.Event_Prompt{Prompt: prompt}}})
}

func transcriptPendingPromptFromSnapshot(sessionID string, snapshot PendingPromptSnapshot, eventType pendingPromptEventType) (*transcriptpb.Prompt, error) {
	result := &transcriptpb.Prompt{Status: transcriptpb.PromptStatus_PROMPT_STATUS_PENDING}
	if eventType == pendingPromptEventResolved {
		result.Status = transcriptpb.PromptStatus_PROMPT_STATUS_RESOLVED
	}
	if snapshot.Request.Approval {
		approval, err := ApprovalFromSnapshot(sessionID, snapshot)
		if err != nil {
			return nil, err
		}
		result.Prompt = &transcriptpb.Prompt_Approval{Approval: approval}
	} else {
		question, err := QuestionFromSnapshot(sessionID, snapshot)
		if err != nil {
			return nil, err
		}
		result.Prompt = &transcriptpb.Prompt_Question{Question: question}
	}
	return result, nil
}

func QuestionFromSnapshot(sessionID string, snapshot PendingPromptSnapshot) (*promptpb.Question, error) {
	id, err := runtimeids.ParseSessionID(sessionID)
	if err != nil {
		return nil, err
	}
	pending, err := PendingAskFromSnapshot(id, snapshot)
	if err != nil {
		return nil, err
	}
	return protoapi.QuestionFromPendingAsk(pending)
}

func ApprovalFromSnapshot(sessionID string, snapshot PendingPromptSnapshot) (*promptpb.Approval, error) {
	id, err := runtimeids.ParseSessionID(sessionID)
	if err != nil {
		return nil, err
	}
	pending, err := PendingApprovalFromSnapshot(id, snapshot)
	if err != nil {
		return nil, err
	}
	return protoapi.ApprovalFromPendingApproval(pending)
}
