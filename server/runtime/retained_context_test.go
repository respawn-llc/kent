package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"core/server/llm"
	"core/server/session"
	"core/server/tools"
	"core/shared/modelcontract"
	"core/shared/textutil"

	"github.com/google/uuid"
)

func TestDispatchOmitsForeignReasoningAndPreservesConversation(t *testing.T) {
	store := mustCreateTestSession(t)
	items := llm.PrepareResponsesInputItems([]llm.ResponseItem{
		{Type: llm.ResponseItemTypeMessage, Role: textutil.Value(llm.RoleUser), Content: textutil.Value("retained user")},
		{
			Type: llm.ResponseItemTypeReasoning, ID: textutil.Value("rs_foreign"),
			EncryptedContent: textutil.Value("foreign-opaque"),
			Attribution:      &modelcontract.ReasoningAttribution{Type: textutil.Value(modelcontract.ReasoningTypeAnthropic)},
			ReasoningSummary: []llm.ReasoningEntry{{Text: "foreign readable summary"}},
		},
		{Type: llm.ResponseItemTypeFunctionCall, ID: textutil.Value("call_kept"), CallID: textutil.Value("call_kept"), Name: textutil.Value("exec_command"), Arguments: json.RawMessage(`{"cmd":"pwd"}`)},
		{Type: llm.ResponseItemTypeFunctionCallOutput, CallID: textutil.Value("call_kept"), Output: json.RawMessage(`"complete"`)},
	})
	persistRetainedItems(t, store, items)
	client := &fakeClient{responses: []llm.Response{finalTextResponse("continued")}}
	engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{})
	sessionID := engine.SessionID()
	if _, err := engine.SubmitUserMessage(context.Background(), "continue"); err != nil {
		t.Fatal(err)
	}
	if engine.SessionID() != sessionID {
		t.Fatal("continuation changed Session identity")
	}
	var found []llm.ResponseItem
	for _, item := range client.calls[0].Items {
		if item.Type == llm.ResponseItemTypeReasoning {
			t.Fatal("dispatch retained foreign reasoning")
		}
		if item.CallID != nil && *item.CallID == "call_kept" || item.Content != nil && *item.Content == "retained user" {
			found = append(found, item)
		}
	}
	if !reflect.DeepEqual(found, []llm.ResponseItem{items[0], items[2], items[3]}) {
		t.Fatalf("retained conversation changed: %+v", found)
	}
	if !reflect.DeepEqual(engine.transcriptRuntimeState().SnapshotItems()[1], items[1]) {
		t.Fatal("omission changed retained reasoning")
	}
}

func persistRetainedItems(t *testing.T, store *session.Store, items []llm.ResponseItem) {
	t.Helper()
	replacement, err := sessionHistoryReplacementRecordFromRuntime(historyReplacementPayload{Engine: "local", Mode: string(compactionModeManual), Items: llm.PrepareResponsesInputItems(items)})
	if err != nil {
		t.Fatal(err)
	}
	stepID := runtimeTestStepID("retained")
	log := mustMaterializeTestEventLog(t, store)
	if _, _, err := log.AppendRecord(&stepID, replacement); err != nil {
		t.Fatal(err)
	}
}

