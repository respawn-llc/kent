package registry

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"core/server/runtime"
	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/textutil"
	"google.golang.org/protobuf/proto"
)

func init() {
	transcriptContractViolationsPanic = true
}

func transcriptBrokerHydration(t *testing.T) *transcriptpb.Event {
	t.Helper()
	sessionID, err := runtimeids.ParseSessionID("session-1")
	if err != nil {
		t.Fatalf("ParseSessionID: %v", err)
	}
	version, err := protoapi.NewReadModelVersion("registry-test", 1, 1)
	if err != nil {
		t.Fatalf("NewReadModelVersion: %v", err)
	}
	hydration := &transcriptpb.Hydration{
		SessionIdentity: &transcriptpb.SessionIdentity{
			SessionId:             sessionID.String(),
			ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_FRESH,
		},
		SessionStatus: &transcriptpb.SessionStatus{
			ReviewerFrequency: "off",
			ThinkingLevel:     "medium",
			CompactionMode:    "local",
		},
		RuntimeReadModelUpdate: &runtimepb.ReadModelUpdate{
			Version: version,
			Activity: &runtimepb.Activity{
				State:    runtimepb.ActivityState_RUNTIME_ACTIVITY_UNAVAILABLE,
				Reviewer: runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_INACTIVE,
			},
		},
		TailSegment: &transcriptpb.TailSegment{Entries: []*transcriptpb.CommittedRow{}},
	}
	return &transcriptpb.Event{Payload: &transcriptpb.Event_Hydration{Hydration: hydration}}
}

func transcriptBrokerDiagnostic(code transcriptpb.OperationalDiagnosticCode, detail string) *transcriptpb.Event {
	return &transcriptpb.Event{Payload: &transcriptpb.Event_OperationalDiagnostic{OperationalDiagnostic: &transcriptpb.OperationalDiagnostic{
		Code:   code,
		Detail: detail,
	}}}
}

func mustRegistryStepID(t *testing.T) runtimeids.StepID {
	t.Helper()
	stepID, err := runtimeids.ParseStepID("22222222-2222-4222-8222-222222222222")
	if err != nil {
		t.Fatalf("ParseStepID: %v", err)
	}
	return stepID
}

func TestTranscriptSubscriptionBrokerSequencesEachSubscriberFromHydration(t *testing.T) {
	broker := newTranscriptSubscriptionBroker()
	first, err := broker.Subscribe(transcriptBrokerHydration(t))
	if err != nil {
		t.Fatalf("Subscribe first: %v", err)
	}
	defer func() { _ = first.Close() }()

	firstHydration := nextTranscriptMessage(t, first)
	if firstHydration.Sequence != 1 || firstHydration.Event.GetHydration() == nil {
		t.Fatalf("first hydration = %+v, want seq=1 hydration", firstHydration)
	}

	broker.Publish([]*transcriptpb.Event{
		transcriptBrokerDiagnostic(transcriptpb.OperationalDiagnosticCode_OPERATIONAL_DIAGNOSTIC_CODE_SLEEP_GUARD_FAILED, "sleep guard failed"),
		transcriptBrokerDiagnostic(transcriptpb.OperationalDiagnosticCode_OPERATIONAL_DIAGNOSTIC_CODE_PROMPT_HISTORY_PERSIST_FAILED, "prompt history failed"),
	})
	if got := nextTranscriptMessage(t, first); got.Sequence != 2 || got.Event.GetOperationalDiagnostic() == nil {
		t.Fatalf("first live one = %+v, want seq=2 operational diagnostic", got)
	}
	if got := nextTranscriptMessage(t, first); got.Sequence != 3 || got.Event.GetOperationalDiagnostic() == nil {
		t.Fatalf("first live two = %+v, want seq=3 operational diagnostic", got)
	}

	second, err := broker.Subscribe(transcriptBrokerHydration(t))
	if err != nil {
		t.Fatalf("Subscribe second: %v", err)
	}
	defer func() { _ = second.Close() }()
	secondHydration := nextTranscriptMessage(t, second)
	if secondHydration.Sequence != 1 || secondHydration.Event.GetHydration() == nil {
		t.Fatalf("second hydration = %+v, want fresh seq=1 hydration", secondHydration)
	}
	if event, err := nextTranscriptMessageTimeout(second, 20*time.Millisecond); err == nil {
		t.Fatalf("second subscriber replayed old live event: %+v", event)
	}
}

