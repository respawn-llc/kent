package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"core/server/llm"
	"core/server/sessionstore"
	"core/server/tools"
	"core/shared/textutil"
	"core/shared/toolspec"
)

func messageLimitError() error {
	return &llm.ProviderAPIError{
		ProviderID: "openai", StatusCode: textutil.Value(200),
		ProviderType: "response.incomplete", ProviderCode: "max_messages",
	}
}

func TestCompactionMessageLimitOmitsNewestAcceptedResponse(t *testing.T) {
	for _, mode := range []string{"native", "local"} {
		t.Run(mode, func(t *testing.T) {
			store := mustCreateTestSession(t)
			client := &fakeCompactionClient{responses: []llm.Response{
				finalOutputItemResponse("older"), finalOutputItemResponse("newer"),
			}}
			engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{
				Model: "gpt-6-sol", CompactionMode: mode,
			})
			for _, input := range []string{"first human", "second human"} {
				if _, err := engine.SubmitUserMessage(t.Context(), input); err != nil {
					t.Fatal(err)
				}
			}
			client.errors = []error{messageLimitError()}
			client.compactionErrors = []error{messageLimitError()}
			client.compactionResponses = []llm.CompactionResponse{remoteCompactionReplacement(100, 10, 200000)}
			client.responses = []llm.Response{finalOutputItemResponse("done")}
			if mode == "local" {
				client.errors = append(client.errors, messageLimitError())
				client.responses = append([]llm.Response{finalOutputItemResponse("summary")}, client.responses...)
			}
			if _, err := engine.SubmitUserMessage(t.Context(), "protected recent human"); err != nil {
				t.Fatal(err)
			}
			var first, second []llm.ResponseItem
			if mode == "native" {
				if len(client.compactionCalls) != 2 {
					t.Fatalf("compaction attempts=%d", len(client.compactionCalls))
				}
				first, second = client.compactionCalls[0].Items, client.compactionCalls[1].Items
			} else {
				if len(client.calls) != 6 {
					t.Fatalf("provider attempts=%d", len(client.calls))
				}
				first, second = client.calls[3].Items, client.calls[4].Items
			}
			var expected []llm.ResponseItem
			for _, item := range first {
				if item.Content == nil || *item.Content != "newer" {
					expected = append(expected, item)
				}
			}
			if len(expected) == len(first) || !reflect.DeepEqual(expected, second) {
				t.Fatalf("retry must omit exactly newest accepted response\nfirst=%+v\nsecond=%+v", first, second)
			}
		})
	}
}

func acceptedLimitResponse(t *testing.T, id string) llm.Message {
	t.Helper()
	raw := []string{
		fmt.Sprintf(`{"type":"reasoning","id":%q,"encrypted_content":"encrypted","summary":[{"type":"summary_text","text":"reason"}]}`, id+"-reason"),
		fmt.Sprintf(`{"type":"message","id":%q,"role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"first"}]}`, id+"-message-1"),
		fmt.Sprintf(`{"type":"function_call","id":%q,"call_id":%q,"name":"view_image","arguments":"{}"}`, id+"-call-1", id+"-call-1"),
		fmt.Sprintf(`{"type":"message","id":%q,"role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"second"}]}`, id+"-message-2"),
		fmt.Sprintf(`{"type":"function_call","id":%q,"call_id":%q,"name":"view_image","arguments":"{}"}`, id+"-call-2", id+"-call-2"),
	}
	body := session.NativeResponseBody{Format: session.NativeResponseOpenAI}
	for _, item := range raw {
		body.Items = append(body.Items, []byte(item))
	}
	message, err := llm.MessageFromNativeResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	return message
}

