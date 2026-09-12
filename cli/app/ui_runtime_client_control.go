package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"core/shared/apicontract"
	"core/shared/client"
	"core/shared/clientui"
	"core/shared/protoapi"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"

	"google.golang.org/protobuf/proto"
)

func (c *sessionRuntimeClient) sessionRuntimeBoundary() {}

func (c *sessionRuntimeClient) ReadChatSettings() (*chatsettingspb.Settings, error) {
	sessionID, err := runtimeids.ParseSessionID(strings.TrimSpace(c.sessionID))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), uiChatSettingsTimeout)
	defer cancel()
	response, err := c.chatSettings.ReadChatSettings(ctx, &chatsettingspb.ReadRequest{
		Target: &chatsettingspb.ReadRequest_Session{Session: &chatsettingspb.SessionTarget{SessionId: sessionID.String()}},
	})
	if err != nil {
		return nil, err
	}
	return response.GetSession().Settings, nil
}

func (c *sessionRuntimeClient) MutateChatSettings(operation *chatsettingspb.MutationOperation) (*chatsettingspb.MutationSuccess, error) {
	sessionID, err := runtimeids.ParseSessionID(strings.TrimSpace(c.sessionID))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), uiChatSettingsTimeout)
	defer cancel()
	response, err := c.chatSettings.MutateChatSettings(ctx, &chatsettingspb.MutationRequest{
		Session:   &chatsettingspb.SessionTarget{SessionId: sessionID.String()},
		Operation: operation,
	})
	if err != nil {
		return nil, err
	}
	return response, nil
}

func runtimeRequestCallNoResult(ctx context.Context, c *sessionRuntimeClient, call func(ctx context.Context) error) error {
	_, err := runtimeRequestCall(ctx, c, true, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, call(ctx)
	})
	return err
}

func (c *sessionRuntimeClient) SetSessionName(name string) error {
	if err := runtimeControlCallNoResult(c, func(ctx context.Context) error {
		return c.controls.SetSessionName(ctx, &runtimepb.SetSessionNameRequest{SessionId: c.sessionID, Name: name})
	}); err != nil {
		return err
	}
	c.patchMainView(func(view *runtimepb.MainView) {
		view.Session.SessionName = proto.String(name)
	})
	return nil
}

func runtimeGoalCall[T any](c *sessionRuntimeClient, appendWarning bool, call func(context.Context) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(context.Background(), client.GoalRequestTimeout)
	defer cancel()
	response, err := runtimeRequestCall(ctx, c, appendWarning, call)
	return response, client.PresentGoalRequestError(err)
}

func (c *sessionRuntimeClient) ShowGoal() (*runtimepb.GoalView, error) {
	resp, err := runtimeGoalCall(c, false, func(ctx context.Context) (*runtimepb.GoalShowSuccess, error) {
		return c.controls.ShowGoal(ctx, &runtimepb.GoalShowRequest{SessionId: c.sessionID})
	})
	if err != nil {
		return nil, err
	}
	return runtimeGoalFromResponse(resp), nil
}

func (c *sessionRuntimeClient) SetGoal(objective string) (clientui.GoalMutationResult, error) {
	resp, err := runtimeGoalCall(c, true, func(ctx context.Context) (*runtimepb.GoalMutationSuccess, error) {
		return c.controls.SetGoal(ctx, &runtimepb.GoalSetRequest{SessionId: c.sessionID, Objective: objective, Actor: "user"})
	})
	return runtimeGoalMutationResult(resp, err)
}

func (c *sessionRuntimeClient) PauseGoal() (clientui.GoalMutationResult, error) {
	return c.setGoalStatus(func(ctx context.Context, req *runtimepb.GoalMutationRequest) (*runtimepb.GoalMutationSuccess, error) {
		return c.controls.PauseGoal(ctx, req)
	})
}

func (c *sessionRuntimeClient) ResumeGoal() (clientui.GoalMutationResult, error) {
	return c.setGoalStatus(func(ctx context.Context, req *runtimepb.GoalMutationRequest) (*runtimepb.GoalMutationSuccess, error) {
		return c.controls.ResumeGoal(ctx, req)
	})
}

func (c *sessionRuntimeClient) CompleteGoal() (clientui.GoalMutationResult, error) {
	return c.setGoalStatus(func(ctx context.Context, req *runtimepb.GoalMutationRequest) (*runtimepb.GoalMutationSuccess, error) {
		return c.controls.CompleteGoal(ctx, req)
	})
}