func TestRetainedReasoningDispatchCompatibility(t *testing.T) {
	for _, test := range []struct {
		name        string
		attribution *modelcontract.ReasoningAttribution
		encrypted   *string
		destination string
		supported   bool
		retained    bool
	}{
		{"matching", &modelcontract.ReasoningAttribution{Type: textutil.Value(modelcontract.ReasoningTypeOpenAI)}, textutil.Value("opaque"), "openai", true, true},
		{"same-type-renamed-connection", &modelcontract.ReasoningAttribution{Type: textutil.Value(modelcontract.ReasoningTypeOpenAI)}, textutil.Value("opaque"), "chatgpt-codex", true, true},
		{"plaintext", &modelcontract.ReasoningAttribution{Type: textutil.Value(modelcontract.ReasoningTypeUnencrypted)}, nil, "openai-compatible", false, true},
		{"legacy-unknown", nil, textutil.Value("opaque"), "openai", true, true},
		{"recorded-unknown", &modelcontract.ReasoningAttribution{}, textutil.Value("opaque"), "openai", true, true},
		{"unknown-destination", &modelcontract.ReasoningAttribution{Type: textutil.Value(modelcontract.ReasoningTypeOpenAI)}, textutil.Value("opaque"), "openai-compatible", true, true},
		{"unsupported", &modelcontract.ReasoningAttribution{Type: textutil.Value(modelcontract.ReasoningTypeOpenAI)}, textutil.Value("opaque"), "openai", false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := mustCreateTestSession(t)
			item := llm.PrepareResponsesInputItems([]llm.ResponseItem{{
				Type: llm.ResponseItemTypeReasoning, ID: textutil.Value("rs_matrix"), Attribution: test.attribution,
				EncryptedContent: test.encrypted, ReasoningSummary: []llm.ReasoningEntry{{Text: "readable trace"}},
			}})[0]
			persistRetainedItems(t, store, []llm.ResponseItem{item})
			caps := defaultTestProviderCapabilities()
			caps.ProviderID, caps.SupportsReasoningEncrypted = test.destination, test.supported
			client := &fakeClient{caps: caps, responses: []llm.Response{finalTextResponse("continued")}}
			engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{})
			if _, err := engine.SubmitUserMessage(context.Background(), "continue"); err != nil {
				t.Fatal(err)
			}
			retained := false
			for _, sent := range client.calls[0].Items {
				if sent.Type == llm.ResponseItemTypeReasoning {
					retained = true
					if !reflect.DeepEqual(sent, item) {
						t.Fatalf("retained item changed: %+v", sent)
					}
				}
			}
			if retained != test.retained {
				t.Fatalf("reasoning retained = %t, want %t", retained, test.retained)
			}
		})
	}
}

func TestActiveCheckpointIsBlockedRatherThanOmitted(t *testing.T) {
	for _, test := range []struct {
		name      string
		origin    *modelcontract.ReasoningAttribution
		provider  string
		responses bool
		blocked   bool
	}{
		{"known-foreign", &modelcontract.ReasoningAttribution{Type: textutil.Value(modelcontract.ReasoningTypeAnthropic)}, "openai", true, true},
		{"no-protocol", nil, "openai-compatible", false, true},
		{"known-matching", &modelcontract.ReasoningAttribution{Type: textutil.Value(modelcontract.ReasoningTypeOpenAI)}, "openai", true, false},
		{"unknown-source", nil, "openai", true, false},
		{"recorded-unknown", &modelcontract.ReasoningAttribution{}, "openai", true, false},
		{"unknown-consumer", &modelcontract.ReasoningAttribution{Type: textutil.Value(modelcontract.ReasoningTypeOpenAI)}, "openai-compatible", true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := mustCreateTestSession(t)
			checkpoint := llm.PrepareResponsesInputItems([]llm.ResponseItem{{
				Type: llm.ResponseItemTypeCompaction, ID: textutil.Value("cmp_kept"),
				EncryptedContent: textutil.Value("checkpoint"), Attribution: test.origin,
			}})[0]
			persistRetainedItems(t, store, []llm.ResponseItem{checkpoint})
			caps := defaultTestProviderCapabilities()
			caps.ProviderID, caps.SupportsResponsesAPI, caps.SupportsResponsesCompact = test.provider, test.responses, false
			client := &fakeClient{caps: caps, responses: []llm.Response{finalTextResponse("continued")}}
			engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{})
			_, err := engine.SubmitUserMessage(context.Background(), "continue")
			if test.blocked {
				var compatibility *llm.RetainedContextCompatibilityError
				if !errors.As(err, &compatibility) || compatibility.ItemType != llm.ResponseItemTypeCompaction {
					t.Fatalf("checkpoint error = %v", err)
				}
				if len(client.calls) != 0 {
					t.Fatal("incompatible checkpoint dispatched")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, item := range client.calls[0].Items {
					if item.Type == llm.ResponseItemTypeCompaction {
						found = true
						if !reflect.DeepEqual(item, checkpoint) {
							t.Fatalf("checkpoint changed: %+v", item)
						}
					}
				}
				if !found {
					t.Fatal("checkpoint omitted")
				}
			}
			if !reflect.DeepEqual(engine.transcriptRuntimeState().SnapshotItems()[0], checkpoint) {
				t.Fatal("retained checkpoint changed")
			}
		})
	}
}