func TestTranscriptSubscriptionBrokerCloseAndOverflowUseTerminalErrors(t *testing.T) {
	t.Run("broker close", func(t *testing.T) {
		broker := newTranscriptSubscriptionBroker()
		sub, err := broker.Subscribe(transcriptBrokerHydration(t))
		if err != nil {
			t.Fatalf("Subscribe: %v", err)
		}
		_ = nextTranscriptMessage(t, sub)
		broker.Close(io.EOF)
		_, err = sub.Next(context.Background())
		if !errors.Is(err, io.EOF) {
			t.Fatalf("Next after close error = %v, want EOF", err)
		}
	})

	t.Run("subscriber overflow", func(t *testing.T) {
		broker := newTranscriptSubscriptionBroker()
		sub, err := broker.Subscribe(transcriptBrokerHydration(t))
		if err != nil {
			t.Fatalf("Subscribe: %v", err)
		}
		for i := 0; i < transcriptSubscriptionBufferSize+1; i++ {
			broker.Publish([]*transcriptpb.Event{transcriptBrokerDiagnostic(transcriptpb.OperationalDiagnosticCode_OPERATIONAL_DIAGNOSTIC_CODE_SLEEP_GUARD_FAILED,
				"sleep guard failed")})
		}
		for {
			_, err = sub.Next(context.Background())
			if err == nil {
				continue
			}
			if !errors.Is(err, serverapi.ErrStreamGap) {
				t.Fatalf("overflow error = %v, want stream gap", err)
			}
			reason, ok := serverapi.TranscriptCloseReasonOf(err)
			if !ok || reason != serverapi.TranscriptCloseReasonSubscriberOverflow {
				t.Fatalf("overflow close reason = %q ok=%t, want subscriber overflow", reason, ok)
			}
			return
		}
	})
}

func TestTranscriptSubscriptionBrokerPanicsOnContractViolationInTestMode(t *testing.T) {
	broker := newTranscriptSubscriptionBroker()
	sub, err := broker.Subscribe(transcriptBrokerHydration(t))
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	_ = nextTranscriptMessage(t, sub)

	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("contract-invalid tool completion did not panic in test mode")
		}
	}()
	broker.Publish([]*transcriptpb.Event{{Payload: &transcriptpb.Event_CommittedRow{CommittedRow: &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_DETAIL,
		Row: &transcriptpb.CommittedRow_Tool{Tool: &transcriptpb.ToolRow{
			StepId: textutil.Value(mustRegistryStepID(t).String()),
		}},
	}}}})
}

func TestTranscriptSubscriptionBrokerPublishesCommittedReasoningTraceRow(t *testing.T) {
	broker := newTranscriptSubscriptionBroker()
	sub, err := broker.Subscribe(transcriptBrokerHydration(t))
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	_ = nextTranscriptMessage(t, sub)

	broker.Publish([]*transcriptpb.Event{{Payload: &transcriptpb.Event_CommittedRow{CommittedRow: &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_DETAIL,
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Locator:    &transcriptpb.CommittedRowLocator{EventSequence: 1, RowOrdinal: 1},
		Row: &transcriptpb.CommittedRow_ReasoningTrace{ReasoningTrace: &transcriptpb.ReasoningTraceRow{
			StepId:      mustRegistryStepID(t).String(),
			CompactText: "Planning",
			Text:        "Planning\nDetails",
		}},
	}}}})

	message := nextTranscriptMessage(t, sub)
	row := message.Event.GetCommittedRow()
	if row.GetReasoningTrace() == nil {
		t.Fatalf("published reasoning row = %+v, want a populated reasoning trace row", message.Event.Payload)
	}
}

func TestSessionFeedSequencerRejectsInvalidBatchBeforePrefixMutation(t *testing.T) {
	broker := newTranscriptSubscriptionBroker()
	sequencer := newSessionFeedSequencer(broker)
	hydration := transcriptBrokerHydration(t).GetHydration()
	sub, err := sequencer.Subscribe(func() (*transcriptpb.Hydration, error) {
		return hydration, nil
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	_ = nextTranscriptMessage(t, sub)
	valid := &transcriptpb.Event{Payload: &transcriptpb.Event_ToolStart{ToolStart: &transcriptpb.ToolStart{
		StepId:     mustRegistryStepID(t).String(),
		ToolCallId: "tool-call-prefix",
		ToolName:   "exec_command",
	}}}
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("invalid batch did not fail fast")
		}
		if _, err := nextTranscriptMessageTimeout(sub, 20*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("valid prefix was delivered after invalid batch: %v", err)
		}
		hydratedSub, err := sequencer.Subscribe(func() (*transcriptpb.Hydration, error) {
			return hydration, nil
		})
		if err != nil {
			t.Fatalf("subscribe after rejected batch: %v", err)
		}
		defer func() { _ = hydratedSub.Close() }()
		hydratedMessage := nextTranscriptMessage(t, hydratedSub)
		hydrated := hydratedMessage.Event.GetHydration()
		if hydrated == nil {
			t.Fatalf("post-batch hydration payload = %T, want TranscriptHydration", hydratedMessage.Event.Payload)
		}
		for _, tool := range hydrated.InFlightTools {
			if tool.ToolCallId == "tool-call-prefix" {
				t.Fatalf("post-batch hydration contains rejected tool prefix: %+v", tool)
			}
		}
	}()
	sequencer.Publish([]*transcriptpb.Event{valid, {}})
}

