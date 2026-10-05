package client

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"buf.build/go/protovalidate"
	"core/shared/protoapi"
	runpromptpb "core/shared/protoapi/gen/kent/api/run_prompt"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/serverapi"
	"golang.org/x/net/websocket"
)

func TestRunPromptRejectsMalformedExpectedProgress(t *testing.T) {
	method := bootstrapMethod(runpromptpb.File_kent_api_run_prompt_run_prompt_proto, "RunService", "Prompt")
	progress, err := protoapi.ResolveProgressOperation(method)
	if err != nil {
		t.Fatal(err)
	}
	server := newRemoteTestServer(t, func(ws *websocket.Conn) {
		acceptRemoteHandshake(t, ws)
		if receiveRemoteDescriptorCallIfOpen(t, ws, method, &runpromptpb.Request{}) == nil {
			return
		}
		payload, err := protoapi.Marshal(&runpromptpb.ProgressEvent{})
		if err != nil {
			t.Error(err)
			return
		}
		frame, err := protoapi.EncodeEnvelope(&sharedpb.Envelope{Frame: &sharedpb.Envelope_NotificationEvent{
			NotificationEvent: &sharedpb.NotificationEvent{Operation: progress.Name, Payload: payload},
		}})
		if err != nil {
			t.Error(err)
			return
		}
		if err := websocket.Message.Send(ws, frame); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	remote, err := DialRemoteURL(ctx, "ws"+server.URL[len("http"):])
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	var delivered atomic.Int32
	result, err := remote.RunPrompt(ctx, serverapi.RunPromptRequest{
		Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
		Prompt: "continue",
	}, serverapi.RunPromptProgressFunc(func(*runpromptpb.ProgressEvent) { delivered.Add(1) }))
	var invalid *protovalidate.ValidationError
	if !errors.As(err, &invalid) || result != nil || delivered.Load() != 0 {
		t.Fatalf("malformed progress: result=%v error=%v delivered=%d", result, err, delivered.Load())
	}
}
