package nativescrollback

import (
	"errors"
	"strings"
	"testing"

	"core/cli/tui"
	"core/shared/clientui"

	xansi "github.com/charmbracelet/x/ansi"
)

func TestLedgerDoesNotAdvanceAckedFrontierWhenFlushMessageIsHandled(t *testing.T) {
	var ledger Ledger
	first, ok := ledger.Enqueue("first", FlushOptions{})
	if !ok {
		t.Fatal("expected first flush to schedule")
	}
	second, ok := ledger.Enqueue("second", FlushOptions{})
	if !ok {
		t.Fatal("expected second flush to schedule")
	}

	write, ready, err := ledger.AcceptFlush(second)
	if err != nil {
		t.Fatalf("accept second: %v", err)
	}
	if ready {
		t.Fatalf("out-of-order second flush produced write %+v", write)
	}
	if got := ledger.AckedSequence(); got != 0 {
		t.Fatalf("acked frontier advanced before terminal ack: %d", got)
	}

	write, ready, err = ledger.AcceptFlush(first)
	if err != nil {
		t.Fatalf("accept first: %v", err)
	}
	if !ready || write.Sequence != first.Sequence || write.Text != "first" {
		t.Fatalf("first write = %+v ready=%v, want first ready", write, ready)
	}
	if got := ledger.AckedSequence(); got != 0 {
		t.Fatalf("acked frontier advanced on write scheduling: %d", got)
	}
	if got := ledger.PendingCount(); got != 2 {
		t.Fatalf("pending count includes in-flight plus buffered second = %d, want 2", got)
	}

	ack, err := ledger.Ack(TerminalWriteResult{Sequence: first.Sequence})
	if err != nil {
		t.Fatalf("ack first: %v", err)
	}
	if ack.Frontier != first.Sequence {
		t.Fatalf("ack frontier = %d, want %d", ack.Frontier, first.Sequence)
	}
	if !ack.HasNext || ack.Next.Sequence != second.Sequence || ack.Next.Text != "second" {
		t.Fatalf("next write after first ack = %+v has=%v, want second", ack.Next, ack.HasNext)
	}
	if got := ledger.AckedSequence(); got != first.Sequence {
		t.Fatalf("acked sequence = %d, want %d", got, first.Sequence)
	}
}

func TestLedgerWriteFailureFailsClosed(t *testing.T) {
	var ledger Ledger
	flush, ok := ledger.Enqueue("payload", FlushOptions{})
	if !ok {
		t.Fatal("expected flush to schedule")
	}
	if _, ready, err := ledger.AcceptFlush(flush); err != nil || !ready {
		t.Fatalf("accept flush ready=%v err=%v", ready, err)
	}

	_, err := ledger.Ack(TerminalWriteResult{Sequence: flush.Sequence, Err: "disk full"})
	if !errors.Is(err, ErrTerminalWrite) {
		t.Fatalf("write failure err = %v, want ErrTerminalWrite", err)
	}
	if !ledger.Failed() {
		t.Fatal("expected ledger to fail closed after terminal write failure")
	}
	if _, ok := ledger.Enqueue("later", FlushOptions{}); ok {
		t.Fatal("failed ledger accepted later flush")
	}
}

func TestLedgerRejectsZeroSequenceCommittedNativeFlush(t *testing.T) {
	var ledger Ledger
	ledger.EnsureCommittedDelivery(0, 1)

	err := ledger.BeginCommittedNativeFlush(clientui.CommittedTranscriptSuffix{
		Revision:        1,
		StartEntryCount: 0,
		NextEntryCount:  1,
	}, 0)
	if err == nil || !strings.Contains(err.Error(), "native flush sequence is required") {
		t.Fatalf("zero sequence committed flush err = %v, want sequence requirement", err)
	}
	if state := ledger.CommittedDeliveryState(); state.NativeFlushInFlight || state.LastEmittedCommittedEntryCount != 0 {
		t.Fatalf("delivery state advanced after zero sequence: %+v", state)
	}
}

func TestLedgerAcceptFlushIsIdempotentForInFlightAndPendingSequences(t *testing.T) {
	var ledger Ledger
	first, ok := ledger.Enqueue("first", FlushOptions{})
	if !ok {
		t.Fatal("expected first flush")
	}
	second, ok := ledger.Enqueue("second", FlushOptions{})
	if !ok {
		t.Fatal("expected second flush")
	}

	firstWrite, ready, err := ledger.AcceptFlush(first)
	if err != nil || !ready || firstWrite.Sequence != first.Sequence {
		t.Fatalf("accept first = %+v ready=%v err=%v", firstWrite, ready, err)
	}
	if write, ready, err := ledger.AcceptFlush(first); err != nil || ready || write.Sequence != 0 {
		t.Fatalf("duplicate in-flight accept = %+v ready=%v err=%v, want ignored", write, ready, err)
	}
	if _, ready, err := ledger.AcceptFlush(second); err != nil || ready {
		t.Fatalf("accept pending second ready=%v err=%v, want buffered", ready, err)
	}
	if write, ready, err := ledger.AcceptFlush(second); err != nil || ready || write.Sequence != 0 {
		t.Fatalf("duplicate pending accept = %+v ready=%v err=%v, want ignored", write, ready, err)
	}
	if got := ledger.PendingCount(); got != 2 {
		t.Fatalf("pending count = %d, want in-flight first plus pending second", got)
	}
}

func TestLedgerRejectsFabricatedFutureFlush(t *testing.T) {
	var ledger Ledger
	if _, _, err := ledger.AcceptFlush(ScheduledFlush{Sequence: 42, Text: "fabricated"}); !errors.Is(err, ErrUnexpectedWriteAck) {
		t.Fatalf("future flush err = %v, want ErrUnexpectedWriteAck", err)
	}
	if !ledger.Failed() {
		t.Fatal("ledger did not fail closed after fabricated future flush")
	}
}