func (c *sessionRuntimeClient) ClearGoal() (clientui.GoalMutationResult, error) {
	resp, err := runtimeGoalCall(c, true, func(ctx context.Context) (*runtimepb.GoalMutationSuccess, error) {
		return c.controls.ClearGoal(ctx, &runtimepb.GoalClearRequest{SessionId: c.sessionID, Actor: "user"})
	})
	return runtimeGoalMutationResult(resp, err)
}

func (c *sessionRuntimeClient) setGoalStatus(call func(context.Context, *runtimepb.GoalMutationRequest) (*runtimepb.GoalMutationSuccess, error)) (clientui.GoalMutationResult, error) {
	resp, err := runtimeGoalCall(c, true, func(ctx context.Context) (*runtimepb.GoalMutationSuccess, error) {
		return call(ctx, &runtimepb.GoalMutationRequest{SessionId: c.sessionID, Actor: "user"})
	})
	return runtimeGoalMutationResult(resp, err)
}

func runtimeGoalMutationResult(resp *runtimepb.GoalMutationSuccess, err error) (clientui.GoalMutationResult, error) {
	if err != nil {
		return clientui.GoalMutationResult{}, err
	}
	return clientui.GoalMutationResult{Kind: resp.Kind, Goal: resp.Goal, Availability: resp.Availability}, nil
}

func runtimeGoalFromResponse(resp *runtimepb.GoalShowSuccess) *runtimepb.GoalView {
	return &runtimepb.GoalView{Goal: resp.Goal, Availability: resp.Availability.Enum()}
}

func cloneRuntimeGoal(goal *runtimepb.GoalView) *runtimepb.GoalView {
	if goal == nil {
		return nil
	}
	return proto.Clone(goal).(*runtimepb.GoalView)
}

func (c *sessionRuntimeClient) AppendCommittedEntry(role, text string) error {
	return c.AppendCommittedEntryWithNoticeID(role, text, "")
}

func (c *sessionRuntimeClient) AppendCommittedEntryWithNoticeID(role, text, noticeID string) error {
	var notice *string
	if value := strings.TrimSpace(noticeID); value != "" {
		notice = &value
	}
	return runtimeControlCallNoResult(c, func(ctx context.Context) error {
		return c.controls.AppendCommittedEntry(ctx, &transcriptpb.AppendCommittedEntryRequest{SessionId: c.sessionID, Role: role, Text: text, NoticeId: notice})
	})
}

func (c *sessionRuntimeClient) SubmitRuntimeInput(ctx context.Context, req clientui.RuntimeSubmitRequest) (clientui.UserTurnSubmission, error) {
	if err := req.Validate(); err != nil {
		return clientui.UserTurnSubmission{}, err
	}
	input, err := protoapi.UserTurnInputToProto(req.Input)
	if err != nil {
		return clientui.UserTurnSubmission{}, err
	}
	resp, err := runtimeRequestCall(ctx, c, true, func(ctx context.Context) (*runtimepb.SubmitUserTurnSuccess, error) {
		return c.controls.SubmitUserTurn(ctx, &runtimepb.SubmitUserTurnRequest{
			SessionId: c.sessionID,
			Input:     input,
		})
	})
	if err != nil {
		return clientui.UserTurnSubmission{}, err
	}
	if err := protoapi.Validate(resp); err != nil {
		return clientui.UserTurnSubmission{}, err
	}
	return userTurnSubmissionFromResponse(resp, runtimeSubmitInputText(req)), nil
}

func runtimeSubmitInputText(input clientui.RuntimeSubmitRequest) string {
	text, err := input.Input.CanonicalHistoryText()
	if err != nil {
		panic("runtime submit input must validate before projection: " + err.Error())
	}
	return text
}

func userTurnSubmissionFromResponse(resp *runtimepb.SubmitUserTurnSuccess, text string) clientui.UserTurnSubmission {
	var submission clientui.UserTurnSubmission
	switch result := resp.Result.(type) {
	case *runtimepb.SubmitUserTurnSuccess_Queued:
		submission.ResultKind = clientui.UserTurnResultKindQueued
		if result.Queued.Steered {
			submission.Queued = clientui.QueuedUserMessage{ID: result.Queued.QueueItemId, Text: text}
		}
	case *runtimepb.SubmitUserTurnSuccess_NoFinal:
		submission.ResultKind = clientui.UserTurnResultKindNoFinal
	case *runtimepb.SubmitUserTurnSuccess_AssistantFinal:
		submission.ResultKind = clientui.UserTurnResultKindAssistantFinal
		submission.Message = proto.String(result.AssistantFinal.Message)
	case *runtimepb.SubmitUserTurnSuccess_SilentFinal:
		submission.ResultKind = clientui.UserTurnResultKindSilentFinal
		submission.Message = proto.String(result.SilentFinal.Message)
	default:
		panic("validated user turn response has no result")
	}
	return submission
}