func TestLegacyCheckpointAttributionUsesOnlyActiveCompactionEvidence(t *testing.T) {
	for _, test := range []struct {
		name        string
		purpose     modelcontract.ProviderOperationPurpose
		before      bool
		attribution *modelcontract.ReasoningAttribution
		inferred    bool
	}{
		{"active-compaction", modelcontract.ProviderOperationPurposeCompaction, false, nil, true},
		{"generation", modelcontract.ProviderOperationPurposeGeneration, false, nil, false},
		{"behind-replacement", modelcontract.ProviderOperationPurposeCompaction, true, nil, false},
		{"recorded-unknown", modelcontract.ProviderOperationPurposeCompaction, false, &modelcontract.ReasoningAttribution{}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := mustCreateTestSession(t)
			log := mustMaterializeTestEventLog(t, store)
			stepID := runtimeTestStepID("retained")
			appendObservation := func() {
				t.Helper()
				if _, _, err := log.AppendRecord(&stepID, session.CacheResponseObservationRecord{
					OperationID: textutil.Value(uuid.NewString()), ObservedAt: textutil.Value(time.Now()),
					SessionID: textutil.Value(store.Meta().SessionID), Purpose: &test.purpose,
					ProviderUsage: &modelcontract.ProviderUsageEvidence{ProviderID: textutil.Value("openai"), RequestedModel: "gpt-6-sol"},
				}); err != nil {
					t.Fatal(err)
				}
			}
			if test.before {
				appendObservation()
			}
			persistRetainedItems(t, store, []llm.ResponseItem{{
				Type: llm.ResponseItemTypeCompaction, EncryptedContent: textutil.Value("checkpoint"), Attribution: test.attribution,
			}})
			if !test.before {
				appendObservation()
			}
			client := &fakeClient{responses: []llm.Response{finalTextResponse("done")}}
			engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{})
			if _, err := engine.SubmitUserMessage(context.Background(), "continue"); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, item := range client.calls[0].Items {
				if item.Type != llm.ResponseItemTypeCompaction {
					continue
				}
				found = true
				known := item.Attribution != nil && item.Attribution.Type != nil
				if known != test.inferred || known && *item.Attribution.Type != modelcontract.ReasoningTypeOpenAI {
					t.Fatalf("checkpoint attribution = %+v", item.Attribution)
				}
				if test.attribution != nil && item.Attribution == nil {
					t.Fatal("recorded unknown attribution lost")
				}
			}
			if !found {
				t.Fatal("legacy checkpoint omitted")
			}
		})
	}
}

func TestLegacyReasoningUsesProducingStepEvidence(t *testing.T) {
	testLegacyReasoningEvidence(t, []string{"openai"}, textutil.Value(modelcontract.ReasoningTypeOpenAI))
}

func TestLegacyReasoningStaysUnknownAcrossSuccessiveBackfills(t *testing.T) {
	testLegacyReasoningEvidence(t, nil, nil)
}

func TestLegacyReasoningConflictingEvidenceStaysUnknown(t *testing.T) {
	testLegacyReasoningEvidence(t, []string{"openai", "anthropic"}, nil)
}