func seedLimitExchanges(t *testing.T, engine *Engine) {
	t.Helper()
	appendMessages := func(step string, messages ...llm.Message) {
		t.Helper()
		if err := steerTestActiveStep(engine, step, steerMessagesWithPersistenceIntent(
			steeringPriorityNormal, steeringMessageEventDefault, true, messages,
		)); err != nil {
			t.Fatal(err)
		}
	}
	complete := func(id string) {
		t.Helper()
		if err := steerTestActiveStep(engine, id, steerToolCompletionIntent(tools.Result{
			CallID: id, Name: toolspec.ToolViewImage,
			Output: json.RawMessage(`[{"type":"input_file","file_data":"data:application/pdf;base64,Zm9v","filename":"doc.pdf"}]`),
		})); err != nil {
			t.Fatal(err)
		}
	}
	appendMessages("protected",
		llm.Message{Role: llm.RoleDeveloper, MessageType: textutil.Value(llm.MessageTypeCompactionSummary), Content: textutil.Value("carryover")},
		llm.Message{Role: llm.RoleDeveloper, MessageType: textutil.Value(llm.MessageTypeWorkflowMode), Content: textutil.Value("assignment")},
	)
	appendMessages("old", acceptedLimitResponse(t, "old"))
	complete("old-call-1")
	appendMessages("new", acceptedLimitResponse(t, "new"))
	complete("new-call-1")
	appendMessages("interleaved", llm.Message{Role: llm.RoleUser, Content: textutil.Value("protected human")})
	complete("new-call-2")
	complete("old-call-2")
}

func TestCompactionMessageLimitKeepsCompleteResponseOwnership(t *testing.T) {
	for _, mode := range []string{"native", "local"} {
		for _, reopen := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reopen=%t", mode, reopen), func(t *testing.T) {
				store := mustCreateTestSession(t)
				client := &fakeCompactionClient{}
				cfg := Config{Model: "gpt-6-sol", CompactionMode: mode}
				engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), cfg)
				seedLimitExchanges(t, engine)
				if reopen {
					if err := engine.Close(); err != nil {
						t.Fatal(err)
					}
					store = mustOpenTestSession(t, store.Dir())
					engine = mustNewTestEngine(t, store, client, tools.NewRegistry(), cfg)
				}
				before, err := collectTestEventRecords(store)
				if err != nil {
					t.Fatal(err)
				}
				client.errors = []error{messageLimitError()}
				client.compactionErrors = []error{messageLimitError()}
				client.compactionResponses = []llm.CompactionResponse{remoteCompactionReplacement(100, 10, 200000)}
				client.responses = []llm.Response{finalOutputItemResponse("done")}
				if mode == "local" {
					client.errors = append(client.errors, messageLimitError())
					client.responses = append([]llm.Response{finalOutputItemResponse("summary")}, client.responses...)
				}
				if _, err := engine.SubmitUserMessage(t.Context(), "continue"); err != nil {
					t.Fatal(err)
				}
				var first, retry []llm.ResponseItem
				if mode == "native" {
					if len(client.compactionCalls) != 2 {
						t.Fatalf("compaction attempts=%d", len(client.compactionCalls))
					}
					first, retry = client.compactionCalls[0].Items, client.compactionCalls[1].Items
				} else {
					if len(client.calls) != 4 {
						t.Fatalf("provider attempts=%d", len(client.calls))
					}
					first, retry = client.calls[1].Items, client.calls[2].Items
				}
				removedIDs := map[string]bool{
					"new-reason": true, "new-message-1": true, "new-message-2": true,
					"new-call-1": true, "new-call-2": true,
				}
				ordinary := client.calls[0].Items
				if len(first) < len(ordinary) || !reflect.DeepEqual(first[:len(ordinary)], ordinary) {
					t.Fatal("first compaction attempt changed active history")
				}
				var expected []llm.ResponseItem
				for _, item := range first {
					if item.ID != nil && removedIDs[*item.ID] ||
						item.CallID != nil && removedIDs[*item.CallID] ||
						item.LinkedCallID != nil && removedIDs[*item.LinkedCallID] {
						continue
					}
					expected = append(expected, item)
				}
				if len(first)-len(expected) != 9 || !reflect.DeepEqual(expected, retry) {
					t.Fatalf("retry must remove one whole native response and both result/attachment pairs; removed=%d\nretry=%+v", len(first)-len(retry), retry)
				}
				finalRequest := client.calls[len(client.calls)-1]
				summaryFound := false
				for _, item := range finalRequest.Items {
					summaryFound = summaryFound || item.Type == llm.ResponseItemTypeCompaction ||
						item.MessageType != nil && *item.MessageType == llm.MessageTypeCompactionSummary
					if item.ID != nil && removedIDs[*item.ID] {
						t.Fatal("ordinary request bypassed committed replacement")
					}
				}
				if !summaryFound {
					t.Fatal("ordinary request did not use committed summary")
				}
				after, err := collectTestEventRecords(store)
				if err != nil {
					t.Fatal(err)
				}
				if len(after) < len(before) || !reflect.DeepEqual(before, after[:len(before)]) {
					t.Fatal("repair changed saved history")
				}
			})
		}
	}
}