func TestLedgerIgnoresStaleAckAfterDiscard(t *testing.T) {
	var ledger Ledger
	flush, ok := ledger.Enqueue("\n", FlushOptions{AllowBlank: true})
	if !ok {
		t.Fatal("expected spacer flush")
	}
	write, ready, err := ledger.AcceptFlush(flush)
	if err != nil {
		t.Fatalf("accept spacer: %v", err)
	}
	if !ready {
		t.Fatal("expected spacer write")
	}

	ledger.DiscardScheduled()
	if got := ledger.AckedSequence(); got != flush.Sequence {
		t.Fatalf("acked after discard = %d, want %d", got, flush.Sequence)
	}
	if _, err := ledger.Ack(TerminalWriteResult{Sequence: write.Sequence}); err != nil {
		t.Fatalf("stale ack after discard failed ledger: %v", err)
	}
	if ledger.Failed() {
		t.Fatal("ledger failed after stale discarded ack")
	}
}

func TestLedgerDiscardScheduledClearsCommittedNativeFlush(t *testing.T) {
	var ledger Ledger
	ledger.EnsureCommittedDelivery(0, 1)
	flush, ok := ledger.Enqueue("committed", FlushOptions{})
	if !ok {
		t.Fatal("expected flush")
	}
	if err := ledger.BeginCommittedNativeFlush(clientui.CommittedTranscriptSuffix{
		Revision:        2,
		StartEntryCount: 0,
		NextEntryCount:  3,
	}, flush.Sequence); err != nil {
		t.Fatalf("begin committed native flush: %v", err)
	}

	ledger.DiscardScheduled()

	state := ledger.CommittedDeliveryState()
	if state.NativeFlushInFlight {
		t.Fatalf("native flush still in flight after discard: %+v", state)
	}
	if state.LastEmittedCommittedEntryCount != 0 {
		t.Fatalf("discard marked committed rows emitted: %+v", state)
	}
	if len(state.PendingCommittedRanges) != 1 || state.PendingCommittedRanges[0].EndEntryCount != 3 {
		t.Fatalf("discard did not requeue committed range: %+v", state)
	}
}

func TestLedgerFailsClosedOnDuplicateAckAfterSuccessfulAck(t *testing.T) {
	var ledger Ledger
	flush, ok := ledger.Enqueue("payload", FlushOptions{})
	if !ok {
		t.Fatal("expected flush")
	}
	write, ready, err := ledger.AcceptFlush(flush)
	if err != nil {
		t.Fatalf("accept flush: %v", err)
	}
	if !ready {
		t.Fatal("expected write")
	}
	if _, err := ledger.Ack(TerminalWriteResult{Sequence: write.Sequence}); err != nil {
		t.Fatalf("first ack: %v", err)
	}
	if _, err := ledger.Ack(TerminalWriteResult{Sequence: write.Sequence}); !errors.Is(err, ErrUnexpectedWriteAck) {
		t.Fatalf("duplicate ack err = %v, want ErrUnexpectedWriteAck", err)
	}
	if !ledger.Failed() {
		t.Fatal("ledger did not fail closed on duplicate ack")
	}
}

func TestLedgerFailsClosedOnOldAckNotFromDiscard(t *testing.T) {
	var ledger Ledger
	first, ok := ledger.Enqueue("first", FlushOptions{})
	if !ok {
		t.Fatal("expected first flush")
	}
	second, ok := ledger.Enqueue("second", FlushOptions{})
	if !ok {
		t.Fatal("expected second flush")
	}
	firstWrite, ready, err := ledger.AcceptFlush(first)
	if err != nil {
		t.Fatalf("accept first: %v", err)
	}
	if !ready {
		t.Fatal("expected first write")
	}
	if _, _, err := ledger.AcceptFlush(second); err != nil {
		t.Fatalf("accept second: %v", err)
	}
	ack, err := ledger.Ack(TerminalWriteResult{Sequence: firstWrite.Sequence})
	if err != nil {
		t.Fatalf("ack first: %v", err)
	}
	if !ack.HasNext {
		t.Fatal("expected second write after first ack")
	}
	if _, err := ledger.Ack(TerminalWriteResult{Sequence: firstWrite.Sequence}); !errors.Is(err, ErrUnexpectedWriteAck) {
		t.Fatalf("old ack err = %v, want ErrUnexpectedWriteAck", err)
	}
	if !ledger.Failed() {
		t.Fatal("ledger did not fail closed on old ack")
	}
}

func TestLedgerCheckpointIsReachedOnlyAfterAckedFrontier(t *testing.T) {
	var ledger Ledger
	first, ok := ledger.Enqueue("first", FlushOptions{})
	if !ok {
		t.Fatal("expected first flush")
	}
	checkpoint := ledger.Checkpoint()
	if ledger.CheckpointReached(checkpoint) {
		t.Fatal("checkpoint reached before terminal write ack")
	}

	write, ready, err := ledger.AcceptFlush(first)
	if err != nil {
		t.Fatalf("accept first: %v", err)
	}
	if !ready {
		t.Fatal("expected first write")
	}
	if ledger.CheckpointReached(checkpoint) {
		t.Fatal("checkpoint reached after scheduling write but before ack")
	}

	if _, err := ledger.Ack(TerminalWriteResult{Sequence: write.Sequence}); err != nil {
		t.Fatalf("ack first: %v", err)
	}
	if !ledger.CheckpointReached(checkpoint) {
		t.Fatal("checkpoint not reached after terminal ack")
	}
}

func TestLedgerZeroCheckpointIsReached(t *testing.T) {
	var ledger Ledger
	if !ledger.CheckpointReached(Checkpoint{}) {
		t.Fatal("zero checkpoint should be reached")
	}
}

