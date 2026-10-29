package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"

	"core/server/tools"
	"core/shared/textutil"
)

func TestEmptyNativeResponsePreservesContinuation(t *testing.T) {
	for _, test := range []struct {
		name      string
		output    json.RawMessage
		endTurn   *bool
		responses int32
		outcome   UserTurnResultKind
	}{
		{"explicit_empty_final", json.RawMessage(`{"type":"message","id":"empty","role":"assistant","phase":"final_answer","status":"completed","content":[{"type":"output_text","text":"","annotations":[]}]}`), nil, 1, UserTurnResultSilentFinal},
		{"empty_final_content_array", json.RawMessage(`{"type":"message","id":"empty","role":"assistant","phase":"final_answer","status":"completed","content":[]}`), nil, 1, UserTurnResultSilentFinal},
		{"empty_without_phase", json.RawMessage(`{"type":"message","id":"empty","role":"assistant","status":"completed","content":[{"type":"output_text","text":"","annotations":[]}]}`), nil, 1, UserTurnResultSilentFinal},
		{"whitespace_final", json.RawMessage(`{"type":"message","id":"empty","role":"assistant","phase":"final_answer","status":"completed","content":[{"type":"output_text","text":" \n","annotations":[]}]}`), nil, 1, UserTurnResultSilentFinal},
		{"no_output_items", nil, nil, 1, UserTurnResultSilentFinal},
		{"explicit_end", nil, textutil.Value(true), 1, UserTurnResultSilentFinal},
		{"explicit_continuation", nil, textutil.Value(false), 2, UserTurnResultAssistantFinal},
		{"reasoning_without_followup", json.RawMessage(`{"type":"reasoning","id":"reasoning","summary":[],"encrypted_content":"opaque"}`), nil, 1, UserTurnResultSilentFinal},
		{"commentary_without_followup", json.RawMessage(`{"type":"message","id":"commentary","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"Commentary","annotations":[]}]}`), nil, 1, UserTurnResultAssistantFinal},
		{"hosted_without_followup", json.RawMessage(`{"type":"web_search_call","id":"hosted","status":"completed","action":{"type":"search","query":"sample"}}`), nil, 1, UserTurnResultSilentFinal},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls, openings atomic.Int32
			handler := nativeResponsesSocketHandler(t, func(raw json.RawMessage) []string {
				output := []json.RawMessage{test.output}
				if test.output == nil {
					output = []json.RawMessage{}
				}
				turn := calls.Add(1)
				if turn > 1 {
					var request struct {
						Input    []json.RawMessage `json:"input"`
						Previous *string           `json:"previous_response_id"`
					}
					if err := json.Unmarshal(raw, &request); err != nil {
						t.Error(err)
					}
					expectedInput := 1
					if turn <= test.responses {
						expectedInput = 0
					}
					if request.Previous == nil || *request.Previous != fmt.Sprintf("resp_%d", turn-1) || len(request.Input) != expectedInput {
						t.Errorf("continuation lost or history retransmitted: %s", raw)
					}
					output = []json.RawMessage{json.RawMessage(`{"type":"message","id":"answer","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"next answer","annotations":[]}]}`)}
				}
				response := map[string]any{
					"id": fmt.Sprintf("resp_%d", turn), "status": "completed", "output": output,
					"usage": map[string]int{"input_tokens": 10, "output_tokens": 0, "total_tokens": 10},
				}
				if turn == 1 && test.endTurn != nil {
					response["end_turn"] = *test.endTurn
				}
				event, err := json.Marshal(map[string]any{"type": "response.completed", "response": response})
				if err != nil {
					t.Error(err)
					return nil
				}
				events := make([]string, 0, len(output)+1)
				for index, item := range output {
					events = append(events, fmt.Sprintf(`{"type":"response.output_item.done","output_index":%d,"item":%s}`, index, item))
				}
				return append(events, string(event))
			})
			client := nativeResponseTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				openings.Add(1)
				handler.ServeHTTP(w, r)
			}))
			store := mustCreateTestSession(t)
			engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{Model: "gpt-6-sol"})
			result, err := engine.SubmitUserMessageWithOutcomeWithHooks(t.Context(), "first", nil, nil)
			if err != nil {
				t.Errorf("empty turn failed: %v", err)
			}
			if result.Kind != test.outcome {
				t.Errorf("turn outcome = %v, want %v", result.Kind, test.outcome)
			}
			if calls.Load() != test.responses {
				t.Errorf("empty turn generated %d responses", calls.Load())
			}
			assistantRows := 0
			for _, row := range TranscriptCommittedRowFactsFromSnapshot(mustEngineNewestSegmentPage(t, engine).Snapshot) {
				if row.Assistant != nil {
					assistantRows++
				}
			}
			expectedRows := 0
			if test.outcome == UserTurnResultAssistantFinal {
				expectedRows = 1
			}
			if assistantRows != expectedRows {
				t.Errorf("visible assistant rows = %d, want %d", assistantRows, expectedRows)
			}
			assertNative := func(engine *Engine) {
				t.Helper()
				if test.output == nil {
					return
				}
				for _, item := range buildActiveTurnRequestForTest(t, engine, nil, true).Items {
					if bytes.Equal(item.Raw, test.output) {
						return
					}
				}
				t.Error("native empty item missing or changed in request history")
			}
			assertNative(engine)
			if _, err := engine.SubmitUserMessage(t.Context(), "next human turn"); err != nil {
				t.Errorf("next human turn failed: %v", err)
			}
			if calls.Load() != test.responses+1 {
				t.Errorf("next human turn generated %d total responses", calls.Load())
			}
			if openings.Load() != 1 {
				t.Errorf("socket was reopened %d times", openings.Load())
			}
			if err := engine.Close(); err != nil {
				t.Fatal(err)
			}
			reopened := mustNewTestEngine(t, mustOpenTestSession(t, store.Dir()), client, tools.NewRegistry(), Config{Model: "gpt-6-sol"})
			assertNative(reopened)
		})
	}
}
