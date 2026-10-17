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
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"

	"google.golang.org/protobuf/proto"
)

func (c *sessionRuntimeClient) sessionRuntimeBoundary() {}

func (c *sessionRuntimeClient) takeChatTarget() (*chatpb.ChatTarget, error) {
	id, err := runtimeids.ParseSessionID(c.sessionID)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	preparation := c.preparation
	c.preparation = nil
	c.mu.Unlock()
	return &chatpb.ChatTarget{Target: &chatpb.ChatTarget_Session{
		Session: &chatpb.ExistingSessionRequestTarget{
			SessionId: protoapi.SessionSelectorToProto(id), Preparation: preparation,
		},
	}}, nil
}

func (c *sessionRuntimeClient) ReadChatSettings() (*chatsettingspb.Settings, error) {
	sessionID, err := runtimeids.ParseSessionID(strings.TrimSpace(c.sessionID))
	if err != nil {
		return nil, err
	}
	return (sessionChatSettingsClient{service: c.chatSettings, sessionID: sessionID}).ReadChatSettings()
}

type sessionChatSettingsClient struct {
	service   apicontract.ChatSettingsService
	sessionID runtimeids.SessionID
}

func (c sessionChatSettingsClient) ReadChatSettings() (*chatsettingspb.Settings, error) {
	ctx, cancel := context.WithTimeout(context.Background(), uiChatSettingsTimeout)
	defer cancel()
	response, err := c.service.ReadChatSettings(ctx, &chatsettingspb.ReadRequest{
		Target: &chatsettingspb.ReadRequest_Session{Session: &chatsettingspb.SessionTarget{SessionId: protoapi.SessionSelectorToProto(c.sessionID)}},
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
	return (sessionChatSettingsClient{service: c.chatSettings, sessionID: sessionID}).MutateChatSettings(operation)
}

func (c sessionChatSettingsClient) MutateChatSettings(operation *chatsettingspb.MutationOperation) (*chatsettingspb.MutationSuccess, error) {
	ctx, cancel := context.WithTimeout(context.Background(), uiChatSettingsTimeout)
	defer cancel()
	response, err := c.service.MutateChatSettings(ctx, &chatsettingspb.MutationRequest{
		Session:   &chatsettingspb.SessionTarget{SessionId: protoapi.SessionSelectorToProto(c.sessionID)},
		Operation: operation,
	})
	if err != nil {
		return nil, err
	}
	return response, nil
}

func (c *sessionRuntimeClient) SetSessionName(mutation *runtimepb.SessionNameMutation) error {
	name, err := protoapi.SessionNameFromMutation(mutation)
	if err != nil {
		return err
	}
	if err := runtimeControlCallNoResult(func(ctx context.Context) error {
		return c.controls.SetSessionName(ctx, &runtimepb.SetSessionNameRequest{SessionId: c.sessionID, Mutation: mutation})
	}); err != nil {
		return err
	}
	c.patchMainView(func(view *runtimepb.MainView) {
		view.Session.SessionName = name
	})
	return nil
}

func runtimeGoalCall[T any](call func(context.Context) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(context.Background(), client.GoalRequestTimeout)
	defer cancel()
	response, err := call(ctx)
	return response, client.PresentGoalRequestError(err)
}

func (c *sessionRuntimeClient) ShowGoal() (*runtimepb.GoalView, error) {
	resp, err := runtimeGoalCall(func(ctx context.Context) (*runtimepb.GoalShowSuccess, error) {
		return c.controls.ShowGoal(ctx, &runtimepb.GoalShowRequest{SessionId: &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: c.sessionID}}})
	})
	if err != nil {
		return nil, err
	}
	return runtimeGoalFromResponse(resp), nil
}

func (c *sessionRuntimeClient) SetGoal(objective string) (*runtimepb.GoalSetSuccess, error) {
	target, err := c.takeChatTarget()
	if err != nil {
		return nil, err
	}
	result := (chatActionTransport{actions: c.chatMutations, goals: c.goalSet}).execute(context.Background(), target, nil, chatGoalAction{objective: objective})
	return result.goal, result.err
}

func (c *sessionRuntimeClient) PauseGoal() (*runtimepb.GoalMutationSuccess, error) {
	return c.setGoalStatus(func(ctx context.Context, req *runtimepb.GoalMutationRequest) (*runtimepb.GoalMutationSuccess, error) {
		return c.controls.PauseGoal(ctx, req)
	})
}

func (c *sessionRuntimeClient) ResumeGoal() (*runtimepb.GoalMutationSuccess, error) {
	return c.setGoalStatus(func(ctx context.Context, req *runtimepb.GoalMutationRequest) (*runtimepb.GoalMutationSuccess, error) {
		return c.controls.ResumeGoal(ctx, req)
	})
}