func TestLedgerRenderedProjectionCommitWaitsForTerminalAckCheckpoint(t *testing.T) {
	var ledger Ledger
	projection := testProjection("hello")
	flush, ok := ledger.Enqueue("hello", FlushOptions{})
	if !ok {
		t.Fatal("expected flush")
	}
	write, ready, err := ledger.AcceptFlush(flush)
	if err != nil {
		t.Fatalf("accept flush: %v", err)
	}
	if !ready {
		t.Fatal("expected write")
	}

	ledger.ScheduleRenderedProjectionCommit(projection, 7, true)
	if update, applied := ledger.ApplyRenderedProjectionCommitIfReady(); applied {
		t.Fatalf("commit applied before ack: %+v", update)
	}
	if rendered := ledger.RenderedProjection(); !rendered.Projection.Empty() || rendered.Snapshot != "" {
		t.Fatalf("rendered projection advanced before ack: %+v", rendered)
	}

	if _, err := ledger.Ack(TerminalWriteResult{Sequence: write.Sequence}); err != nil {
		t.Fatalf("ack flush: %v", err)
	}
	update, applied := ledger.ApplyRenderedProjectionCommitIfReady()
	if !applied || !update.ResetStreaming {
		t.Fatalf("commit update = %+v applied=%v, want reset commit", update, applied)
	}
	rendered := ledger.RenderedProjection()
	if rendered.BaseOffset != 7 || rendered.Snapshot != projection.Render(tui.TranscriptDivider) {
		t.Fatalf("rendered state = %+v, want projection at base 7", rendered)
	}
}

func TestLedgerRenderedProjectionCommitPreservesPendingStreamingReset(t *testing.T) {
	var ledger Ledger
	firstProjection := testProjection("final assistant")
	secondProjection := testProjection("resized final assistant")
	firstFlush, ok := ledger.Enqueue("final assistant", FlushOptions{})
	if !ok {
		t.Fatal("expected first flush")
	}
	if _, ready, err := ledger.AcceptFlush(firstFlush); err != nil || !ready {
		t.Fatalf("accept first flush ready=%v err=%v", ready, err)
	}
	ledger.ScheduleRenderedProjectionCommit(firstProjection, 0, true)

	secondFlush, ok := ledger.Enqueue("resized final assistant", FlushOptions{})
	if !ok {
		t.Fatal("expected second flush")
	}
	ledger.ScheduleRenderedProjectionCommit(secondProjection, 0, false)
	if !ledger.RenderedProjectionCommitPendingResetStreaming() {
		t.Fatal("replacement projection commit dropped pending streaming reset")
	}
	if _, err := ledger.Ack(TerminalWriteResult{Sequence: firstFlush.Sequence}); err != nil {
		t.Fatalf("ack first flush: %v", err)
	}
	if update, applied := ledger.ApplyRenderedProjectionCommitIfReady(); applied {
		t.Fatalf("commit applied before replacement flush ack: %+v", update)
	}
	if _, ready, err := ledger.AcceptFlush(secondFlush); err != nil || !ready {
		t.Fatalf("accept second flush ready=%v err=%v", ready, err)
	}
	if _, err := ledger.Ack(TerminalWriteResult{Sequence: secondFlush.Sequence}); err != nil {
		t.Fatalf("ack second flush: %v", err)
	}
	update, applied := ledger.ApplyRenderedProjectionCommitIfReady()
	if !applied || !update.ResetStreaming {
		t.Fatalf("replacement commit update = %+v applied=%v, want preserved streaming reset", update, applied)
	}
}

func TestLedgerProjectionSnapshotsAreCloned(t *testing.T) {
	var ledger Ledger
	projection := testProjection("original")
	ledger.SetCurrentProjection(projection, 3, 4)
	projection.Blocks[0].Lines[0] = "mutated caller"

	current := ledger.CurrentProjection()
	if got := current.Projection.Blocks[0].Lines[0]; got != "original" {
		t.Fatalf("current projection mutated through caller alias: %q", got)
	}
	current.Projection.Blocks[0].Lines[0] = "mutated snapshot"
	if got := ledger.CurrentProjection().Projection.Blocks[0].Lines[0]; got != "original" {
		t.Fatalf("current projection mutated through returned snapshot: %q", got)
	}
}

func TestLedgerFailsClosedOnFutureAck(t *testing.T) {
	var ledger Ledger
	_, err := ledger.Ack(TerminalWriteResult{Sequence: 5})
	if !errors.Is(err, ErrUnexpectedWriteAck) {
		t.Fatalf("future ack err = %v, want ErrUnexpectedWriteAck", err)
	}
	if !ledger.Failed() {
		t.Fatal("ledger did not fail closed on future ack")
	}
}

func TestLedgerClearBelowIsAppliedOnlyToTerminalWrite(t *testing.T) {
	var ledger Ledger
	flush, ok := ledger.Enqueue("tail", FlushOptions{ClearBelowBefore: true})
	if !ok {
		t.Fatal("expected flush")
	}
	if strings.HasPrefix(flush.Text, "\x1b[J") {
		t.Fatalf("scheduled flush mutated text before terminal write: %q", flush.Text)
	}
	write, ready, err := ledger.AcceptFlush(flush)
	if err != nil {
		t.Fatalf("accept flush: %v", err)
	}
	if !ready {
		t.Fatal("expected write")
	}
	if got, want := write.Text, "\x1b[Jtail"; got != want {
		t.Fatalf("terminal write text = %q, want %q", got, want)
	}
}

func TestLedgerAllowsBlankSpacerWritesToAckFrontier(t *testing.T) {
	var ledger Ledger
	flush, ok := ledger.Enqueue("\n\n", FlushOptions{AllowBlank: true})
	if !ok {
		t.Fatal("expected blank spacer flush to schedule")
	}

	write, ready, err := ledger.AcceptFlush(flush)
	if err != nil {
		t.Fatalf("accept blank spacer: %v", err)
	}
	if !ready {
		t.Fatal("expected blank spacer write to be ready")
	}
	if !write.AllowBlank || write.Text != "\n\n" {
		t.Fatalf("blank spacer write = %+v, want allowed blank payload", write)
	}

	if _, err := ledger.Ack(TerminalWriteResult{Sequence: write.Sequence}); err != nil {
		t.Fatalf("ack blank spacer: %v", err)
	}
	if got := ledger.AckedSequence(); got != write.Sequence {
		t.Fatalf("acked sequence = %d, want %d", got, write.Sequence)
	}
}