func TestCompactionMessageLimitExhaustionDoesNotRestart(t *testing.T) {
	for _, mode := range []string{"native", "local"} {
		for _, trigger := range []string{"generation", "threshold", "handoff"} {
			t.Run(mode+"/"+trigger, func(t *testing.T) {
				store := mustCreateTestSession(t)
				client := &fakeCompactionClient{compactionErr: messageLimitError()}
				cfg := Config{
					Model: "gpt-6-sol", CompactionMode: mode,
					ContextWindowTokens: 2000, AutoCompactTokenLimit: 1000,
				}
				if trigger == "generation" {
					cfg.ContextWindowTokens, cfg.AutoCompactTokenLimit = 200000, 150000
				}
				engine := mustNewHandoffTestEngine(t, store, client, cfg)
				seedLimitExchanges(t, engine)
				if trigger == "threshold" {
					engine.setLastUsage(llm.Usage{InputTokens: textutil.Value(1900), WindowTokens: 2000,
						ContextUsage: &llm.ContextUsage{Tokens: 1900, MeasurementPoint: llm.ContextMeasurementInput}})
				}
				if trigger == "handoff" {
					engine.handoffRuntimeState().QueueRequest("summary", "")
				}
				if trigger == "generation" {
					client.errors = append(client.errors, messageLimitError())
				}
				if mode == "local" {
					for range 4 {
						client.errors = append(client.errors, messageLimitError())
					}
				}
				_, err := engine.SubmitUserMessage(t.Context(), "continue")
				var provider *llm.ProviderAPIError
				if !errors.As(err, &provider) || provider.ProviderCode != "max_messages" {
					t.Fatalf("expected provider limit, got %v", err)
				}
				var attempts [][]llm.ResponseItem
				ordinary := 0
				if trigger == "generation" {
					ordinary = 1
				}
				if mode == "native" {
					if len(client.calls) != ordinary {
						t.Fatalf("ordinary generation=%d, want %d", len(client.calls), ordinary)
					}
					for _, call := range client.compactionCalls {
						attempts = append(attempts, call.Items)
					}
				} else {
					for _, call := range client.calls[ordinary:] {
						attempts = append(attempts, call.Items)
					}
				}
				if len(attempts) != 3 {
					t.Fatalf("compaction attempts=%d, want initial plus two exchanges", len(attempts))
				}
				if len(attempts[0])-len(attempts[1]) != 9 || len(attempts[1])-len(attempts[2]) != 9 {
					t.Fatal("exhaustion must remove one complete response per request")
				}
				for _, messageType := range []llm.MessageType{llm.MessageTypeWorkflowMode, llm.MessageTypeCompactionSummary} {
					found := false
					for _, item := range attempts[2] {
						found = found || item.MessageType != nil && *item.MessageType == messageType
					}
					if !found {
						t.Fatalf("lost protected %s", messageType)
					}
				}
			})
		}
	}
}