func TestSessionFeedSequencerBuildsHydrationWithoutPriorRuntimeReadModel(t *testing.T) {
	h := transcriptBrokerHydration(t).GetHydration()
	sub, err := newSessionFeedSequencer(newTranscriptSubscriptionBroker()).Subscribe(func() (*transcriptpb.Hydration, error) {
		return h, nil
	})
	if err != nil {
		t.Fatalf("subscribe without prior read-model: %v", err)
	}
	defer func() { _ = sub.Close() }()
	if message := nextTranscriptMessage(t, sub); message.Sequence != 1 || message.Event.GetHydration() == nil {
		t.Fatalf("first message = %+v, want sequence 1 hydration", message)
	}
}

func TestSessionFeedSequencerHydrationProjectionUsesContractFailurePolicy(t *testing.T) {
	t.Run("production returns contract error", func(t *testing.T) {
		restore := withTranscriptContractViolationPanic(false)
		defer restore()
		_, err := newSessionFeedSequencer(newTranscriptSubscriptionBroker()).Subscribe(func() (*transcriptpb.Hydration, error) {
			return &transcriptpb.Hydration{}, transcriptHydrationProjectionError{cause: errors.New("malformed hydration locator")}
		})
		if err == nil {
			t.Fatal("malformed hydration projection returned nil error")
		}
		reason, ok := serverapi.TranscriptCloseReasonOf(err)
		if !ok || reason != serverapi.TranscriptCloseReasonContractViolation {
			t.Fatalf("hydration error close reason = %q ok=%t, want contract violation", reason, ok)
		}
	})
	t.Run("development panics with contract diagnostic", func(t *testing.T) {
		restore := withTranscriptContractViolationPanic(true)
		defer restore()
		defer func() {
			if recover() == nil {
				t.Fatal("malformed hydration projection did not panic")
			}
		}()
		_, _ = newSessionFeedSequencer(newTranscriptSubscriptionBroker()).Subscribe(func() (*transcriptpb.Hydration, error) {
			return &transcriptpb.Hydration{}, transcriptHydrationProjectionError{cause: errors.New("malformed hydration locator")}
		})
	})
}

func TestRegistryHydrationCompositionUsesContractFailurePolicy(t *testing.T) {
	registry := NewRuntimeRegistry()
	tailPage := runtime.TranscriptSegmentPage{
		Snapshot: runtime.ChatSnapshot{Entries: []runtime.ChatEntry{{
			Role:                "user",
			Text:                "malformed",
			CommittedProvenance: &runtime.TranscriptCommittedRowProvenance{},
		}}},
	}
	t.Run("production returns contract error", func(t *testing.T) {
		restore := withTranscriptContractViolationPanic(false)
		defer restore()
		_, err := newSessionFeedSequencer(newTranscriptSubscriptionBroker()).Subscribe(func() (*transcriptpb.Hydration, error) {
			return registry.composeTranscriptHydration(context.Background(), "", nil, runtime.TranscriptHydrationSnapshot{}, tailPage)
		})
		if err == nil {
			t.Fatal("malformed hydration composition returned nil error")
		}
		reason, ok := serverapi.TranscriptCloseReasonOf(err)
		if !ok || reason != serverapi.TranscriptCloseReasonContractViolation {
			t.Fatalf("hydration composition close reason = %q ok=%t, want contract violation", reason, ok)
		}
	})
	t.Run("development panics with contract diagnostic", func(t *testing.T) {
		restore := withTranscriptContractViolationPanic(true)
		defer restore()
		defer func() {
			if recover() == nil {
				t.Fatal("malformed hydration composition did not panic")
			}
		}()
		_, _ = newSessionFeedSequencer(newTranscriptSubscriptionBroker()).Subscribe(func() (*transcriptpb.Hydration, error) {
			return registry.composeTranscriptHydration(context.Background(), "", nil, runtime.TranscriptHydrationSnapshot{}, tailPage)
		})
	})
}