func TestLedgerRejectsDisallowedBlankWritesBeforeScheduling(t *testing.T) {
	var ledger Ledger
	if _, ok := ledger.Enqueue("\n\n", FlushOptions{}); ok {
		t.Fatal("disallowed blank flush scheduled")
	}
	if got := ledger.LastScheduledSequence(); got != 0 {
		t.Fatalf("scheduled sequence = %d, want 0", got)
	}
}

func TestLedgerCommittedDeliveryAdvancesOnlyAfterNativeFlushAck(t *testing.T) {
	var ledger Ledger
	ledger.ResetCommittedDelivery(2, 10)
	suffix := clientui.CommittedTranscriptSuffix{
		Revision:        11,
		StartEntryCount: 2,
		NextEntryCount:  4,
		Entries:         []clientui.ChatEntry{{Role: "assistant", Text: "a"}, {Role: "assistant", Text: "b"}},
	}

	if err := ledger.BeginCommittedNativeFlush(suffix, 7); err != nil {
		t.Fatalf("begin native flush: %v", err)
	}
	if got := ledger.CommittedDeliveryState().LastEmittedCommittedEntryCount; got != 2 {
		t.Fatalf("cursor advanced before ack: got %d want 2", got)
	}
	if advanced := ledger.AckCommittedNativeFlush(6); advanced {
		t.Fatalf("ack before target advanced cursor: %+v", ledger.CommittedDeliveryState())
	}
	if got := ledger.CommittedDeliveryState().LastEmittedCommittedEntryCount; got != 2 {
		t.Fatalf("cursor advanced after stale ack: got %d want 2", got)
	}
	if advanced := ledger.AckCommittedNativeFlush(8); advanced {
		t.Fatalf("ack for a different newer flush advanced cursor: %+v", ledger.CommittedDeliveryState())
	}
	if got := ledger.CommittedDeliveryState().LastEmittedCommittedEntryCount; got != 2 {
		t.Fatalf("cursor advanced after mismatched ack: got %d want 2", got)
	}
	if advanced := ledger.AckCommittedNativeFlush(7); !advanced {
		t.Fatalf("expected target ack to advance cursor: %+v", ledger.CommittedDeliveryState())
	}
	state := ledger.CommittedDeliveryState()
	if state.LastEmittedCommittedEntryCount != 4 {
		t.Fatalf("last emitted count = %d, want 4", state.LastEmittedCommittedEntryCount)
	}
	if state.LastEmittedTranscriptRevision != 11 {
		t.Fatalf("last revision = %d, want 11", state.LastEmittedTranscriptRevision)
	}
	if state.NativeFlushInFlight {
		t.Fatalf("flush still in flight after ack: %+v", state)
	}
}

func TestLedgerCommittedDeliveryExtendsInFlightFlush(t *testing.T) {
	var ledger Ledger
	ledger.ResetCommittedDelivery(0, 10)
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

	if err := ledger.BeginCommittedNativeFlush(first, 5); err != nil {
		t.Fatalf("begin first flush: %v", err)
	}
	if err := ledger.BeginCommittedNativeFlush(second, 6); err != nil {
		t.Fatalf("extend flush: %v", err)
	}
	if got := ledger.CommittedDeliveryState().LastAppliedCommittedEntryCount; got != 3 {
		t.Fatalf("applied cursor = %d, want 3", got)
	}
	if advanced := ledger.AckCommittedNativeFlush(5); advanced {
		t.Fatalf("old flush ack advanced extended cursor: %+v", ledger.CommittedDeliveryState())
	}
	if got := ledger.CommittedDeliveryState().LastEmittedCommittedEntryCount; got != 0 {
		t.Fatalf("emitted cursor advanced before extended ack: got %d want 0", got)
	}
	if advanced := ledger.AckCommittedNativeFlush(6); !advanced {
		t.Fatalf("expected extended flush ack to advance cursor: %+v", ledger.CommittedDeliveryState())
	}
	if got := ledger.CommittedDeliveryState().LastEmittedCommittedEntryCount; got != 3 {
		t.Fatalf("emitted cursor = %d, want 3", got)
	}
}

func TestLedgerCommittedDeliveryExtendsInFlightFlushWithoutNewWrite(t *testing.T) {
	var ledger Ledger
	ledger.ResetCommittedDelivery(0, 10)
	first := clientui.CommittedTranscriptSuffix{
		Revision:        11,
		StartEntryCount: 0,
		NextEntryCount:  1,
		Entries:         []clientui.ChatEntry{{Role: "assistant", Text: "visible"}},
	}
	invisibleExtension := clientui.CommittedTranscriptSuffix{
		Revision:        12,
		StartEntryCount: 1,
		NextEntryCount:  3,
		Entries:         []clientui.ChatEntry{{Role: "system", Text: ""}, {Role: "system", Text: ""}},
	}

	if err := ledger.BeginCommittedNativeFlush(first, 5); err != nil {
		t.Fatalf("begin first flush: %v", err)
	}
	if err := ledger.BeginCommittedNativeFlush(invisibleExtension, 5); err != nil {
		t.Fatalf("extend same flush: %v", err)
	}
	state := ledger.CommittedDeliveryState()
	if state.FlushSequence != 5 || state.FlushNextEntryCount != 3 {
		t.Fatalf("extended flush state = %+v, want sequence 5 through entry 3", state)
	}
	if got := state.LastEmittedCommittedEntryCount; got != 0 {
		t.Fatalf("emitted cursor advanced before original write ack: got %d want 0", got)
	}
	if advanced := ledger.AckCommittedNativeFlush(5); !advanced {
		t.Fatalf("expected original write ack to advance extended cursor: %+v", ledger.CommittedDeliveryState())
	}
	if got := ledger.CommittedDeliveryState().LastEmittedCommittedEntryCount; got != 3 {
		t.Fatalf("emitted cursor after original write ack = %d, want 3", got)
	}
}