func TestCompactionMessageLimitOmitsNonNativeCustomExchange(t *testing.T) {
	for _, mode := range []string{"native", "local"} {
		t.Run(mode, func(t *testing.T) {
			client := &fakeCompactionClient{
				errors:              []error{messageLimitError()},
				compactionErrors:    []error{messageLimitError()},
				compactionResponses: []llm.CompactionResponse{remoteCompactionReplacement(100, 10, 200000)},
				responses:           []llm.Response{finalOutputItemResponse("summary")},
			}
			engine := mustNewTestEngine(t, mustCreateTestSession(t), client, tools.NewRegistry(), Config{
				Model: "gpt-6-sol", CompactionMode: mode,
			})
			if err := steerTestActiveStep(engine, "custom", steerMessagesWithPersistenceIntent(
				steeringPriorityNormal, steeringMessageEventDefault, true, []llm.Message{
					{Role: llm.RoleAssistant, Content: textutil.Value("custom exchange"),
						ReasoningItems: []llm.ReasoningItem{{ID: "custom-reason", EncryptedContent: "encrypted"}},
						ToolCalls:      []llm.ToolCall{{ID: "custom-call", Name: "patch", Custom: true, CustomInput: textutil.Value("patch")}}},
					{Role: llm.RoleUser, Content: textutil.Value("interleaved human")},
					{Role: llm.RoleTool, ToolCallID: textutil.Value("custom-call"), Content: textutil.Value(`"result"`),
						MessageType: textutil.Value(llm.MessageTypeCustomToolCallOutput)},
				},
			)); err != nil {
				t.Fatal(err)
			}
			if err := steerTestActiveStep(engine, "protected-envelope", steerMessagesWithPersistenceIntent(
				steeringPriorityNormal, steeringMessageEventDefault, true, []llm.Message{{
					Role: llm.RoleAssistant, MessageType: textutil.Value(llm.MessageTypeCompactionSummary),
					Content: textutil.Value("protected summary envelope"),
				}},
			)); err != nil {
				t.Fatal(err)
			}
			if _, _, err := compactNowInActiveTestRun(t, engine, compactionModeManual, compactionInstructionsInput{}); err != nil {
				t.Fatal(err)
			}
			var first, retry []llm.ResponseItem
			if mode == "native" {
				first, retry = client.compactionCalls[0].Items, client.compactionCalls[1].Items
			} else {
				first, retry = client.calls[0].Items, client.calls[1].Items
			}
			var expected []llm.ResponseItem
			for _, item := range first {
				if item.ID != nil && (*item.ID == "custom-reason" || *item.ID == "custom-call") ||
					item.CallID != nil && *item.CallID == "custom-call" ||
					item.Content != nil && *item.Content == "custom exchange" {
					continue
				}
				expected = append(expected, item)
			}
			if len(first)-len(expected) != 4 || !reflect.DeepEqual(expected, retry) {
				t.Fatal("custom call/result, reasoning and assistant must be one omission unit; protected envelope must stay")
			}
		})
	}
}

func TestCompactionMessageLimitProtectsIncompleteResponses(t *testing.T) {
	for _, mode := range []string{"native", "local"} {
		t.Run(mode, func(t *testing.T) {
			client := &fakeCompactionClient{
				compactionErr: messageLimitError(),
				errors:        []error{messageLimitError(), messageLimitError(), messageLimitError()},
			}
			engine := mustNewTestEngine(t, mustCreateTestSession(t), client, tools.NewRegistry(), Config{
				Model: "gpt-6-sol", CompactionMode: mode,
			})
			for _, entry := range []struct {
				step    string
				message llm.Message
			}{
				{"complete", llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("eligible")}},
				{"incomplete", acceptedLimitResponse(t, "incomplete")},
			} {
				if err := steerTestActiveStep(engine, entry.step, steerMessagesWithPersistenceIntent(
					steeringPriorityNormal, steeringMessageEventDefault, true, []llm.Message{entry.message},
				)); err != nil {
					t.Fatal(err)
				}
			}
			_, _, err := compactNowInActiveTestRun(t, engine, compactionModeManual, compactionInstructionsInput{})
			var provider *llm.ProviderAPIError
			if !errors.As(err, &provider) || provider.ProviderCode != "max_messages" {
				t.Fatalf("expected original failure, got %v", err)
			}
			var first, last []llm.ResponseItem
			if mode == "native" {
				if len(client.compactionCalls) != 2 {
					t.Fatalf("compaction attempts=%d", len(client.compactionCalls))
				}
				first, last = client.compactionCalls[0].Items, client.compactionCalls[1].Items
			} else {
				if len(client.calls) != 2 {
					t.Fatalf("compaction attempts=%d", len(client.calls))
				}
				first, last = client.calls[0].Items, client.calls[1].Items
			}
			var expected []llm.ResponseItem
			for _, item := range first {
				if item.Content == nil || *item.Content != "eligible" {
					expected = append(expected, item)
				}
			}
			if len(first)-len(expected) != 1 || !reflect.DeepEqual(last, expected) {
				t.Fatal("incomplete response must stay intact while an older complete response is omitted")
			}
		})
	}
}

