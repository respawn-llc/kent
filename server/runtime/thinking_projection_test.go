package runtime

import (
	"testing"

	"core/server/llm"
	"core/server/tools"
	"core/shared/textutil"
)

func TestNativeThinkingProjection(t *testing.T) {
	update := func(effort string) llm.ResponseItem {
		return llm.PrepareOpenAIInputItems([]llm.ResponseItem{{
			Type: llm.ResponseItemTypeConfigurationUpdate, ConfigurationEffort: &effort,
		}})[0]
	}
	user := llm.ItemsFromMessages([]llm.Message{{Role: llm.RoleUser, Content: textutil.Value("input")}})[0]
	for _, tc := range []struct {
		name        string
		items       []llm.ResponseItem
		desired     string
		replacement *int
		want        bool
	}{
		{name: "no-op", desired: "high"},
		{name: "changed", desired: "low", want: true},
		{name: "return to original", items: []llm.ResponseItem{update("low"), user}, desired: "high", want: true},
		{name: "already applied", items: []llm.ResponseItem{update("low"), user}, desired: "low"},
		{name: "adjacent deferred", items: []llm.ResponseItem{update("low")}, desired: "high"},
		{name: "post compaction baseline", items: []llm.ResponseItem{user}, desired: "high", replacement: textutil.Value(1), want: true},
		{name: "post compaction applied", items: []llm.ResponseItem{user, update("high"), user}, desired: "high", replacement: textutil.Value(1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projected, err := prepareNativeThinking(tc.items, tc.replacement, tc.desired, textutil.Value("high"), true)
			if err != nil {
				t.Fatal(err)
			}
			if projected.effort != "high" || (projected.update != nil) != tc.want {
				t.Fatalf("projection = %+v", projected)
			}
			if projected.update != nil && *projected.update.ConfigurationEffort != tc.desired {
				t.Fatal("projection did not use final desired effort")
			}
		})
	}
}

func TestThinkingInspectionDoesNotAdoptOrCommit(t *testing.T) {
	store := mustCreateTestSession(t)
	client := &fakeClient{caps: llm.ProviderCapabilities{
		ProviderID: "openai", SupportsResponsesAPI: true, SupportsNativeThinkingUpdates: true,
	}}
	engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{
		Model: "gpt-6-astra", ThinkingLevel: "high",
	})
	first, err := PrepareInspectionRequest(t.Context(), engine, false)
	if err != nil {
		t.Fatal(err)
	}
	if first.ReasoningEffort != "high" || store.Meta().OriginalThinkingEffort != nil {
		t.Fatal("inspection adopted an original effort")
	}
	if err := store.AdoptOriginalThinkingEffort("high"); err != nil {
		t.Fatal(err)
	}
	if err := engine.SetThinkingLevel(t.Context(), "low"); err != nil {
		t.Fatal(err)
	}
	before := store.Meta().LastSequence
	second, err := PrepareInspectionRequest(t.Context(), engine, false)
	if err != nil {
		t.Fatal(err)
	}
	last := second.Items[len(second.Items)-1]
	if second.ReasoningEffort != "high" || last.ConfigurationEffort == nil || *last.ConfigurationEffort != "low" ||
		store.Meta().LastSequence != before || len(client.calls) != 0 || engine.ThinkingLevel() != "low" {
		t.Fatal("inspection did not purely project desired Thinking")
	}
}

func TestThinkingLookupNearActiveContextLimit(t *testing.T) {
	items := make([]llm.ResponseItem, 25000)
	items[0] = llm.ResponseItem{Type: llm.ResponseItemTypeConfigurationUpdate, ConfigurationEffort: textutil.Value("low")}
	for i := 1; i < len(items); i++ {
		items[i] = llm.ResponseItem{Type: llm.ResponseItemTypeMessage, Content: textutil.Value("representative active context content")}
	}
	projection, err := prepareNativeThinking(items, nil, "low", textutil.Value("high"), true)
	if err != nil || projection.update != nil || projection.effort != "high" {
		t.Fatalf("near-limit lookup = %+v, %v", projection, err)
	}
}

func TestThinkingEligibilityUsesPreparedTransportDespiteOverride(t *testing.T) {
	for _, supported := range []bool{true, false} {
		caps := llm.ProviderCapabilities{ProviderID: "openai", SupportsResponsesAPI: true, SupportsNativeThinkingUpdates: supported}
		override := caps
		override.SupportsNativeThinkingUpdates = !supported
		store := mustCreateTestSession(t)
		if err := store.AdoptOriginalThinkingEffort("high"); err != nil {
			t.Fatal(err)
		}
		engine := mustNewTestEngine(t, store, &fakeClient{caps: caps}, tools.NewRegistry(), Config{
			Model: "gpt-6-astra", ThinkingLevel: "low", ProviderCapabilitiesOverride: &override,
		})
		request, err := PrepareInspectionRequest(t.Context(), engine, false)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range request.Items {
			found = found || item.Type == llm.ResponseItemTypeConfigurationUpdate
		}
		if found != supported {
			t.Fatalf("native eligibility=%v, transport support=%v", found, supported)
		}
	}
}