func TestLedgerCommittedDeliveryRecordsPendingRangeWhileEmissionDisabled(t *testing.T) {
	var ledger Ledger
	ledger.ResetCommittedDelivery(2, 10)
	ledger.SetCommittedDeliveryEmissionEnabled(false)

	ledger.RecordCommittedAdvance(5, 12)
	ledger.RecordCommittedAdvance(7, 13)

	state := ledger.CommittedDeliveryState()
	if state.LastEmittedCommittedEntryCount != 2 {
		t.Fatalf("cursor advanced while emission disabled: %+v", state)
	}
	if len(state.PendingCommittedRanges) != 1 {
		t.Fatalf("pending ranges = %+v, want one compact range", state.PendingCommittedRanges)
	}
	pending := state.PendingCommittedRanges[0]
	if pending.StartEntryCount != 2 || pending.EndEntryCount != 7 || pending.Revision != 13 {
		t.Fatalf("unexpected pending range: %+v", pending)
	}
	if _, ok := ledger.CommittedDeliveryNextSuffixRequest(); !ok {
		t.Fatal("expected suffix request for pending range")
	}
}

func TestLedgerCommittedDeliveryRetryKeepsCursorWhenFlushFails(t *testing.T) {
	var ledger Ledger
	ledger.ResetCommittedDelivery(3, 10)
	suffix := clientui.CommittedTranscriptSuffix{
		Revision:        11,
		StartEntryCount: 3,
		NextEntryCount:  6,
		Entries:         []clientui.ChatEntry{{Role: "assistant", Text: "a"}},
	}

	if err := ledger.BeginCommittedNativeFlush(suffix, 9); err != nil {
		t.Fatalf("begin native flush: %v", err)
	}
	ledger.FailCommittedNativeFlush(10)
	if !ledger.CommittedDeliveryState().NativeFlushInFlight {
		t.Fatalf("mismatched failure cleared flush: %+v", ledger.CommittedDeliveryState())
	}
	ledger.FailCommittedNativeFlush(9)

	state := ledger.CommittedDeliveryState()
	if state.NativeFlushInFlight {
		t.Fatalf("flush still in flight after failure: %+v", state)
	}
	if state.LastEmittedCommittedEntryCount != 3 || state.LastEmittedTranscriptRevision != 10 {
		t.Fatalf("cursor changed after failed flush: %+v", state)
	}
	if _, ok := ledger.CommittedDeliveryNextSuffixRequest(); !ok {
		t.Fatal("expected retry request after failed flush")
	}
}

func TestLedgerKeepsPromotedAssistantStableLinesLiveUntilTerminalAck(t *testing.T) {
	var ledger Ledger
	update := ledger.ApplyAssistantStreamSource(AssistantStreamInput{
		Source: "line1\nline2\n",
		Theme:  "dark",
		Width:  80,
	})
	if stable := testProjectionLinesText(update.Stable); !strings.Contains(stable, "line1") || strings.Contains(stable, "line2") {
		t.Fatalf("stable lines = %q, want only first line promoted", stable)
	}
	if live := testProjectionLinesText(update.Live); !strings.Contains(live, "line1") || !strings.Contains(live, "line2") {
		t.Fatalf("live lines before scheduling = %q, want promoted line still live until ack", live)
	}
	flush, ok := ledger.Enqueue("line1", FlushOptions{})
	if !ok {
		t.Fatal("expected stable line flush to schedule")
	}
	ledger.BindAssistantStableFlush(flush.Sequence, update.StableLineCount)
	if live := testProjectionLinesText(ledger.AssistantStreamLiveLines()); !strings.Contains(live, "line1") || !strings.Contains(live, "line2") {
		t.Fatalf("live lines after scheduling = %q, want scheduled line still live before ack", live)
	}

	write, ready, err := ledger.AcceptFlush(flush)
	if err != nil {
		t.Fatalf("accept stable flush: %v", err)
	}
	if !ready {
		t.Fatal("expected stable flush write")
	}
	if live := testProjectionLinesText(ledger.AssistantStreamLiveLines()); !strings.Contains(live, "line1") || !strings.Contains(live, "line2") {
		t.Fatalf("live lines before terminal ack = %q, want scheduled line still live", live)
	}

	if _, err := ledger.Ack(TerminalWriteResult{Sequence: write.Sequence}); err != nil {
		t.Fatalf("ack stable flush: %v", err)
	}
	if live := testProjectionLinesText(ledger.AssistantStreamLiveLines()); strings.Contains(live, "line1") || !strings.Contains(live, "line2") {
		t.Fatalf("live lines after terminal ack = %q, want only mutable tail", live)
	}
}

func TestLedgerAssistantLiveLinesForWidthMismatchKeepsAckedStablePrefixOut(t *testing.T) {
	var ledger Ledger
	source := "line1\nline2\n"
	update := ledger.ApplyAssistantStreamSource(AssistantStreamInput{
		Source: source,
		Theme:  "dark",
		Width:  80,
	})
	flush, ok := ledger.Enqueue("line1", FlushOptions{})
	if !ok {
		t.Fatal("expected stable line flush to schedule")
	}
	ledger.BindAssistantStableFlush(flush.Sequence, update.StableLineCount)
	write, ready, err := ledger.AcceptFlush(flush)
	if err != nil {
		t.Fatalf("accept stable flush: %v", err)
	}
	if !ready {
		t.Fatal("expected stable flush write")
	}
	if _, err := ledger.Ack(TerminalWriteResult{Sequence: write.Sequence}); err != nil {
		t.Fatalf("ack stable flush: %v", err)
	}

	live := testProjectionLinesText(ledger.AssistantStreamLiveLinesFor(AssistantStreamInput{
		Source: source,
		Theme:  "dark",
		Width:  24,
	}))
	if strings.Contains(live, "line1") || !strings.Contains(live, "line2") {
		t.Fatalf("width-mismatched live lines = %q, want acked prefix excluded and tail retained", live)
	}
}

