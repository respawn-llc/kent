package client

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
	"core/shared/serverapi"

	"golang.org/x/net/websocket"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestRemoteTranscriptPageRejectsMalformedLocatorPayload(t *testing.T) {
	server := newRemoteTestServer(t, func(ws *websocket.Conn) {
		acceptRemoteHandshake(t, ws)
		request := receiveRemoteGeneratedCall(t, ws, "ReadService", "GetPage", &transcriptpb.PageRequest{})
		response := &transcriptpb.PageResult{Outcome: &transcriptpb.PageResult_Success{Success: &transcriptpb.PageSuccess{
			Transcript: &transcriptpb.Page{
				SessionId:             "12345678-1234-4234-8234-123456789012",
				ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED,
				Entries: []*transcriptpb.CommittedRow{{
					Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING,
					Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
					Row: &transcriptpb.CommittedRow_Assistant{Assistant: &transcriptpb.AssistantRow{
						StepId: transcriptRemoteTestStepID(t).String(),
						Text:   "done",
						Phase:  transcriptpb.AssistantPhase_ASSISTANT_PHASE_FINAL,
					}},
				}},
			},
		}}}
		sendRemoteGeneratedResult(t, ws, request, response)
	})

	remote, err := DialRemoteURL(context.Background(), "ws"+server.URL[len("http"):])
	if err != nil {
		t.Fatalf("dial remote: %v", err)
	}
	defer func() { _ = remote.Close() }()

	if _, err := remote.GetSessionTranscriptPage(context.Background(), &transcriptpb.PageRequest{
		SessionId: "12345678-1234-4234-8234-123456789012",
	}); err == nil {
		t.Fatal("accepted transcript page with missing locator")
	}
}

func TestRemoteTranscriptSubscriptionRejectsMalformedLocatorPayload(t *testing.T) {
	server := newRemoteTestServer(t, func(ws *websocket.Conn) {
		acceptRemoteHandshake(t, ws)
		if acceptRemoteSessionAttachmentOrClosed(t, ws, "project-1", "workspace-1", "/workspace") == nil {
			return
		}
		req := receiveRemoteGeneratedCall(t, ws, "StreamService", "Subscribe", &transcriptpb.SubscribeRequest{})
		sendRemoteGeneratedResult(t, ws, req, &transcriptpb.SubscribeResult{Outcome: &transcriptpb.SubscribeResult_Success{Success: &emptypb.Empty{}}})
		message := &transcriptpb.Message{Sequence: 2, Event: &transcriptpb.Event{Payload: &transcriptpb.Event_CommittedRow{CommittedRow: &transcriptpb.CommittedRow{
			Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING,
			Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
			Row: &transcriptpb.CommittedRow_Assistant{Assistant: &transcriptpb.AssistantRow{
				StepId: transcriptRemoteTestStepID(t).String(),
				Text:   "done",
				Phase:  transcriptpb.AssistantPhase_ASSISTANT_PHASE_FINAL,
			}},
		}}}}
		operation, err := protoapi.OperationFromDescriptor(bootstrapMethod(transcriptpb.File_kent_api_transcript_transcript_proto, "StreamService", "Event"))
		if err != nil {
			t.Fatal(err)
		}
		// Deliberately bypass payload validation to exercise rejection of a malformed peer event.
		payload, err := protoapi.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := protoapi.EncodeEnvelope(&sharedpb.Envelope{Frame: &sharedpb.Envelope_NotificationEvent{
			NotificationEvent: &sharedpb.NotificationEvent{Operation: operation.Name, Payload: payload},
		}})
		if err != nil {
			t.Fatal(err)
		}
		if err := websocket.Message.Send(ws, encoded); err != nil {
			t.Fatalf("send transcript event: %v", err)
		}
	})

	remote, err := DialRemoteURL(context.Background(), "ws"+server.URL[len("http"):])
	if err != nil {
		t.Fatalf("dial remote: %v", err)
	}
	defer func() { _ = remote.Close() }()
	sub, err := remote.SubscribeSessionTranscript(context.Background(), &transcriptpb.SubscribeRequest{SessionId: "session-1"})
	if err != nil {
		t.Fatalf("subscribe transcript: %v", err)
	}
	defer func() { _ = sub.Close() }()

	if _, err := sub.Next(context.Background()); err == nil || !errors.Is(err, serverapi.ErrStreamFailed) {
		t.Fatalf("malformed transcript event error = %v, want stream failure", err)
	}
}

