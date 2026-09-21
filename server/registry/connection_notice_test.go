package registry

import (
	"testing"

	"core/server/runtime"
	"core/server/tools"
	"core/shared/config"

	"google.golang.org/protobuf/proto"
)

func TestConnectionReplacementLiveDeliveryMatchesHydration(t *testing.T) {
	registry := NewRuntimeRegistry()
	engine := newRegistryRuntime(t, registryRuntimeFakeClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5", ThinkingLevel: "medium"},
		func(engine *runtime.Engine, event runtime.Event) {
			if engine != nil {
				registry.PublishAuthorityRuntimeEvent(registryTestResourceRef(engine.SessionID()), event)
			}
		},
	)
	if err := engine.PrepareConnectionReplacement(config.ConnectionReplacement{Previous: "removed", Current: "work"}); err != nil {
		t.Fatal(err)
	}
	registerResource(t, registry, registryTestResourceRef(engine.SessionID()), engine)
	subscription := subscribeTranscriptForTest(t, registry, engine.SessionID())
	t.Cleanup(func() { _ = subscription.Close() })
	hydration := nextTranscriptMessage(t, subscription).GetEvent().GetHydration()
	if hydration.ConnectionReplacement == nil || hydration.ConnectionReplacement.PreviousId != "removed" ||
		hydration.ConnectionReplacement.CurrentId != "work" {
		t.Fatalf("hydrated replacement = %+v", hydration.ConnectionReplacement)
	}
	if err := engine.PublishConnectionReplacement(); err != nil {
		t.Fatal(err)
	}
	event := nextTranscriptMessage(t, subscription).GetEvent().GetConnectionReplaced()
	if !proto.Equal(event, hydration.ConnectionReplacement) {
		t.Fatalf("live replacement = %+v, hydration = %+v", event, hydration.ConnectionReplacement)
	}
}