func TestLedgerWriteFailureKeepsPromotedAssistantStableLinesLiveAndFailsClosed(t *testing.T) {
	var ledger Ledger
	update := ledger.ApplyAssistantStreamSource(AssistantStreamInput{
		Source: "line1\nline2\n",
		Theme:  "dark",
		Width:  80,
	})
	flush, ok := ledger.Enqueue("line1", FlushOptions{})
	if !ok {
		t.Fatal("expected stable line flush to schedule")
	}
	ledger.BindAssistantStableFlush(flush.Sequence, update.StableLineCount)
	write, ready, err := ledger.AcceptFlush(flush)
	if err != nil {
		t.Fatalf("accept stable flush: %v", err)
	}
	if !ready {
		t.Fatal("expected stable flush write")
	}

	if _, err := ledger.Ack(TerminalWriteResult{Sequence: write.Sequence, Err: "write failed"}); !errors.Is(err, ErrTerminalWrite) {
		t.Fatalf("ack failure err = %v, want ErrTerminalWrite", err)
	}
	if !ledger.Failed() {
		t.Fatal("expected terminal write failure to fail ledger closed")
	}
	if live := testProjectionLinesText(ledger.AssistantStreamLiveLines()); !strings.Contains(live, "line1") || !strings.Contains(live, "line2") {
		t.Fatalf("live lines after terminal failure = %q, want scheduled stable line retained live", live)
	}
}

func TestLedgerAssistantFinalizationSkipsScheduledStablePrefix(t *testing.T) {
	var ledger Ledger
	update := ledger.ApplyAssistantStreamSource(AssistantStreamInput{
		Source: "line1\nline2\n",
		Theme:  "dark",
		Width:  80,
	})
	flush, ok := ledger.Enqueue("line1", FlushOptions{})
	if !ok {
		t.Fatal("expected stable line flush to schedule")
	}
	ledger.BindAssistantStableFlush(flush.Sequence, update.StableLineCount)

	final := ledger.FinalizeAssistantStreamSource(AssistantStreamInput{
		Source: "line1\nline2\nfinal tail\n",
		Theme:  "dark",
		Width:  80,
	})
	stable := testProjectionLinesText(final.Stable)
	if strings.Contains(stable, "line1") {
		t.Fatalf("finalization duplicated scheduled stable prefix: %q", stable)
	}
	if !strings.Contains(stable, "line2") || !strings.Contains(stable, "final") || !strings.Contains(stable, "tail") {
		t.Fatalf("finalization stable tail = %q, want remaining unscheduled lines", stable)
	}
}

func TestLedgerAssistantFinalizationFailsClosedWhenScheduledPrefixChanges(t *testing.T) {
	var ledger Ledger
	update := ledger.ApplyAssistantStreamSource(AssistantStreamInput{
		Source: "original stable\n\nmutable tail",
		Theme:  "dark",
		Width:  80,
	})
	bindAssistantStableFlushForTest(t, &ledger, update)

	final := ledger.FinalizeAssistantStreamSource(AssistantStreamInput{
		Source: "rewritten stable\n\nmutable tail",
		Theme:  "dark",
		Width:  80,
	})
	if !final.NeedsReplay {
		t.Fatal("expected finalization to fail closed when final text rewrites a scheduled stable prefix")
	}
	if state := ledger.AssistantStreamState(); !state.NeedsReplay {
		t.Fatalf("state = %+v, want invalidated stream after scheduled prefix rewrite", state)
	}
}

func TestLedgerAssistantStreamPromotesCompletedMarkdownLines(t *testing.T) {
	var ledger Ledger

	first := ledger.ApplyAssistantStreamSource(AssistantStreamInput{
		Source: "first **line**\nsecond",
		Theme:  "dark",
		Width:  48,
	})
	if got := len(first.Stable); got != 0 {
		t.Fatalf("expected last complete line to stay live until next line proves it is not setext, got stable %#v", first.Stable)
	}
	if got := testProjectionLinesText(first.Live); !strings.Contains(got, "first line") || !strings.Contains(got, "second") {
		t.Fatalf("expected held first line and incomplete second line in live tail, got %q", got)
	}

	second := ledger.ApplyAssistantStreamSource(AssistantStreamInput{
		Source: "first **line**\nsecond line\n",
		Theme:  "dark",
		Width:  48,
	})
	if got := testProjectionLinesText(second.Stable); !strings.Contains(got, "first line") || strings.Contains(got, "second line") {
		t.Fatalf("expected first line to promote and last complete line to stay live, got %q", got)
	}
	bindAssistantStableFlushForTest(t, &ledger, second)

	third := ledger.ApplyAssistantStreamSource(AssistantStreamInput{
		Source: "first **line**\nsecond line\n\n",
		Theme:  "dark",
		Width:  48,
	})
	if got := testProjectionLinesText(third.Stable); !strings.Contains(got, "second line") {
		t.Fatalf("expected second line to promote after blank-line boundary, got %q", got)
	}
}

func TestLedgerAssistantStreamHoldsPipeTableUntilFinalize(t *testing.T) {
	var ledger Ledger

	update := ledger.ApplyAssistantStreamSource(AssistantStreamInput{
		Source: "| Name | Value |\n| --- | --- |\n| alpha | beta |\n",
		Theme:  "dark",
		Width:  72,
	})
	if got := len(update.Stable); got != 0 {
		t.Fatalf("expected active table to stay live, got stable %#v", update.Stable)
	}
	if got := testProjectionLinesText(update.Live); !strings.Contains(got, "alpha") || !strings.Contains(got, "beta") {
		t.Fatalf("expected table in live tail, got %q", got)
	}

	final := ledger.FinalizeAssistantStreamSource(AssistantStreamInput{Theme: "dark", Width: 72})
	if got := testProjectionLinesText(final.Stable); !strings.Contains(got, "alpha") || !strings.Contains(got, "beta") {
		t.Fatalf("expected final table to promote, got %q", got)
	}
	if got := len(final.Live); got != 0 {
		t.Fatalf("expected final live lines empty, got %#v", final.Live)
	}
}