func TestTranscriptBrokerRejectsUninitializedEventWithoutSequenceOrDelivery(t *testing.T) {
	restore := withTranscriptContractViolationPanic(false)
	defer restore()
	broker := newTranscriptSubscriptionBroker()
	sub, err := broker.Subscribe(transcriptBrokerHydration(t))
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	_ = nextTranscriptMessage(t, sub)
	broker.Publish([]*transcriptpb.Event{{}})
	_, err = nextTranscriptMessageTimeout(sub, time.Second)
	if err == nil {
		t.Fatal("uninitialized event was delivered")
	}
	reason, ok := serverapi.TranscriptCloseReasonOf(err)
	if !ok || reason != serverapi.TranscriptCloseReasonContractViolation {
		t.Fatalf("close reason = %q ok=%t, want contract violation", reason, ok)
	}
}

func TestTranscriptSubscriptionBrokerClosesOnContractViolationWhenPanicDisabled(t *testing.T) {
	restore := withTranscriptContractViolationPanic(false)
	defer restore()

	broker := newTranscriptSubscriptionBroker()
	sub, err := broker.Subscribe(transcriptBrokerHydration(t))
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	_ = nextTranscriptMessage(t, sub)

	broker.Publish([]*transcriptpb.Event{{Payload: &transcriptpb.Event_CommittedRow{CommittedRow: &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_DETAIL,
		Row: &transcriptpb.CommittedRow_Tool{Tool: &transcriptpb.ToolRow{
			StepId: textutil.Value(mustRegistryStepID(t).String()),
		}},
	}}}})
	_, err = sub.Next(context.Background())
	if err == nil {
		t.Fatal("contract-invalid tool completion was delivered")
	}
	reason, ok := serverapi.TranscriptCloseReasonOf(err)
	if !ok || reason != serverapi.TranscriptCloseReasonContractViolation {
		t.Fatalf("contract violation reason = %q ok=%t, want contract_violation; err=%v", reason, ok, err)
	}
}

func TestTranscriptSubscriptionBoundaryValidatesCommittedRowIntegrity(t *testing.T) {
	valid := &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_DETAIL,
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Locator:    &transcriptpb.CommittedRowLocator{EventSequence: 1, RowOrdinal: 1},
		Row: &transcriptpb.CommittedRow_User{User: &transcriptpb.UserRow{
			StepId: textutil.Value(mustRegistryStepID(t).String()), Text: "valid",
		}},
	}
	if err := validateCommittedRow(valid); err != nil {
		t.Fatalf("valid integrity was rejected: %v", err)
	}

	invalidRows := []*transcriptpb.CommittedRow{proto.Clone(valid).(*transcriptpb.CommittedRow), proto.Clone(valid).(*transcriptpb.CommittedRow)}
	invalidRows[0].Integrity = transcriptpb.RowIntegrity(255)
	invalidRows[1].Visibility = transcriptpb.EntryVisibility_ENTRY_VISIBILITY_UNSPECIFIED
	for _, invalid := range invalidRows {
		err := validateCommittedRow(invalid)
		if err == nil {
			t.Fatalf("invalid committed row was accepted: %#v", invalid)
		}
		var violation transcriptContractViolation
		if !errors.As(err, &violation) {
			t.Fatalf("broker validation error = %T, want transcript contract violation", err)
		}
	}
}