func (c *sessionRuntimeClient) RunUserShell(ctx context.Context, req clientui.RuntimeShellRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	return runtimeRequestCallNoResult(ctx, c, func(ctx context.Context) error {
		return c.controls.SubmitUserShellCommand(ctx, &runtimepb.ShellCommandRequest{SessionId: c.sessionID, Command: req.Command})
	})
}

func (c *sessionRuntimeClient) CompactRuntime(ctx context.Context, req clientui.RuntimeCompactRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	return runtimeRequestCallNoResult(ctx, c, func(ctx context.Context) error {
		return c.controls.CompactContext(ctx, &runtimepb.CompactContextRequest{
			SessionId: c.sessionID,
			RequestId: req.RequestID.String(),
			Admission: protoapi.ManualCompactionAdmissionToProto(req.Admission),
		})
	})
}

func (c *sessionRuntimeClient) Interrupt() error {
	_, err := c.interruptRuntimeCandidate()
	return err
}

func (c *sessionRuntimeClient) interruptRuntimeCandidate() (runtimeTupleCandidate, error) {
	resp, err := runtimeControlCall(c, true, func(ctx context.Context) (*runtimepb.ReadModelUpdate, error) {
		return c.controls.Interrupt(ctx, &runtimepb.InterruptRequest{SessionId: c.sessionID})
	})
	if err != nil {
		return runtimeTupleCandidate{}, err
	}
	candidate := runtimeTupleCandidate{
		Version:  resp.Version,
		Activity: resp.Activity,
	}
	return candidate, nil
}

func (c *sessionRuntimeClient) DiscardQueuedUserMessage(queueItemID string) bool {
	pendingWork, ok := c.controls.(apicontract.RuntimePendingWorkService)
	if !ok {
		return false
	}
	itemID, err := runtimeids.ParseQueueItemID(queueItemID)
	if err != nil {
		return false
	}
	_, err = runtimeControlCall(c, true, func(ctx context.Context) (*runtimepb.RemovePendingWorkSuccess, error) {
		return pendingWork.RemovePendingWork(ctx, &runtimepb.RemovePendingWorkRequest{SessionId: c.sessionID, ItemId: itemID.String()})
	})
	return err == nil
}

func (c *sessionRuntimeClient) ListPendingWork(sessionID runtimeids.SessionID) (runtimeinput.PendingWork, error) {
	if c == nil {
		return runtimeinput.PendingWork{}, errors.New("runtime client is required")
	}
	if sessionID.IsZero() {
		return runtimeinput.PendingWork{}, errors.New("Pending Work Session ID is required")
	}
	if sessionID.String() != c.sessionID {
		return runtimeinput.PendingWork{}, fmt.Errorf(
			"Pending Work Session ID %q does not match runtime client Session %q",
			sessionID.String(),
			c.sessionID,
		)
	}
	pendingWork, ok := c.controls.(apicontract.RuntimePendingWorkService)
	if !ok {
		return runtimeinput.PendingWork{}, errors.New("runtime Pending Work service is unavailable")
	}
	resp, err := runtimeControlCall(c, false, func(ctx context.Context) (*runtimepb.ListPendingWorkSuccess, error) {
		return pendingWork.ListPendingWork(ctx, &runtimepb.ListPendingWorkRequest{SessionId: sessionID.String()})
	})
	if err != nil {
		return runtimeinput.PendingWork{}, err
	}
	if err := protoapi.Validate(resp); err != nil {
		return runtimeinput.PendingWork{}, fmt.Errorf("validate Pending Work list response: %w", err)
	}
	return protoapi.PendingWorkFromProto(resp.PendingWork)
}

func (c *sessionRuntimeClient) RecordPromptHistory(text string) error {
	return runtimeControlCallNoResult(c, func(ctx context.Context) error {
		return c.controls.RecordPromptHistory(ctx, &promptpb.RecordHistoryRequest{SessionId: c.sessionID, Text: text})
	})
}