func TestMessageLimitRecoveryLeavesOtherErrorsUnchanged(t *testing.T) {
	for _, mode := range []string{"native", "local"} {
		for _, operation := range []string{"generation", "compaction"} {
			t.Run(mode+"/"+operation, func(t *testing.T) {
				other := &llm.ProviderAPIError{ProviderID: "openai", StatusCode: textutil.Value(200),
					ProviderType: "response.incomplete", ProviderCode: "max_output_tokens"}
				client := &fakeCompactionClient{}
				engine := mustNewTestEngine(t, mustCreateTestSession(t), client, tools.NewRegistry(), Config{
					Model: "gpt-6-sol", CompactionMode: mode,
				})
				seedLimitExchanges(t, engine)
				client.errors = []error{other}
				if operation == "compaction" {
					client.errors = []error{messageLimitError()}
					client.compactionErr = other
					if mode == "local" {
						client.errors = append(client.errors, other)
					}
				}
				_, err := engine.SubmitUserMessage(t.Context(), "continue")
				if !errors.Is(err, other) {
					t.Fatalf("expected unchanged provider error, got %v", err)
				}
				generationCount, compactionCount := 1, 0
				if operation == "compaction" {
					if mode == "local" {
						generationCount++
					} else {
						compactionCount++
					}
				}
				if len(client.calls) != generationCount || len(client.compactionCalls) != compactionCount {
					t.Fatalf("unexpected retry: generation=%d compaction=%d", len(client.calls), len(client.compactionCalls))
				}
			})
		}
	}
}

func TestGenerationMessageLimitHonorsDisabledCompaction(t *testing.T) {
	for _, mode := range []string{"none", "native"} {
		t.Run(mode, func(t *testing.T) {
			client := &fakeCompactionClient{errors: []error{messageLimitError()}}
			cfg := Config{Model: "gpt-6-sol", CompactionMode: mode}
			if mode == "native" {
				cfg.AutoCompactionEnabled = textutil.Value(false)
			}
			engine := mustNewTestEngine(t, mustCreateTestSession(t), client, tools.NewRegistry(), cfg)
			_, err := engine.SubmitUserMessage(t.Context(), "continue")
			var provider *llm.ProviderAPIError
			if !errors.As(err, &provider) || provider.ProviderCode != "max_messages" {
				t.Fatalf("expected provider failure, got %v", err)
			}
			if len(client.calls) != 1 || len(client.compactionCalls) != 0 {
				t.Fatalf("generation=%d compaction=%d", len(client.calls), len(client.compactionCalls))
			}
		})
	}
}

func TestGenerationMessageLimitStopsWithoutProgress(t *testing.T) {
	client := &fakeCompactionClient{
		errors: []error{messageLimitError(), messageLimitError()},
		compactionResponses: []llm.CompactionResponse{{OutputItems: []llm.ResponseItem{{
			Type: llm.ResponseItemTypeCompaction, EncryptedContent: textutil.Value("summary"),
		}}}},
	}
	engine := mustNewTestEngine(t, mustCreateTestSession(t), client, tools.NewRegistry(), Config{
		Model: "gpt-6-sol", CompactionMode: "native",
	})
	_, err := engine.SubmitUserMessage(t.Context(), "continue")
	var provider *llm.ProviderAPIError
	if !errors.As(err, &provider) || provider.ProviderCode != "max_messages" {
		t.Fatalf("expected provider failure, got %v", err)
	}
	if len(client.calls) != 2 || len(client.compactionCalls) != 1 {
		t.Fatalf("generation=%d compaction=%d", len(client.calls), len(client.compactionCalls))
	}
}