func TestRemoteTranscriptSubscriptionDecodesProviderModelMismatchNotice(t *testing.T) {
	unknown := protowire.AppendVarint(protowire.AppendTag(nil, 999, protowire.VarintType), 17)
	server := newRemoteTestServer(t, func(ws *websocket.Conn) {
		acceptRemoteHandshake(t, ws)
		if acceptRemoteSessionAttachmentOrClosed(t, ws, "project-1", "workspace-1", "/workspace") == nil {
			return
		}
		req := receiveRemoteGeneratedCall(t, ws, "StreamService", "Subscribe", &transcriptpb.SubscribeRequest{})
		sendRemoteGeneratedResult(t, ws, req, &transcriptpb.SubscribeResult{Outcome: &transcriptpb.SubscribeResult_Success{Success: &emptypb.Empty{}}})
		stepID := transcriptRemoteTestStepID(t)
		message := &transcriptpb.Message{Sequence: 2, Event: &transcriptpb.Event{Payload: &transcriptpb.Event_CommittedRow{CommittedRow: &transcriptpb.CommittedRow{
			Locator: &transcriptpb.CommittedRowLocator{
				EventSequence: 1,
				RowOrdinal:    1,
			},
			Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING,
			Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
			Row: &transcriptpb.CommittedRow_Notice{Notice: &transcriptpb.NoticeRow{
				StepId:   proto.String(stepID.String()),
				Reason:   transcriptpb.NoticeReason_NOTICE_REASON_PROVIDER_MODEL_MISMATCH,
				Severity: transcriptpb.NoticeSeverity_NOTICE_SEVERITY_WARNING,
				ProviderModelMismatch: &transcriptpb.ProviderModelMismatch{
					RequestedModel: "requested-model",
					ServedModel:    "served-model",
				},
			}},
		}}}}
		message.ProtoReflect().SetUnknown(unknown)
		sendRemoteGeneratedNotification(t, ws, bootstrapMethod(transcriptpb.File_kent_api_transcript_transcript_proto, "StreamService", "Event"), message)
	})

	remote, err := DialRemoteURL(context.Background(), "ws"+server.URL[len("http"):])
	if err != nil {
		t.Fatalf("dial remote: %v", err)
	}
	defer func() { _ = remote.Close() }()
	sub, err := remote.SubscribeSessionTranscript(context.Background(), &transcriptpb.SubscribeRequest{SessionId: "session-1"})
	if err != nil {
		t.Fatalf("subscribe transcript: %v", err)
	}
	defer func() { _ = sub.Close() }()

	message, err := sub.Next(context.Background())
	if err != nil {
		t.Fatalf("next transcript message: %v", err)
	}
	mismatch := message.GetEvent().GetCommittedRow().GetNotice().GetProviderModelMismatch()
	if mismatch == nil {
		t.Fatalf("transcript message = %+v, want provider-model mismatch notice", message)
	}
	if mismatch.RequestedModel != "requested-model" || mismatch.ServedModel != "served-model" {
		t.Fatalf("mismatch = %+v", mismatch)
	}
	if !bytes.Equal(message.ProtoReflect().GetUnknown(), unknown) {
		t.Fatal("generated transcript decoding discarded unknown fields")
	}
	message.GetEvent().GetCommittedRow().Visibility = transcriptpb.EntryVisibility(127)
	encoded, err := protoapi.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	if err := protoapi.Decode(encoded, &transcriptpb.Message{}); err == nil {
		t.Fatal("generated transcript decoding accepted an undeclared visibility enum")
	}
}

func transcriptRemoteTestStepID(t *testing.T) runtimeids.StepID {
	t.Helper()
	id, err := runtimeids.ParseStepID("22222222-2222-4222-8222-222222222222")
	if err != nil {
		t.Fatalf("parse step id: %v", err)
	}
	return id
}