func TestLedgerAssistantStreamHoldsSetextHeadingCandidate(t *testing.T) {
	var ledger Ledger

	pending := ledger.ApplyAssistantStreamSource(AssistantStreamInput{
		Source: "Heading\n",
		Theme:  "dark",
		Width:  80,
	})
	if got := len(pending.Stable); got != 0 {
		t.Fatalf("expected setext candidate to stay live, got stable %#v", pending.Stable)
	}
	if got := testProjectionLinesText(pending.Live); !strings.Contains(got, "Heading") {
		t.Fatalf("expected setext candidate in live tail, got %q", got)
	}

	confirmed := ledger.ApplyAssistantStreamSource(AssistantStreamInput{
		Source: "Heading\n---\n",
		Theme:  "dark",
		Width:  80,
	})
	if got := testProjectionLinesText(confirmed.Stable); !strings.Contains(got, "## Heading") {
		t.Fatalf("expected confirmed setext heading to promote as heading, got %q", got)
	}
	if got := testProjectionLinesText(confirmed.Stable); strings.Contains(got, "❮ Heading") {
		t.Fatalf("expected stale paragraph render not promoted, got %q", got)
	}
}

func TestLedgerAssistantStreamHoldsReferenceLinksUntilDefinition(t *testing.T) {
	var ledger Ledger

	pending := ledger.ApplyAssistantStreamSource(AssistantStreamInput{
		Source: "This is [link][id]\n",
		Theme:  "dark",
		Width:  100,
	})
	if got := len(pending.Stable); got != 0 {
		t.Fatalf("expected unresolved reference link to stay live, got stable %#v", pending.Stable)
	}

	defined := ledger.ApplyAssistantStreamSource(AssistantStreamInput{
		Source: "This is [link][id]\n\n[id]: https://example.com\n",
		Theme:  "dark",
		Width:  100,
	})
	if got := testProjectionLinesText(defined.Stable); !strings.Contains(got, "link https://example.com") {
		t.Fatalf("expected resolved reference link to promote with renderer-supported link output, got %q", got)
	}
	if got := testProjectionLinesText(defined.Stable); strings.Contains(got, "[link][id]") {
		t.Fatalf("expected stale unresolved reference output not promoted, got %q", got)
	}
}

func TestLedgerAssistantStreamHoldsShortcutReferenceLinksUntilDefinition(t *testing.T) {
	var ledger Ledger

	pending := ledger.ApplyAssistantStreamSource(AssistantStreamInput{
		Source: "This is [id]\n",
		Theme:  "dark",
		Width:  100,
	})
	if got := len(pending.Stable); got != 0 {
		t.Fatalf("expected unresolved shortcut reference link to stay live, got stable %#v", pending.Stable)
	}

	defined := ledger.ApplyAssistantStreamSource(AssistantStreamInput{
		Source: "This is [id]\n\n[id]: https://example.com\n",
		Theme:  "dark",
		Width:  100,
	})
	if got := testProjectionLinesText(defined.Stable); !strings.Contains(got, "id https://example.com") {
		t.Fatalf("expected resolved shortcut reference link to promote with renderer-supported link output, got %q", got)
	}
}

func TestLedgerAssistantStreamPromotesOpenCodeFenceLinesOptimistically(t *testing.T) {
	var ledger Ledger

	update := ledger.ApplyAssistantStreamSource(AssistantStreamInput{
		Source: "```go\nfunc main() {}\n",
		Theme:  "dark",
		Width:  80,
	})
	if got := testProjectionLinesText(update.Stable); !strings.Contains(got, "func main()") {
		t.Fatalf("expected complete open-fence code line to promote, got %q", got)
	}
}

func TestLedgerAssistantStreamResizeInvalidatesFurtherPromotion(t *testing.T) {
	var ledger Ledger
	initial := ledger.ApplyAssistantStreamSource(AssistantStreamInput{
		Source: "stable line\n\n",
		Theme:  "dark",
		Width:  72,
	})
	bindAssistantStableFlushForTest(t, &ledger, initial)

	update := ledger.ApplyAssistantStreamSource(AssistantStreamInput{
		Source: "stable line\n\nnew stable line\n",
		Theme:  "dark",
		Width:  24,
	})
	if got := len(update.Stable); got != 0 {
		t.Fatalf("expected resize-invalidated stream to stop promotion, got %#v", update.Stable)
	}
	if got := testProjectionLinesText(update.Live); !strings.Contains(got, "new stable line") {
		t.Fatalf("expected resized content in live tail, got %q", got)
	}
	if !update.NeedsReplay {
		t.Fatalf("expected resize-invalidated update to request replay")
	}
}

func TestLedgerAssistantStreamReRenderShorterThanScheduledDoesNotPanic(t *testing.T) {
	var ledger Ledger
	source := "This is a fairly long paragraph that wraps across several lines.\n\n"
	initial := ledger.ApplyAssistantStreamSource(AssistantStreamInput{
		Source: source,
		Theme:  "dark",
		Width:  24,
	})
	bindAssistantStableFlushForTest(t, &ledger, initial)
	if got := ledger.AssistantStreamState().ScheduledStableLines; got < 2 {
		t.Fatalf("expected wrapped paragraph to schedule multiple stable lines, got %d", got)
	}

	final := ledger.FinalizeAssistantStreamSource(AssistantStreamInput{
		Source: source,
		Theme:  "dark",
		Width:  400,
	})
	if got := len(final.Stable); got != 0 {
		t.Fatalf("expected no stable promotion when re-render shrank below scheduled count, got %#v", final.Stable)
	}
	if final.Live != nil {
		t.Fatalf("expected finalize to clear the live tail, got %#v", final.Live)
	}
}

func TestLedgerAssistantStreamApplySourceShorterThanScheduledDoesNotPanic(t *testing.T) {
	var ledger Ledger
	source := "This is a fairly long paragraph that wraps across several lines.\n\n"
	initial := ledger.ApplyAssistantStreamSource(AssistantStreamInput{
		Source: source,
		Theme:  "dark",
		Width:  24,
	})
	bindAssistantStableFlushForTest(t, &ledger, initial)
	if got := ledger.AssistantStreamState().ScheduledStableLines; got < 2 {
		t.Fatalf("expected wrapped paragraph to schedule multiple stable lines, got %d", got)
	}

	update := ledger.ApplyAssistantStreamSource(AssistantStreamInput{
		Source: source,
		Theme:  "dark",
		Width:  400,
	})
	if got := len(update.Stable); got != 0 {
		t.Fatalf("expected no stable promotion when re-render shrank below scheduled count, got %#v", update.Stable)
	}
}

