package client

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"core/server/onboarding"
	"core/server/promptcommands"
	"core/shared/protoapi"
	onboardingpb "core/shared/protoapi/gen/kent/api/onboarding"
	promptcommandpb "core/shared/protoapi/gen/kent/api/prompt_command"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/rpcwire"
	"core/shared/runtimeids"
	"core/shared/serverapi"

	"github.com/google/uuid"
	"golang.org/x/net/websocket"
)

func TestRemotePromptCommandCatalogUsesAttachedWorkspaceAndValidatesResponse(t *testing.T) {
	server := newRemoteTestServer(t, func(ws *websocket.Conn) {
		acceptRemoteHandshake(t, ws)
		acceptRemoteProjectAttachment(t, ws, "workspace-b", "/workspace-b")
		req := receiveRemoteGeneratedCall(t, ws, "PromptCommandService", "GetCatalog", &promptcommandpb.GetCatalogRequest{})
		sendRemoteGeneratedResult(t, ws, req, &promptcommandpb.GetCatalogResult{
			Outcome: &promptcommandpb.GetCatalogResult_Success{Success: &promptcommandpb.Catalog{
				Commands: []*promptcommandpb.CatalogEntry{{Name: "prompt:remote_demo", Preview: "remote"}},
			}},
		})
	})

	remote, err := DialRemoteURLForProjectWorkspace(context.Background(), "ws"+server.URL[len("http"):], "project-1", "/workspace-b")
	if err != nil {
		t.Fatalf("DialRemoteURLForProjectWorkspace: %v", err)
	}
	defer func() { _ = remote.Close() }()
	catalog, err := remote.GetPromptCommandCatalog(context.Background(), &promptcommandpb.GetCatalogRequest{})
	if err != nil {
		t.Fatalf("GetPromptCommandCatalog: %v", err)
	}
	foundRemoteCommand := false
	for _, command := range catalog.Commands {
		if command.Name == "prompt:remote_demo" && command.Preview == "remote" {
			foundRemoteCommand = true
			break
		}
	}
	if !foundRemoteCommand {
		t.Fatalf("catalog = %+v", catalog.Commands)
	}
}

func TestRemotePromptCommandErrorRoundTripsTypedKind(t *testing.T) {
	command := "prompt:stale"
	server := newRemoteTestServer(t, func(ws *websocket.Conn) {
		acceptRemoteHandshake(t, ws)
		req := receiveRemoteGeneratedCall(t, ws, "PromptCommandService", "GetCatalog", &promptcommandpb.GetCatalogRequest{})
		sendRemoteGeneratedResult(t, ws, req, &promptcommandpb.GetCatalogResult{
			Outcome: &promptcommandpb.GetCatalogResult_Error{Error: &promptcommandpb.GetCatalogError{
				Code:   "command_not_found",
				Detail: &promptcommandpb.GetCatalogError_CommandNotFound{CommandNotFound: &promptcommandpb.CommandNotFoundDetails{Command: command}},
			}},
		})
	})
	remote, err := DialRemoteURL(context.Background(), "ws"+server.URL[len("http"):])
	if err != nil {
		t.Fatalf("DialRemoteURL: %v", err)
	}
	defer func() { _ = remote.Close() }()
	var typed *serverapi.PromptCommandError
	_, err = remote.GetPromptCommandCatalog(context.Background(), &promptcommandpb.GetCatalogRequest{})
	if !errors.As(err, &typed) {
		t.Fatalf("error = %T %v, want PromptCommandError", err, err)
	}
	if typed.Command == nil || *typed.Command != command {
		t.Fatalf("typed error = %+v", typed)
	}
}

