package client

import (
	"context"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"core/shared/protoapi"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	"core/shared/rpcwire"
	"core/shared/runtimeids"
	"golang.org/x/net/websocket"
	"google.golang.org/protobuf/proto"
)

func TestRemoteAnswerPromptBatchValidatesExactResponseIdentitySet(t *testing.T) {
	tests := []struct {
		name     string
		response *promptpb.AnswerBatchSuccess
		wantErr  bool
	}{
		{
			name: "reordered exact set",
			response: &promptpb.AnswerBatchSuccess{Results: []*promptpb.AnswerBatchEntryResult{
				{ToolCallId: "approval-1", Outcome: promptpb.AnswerBatchOutcome_ANSWER_BATCH_OUTCOME_SKIPPED},
				{ToolCallId: "question-1", Outcome: promptpb.AnswerBatchOutcome_ANSWER_BATCH_OUTCOME_RESOLVED},
			}},
		},
		{
			name: "missing identity",
			response: &promptpb.AnswerBatchSuccess{Results: []*promptpb.AnswerBatchEntryResult{
				{ToolCallId: "question-1", Outcome: promptpb.AnswerBatchOutcome_ANSWER_BATCH_OUTCOME_RESOLVED},
			}},
			wantErr: true,
		},
		{
			name: "foreign identity",
			response: &promptpb.AnswerBatchSuccess{Results: []*promptpb.AnswerBatchEntryResult{
				{ToolCallId: "question-1", Outcome: promptpb.AnswerBatchOutcome_ANSWER_BATCH_OUTCOME_RESOLVED},
				{ToolCallId: "foreign", Outcome: promptpb.AnswerBatchOutcome_ANSWER_BATCH_OUTCOME_SKIPPED},
			}},
			wantErr: true,
		},
		{
			name: "duplicate identity",
			response: &promptpb.AnswerBatchSuccess{Results: []*promptpb.AnswerBatchEntryResult{
				{ToolCallId: "question-1", Outcome: promptpb.AnswerBatchOutcome_ANSWER_BATCH_OUTCOME_RESOLVED},
				{ToolCallId: "question-1", Outcome: promptpb.AnswerBatchOutcome_ANSWER_BATCH_OUTCOME_SKIPPED},
			}},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newRemoteTestServer(t, func(ws *websocket.Conn) {
				acceptRemoteHandshake(t, ws)
				call := receiveRemoteGeneratedCall(t, ws, "AnswerService", "AnswerBatch", &promptpb.AnswerBatchRequest{})
				method := bootstrapMethod(promptpb.File_kent_api_prompt_prompt_proto, "AnswerService", "AnswerBatch")
				frame, err := remoteDescriptorResultFrame(method, call.Correlation, &promptpb.AnswerBatchResult{
					Outcome: &promptpb.AnswerBatchResult_Success{Success: test.response},
				}, proto.Marshal)
				if err != nil {
					t.Errorf("encode prompt answer batch response: %v", err)
					return
				}
				if err := websocket.Message.Send(ws, frame.Payload); err != nil {
					t.Errorf("send prompt answer batch response: %v", err)
				}
			})
			remote, err := DialRemoteURL(context.Background(), "ws"+server.URL[len("http"):])
			if err != nil {
				t.Fatalf("DialRemoteURL: %v", err)
			}
			defer func() { _ = remote.Close() }()

			response, err := remote.AnswerPromptBatch(context.Background(), remotePromptAnswerBatchRequest(t))
			if test.wantErr {
				if err == nil {
					t.Fatalf("AnswerPromptBatch response = %+v, want contract error", response)
				}
				return
			}
			if err != nil {
				t.Fatalf("AnswerPromptBatch: %v", err)
			}
			if err := protoapi.ValidatePromptAnswerBatchResponse(remotePromptAnswerBatchRequest(t), response); err != nil {
				t.Fatalf("validated response: %v", err)
			}
		})
	}
}

