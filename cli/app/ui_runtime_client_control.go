package app

import (
	"context"

	"core/shared/apicontract"
	"core/shared/client"
	"core/shared/protoapi"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"

	"google.golang.org/protobuf/proto"
)

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

func runtimeGoalCall[T any](call func(context.Context) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(context.Background(), client.GoalRequestTimeout)
	defer cancel()
	response, err := call(ctx)
	return response, client.PresentGoalRequestError(err)
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

func chatActivationFromInput(input runtimeinput.Input) (*chatpb.Activation, error) {
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
