package runtime

import (
	"reflect"
	"testing"

	"core/server/tools"
	"core/shared/config"
	"core/shared/textutil"
	"core/shared/transcript"
)

func TestCacheWarningStepIdentitySurvivesLiveSnapshotAndReopen(t *testing.T) {
	store := mustCreateTestSession(t)
	var events []Event
	cfg := Config{
		Model: "gpt-6-sol", CacheWarningMode: config.CacheWarningModeVerbose,
		OnEvent: func(event Event) {
			if event.Kind == EventCacheWarning {
				events = append(events, event)
			}
		},
	}
	engine := mustNewTestEngine(t, store, &fakeClient{}, tools.NewRegistry(), cfg)
	stepID := runtimeTestStepID("cache-step")
	if err := steerTestActiveStep(engine, stepID, steerCacheWarningIntent(
		transcript.CacheWarning{
			Scope: transcript.CacheWarningScopeConversation, Reason: transcript.CacheWarningReasonReuseDropped,
		},
		transcript.EntryVisibilityOngoing,
		true,
	)); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one committed warning, got %d", len(events))
	}
	live := TranscriptCommittedRowFactsFromEvent(events[0])
	if len(live) != 1 || !textutil.EqualOptional(live[0].StepID, &stepID) {
		t.Fatalf("live warning lost its Step: %+v", live)
	}
	assertSnapshot := func(engine *Engine) {
		t.Helper()
		for _, facts := range [][]TranscriptCommittedRowFact{
			TranscriptCommittedRowFactsFromSnapshot(engine.ChatSnapshot()),
			mustNewestTranscriptFacts(t, engine),
		} {
			if !reflect.DeepEqual(live, facts) {
				t.Fatalf("warning changed across live/reopened snapshots:\nlive: %+v\nsnapshot: %+v", live, facts)
			}
		}
	}
	assertSnapshot(engine)
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
	cfg.OnEvent = nil
	reopened := mustNewTestEngine(t, mustOpenTestSession(t, store.Dir()), &fakeClient{}, tools.NewRegistry(), cfg)
	assertSnapshot(reopened)
}
