package runtime

import (
	"bytes"
	"reflect"
	"testing"

	"core/server/llm"
	"core/server/session"
	"core/server/tools"
	"core/shared/sessioncontract"
	"core/shared/textutil"
)

func TestThinkingForkPortability(t *testing.T) {
	for _, checkpoint := range []bool{false, true} {
		for _, supported := range []bool{false, true} {
			name := "ordinary"
			if checkpoint {
				name = "checkpoint"
			}
			if supported {
				name += "/supported"
			} else {
				name += "/unsupported"
			}
			t.Run(name, func(t *testing.T) {
				parent := mustCreateTestSession(t)
				log := mustMaterializeTestEventLog(t, parent)
				update := llm.PrepareOpenAIInputItems([]llm.ResponseItem{{
					Type: llm.ResponseItemTypeConfigurationUpdate, ConfigurationEffort: textutil.Value("high"),
				}})[0]
				history, err := sessionProviderHistoryItemFromLLM(0, update)
				if err != nil {
					t.Fatal(err)
				}
				var payload session.EventRecordPayload = session.ConfigurationUpdateRecord{Item: history}
				if checkpoint {
					payload = session.HistoryReplacementRecord{
						Engine: "remote", Mode: session.CompactionModeManual, Items: []session.ProviderHistoryItem{history},
					}
				}
				if _, _, err := log.AppendRecord(textutil.Value("source-step"), payload); err != nil {
					t.Fatal(err)
				}
				if _, _, err := log.AppendRecord(nil, session.MessageRecord{
					Role: session.MessageRoleUser, Content: textutil.Value("retained"),
				}); err != nil {
					t.Fatal(err)
				}
				target, _, err := log.AppendRecord(nil, session.MessageRecord{
					Role: session.MessageRoleUser, Content: textutil.Value("cut"),
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := parent.AdoptOriginalThinkingEffort("xhigh"); err != nil {
					t.Fatal(err)
				}
				if err := parent.SetThinkingOverride(textutil.Value("low")); err != nil {
					t.Fatal(err)
				}
				before := parent.Meta()
				child, _, err := session.ForkAtUserMessage(log, target.Seq(), "child", sessioncontract.SessionCategoryMain,
					session.ForkThinking{Desired: "low", PreserveNativeUpdates: supported})
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(before, parent.Meta()) {
					t.Fatal("fork mutated parent metadata")
				}
				client := &fakeClient{
					caps:      llm.ProviderCapabilities{ProviderID: "openai", SupportsResponsesAPI: true, SupportsNativeThinkingUpdates: supported},
					responses: []llm.Response{finalOutputItemResponse("done")},
				}
				model := "gpt-6-astra"
				if !supported {
					model = "gpt-5.4"
				}
				reopened := mustOpenTestSession(t, child.Dir())
				engine := mustNewTestEngine(t, reopened, client, tools.NewRegistry(), Config{
					Model: model, ThinkingLevel: *reopened.Meta().ChatSettings.Thinking,
				})
				if _, err := engine.SubmitUserMessage(t.Context(), "new input"); err != nil {
					t.Fatal(err)
				}
				request := client.calls[0]
				updates := 0
				var userInputs []string
				for i, item := range request.Items {
					if item.Type == llm.ResponseItemTypeConfigurationUpdate {
						updates++
						if updates == 1 && !bytes.Equal(item.Raw, update.Raw) {
							t.Fatal("copied update bytes changed")
						}
						if updates == 2 && (item.ConfigurationEffort == nil || *item.ConfigurationEffort != "low" ||
							i+1 >= len(request.Items) || request.Items[i+1].Role == nil || *request.Items[i+1].Role != llm.RoleUser) {
							t.Fatal("desired selection was not reconciled before new input")
						}
					}
					for _, message := range llm.MessagesFromItems([]llm.ResponseItem{item}) {
						if message.Role == llm.RoleUser && message.Content != nil {
							userInputs = append(userInputs, *message.Content)
						}
					}
				}
				if !reflect.DeepEqual(userInputs, []string{"retained", "new input"}) {
					t.Fatalf("child user input order = %v", userInputs)
				}
				if supported {
					if updates != 2 || request.ReasoningEffort != "xhigh" {
						t.Fatalf("supported fork updates=%d effort=%q", updates, request.ReasoningEffort)
					}
				} else if updates != 0 || request.ReasoningEffort != "low" || reopened.Meta().OriginalThinkingEffort != nil {
					t.Fatalf("unsupported fork retained native state: updates=%d effort=%q baseline=%v", updates, request.ReasoningEffort, reopened.Meta().OriginalThinkingEffort)
				}
				parentItems := collectThinkingForkItems(t, log)
				if len(parentItems) != 1 || !bytes.Equal(parentItems[0].Raw, update.Raw) {
					t.Fatal("fork changed parent configuration history")
				}
			})
		}
	}
}

func collectThinkingForkItems(t *testing.T, log session.MaterializedEventLog) []session.ProviderHistoryItem {
	t.Helper()
	var items []session.ProviderHistoryItem
	if err := log.WalkRecords(func(record session.EventRecord) error {
		payload, err := record.Payload()
		if err != nil {
			return err
		}
		switch payload := payload.(type) {
		case session.ConfigurationUpdateRecord:
			items = append(items, payload.Item)
		case session.HistoryReplacementRecord:
			items = append(items, payload.Items...)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return items
}