func TestRemoteAnswerPromptBatchDoesNotReconnectOrReplayAfterConnectionLoss(t *testing.T) {
	method := bootstrapMethod(promptpb.File_kent_api_prompt_prompt_proto, "AnswerService", "AnswerBatch")
	operation, err := protoapi.OperationFromDescriptor(method)
	if err != nil {
		t.Fatal(err)
	}
	var connectionCount atomic.Int32
	var requestCount atomic.Int32
	firstRequestCommitted := make(chan struct{}, 1)
	handlerErrs := make(chan error, 8)
	server := httptest.NewServer(rpcwire.NewWebSocketTransport().Handler(func(ctx context.Context, conn rpcwire.Conn) {
		connectionIndex := connectionCount.Add(1)
		handshaken := false
		for event := range conn.Events() {
			if event.Err != nil {
				return
			}
			if kind, handled, err := handleRemoteTestSetupFrame(ctx, conn, event.Frame, remoteTestSetupResponse{}); handled {
				if err != nil {
					reportHandlerError(handlerErrs, "connection %d setup: %v", connectionIndex, err)
					return
				}
				handshaken = handshaken || kind == remoteTestSetupHandshake
				continue
			}
			if !handshaken {
				reportHandlerError(handlerErrs, "connection %d sent application traffic before handshake", connectionIndex)
				return
			}
			envelope, err := protoapi.DecodeEnvelope(event.Frame.Payload)
			if err != nil {
				reportHandlerError(handlerErrs, "connection %d decode envelope: %v", connectionIndex, err)
				return
			}
			call := envelope.GetCall()
			if call == nil || call.Operation != operation.Name {
				reportHandlerError(handlerErrs, "connection %d unexpected call: %+v", connectionIndex, call)
				return
			}
			var params promptpb.AnswerBatchRequest
			if err := protoapi.Decode(call.Payload, &params); err != nil {
				reportHandlerError(handlerErrs, "connection %d decode prompt answer batch: %v", connectionIndex, err)
				return
			}
			requestCount.Add(1)
			if connectionIndex == 1 {
				firstRequestCommitted <- struct{}{}
				return
			}
			response := &promptpb.AnswerBatchSuccess{
				Results: make([]*promptpb.AnswerBatchEntryResult, 0, len(params.Entries)),
			}
			for _, entry := range params.Entries {
				response.Results = append(response.Results, &promptpb.AnswerBatchEntryResult{
					ToolCallId: entry.ToolCallId,
					Outcome:    promptpb.AnswerBatchOutcome_ANSWER_BATCH_OUTCOME_SKIPPED,
				})
			}
			frame, err := remoteDescriptorResultFrame(method, call.Correlation, &promptpb.AnswerBatchResult{
				Outcome: &promptpb.AnswerBatchResult_Success{Success: response},
			}, protoapi.Encode)
			if err != nil {
				reportHandlerError(handlerErrs, "connection %d encode prompt answer batch response: %v", connectionIndex, err)
				return
			}
			if err := conn.Send(ctx, frame); err != nil {
				reportHandlerError(handlerErrs, "connection %d send prompt answer batch response: %v", connectionIndex, err)
			}
			return
		}
	}))
	defer server.Close()

	remote, err := DialRemoteURL(context.Background(), "ws"+server.URL[len("http"):])
	if err != nil {
		t.Fatalf("DialRemoteURL: %v", err)
	}
	defer func() { _ = remote.Close() }()
	request := remotePromptAnswerBatchRequest(t)
	firstDone := make(chan error, 1)
	go func() {
		_, callErr := remote.AnswerPromptBatch(context.Background(), request)
		firstDone <- callErr
	}()
	select {
	case <-firstRequestCommitted:
	case err := <-handlerErrs:
		t.Fatal(err)
	}
	if err := <-firstDone; err == nil {
		t.Fatal("connection-lost batch unexpectedly succeeded")
	}
	if got := connectionCount.Load(); got != 1 {
		t.Fatalf("connections after in-flight failure = %d, want 1", got)
	}
	if got := requestCount.Load(); got != 1 {
		t.Fatalf("requests after in-flight failure = %d, want 1", got)
	}

	response, err := remote.AnswerPromptBatch(context.Background(), request)
	if err != nil {
		t.Fatalf("explicit batch after connection loss: %v", err)
	}
	if got := connectionCount.Load(); got != 2 {
		t.Fatalf("connections after explicit retry = %d, want 2", got)
	}
	if got := requestCount.Load(); got != 2 {
		t.Fatalf("requests after explicit retry = %d, want 2", got)
	}
	for _, result := range response.Results {
		if result.Outcome != promptpb.AnswerBatchOutcome_ANSWER_BATCH_OUTCOME_SKIPPED {
			t.Fatalf("explicit all-stale response = %+v", response)
		}
	}
	requireNoHandlerError(t, handlerErrs)
}

func remotePromptAnswerBatchRequest(t *testing.T) *promptpb.AnswerBatchRequest {
	t.Helper()
	sessionID, err := runtimeids.ParseSessionID("11111111-1111-4111-8111-111111111111")
	if err != nil {
		t.Fatalf("ParseSessionID: %v", err)
	}
	stepID, err := runtimeids.ParseStepID("22222222-2222-4222-8222-222222222222")
	if err != nil {
		t.Fatalf("ParseStepID: %v", err)
	}
	selected := int32(1)
	return &promptpb.AnswerBatchRequest{
		SessionId: sessionID.String(),
		StepId:    stepID.String(),
		Entries: []*promptpb.AnswerBatchEntry{
			{
				ToolCallId: "question-1",
				Answer:     &promptpb.AnswerBatchEntry_QuestionAnswer{QuestionAnswer: &promptpb.QuestionAnswer{SelectedOptionNumber: &selected}},
			},
			{
				ToolCallId: "approval-1",
				Answer:     &promptpb.AnswerBatchEntry_Declined{Declined: &promptpb.Declined{}},
			},
		},
	}
}