func TestRemotePromptCommandImportCatalogAndInvocationUseServerRoots(t *testing.T) {
	serverRoot := t.TempDir()
	serverWorkspace := t.TempDir()
	clientRoot := t.TempDir()
	home := t.TempDir()
	sourceRoot := filepath.Join(home, ".claude", "commands")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "remote_demo.md"), []byte("server body $ARGUMENTS"), 0o600); err != nil {
		t.Fatal(err)
	}
	var providerUUID uuid.UUID
	for _, provider := range onboarding.ProductionProviderCatalog() {
		if provider.HomeEntry == ".claude" {
			providerUUID = provider.UUID
			break
		}
	}
	if providerUUID == uuid.Nil {
		t.Fatal("Claude Code provider UUID is missing")
	}
	finalizer, err := onboarding.NewFinalizer(onboarding.Options{
		PersistenceRoot: serverRoot,
		WorkspaceRoot:   serverWorkspace,
		HomeDir:         home,
	})
	if err != nil {
		t.Fatalf("NewFinalizer: %v", err)
	}
	providerUUIDText := providerUUID.String()
	if _, err := finalizer.Finalize(context.Background(), &onboardingpb.FinalizeRequest{
		CommandsImport: &onboardingpb.ImportSelection{
			Mode:         onboardingpb.ImportMode_IMPORT_MODE_SYMLINK_SOURCE,
			ProviderUuid: &providerUUIDText,
		},
	}); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	service := promptcommands.New(serverRoot, serverWorkspace)
	resolvedContent := make(chan string, 1)
	submitMethod := runtimepb.File_kent_api_runtime_runtime_proto.Services().ByName("TurnService").Methods().ByName("SubmitUserTurn")
	submitOperation, err := protoapi.OperationFromDescriptor(submitMethod)
	if err != nil {
		t.Fatal(err)
	}
	catalogMethod := promptcommandpb.File_kent_api_prompt_command_prompt_command_proto.Services().ByName("PromptCommandService").Methods().ByName("GetCatalog")
	catalogOperation, err := protoapi.OperationFromDescriptor(catalogMethod)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(rpcwire.NewWebSocketTransport().Handler(func(ctx context.Context, conn rpcwire.Conn) {
		for event := range conn.Events() {
			if event.Err != nil {
				return
			}
			_, handled, err := handleRemoteTestSetupFrame(ctx, conn, event.Frame, remoteTestSetupResponse{
				projectID: "project-1", workspaceID: "workspace-server", workspaceRoot: serverWorkspace,
			})
			if handled {
				if err != nil {
					t.Errorf("setup: %v", err)
					return
				}
				continue
			}
			switch event.Frame.Kind {
			case rpcwire.FrameBinary:
				envelope, err := protoapi.DecodeEnvelope(event.Frame.Payload)
				if err != nil {
					t.Errorf("decode submit envelope: %v", err)
					return
				}
				call := envelope.GetCall()
				if call != nil && call.Operation == catalogOperation.Name {
					if err := protoapi.Decode(call.Payload, &promptcommandpb.GetCatalogRequest{}); err != nil {
						t.Errorf("decode catalog request: %v", err)
						return
					}
					entries, err := service.Catalog()
					if err != nil {
						t.Errorf("Catalog: %v", err)
						return
					}
					catalog := &promptcommandpb.Catalog{}
					for _, entry := range entries {
						catalog.Commands = append(catalog.Commands, &promptcommandpb.CatalogEntry{Name: entry.Name, Preview: entry.Preview})
					}
					frame, err := remoteDescriptorResultFrame(catalogMethod, call.Correlation, &promptcommandpb.GetCatalogResult{
						Outcome: &promptcommandpb.GetCatalogResult_Success{Success: catalog},
					}, protoapi.Encode)
					if err != nil {
						t.Errorf("encode catalog response: %v", err)
						return
					}
					if err := conn.Send(ctx, frame); err != nil {
						t.Errorf("send catalog response: %v", err)
						return
					}
					continue
				}
				if call == nil || call.Operation != submitOperation.Name {
					t.Errorf("unexpected binary call: %+v", call)
					return
				}
				var submit runtimepb.SubmitUserTurnRequest
				if err := protoapi.Decode(call.Payload, &submit); err != nil {
					t.Errorf("decode submit request: %v", err)
					return
				}
				command := submit.Input.GetPromptCommand()
				if command == nil {
					t.Errorf("submit input = %+v, want typed prompt command", submit.Input)
					return
				}
				content, err := service.Resolve(command.Name, command.Arguments)
				if err != nil {
					t.Errorf("Resolve: %v", err)
					return
				}
				resolvedContent <- content.Text
				frame, err := remoteDescriptorResultFrame(submitMethod, call.Correlation, &runtimepb.SubmitUserTurnResult{
					Outcome: &runtimepb.SubmitUserTurnResult_Success{Success: &runtimepb.SubmitUserTurnSuccess{
						Result: &runtimepb.SubmitUserTurnSuccess_AssistantFinal{AssistantFinal: &runtimepb.SubmitUserTurnAssistantFinal{Message: "accepted"}},
					}},
				}, protoapi.Encode)
				if err != nil {
					t.Errorf("encode submit response: %v", err)
					return
				}
				if err := conn.Send(ctx, frame); err != nil {
					t.Errorf("send submit response: %v", err)
				}
				return
			default:
				t.Errorf("unexpected frame kind: %v", event.Frame.Kind)
				return
			}
		}
	}))
	defer server.Close()
	remote, err := DialRemoteURLForProjectWorkspace(context.Background(), "ws"+server.URL[len("http"):], "project-1", clientRoot)
	if err != nil {
		t.Fatalf("DialRemoteURLForProjectWorkspace: %v", err)
	}
	defer func() { _ = remote.Close() }()
	catalog, err := remote.GetPromptCommandCatalog(context.Background(), &promptcommandpb.GetCatalogRequest{})
	if err != nil {
		t.Fatalf("GetPromptCommandCatalog: %v", err)
	}
	foundRemoteCommand := false
	for _, command := range catalog.Commands {
		if command.Name == "prompt:remote_demo" && command.Preview == "server body $ARGUMENTS" {
			foundRemoteCommand = true
			break
		}
	}
	if !foundRemoteCommand {
		t.Fatalf("catalog = %+v", catalog.Commands)
	}
	_, err = remote.SubmitUserTurn(context.Background(), &runtimepb.SubmitUserTurnRequest{
		SessionId: runtimeids.NewSessionID().String(),
		Input:     &runtimepb.UserTurnInput{Input: &runtimepb.UserTurnInput_PromptCommand{PromptCommand: &runtimepb.PromptCommandInput{Name: "prompt:remote_demo", Arguments: "hello world"}}},
	})
	if err != nil {
		t.Fatalf("SubmitUserTurn: %v", err)
	}
	if got := <-resolvedContent; got != "server body hello world" {
		t.Fatalf("resolved content = %q", got)
	}
}
