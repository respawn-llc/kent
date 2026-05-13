package app

import (
	"testing"

	"builder/shared/clientui"
)

func TestOngoingDeliveryCursorAdvancesOnlyAfterNativeFlushAck(t *testing.T) {
	cursor := newOngoingCommittedDeliveryCursor(2, 10)
	suffix := clientui.CommittedTranscriptSuffix{
		Revision:        11,
		StartEntryCount: 2,
		NextEntryCount:  4,
		Entries:         []clientui.ChatEntry{{Role: "assistant", Text: "a"}, {Role: "assistant", Text: "b"}},
	}

	if err := cursor.beginNativeFlush(suffix, 7); err != nil {
		t.Fatalf("begin native flush: %v", err)
	}
	if cursor.lastEmittedCommittedEntryCount != 2 {
		t.Fatalf("cursor advanced before ack: %+v", cursor)
	}
	if advanced := cursor.ackNativeFlush(6); advanced {
		t.Fatalf("ack before target advanced cursor: %+v", cursor)
	}
	if cursor.lastEmittedCommittedEntryCount != 2 {
		t.Fatalf("cursor advanced after stale ack: %+v", cursor)
	}
	if advanced := cursor.ackNativeFlush(8); advanced {
		t.Fatalf("ack for a different newer flush advanced cursor: %+v", cursor)
	}
	if cursor.lastEmittedCommittedEntryCount != 2 {
		t.Fatalf("cursor advanced after mismatched ack: %+v", cursor)
	}
	if advanced := cursor.ackNativeFlush(7); !advanced {
		t.Fatalf("expected target ack to advance cursor: %+v", cursor)
	}
	if cursor.lastEmittedCommittedEntryCount != 4 {
		t.Fatalf("last emitted count = %d, want 4", cursor.lastEmittedCommittedEntryCount)
	}
	if cursor.lastEmittedTranscriptRevision != 11 {
		t.Fatalf("last revision = %d, want 11", cursor.lastEmittedTranscriptRevision)
	}
	if cursor.nativeFlushInFlight {
		t.Fatalf("flush still in flight after ack: %+v", cursor)
	}
}

func TestOngoingDeliveryCursorExtendsInFlightFlush(t *testing.T) {
	cursor := newOngoingCommittedDeliveryCursor(0, 10)
	first := clientui.CommittedTranscriptSuffix{
		Revision:        11,
		StartEntryCount: 0,
		NextEntryCount:  1,
		Entries:         []clientui.ChatEntry{{Role: "assistant", Text: "first"}},
	}
	second := clientui.CommittedTranscriptSuffix{
		Revision:        12,
		StartEntryCount: 1,
		NextEntryCount:  3,
		Entries:         []clientui.ChatEntry{{Role: "assistant", Text: "second"}, {Role: "assistant", Text: "third"}},
	}

	if err := cursor.beginNativeFlush(first, 5); err != nil {
		t.Fatalf("begin first flush: %v", err)
	}
	if err := cursor.beginNativeFlush(second, 6); err != nil {
		t.Fatalf("extend flush: %v", err)
	}
	if cursor.lastAppliedCommittedEntryCount != 3 {
		t.Fatalf("applied cursor = %d, want 3", cursor.lastAppliedCommittedEntryCount)
	}
	if advanced := cursor.ackNativeFlush(5); advanced {
		t.Fatalf("old flush ack advanced extended cursor: %+v", cursor)
	}
	if cursor.lastEmittedCommittedEntryCount != 0 {
		t.Fatalf("emitted cursor advanced before extended ack: %+v", cursor)
	}
	if advanced := cursor.ackNativeFlush(6); !advanced {
		t.Fatalf("expected extended flush ack to advance cursor: %+v", cursor)
	}
	if cursor.lastEmittedCommittedEntryCount != 3 {
		t.Fatalf("emitted cursor = %d, want 3", cursor.lastEmittedCommittedEntryCount)
	}
}

func TestOngoingDeliveryCursorRecordsPendingRangeWhileEmissionDisabled(t *testing.T) {
	cursor := newOngoingCommittedDeliveryCursor(2, 10)
	cursor.setEmissionEnabled(false)

	cursor.recordCommittedAdvance(5, 12)
	cursor.recordCommittedAdvance(7, 13)

	if cursor.lastEmittedCommittedEntryCount != 2 {
		t.Fatalf("cursor advanced while emission disabled: %+v", cursor)
	}
	if len(cursor.pendingCommittedRanges) != 1 {
		t.Fatalf("pending ranges = %+v, want one compact range", cursor.pendingCommittedRanges)
	}
	pending := cursor.pendingCommittedRanges[0]
	if pending.startEntryCount != 2 || pending.endEntryCount != 7 || pending.revision != 13 {
		t.Fatalf("unexpected pending range: %+v", pending)
	}
	req, ok := cursor.nextSuffixRequest()
	if !ok {
		t.Fatal("expected suffix request for pending range")
	}
	if req.AfterEntryCount != 2 {
		t.Fatalf("after entry count = %d, want 2", req.AfterEntryCount)
	}
	if req.Limit != 5 {
		t.Fatalf("limit = %d, want 5", req.Limit)
	}
}

func TestOngoingDeliveryCursorRetryKeepsCursorWhenFlushFails(t *testing.T) {
	cursor := newOngoingCommittedDeliveryCursor(3, 10)
	suffix := clientui.CommittedTranscriptSuffix{
		Revision:        11,
		StartEntryCount: 3,
		NextEntryCount:  6,
		Entries:         []clientui.ChatEntry{{Role: "assistant", Text: "a"}},
	}

	if err := cursor.beginNativeFlush(suffix, 9); err != nil {
		t.Fatalf("begin native flush: %v", err)
	}
	cursor.failNativeFlush(10)
	if !cursor.nativeFlushInFlight {
		t.Fatalf("mismatched failure cleared flush: %+v", cursor)
	}
	cursor.failNativeFlush(9)

	if cursor.nativeFlushInFlight {
		t.Fatalf("flush still in flight after failure: %+v", cursor)
	}
	if cursor.lastEmittedCommittedEntryCount != 3 || cursor.lastEmittedTranscriptRevision != 10 {
		t.Fatalf("cursor changed after failed flush: %+v", cursor)
	}
	req, ok := cursor.nextSuffixRequest()
	if !ok {
		t.Fatal("expected retry request after failed flush")
	}
	if req.AfterEntryCount != 3 {
		t.Fatalf("retry after entry count = %d, want 3", req.AfterEntryCount)
	}
}
