package transport

import (
	"context"
	"testing"

	"core/shared/protoapi"
	contextpb "core/shared/protoapi/gen/kent/api/chat_context"
)

func TestChatContextRouteScopeUsesSessionTarget(t *testing.T) {
	fixture := newRoutePolicyFixture(t)
	executor := newRoutePolicyExecutor(fixture.gateway)
	operation, err := protoapi.OperationFromDescriptor(contextpb.File_kent_api_chat_context_chat_context_proto.Services().ByName("ChatContextService").Methods().ByName("Get"))
	if err != nil {
		t.Fatal(err)
	}

	if err := executor.authorizeScopeFacts(
		context.Background(),
		&connectionState{attachedProject: fixture.bindingA.ProjectID},
		routeScopePolicy(operation.Options.ScopePolicy),
		operation.Name,
		routeScopeParams{sessionID: fixture.ownSessionID},
	); err != nil {
		t.Fatalf("Session target scope: %v", err)
	}
	if err := executor.authorizeScopeFacts(
		context.Background(),
		&connectionState{attachedProject: fixture.bindingA.ProjectID},
		routeScopePolicy(operation.Options.ScopePolicy),
		operation.Name,
		routeScopeParams{sessionID: fixture.foreignSessionID},
	); err == nil {
		t.Fatal("foreign Session target unexpectedly passed active-project scope")
	}
}