func TestLedgerFinalAssistantCommitBindsByStepID(t *testing.T) {
	var ledger Ledger
	ledger.SetAssistantStreamStepID("step-1")

	if _, ok := ledger.ObserveAssistantCommitCandidate(AssistantCommitCandidate{
		StepID:          "other-step",
		StartEntryCount: 4,
		Entries:         []AssistantCommitEntry{{Role: "assistant", Text: "wrong turn"}},
	}); ok {
		t.Fatal("commit from another step bound to active assistant stream")
	}
	if state := ledger.AssistantStreamState(); state.CommitRangeSet {
		t.Fatalf("mismatched commit changed stream binding: %+v", state)
	}

	binding, ok := ledger.ObserveAssistantCommitCandidate(AssistantCommitCandidate{
		StepID:          " step-1 ",
		StartEntryCount: 4,
		Entries:         []AssistantCommitEntry{{Role: "assistant", Text: "right turn"}},
	})
	if !ok {
		t.Fatal("matching step final assistant commit did not bind")
	}
	if binding.StartEntryCount != 4 || binding.EndEntryCount != 5 {
		t.Fatalf("binding = %+v, want [4,5)", binding)
	}
	state := ledger.AssistantStreamState()
	if !state.CommitRangeSet || state.CommitStartEntryCount != 4 || state.CommitEndEntryCount != 5 {
		t.Fatalf("state = %+v, want committed range [4,5)", state)
	}
}

func TestLedgerSuffixFinalizerTextBindingOnlyWithoutActiveStepID(t *testing.T) {
	var ledger Ledger
	ledger.ApplyAssistantStreamSource(AssistantStreamInput{
		Source: "same final text",
		Theme:  "dark",
		Width:  80,
	})
	if !ledger.AssistantSuffixCanFinalizeText(" same final text ") {
		t.Fatal("matching suffix text should finalize stream without active step ID")
	}

	ledger.SetAssistantStreamStepID("step-1")
	if ledger.AssistantSuffixCanFinalizeText("same final text") {
		t.Fatal("matching suffix text must not finalize stream when active step ID is known")
	}
}

func TestLedgerParallelToolStableFrontierKeepsLaterCompletedToolLive(t *testing.T) {
	entries := []tui.TranscriptEntry{
		{Role: "user", Text: "prompt"},
		{Role: "tool_call", Text: "echo a", ToolCallID: "call_a"},
		{Role: "tool_call", Text: "echo b", ToolCallID: "call_b"},
		{Role: "tool_result_ok", Text: "out-b", ToolCallID: "call_b"},
	}

	committed := CommittedOngoingEntries(entries)
	if len(committed) != 1 || committed[0].Role != tui.TranscriptRoleUser {
		t.Fatalf("committed entries = %+v, want only prompt before unresolved earlier tool", committed)
	}
	pending := PendingOngoingEntries(entries)
	if got := testEntryKeys(pending); got != "tool_call:call_a,tool_call:call_b,tool_result_ok:call_b" {
		t.Fatalf("pending entries = %s, want unresolved call_a plus completed later call_b", got)
	}
}

func TestLedgerCommittedOngoingPrefixEndIncludesEntriesFilteredFromRenderedOutput(t *testing.T) {
	entries := []tui.TranscriptEntry{
		{Role: "user", Text: "prompt"},
		{Role: "assistant", Text: ""},
		{Role: "tool_call", Text: "pending", ToolCallID: "call_a", Transient: true},
	}

	if got := CommittedOngoingPrefixEnd(entries); got != 2 {
		t.Fatalf("committed prefix end = %d, want stable transcript boundary before pending tool", got)
	}
	committed := CommittedOngoingEntries(entries)
	if got := testEntryKeys(committed); got != "user:" {
		t.Fatalf("rendered committed entries = %s, want empty assistant filtered from render output", got)
	}
}

func TestLedgerParallelToolFrontierCommitsResolvedPrefixOnce(t *testing.T) {
	entries := []tui.TranscriptEntry{
		{Role: "user", Text: "prompt"},
		{Role: "tool_call", Text: "echo a", ToolCallID: "call_a"},
		{Role: "tool_call", Text: "echo b", ToolCallID: "call_b"},
		{Role: "tool_result_ok", Text: "out-b", ToolCallID: "call_b"},
		{Role: "tool_result_ok", Text: "out-a", ToolCallID: "call_a"},
	}

	committed := CommittedOngoingEntries(entries)
	if got := testEntryKeys(committed); got != "user:,tool_call:call_a,tool_call:call_b,tool_result_ok:call_b,tool_result_ok:call_a" {
		t.Fatalf("committed entries = %s, want resolved prefix in transcript order", got)
	}
	if pending := PendingOngoingEntries(entries); len(pending) != 0 {
		t.Fatalf("pending entries after stable frontier resolved = %+v, want empty", pending)
	}
}

func testProjection(line string) tui.TranscriptProjection {
	return tui.TranscriptProjection{Blocks: []tui.TranscriptProjectionBlock{{
		Role:         tui.RenderIntentAssistant,
		DividerGroup: "assistant",
		EntryIndex:   1,
		EntryEnd:     1,
		Lines:        []string{line},
	}}}
}

func testProjectionLinesText(lines []tui.TranscriptProjectionLine) string {
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		parts = append(parts, xansi.Strip(line.Text))
	}
	return strings.Join(parts, "\n")
}

func bindAssistantStableFlushForTest(t *testing.T, ledger *Ledger, update AssistantStreamUpdate) {
	t.Helper()
	if update.StableLineCount <= ledger.AssistantStreamState().ScheduledStableLines {
		return
	}
	flush, ok := ledger.Enqueue("assistant stable test flush", FlushOptions{})
	if !ok {
		t.Fatal("expected assistant stable test flush to enqueue")
	}
	ledger.BindAssistantStableFlush(flush.Sequence, update.StableLineCount)
}

func testEntryKeys(entries []tui.TranscriptEntry) string {
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		parts = append(parts, string(entry.Role)+":"+entry.ToolCallID)
	}
	return strings.Join(parts, ",")
}
