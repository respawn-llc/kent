package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"core/server/llm"
	"core/server/session"
	"core/server/session/sessiontest"
	"core/server/tools"
	"core/shared/modelcontract"
)

func TestProviderUsageExcludesFailedTransportAndRetainsSuccessfulRetry(t *testing.T) {
	t.Parallel()
	withGenerateRetryDelays(t, []time.Duration{0})
	store := mustCreateTestSession(t)
	client := &fakeClient{
		errors:    []error{errors.New("temporary provider failure")},
		responses: []llm.Response{providerUsageTestResponse(29)},
	}
	engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{Model: "gpt-5"})

	if _, err := generateTestActiveStep(
		context.Background(),
		engine,
		"retry-usage",
		client,
		providerUsageTestRequest(store.Meta().SessionID, false),
	); err != nil {
		t.Fatalf("successful retry: %v", err)
	}
	if got := fakeClientCallCount(client); got != 2 {
		t.Fatalf("provider calls = %d, want failed attempt plus successful retry", got)
	}
	records := providerUsageTestRecords(t, store)
	if len(records) != 1 {
		t.Fatalf("usage records = %+v, want only successful retry", records)
	}
	assertProviderUsageOutputTokens(t, records[0], 29)
}

func TestProviderUsagePersistenceFailureDoesNotRetryProviderAndRetainsCommittedFact(t *testing.T) {
	t.Parallel()
	observerErr := errors.New("usage persistence observer failure")
	gate := sessiontest.NewPersistenceGate(runtimeTestSessionPersistence)
	store := mustCreateTestSessionAt(t, t.TempDir(), session.WithPersistenceObserver(gate))
	client := &fakeClient{responses: []llm.Response{providerUsageTestResponse(31)}}
	engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{Model: "gpt-5"})
	gate.FailNext(observerErr)

	_, err := generateTestActiveStep(
		context.Background(),
		engine,
		"persist-usage",
		client,
		providerUsageTestRequest(store.Meta().SessionID, false),
	)
	if err == nil || !errors.Is(err, observerErr) {
		t.Fatalf("usage persistence error = %v, want %v", err, observerErr)
	}
	if got := fakeClientCallCount(client); got != 1 {
		t.Fatalf("provider calls = %d, want one without accounting retry", got)
	}
	records := providerUsageTestRecords(t, store)
	if len(records) != 1 {
		t.Fatalf("committed usage records = %+v, want one", records)
	}
	assertProviderUsageOutputTokens(t, records[0], 31)
}

func assertProviderUsageOutputTokens(t *testing.T, evidence modelcontract.ProviderUsageEvidence, want int) {
	t.Helper()
	var usage struct {
		OutputTokens int `json:"output_tokens"`
	}
	if evidence.Usage == nil {
		t.Fatal("provider usage evidence is absent")
	}
	if err := json.Unmarshal(*evidence.Usage, &usage); err != nil {
		t.Fatalf("decode provider usage evidence: %v", err)
	}
	if usage.OutputTokens != want {
		t.Fatalf("output tokens = %d, want %d", usage.OutputTokens, want)
	}
}
