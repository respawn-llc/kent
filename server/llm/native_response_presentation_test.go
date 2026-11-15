package llm

import (
	"bytes"
	"fmt"
	"testing"

	"core/server/sessionstore"
)

func TestNativeReasoningPresentationSeparatesThinkingStatus(t *testing.T) {
	for _, body := range []string{"", "Reasoning body"} {
		t.Run(fmt.Sprintf("body=%t", body != ""), func(t *testing.T) {
			summary := "**Transient status**"
			if body != "" {
				summary += "\n\n" + body
			}
			raw := []byte(fmt.Sprintf(`{"type":"reasoning","id":"reasoning-status","summary":[{"type":"summary_text","text":%q}],"encrypted_content":"opaque"}`, summary))
			message, err := MessageFromNativeResponse(session.NativeResponseBody{
				Format: session.NativeResponseOpenAI,
				Items:  [][]byte{raw},
			})
			if err != nil {
				t.Fatal(err)
			}
			native := message.NativeResponse
			display := native.PresentationItems()
			if body == "" {
				if len(display[0].ReasoningSummary) != 0 {
					t.Fatal("status-only native summary became transcript reasoning")
				}
			} else if len(display[0].ReasoningSummary) != 1 || display[0].ReasoningSummary[0].Text != body {
				t.Fatalf("display reasoning = %+v, want only body", display[0].ReasoningSummary)
			}
			if !bytes.Equal(native.Record().Items[0], raw) {
				t.Fatal("display projection changed persisted provider output")
			}
			prepared := ItemsFromMessages([]Message{message})
			if len(prepared) != 1 || !bytes.Equal(prepared[0].Raw, raw) {
				t.Fatal("display projection changed provider continuation")
			}
		})
	}
}

func TestNativeGrokReasoningPresentationPreservesBoldText(t *testing.T) {
	summary := "**Reasoning heading**\n\nReasoning body"
	raw := []byte(fmt.Sprintf(`{"type":"reasoning","id":"grok-reasoning","summary":[{"type":"summary_text","text":%q}]}`, summary))
	message, err := MessageFromNativeResponse(session.NativeResponseBody{
		Format: session.NativeResponseGrok,
		Items:  [][]byte{raw},
	})
	if err != nil {
		t.Fatal(err)
	}
	display := message.NativeResponse.PresentationItems()
	if len(display) != 1 || len(display[0].ReasoningSummary) != 1 || display[0].ReasoningSummary[0].Text != summary {
		t.Fatalf("Grok reasoning presentation = %+v, want unchanged summary", display)
	}
}