func TestGenerationMessageLimitCanRecoverAgainAfterCompletedWork(t *testing.T) {
	client := &fakeCompactionClient{
		errors: []error{messageLimitError(), nil, messageLimitError(), nil},
		responses: []llm.Response{{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("working"),
				Phase: textutil.Value(llm.MessagePhaseCommentary)},
			ToolCalls: []llm.ToolCall{{ID: "work", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"cmd":"true"}`)}},
		}, finalOutputItemResponse("done")},
		compactionResponses: []llm.CompactionResponse{
			remoteCompactionReplacement(100, 10, 200000), remoteCompactionReplacement(100, 10, 200000),
		},
	}
	engine := mustNewExecTestEngine(t, mustCreateTestSession(t), client, Config{
		Model: "gpt-6-sol", CompactionMode: "native",
	})
	if _, err := engine.SubmitUserMessage(t.Context(), "continue"); err != nil {
		t.Fatal(err)
	}
	if len(client.calls) != 4 || len(client.compactionCalls) != 2 {
		t.Fatalf("generation=%d compaction=%d", len(client.calls), len(client.compactionCalls))
	}
}

func TestPreparationCompactionMessageLimitDoesNotRestartRecovery(t *testing.T) {
	for _, trigger := range []string{"threshold", "handoff"} {
		t.Run(trigger, func(t *testing.T) {
			client := &fakeCompactionClient{compactionErr: messageLimitError()}
			engine := mustNewHandoffTestEngine(t, mustCreateTestSession(t), client, Config{
				Model: "gpt-6-sol", CompactionMode: "native",
				ContextWindowTokens: 2000, AutoCompactTokenLimit: 1000,
			})
			if trigger == "handoff" {
				engine.handoffRuntimeState().QueueRequest("summary", "")
			}
			engine.setLastUsage(llm.Usage{InputTokens: textutil.Value(1900), WindowTokens: 2000,
				ContextUsage: &llm.ContextUsage{Tokens: 1900, MeasurementPoint: llm.ContextMeasurementInput}})
			_, err := engine.SubmitUserMessage(t.Context(), "continue")
			var provider *llm.ProviderAPIError
			if !errors.As(err, &provider) || provider.ProviderCode != "max_messages" {
				t.Fatalf("expected compaction failure, got %v", err)
			}
			if len(client.calls) != 0 || len(client.compactionCalls) != 1 {
				t.Fatalf("generation=%d compaction=%d", len(client.calls), len(client.compactionCalls))
			}
		})
	}
}

func TestGenerationMessageLimitCompactsAndContinues(t *testing.T) {
	store := mustCreateTestSession(t)
	client := &fakeCompactionClient{
		errors: []error{messageLimitError()},
		responses: []llm.Response{{Assistant: llm.Message{
			Role: llm.RoleAssistant, Content: textutil.Value("done"),
		}}},
		compactionResponses: []llm.CompactionResponse{{OutputItems: []llm.ResponseItem{{
			Type: llm.ResponseItemTypeCompaction, EncryptedContent: textutil.Value("summary"),
		}}}},
	}
	engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{
		Model: "gpt-6-sol", CompactionMode: "native",
	})
	if _, err := engine.SubmitUserMessage(t.Context(), "continue"); err != nil {
		t.Fatal(err)
	}
	if len(client.calls) != 2 || len(client.compactionCalls) != 1 {
		t.Fatalf("generation=%d compaction=%d", len(client.calls), len(client.compactionCalls))
	}
	found := false
	for _, item := range client.calls[1].Items {
		found = found || item.Type == llm.ResponseItemTypeCompaction
	}
	if !found {
		t.Fatal("continued generation did not use committed summary")
	}
}