func (c *sessionRuntimeClient) CompleteGoal() (*runtimepb.GoalMutationSuccess, error) {
	return c.setGoalStatus(func(ctx context.Context, req *runtimepb.GoalMutationRequest) (*runtimepb.GoalMutationSuccess, error) {
		return c.controls.CompleteGoal(ctx, req)
	})
}

func (c *sessionRuntimeClient) ClearGoal() (*runtimepb.GoalMutationSuccess, error) {
	resp, err := runtimeGoalCall(func(ctx context.Context) (*runtimepb.GoalMutationSuccess, error) {
		return c.controls.ClearGoal(ctx, &runtimepb.GoalClearRequest{SessionId: &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: c.sessionID}}, Actor: "user"})
	})
	return resp, err
}

func (c *sessionRuntimeClient) setGoalStatus(call func(context.Context, *runtimepb.GoalMutationRequest) (*runtimepb.GoalMutationSuccess, error)) (*runtimepb.GoalMutationSuccess, error) {
	resp, err := runtimeGoalCall(func(ctx context.Context) (*runtimepb.GoalMutationSuccess, error) {
		return call(ctx, &runtimepb.GoalMutationRequest{SessionId: &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: c.sessionID}}, Actor: "user"})
	})
	return resp, err
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
	return runtimeControlCallNoResult(func(ctx context.Context) error {
		return c.controls.AppendCommittedEntry(ctx, &transcriptpb.AppendCommittedEntryRequest{SessionId: c.sessionID, Role: role, Text: text, NoticeId: notice})
	})
}

func (c *sessionRuntimeClient) SubmitRuntimeInput(ctx context.Context, req clientui.RuntimeSubmitRequest) (clientui.UserTurnSubmission, error) {
	if err := req.Validate(); err != nil {
		return clientui.UserTurnSubmission{}, err
	}
	activation, err := chatActivationFromInput(req.Input)
	if err != nil {
		return clientui.UserTurnSubmission{}, err
	}
	target, err := c.takeChatTarget()
	if err != nil {
		return clientui.UserTurnSubmission{}, err
	}
	result := (chatActionTransport{actions: c.chatMutations}).execute(ctx, target, nil, chatInputAction{input: activation, queueItemID: runtimeids.NewQueueItemID()})
	return result.submission(runtimeSubmitInputText(req))
}

func chatActivationFromInput(input runtimeinput.Input) (*chatpb.Activation, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	if input.Kind == runtimeinput.KindText {
		return &chatpb.Activation{Input: &chatpb.Activation_Text{Text: *input.Text}}, nil
	}
	command := input.PromptCommand
	separator := ""
	if command.Arguments != "" {
		separator = " "
	}
	return &chatpb.Activation{Input: &chatpb.Activation_Command{Command: &chatpb.CommandInvocation{
		CatalogIdentity: command.Name, Token: "/" + command.Name,
		SeparatorWhitespace: separator, Arguments: command.Arguments,
	}}}, nil
}

func runtimeSubmitInputText(input clientui.RuntimeSubmitRequest) string {
	text, err := input.Input.CanonicalHistoryText()
	if err != nil {
		panic("runtime submit input must validate before projection: " + err.Error())
	}
	return text
}

func (c *sessionRuntimeClient) RunUserShell(ctx context.Context, req clientui.RuntimeShellRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	target, err := c.takeChatTarget()
	if err != nil {
		return err
	}
	result := (chatActionTransport{actions: c.chatMutations}).execute(ctx, target, nil, chatShellAction{command: req.Command})
	return result.err
}

func (c *sessionRuntimeClient) CompactRuntime(ctx context.Context, req clientui.RuntimeCompactRequest) (clientui.RuntimeCompactResult, error) {
	if err := req.Validate(); err != nil {
		return clientui.RuntimeCompactResult{}, err
	}
	target, err := c.takeChatTarget()
	if err != nil {
		return clientui.RuntimeCompactResult{}, err
	}
	result := (chatActionTransport{actions: c.chatMutations}).execute(ctx, target, nil, chatCompactionAction{invocation: chatCompactionInvocation(req.Admission.Guidance)})
	return clientui.RuntimeCompactResult{Diagnostic: result.diagnostic}, result.err
}

func (c *sessionRuntimeClient) Interrupt() error {
	_, err := c.interruptRuntimeCandidate()
	return err
}

func (c *sessionRuntimeClient) interruptRuntimeCandidate() (runtimeTupleCandidate, error) {
	resp, err := runtimeControlCall(func(ctx context.Context) (*runtimepb.ReadModelUpdate, error) {
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
	_, err = runtimeControlCall(func(ctx context.Context) (*runtimepb.RemovePendingWorkSuccess, error) {
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
	resp, err := runtimeControlCall(func(ctx context.Context) (*runtimepb.ListPendingWorkSuccess, error) {
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
	return runtimeControlCallNoResult(func(ctx context.Context) error {
		return c.controls.RecordPromptHistory(ctx, &promptpb.RecordHistoryRequest{SessionId: c.sessionID, Text: text})
	})
}
