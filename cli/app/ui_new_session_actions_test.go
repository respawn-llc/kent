package app

import (
	"context"
	"io"
	"testing"

	"core/shared/apicontract"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	"core/shared/runtimeinput"
	"core/shared/serverapi"
)

type concurrentNewChatClient struct {
	apicontract.ChatMutationService
	requests []*chatpb.SteerRequest
}

func (client *concurrentNewChatClient) Steer(_ context.Context, request *chatpb.SteerRequest) (*chatpb.InputMutationSuccess, error) {
	client.requests = append(client.requests, request)
	return nil, io.ErrUnexpectedEOF
}

func TestNewSessionFirstRequestsCaptureIndependentInputs(t *testing.T) {
	_, workspace := newRegisteredAppWorkspace(t)
	t.Setenv("REMOTE_TEST_KEY", "ready-key")
	fixture := startRemoteAuthTestFixture(t, workspace)
	server, err := ensureInteractiveProjectBinding(t.Context(), fixture.server)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Close() }()
	request, err := prepareNewSessionUIRun(t.Context(), server, nil, serverapi.RunPromptOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	composition, err := composeUIProgram(request, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer composition.close()
	model := composition.model
	model.Update(model.loadNewSessionSettingsCmd(nil)())
	client := &concurrentNewChatClient{}
	model.newSessionTarget().mutations = client
	first := model.inputController().submitCmd("first", runtimeinput.Text("first"), "", activeSubmitOriginDirect, nil)
	model.newChatTarget().Name = appStringPointer("second name")
	second := model.inputController().submitCmd("second", runtimeinput.Text("second"), "", activeSubmitOriginDirect, nil)
	second()
	first()
	if len(client.requests) != 2 {
		t.Fatalf("first requests = %d, want 2", len(client.requests))
	}
	if client.requests[0].Activation.GetText() != "second" || client.requests[1].Activation.GetText() != "first" {
		t.Fatal("pending requests lost their captured input")
	}
	if client.requests[0].Target.GetNewChat().GetName() != "second name" || client.requests[1].Target.GetNewChat().Name != nil {
		t.Fatal("pending requests shared mutable creation inputs")
	}
	if model.activeSubmit.token != 0 {
		t.Fatal("independent New requests replaced the singleton submission")
	}
}
