package session

import (
	"testing"

	"core/shared/textutil"
)

func TestForkInheritsCurrentThinkingOutsideRollbackHistory(t *testing.T) {
	parent := newSessionTestStore(t)
	log := materializedForkEventLog(t, parent)
	target, _, err := log.AppendRecord(nil, forkUserMessageRecord("older input"))
	if err != nil {
		t.Fatal(err)
	}
	if err := parent.SetThinkingOverride(textutil.Value("low")); err != nil {
		t.Fatal(err)
	}
	if err := parent.AdoptOriginalThinkingEffort("xhigh"); err != nil {
		t.Fatal(err)
	}
	child, _, err := ForkAtUserMessage(log, target.Seq(), "fork", testSessionCategory, ForkThinking{Desired: "low", PreserveNativeUpdates: true})
	if err != nil {
		t.Fatal(err)
	}
	meta := child.Meta()
	if meta.ChatSettings == nil || meta.ChatSettings.Thinking == nil || *meta.ChatSettings.Thinking != "low" {
		t.Fatalf("fork lost current desired Thinking: %+v", meta.ChatSettings)
	}
	if meta.OriginalThinkingEffort == nil || *meta.OriginalThinkingEffort != "xhigh" {
		t.Fatalf("fork lost original Thinking baseline: %v", meta.OriginalThinkingEffort)
	}
}

func TestIndependentChildDoesNotInheritThinkingProtocolState(t *testing.T) {
	root := t.TempDir()
	parent := newSessionTestStoreAt(t, root)
	if err := parent.SetThinkingOverride(textutil.Value("low")); err != nil {
		t.Fatal(err)
	}
	if err := parent.AdoptOriginalThinkingEffort("xhigh"); err != nil {
		t.Fatal(err)
	}
	log := materializedForkEventLog(t, parent)
	if _, _, err := log.AppendRecord(nil, ConfigurationUpdateRecord{Item: ProviderHistoryItem{
		Type: ProviderHistoryItemTypeConfigurationUpdate, ConfigurationEffort: textutil.Value("low"),
		Raw: []byte(`{"type":"configuration_update","reasoning":{"effort":"low"}}`),
	}}); err != nil {
		t.Fatal(err)
	}
	child := newSessionTestLazyStoreAt(t, root)
	if err := InitializeCreationContext(child, parent, SessionCreationSourceParentAgent, ChildContextOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := child.EnsureDurable(); err != nil {
		t.Fatal(err)
	}
	if child.Meta().OriginalThinkingEffort != nil || child.Meta().ChatSettings != nil {
		t.Fatal("independent child inherited parent Thinking")
	}
	if records := collectForkRecords(t, materializedForkEventLog(t, child)); len(records) != 0 {
		t.Fatal("independent child inherited parent input")
	}
}