func TestTranscriptSubscriptionContractValidatesLiveLocatorProgression(t *testing.T) {
	restore := withTranscriptContractViolationPanic(false)
	defer restore()

	tests := []struct {
		name        string
		hydrated    []*transcriptpb.CommittedRowLocator
		live        []*transcriptpb.CommittedRowLocator
		wantFailure bool
	}{
		{
			name: "empty hydration starts at first live event",
			live: []*transcriptpb.CommittedRowLocator{{EventSequence: 5, RowOrdinal: 1}},
		},
		{
			name:     "hydrated watermark accepts newer event",
			hydrated: []*transcriptpb.CommittedRowLocator{{EventSequence: 10, RowOrdinal: 1}},
			live:     []*transcriptpb.CommittedRowLocator{{EventSequence: 11, RowOrdinal: 1}},
		},
		{
			name:     "same event continues contiguously",
			hydrated: []*transcriptpb.CommittedRowLocator{{EventSequence: 10, RowOrdinal: 1}},
			live: []*transcriptpb.CommittedRowLocator{
				{EventSequence: 11, RowOrdinal: 1},
				{EventSequence: 11, RowOrdinal: 2},
				{EventSequence: 12, RowOrdinal: 1},
			},
		},
		{
			name:        "duplicate ordinal fails",
			hydrated:    []*transcriptpb.CommittedRowLocator{{EventSequence: 10, RowOrdinal: 1}},
			live:        []*transcriptpb.CommittedRowLocator{{EventSequence: 11, RowOrdinal: 1}, {EventSequence: 11, RowOrdinal: 1}},
			wantFailure: true,
		},
		{
			name:        "regressed event fails",
			hydrated:    []*transcriptpb.CommittedRowLocator{{EventSequence: 10, RowOrdinal: 1}},
			live:        []*transcriptpb.CommittedRowLocator{{EventSequence: 11, RowOrdinal: 1}, {EventSequence: 10, RowOrdinal: 1}},
			wantFailure: true,
		},
		{
			name:        "skipped ordinal fails",
			hydrated:    []*transcriptpb.CommittedRowLocator{{EventSequence: 10, RowOrdinal: 1}},
			live:        []*transcriptpb.CommittedRowLocator{{EventSequence: 11, RowOrdinal: 1}, {EventSequence: 11, RowOrdinal: 3}},
			wantFailure: true,
		},
		{
			name:        "first live row must be newer than hydration",
			hydrated:    []*transcriptpb.CommittedRowLocator{{EventSequence: 10, RowOrdinal: 1}},
			live:        []*transcriptpb.CommittedRowLocator{{EventSequence: 10, RowOrdinal: 1}},
			wantFailure: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			hydration := transcriptBrokerHydrationWithLocators(t, test.hydrated)
			broker := newTranscriptSubscriptionBroker()
			sub, err := broker.Subscribe(hydration)
			if err != nil {
				t.Fatalf("subscribe with test broker: %v", err)
			}
			defer func() { _ = sub.Close() }()
			_ = nextTranscriptMessage(t, sub)
			for _, locator := range test.live {
				broker.Publish([]*transcriptpb.Event{{Payload: &transcriptpb.Event_CommittedRow{CommittedRow: transcriptBrokerCommittedRow(t, locator)}}})
			}
			if !test.wantFailure {
				for range test.live {
					if _, err := nextTranscriptMessageTimeout(sub, time.Second); err != nil {
						t.Fatalf("valid live locator was rejected: %v", err)
					}
				}
				return
			}
			for range test.live[:len(test.live)-1] {
				if _, err := nextTranscriptMessageTimeout(sub, time.Second); err != nil {
					t.Fatalf("valid prefix before invalid locator was rejected: %v", err)
				}
			}
			if _, err := sub.Next(context.Background()); err == nil {
				t.Fatal("invalid live locator progression was delivered")
			}
		})
	}
}

func transcriptBrokerHydrationWithLocators(t *testing.T, locators []*transcriptpb.CommittedRowLocator) *transcriptpb.Event {
	t.Helper()
	hydration := transcriptBrokerHydration(t).GetHydration()
	for _, locator := range locators {
		hydration.TailSegment.Entries = append(hydration.TailSegment.Entries, transcriptBrokerCommittedRow(t, locator))
	}
	return &transcriptpb.Event{Payload: &transcriptpb.Event_Hydration{Hydration: hydration}}
}

func transcriptBrokerCommittedRow(t *testing.T, locator *transcriptpb.CommittedRowLocator) *transcriptpb.CommittedRow {
	t.Helper()
	return &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING,
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Locator:    locator,
		Row: &transcriptpb.CommittedRow_User{User: &transcriptpb.UserRow{
			StepId: textutil.Value(mustRegistryStepID(t).String()), Text: "row",
		}},
	}
}

func withTranscriptContractViolationPanic(enabled bool) func() {
	previous := transcriptContractViolationsPanic
	transcriptContractViolationsPanic = enabled
	return func() {
		transcriptContractViolationsPanic = previous
	}
}

func nextTranscriptMessage(t *testing.T, sub serverapi.TranscriptSubscription) *transcriptpb.Message {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	message, err := sub.Next(ctx)
	if err != nil {
		t.Fatalf("transcript subscription next: %v", err)
	}
	return message
}

func nextTranscriptMessageTimeout(sub serverapi.TranscriptSubscription, timeout time.Duration) (*transcriptpb.Message, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return sub.Next(ctx)
}