func TestLegacyReasoningUnknownFormatEvidenceStaysUnknown(t *testing.T) {
	testLegacyReasoningEvidence(t, []string{"openai-compatible"}, nil)
}

func TestLegacyReasoningIgnoresUnrelatedEvidence(t *testing.T) {
	for _, scope := range []struct {
		name    string
		stepID  string
		session *string
		purpose modelcontract.ProviderOperationPurpose
	}{
		{"different-step", runtimeTestStepID("other"), nil, modelcontract.ProviderOperationPurposeGeneration},
		{"different-session", runtimeTestStepID("producer"), textutil.Value(uuid.NewString()), modelcontract.ProviderOperationPurposeGeneration},
		{"reviewer", runtimeTestStepID("producer"), nil, modelcontract.ProviderOperationPurposeReviewer},
		{"compaction", runtimeTestStepID("producer"), nil, modelcontract.ProviderOperationPurposeCompaction},
	} {
		t.Run(scope.name, func(t *testing.T) {
			testLegacyReasoningEvidence(t, []string{"openai"}, nil, func(record *session.CacheResponseObservationRecord) string {
				record.Purpose = &scope.purpose
				if scope.session != nil {
					record.SessionID = scope.session
				}
				return scope.stepID
			})
		})
	}
}

func testLegacyReasoningEvidence(t *testing.T, providers []string, want *modelcontract.ReasoningType, overrides ...func(*session.CacheResponseObservationRecord) string) {
	t.Helper()
	store := mustCreateTestSession(t)
	stepID := runtimeTestStepID("producer")
	log := mustMaterializeTestEventLog(t, store)
	if _, _, err := log.AppendRecord(&stepID, session.MessageRecord{
		Role:           session.MessageRoleAssistant,
		ReasoningItems: []session.MessageReasoningRecord{{ID: "rs_legacy", EncryptedContent: "legacy-opaque"}},
	}); err != nil {
		t.Fatal(err)
	}
	for _, provider := range providers {
		observation := session.CacheResponseObservationRecord{
			OperationID:   textutil.Value(uuid.NewString()),
			ObservedAt:    textutil.Value(time.Now()),
			SessionID:     textutil.Value(store.Meta().SessionID),
			Purpose:       textutil.Value(modelcontract.ProviderOperationPurposeGeneration),
			ProviderUsage: &modelcontract.ProviderUsageEvidence{ProviderID: textutil.Value(provider), RequestedModel: "gpt-6-sol"},
		}
		observationStep := stepID
		for _, override := range overrides {
			observationStep = override(&observation)
		}
		if _, _, err := log.AppendRecord(&observationStep, observation); err != nil {
			t.Fatal(err)
		}
	}
	for open := range 2 {
		caps := defaultTestProviderCapabilities()
		if open == 0 {
			caps.ProviderID = "openai-compatible"
		}
		client := &fakeClient{caps: caps, responses: []llm.Response{finalTextResponse("done")}}
		engine := mustNewTestEngine(t, mustOpenTestSession(t, store.Dir()), client, tools.NewRegistry(), Config{})
		if _, err := engine.SubmitUserMessage(context.Background(), "continue"); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range client.calls[0].Items {
			if item.Type != llm.ResponseItemTypeReasoning {
				continue
			}
			found = true
			if want == nil {
				if item.Attribution != nil && item.Attribution.Type != nil {
					t.Fatalf("unidentified producer inherited attribution: %+v", item.Attribution)
				}
			} else if item.Attribution == nil || item.Attribution.Type == nil || *item.Attribution.Type != *want {
				t.Fatalf("legacy attribution = %+v", item.Attribution)
			}
		}
		if !found {
			t.Fatal("legacy reasoning lost")
		}
		if err := engine.Close(); err != nil {
			t.Fatal(err)
		}
	}
	window, err := mustMaterializeTestEventLog(t, mustOpenTestSession(t, store.Dir())).ReadNewestSegmentBackward(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range window.Records {
		payload, err := record.Payload()
		if err != nil {
			t.Fatal(err)
		}
		if message, ok := payload.(session.MessageRecord); ok && len(message.ReasoningItems) > 0 && message.ReasoningItems[0].Attribution != nil {
			t.Fatal("historical reasoning was rewritten")
		}
	}
}

func TestProducedCheckpointAttributionSurvivesResume(t *testing.T) {
	testProducedCheckpointAttributionSurvivesResume(t, defaultTestProviderCapabilities(), textutil.Value(modelcontract.ReasoningTypeOpenAI), textutil.Value("openai"))
}

func TestRecordedUnknownCheckpointSurvivesResume(t *testing.T) {
	for _, provider := range []*string{nil, textutil.Value("openai-compatible"), textutil.Value("unregistered-provider")} {
		testProducedCheckpointAttributionSurvivesResume(t, defaultTestProviderCapabilities(), nil, provider)
	}
}

func testProducedCheckpointAttributionSurvivesResume(t *testing.T, caps llm.ProviderCapabilities, want *modelcontract.ReasoningType, provider *string) {
	t.Helper()
	store := mustCreateTestSession(t)
	raw := json.RawMessage(`{"type":"compaction","id":"cmp_origin","encrypted_content":"checkpoint-opaque"}`)
	client := &fakeCompactionClient{
		caps: caps,
		compactionResponses: []llm.CompactionResponse{{ProviderEvidence: modelcontract.ProviderUsageEvidence{ProviderID: provider}, OutputItems: []llm.ResponseItem{{
			Type: llm.ResponseItemTypeCompaction, Raw: raw,
		}}}},
	}
	engine := mustNewTestEngine(t, store, &completedDispatchClient{Client: client}, tools.NewRegistry(), Config{CompactionMode: "native"})
	completeManualEligibilityAgentStep(t, engine)
	scheduleManualCompactionAndWait(t, engine)
	if len(client.compactionCalls) != 1 {
		t.Fatalf("compaction calls = %d", len(client.compactionCalls))
	}
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
	reopenedClient := &fakeClient{responses: []llm.Response{finalTextResponse("continued")}}
	reopened := mustNewTestEngine(t, mustOpenTestSession(t, store.Dir()), reopenedClient, tools.NewRegistry(), Config{})
	if _, err := reopened.SubmitUserMessage(context.Background(), "continue"); err != nil {
		t.Fatal(err)
	}
	for _, item := range reopenedClient.calls[0].Items {
		if item.Type != llm.ResponseItemTypeCompaction {
			continue
		}
		if item.Attribution == nil ||
			(want == nil && item.Attribution.Type != nil) ||
			(want != nil && (item.Attribution.Type == nil || *item.Attribution.Type != *want)) {
			t.Fatalf("checkpoint attribution = %+v", item.Attribution)
		}
		if !bytes.Equal(item.Raw, raw) {
			t.Fatalf("checkpoint bytes changed: %s", item.Raw)
		}
		return
	}
	t.Fatal("checkpoint lost on resume")
}

func TestProducedReasoningAttributionSurvivesResume(t *testing.T) {
	testProducedReasoningAttributionSurvivesResume(t, defaultTestProviderCapabilities(), textutil.Value(modelcontract.ReasoningTypeOpenAI), textutil.Value("openai"))
}

func TestRecordedUnknownReasoningDoesNotInheritOldContractOnResume(t *testing.T) {
	for _, provider := range []*string{nil, textutil.Value("openai-compatible"), textutil.Value("unregistered-provider")} {
		testProducedReasoningAttributionSurvivesResume(t, defaultTestProviderCapabilities(), nil, provider)
	}
}

func testProducedReasoningAttributionSurvivesResume(t *testing.T, producingCaps llm.ProviderCapabilities, want *modelcontract.ReasoningType, provider *string) {
	t.Helper()
	store := mustCreateTestSession(t)
	original := mustNewTestEngine(t, store, &fakeClient{responses: []llm.Response{finalTextResponse("original")}}, tools.NewRegistry(), Config{})
	if _, err := original.SubmitUserMessage(context.Background(), "original"); err != nil {
		t.Fatal(err)
	}
	if err := original.Close(); err != nil {
		t.Fatal(err)
	}
	raw := json.RawMessage(`{"type":"reasoning","id":"rs_origin","encrypted_content":"opaque","summary":[]}`)
	response := llm.Response{
		ProviderEvidence: modelcontract.ProviderUsageEvidence{ProviderID: provider, RequestedModel: "gpt-6-sol"},
		Assistant:        llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("done"), Phase: textutil.Value(llm.MessagePhaseFinal)},
		ReasoningItems:   []llm.ReasoningItem{{ID: "rs_origin", EncryptedContent: "opaque"}},
		OutputItems: []llm.ResponseItem{
			{Type: llm.ResponseItemTypeReasoning, ID: textutil.Value("rs_origin"), EncryptedContent: textutil.Value("opaque"), Raw: raw},
			{Type: llm.ResponseItemTypeMessage, Role: textutil.Value(llm.RoleAssistant), Content: textutil.Value("done"), Phase: textutil.Value(llm.MessagePhaseFinal)},
		},
	}
	engine := mustNewTestEngine(t, store, &completedDispatchClient{Client: &fakeClient{responses: []llm.Response{response}, caps: producingCaps}}, tools.NewRegistry(), Config{})
	if _, err := engine.SubmitUserMessage(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{responses: []llm.Response{{Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("continued"), Phase: textutil.Value(llm.MessagePhaseFinal)}}}}
	reopened := mustNewTestEngine(t, mustOpenTestSession(t, store.Dir()), client, tools.NewRegistry(), Config{})
	if _, err := reopened.SubmitUserMessage(context.Background(), "continue"); err != nil {
		t.Fatal(err)
	}
	for _, item := range client.calls[0].Items {
		if item.Type == llm.ResponseItemTypeReasoning {
			if item.Attribution == nil ||
				(want == nil && item.Attribution.Type != nil) ||
				(want != nil && (item.Attribution.Type == nil || *item.Attribution.Type != *want)) {
				t.Fatalf("restored attribution = %+v", item.Attribution)
			}
			if item.EncryptedContent == nil || *item.EncryptedContent != "opaque" {
				t.Fatalf("reasoning payload changed: %+v", item.EncryptedContent)
			}
			return
		}
	}
	t.Fatal("restored request lost reasoning")
}

// The producing response remains valid even if credentials become unavailable
// or the connection changes immediately after the provider returns.
type completedDispatchClient struct {
	llm.Client
	completed bool
}

func (c *completedDispatchClient) ProviderCapabilities(ctx context.Context) (llm.ProviderCapabilities, error) {
	if c.completed {
		return llm.ProviderCapabilities{}, errors.New("credentials no longer available")
	}
	return c.Client.(llm.ProviderCapabilitiesClient).ProviderCapabilities(ctx)
}

func (c *completedDispatchClient) Generate(ctx context.Context, request llm.Request, callbacks llm.StreamCallbacks) (llm.Response, error) {
	response, err := c.Client.Generate(ctx, request, callbacks)
	c.completed = true
	return response, err
}

func (c *completedDispatchClient) Compact(ctx context.Context, request llm.CompactionRequest) (llm.CompactionResponse, error) {
	response, err := c.Client.(llm.CompactionClient).Compact(ctx, request)
	c.completed = true
	return response, err
}

func (c *completedDispatchClient) PrepareCompaction(request llm.CompactionRequest) llm.CompactionRequest {
	return c.Client.(llm.CompactionClient).PrepareCompaction(request)
}
