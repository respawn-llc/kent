package nativescrollback

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"core/cli/tui"
)

type ledgerProtocolOp uint8

const (
	ledgerProtocolEnqueue ledgerProtocolOp = iota
	ledgerProtocolAcceptOldest
	ledgerProtocolAcceptNewest
	ledgerProtocolAckInFlight
	ledgerProtocolFailInFlight
	ledgerProtocolDuplicateAck
	ledgerProtocolFutureAck
	ledgerProtocolWrongInFlightAck
)

func (op ledgerProtocolOp) String() string {
	switch op {
	case ledgerProtocolEnqueue:
		return "enqueue"
	case ledgerProtocolAcceptOldest:
		return "accept_oldest"
	case ledgerProtocolAcceptNewest:
		return "accept_newest"
	case ledgerProtocolAckInFlight:
		return "ack_in_flight"
	case ledgerProtocolFailInFlight:
		return "fail_in_flight"
	case ledgerProtocolDuplicateAck:
		return "duplicate_ack"
	case ledgerProtocolFutureAck:
		return "future_ack"
	case ledgerProtocolWrongInFlightAck:
		return "wrong_in_flight_ack"
	default:
		return fmt.Sprintf("op_%d", op)
	}
}

type modelFlush struct {
	flush    ScheduledFlush
	payload  string
	accepted bool
}

type ledgerProtocolModel struct {
	ledger         Ledger
	flushes        []modelFlush
	inFlight       *TerminalWrite
	permanent      []string
	writeSeen      map[Sequence]int
	failed         bool
	expectedFailed bool
}

func TestLedgerFlushProtocolModelPreventsMissingDuplicateAndUnackedPermanent(t *testing.T) {
	initial := ledgerProtocolModel{writeSeen: map[Sequence]int{}}
	visited := 0
	walkLedgerProtocolModel(t, initial, nil, 0, &visited)
	if visited < 50 {
		t.Fatalf("model explored too few states: %d", visited)
	}
}

type assistantStreamModelOp uint8

const (
	assistantStreamGrowSource assistantStreamModelOp = iota
	assistantStreamScheduleStable
	assistantStreamAcceptNext
	assistantStreamAckInFlight
	assistantStreamFailInFlight
	assistantStreamWrongInFlightAck
)

func (op assistantStreamModelOp) String() string {
	switch op {
	case assistantStreamGrowSource:
		return "grow_source"
	case assistantStreamScheduleStable:
		return "schedule_stable"
	case assistantStreamAcceptNext:
		return "accept_next"
	case assistantStreamAckInFlight:
		return "ack_in_flight"
	case assistantStreamFailInFlight:
		return "fail_in_flight"
	case assistantStreamWrongInFlightAck:
		return "wrong_in_flight_ack"
	default:
		return fmt.Sprintf("assistant_op_%d", op)
	}
}

type assistantStreamModelFlush struct {
	flush           ScheduledFlush
	stableLineCount int
	lines           []string
	accepted        bool
}

type assistantStreamModel struct {
	ledger           Ledger
	sourceLineCount  int
	lastStableTarget int
	lastStableLines  []string
	flushes          []assistantStreamModelFlush
	inFlight         *TerminalWrite
	permanentLines   []string
	writeSeen        map[Sequence]int
	failed           bool
	expectedFailed   bool
}

const (
	assistantStreamModelMaxSourceLines = 4
	assistantStreamModelTheme          = "dark"
	assistantStreamModelWidth          = 80
)

func TestLedgerAssistantStableLineModelPreventsStuckLiveMissingDuplicateAndUnackedPermanent(t *testing.T) {
	initial := assistantStreamModel{writeSeen: map[Sequence]int{}}
	visited := 0
	walkAssistantStreamModel(t, initial, nil, 0, &visited)
	if visited < 80 {
		t.Fatalf("assistant stream model explored too few states: %d", visited)
	}
}

func walkAssistantStreamModel(t *testing.T, state assistantStreamModel, path []assistantStreamModelOp, depth int, visited *int) {
	t.Helper()
	state.assertInvariants(t, path)
	*visited = *visited + 1
	if depth >= 11 || state.failed {
		return
	}
	for _, op := range state.availableOps() {
		next := state.clone()
		next.apply(t, op, append(path, op))
		walkAssistantStreamModel(t, next, append(path, op), depth+1, visited)
	}
}

func (m assistantStreamModel) clone() assistantStreamModel {
	cloned := m
	cloned.ledger = cloneAssistantStreamModelLedger(m.ledger)
	cloned.lastStableLines = append([]string(nil), m.lastStableLines...)
	cloned.flushes = make([]assistantStreamModelFlush, 0, len(m.flushes))
	for _, flush := range m.flushes {
		flush.lines = append([]string(nil), flush.lines...)
		cloned.flushes = append(cloned.flushes, flush)
	}
	cloned.permanentLines = append([]string(nil), m.permanentLines...)
	cloned.writeSeen = make(map[Sequence]int, len(m.writeSeen))
	for sequence, count := range m.writeSeen {
		cloned.writeSeen[sequence] = count
	}
	if m.inFlight != nil {
		write := *m.inFlight
		cloned.inFlight = &write
	}
	return cloned
}

func cloneAssistantStreamModelLedger(ledger Ledger) Ledger {
	cloned := ledger
	if ledger.pending != nil {
		cloned.pending = make(map[Sequence]ScheduledFlush, len(ledger.pending))
		for sequence, flush := range ledger.pending {
			cloned.pending[sequence] = flush
		}
	}
	if ledger.discardedInFlightAcks != nil {
		cloned.discardedInFlightAcks = make(map[Sequence]struct{}, len(ledger.discardedInFlightAcks))
		for sequence := range ledger.discardedInFlightAcks {
			cloned.discardedInFlightAcks[sequence] = struct{}{}
		}
	}
	cloned.assistant.rendered = append([]tui.TranscriptProjectionLine(nil), ledger.assistant.rendered...)
	cloned.assistant.stableFlushes = append([]assistantStableFlush(nil), ledger.assistant.stableFlushes...)
	return cloned
}

func (m assistantStreamModel) availableOps() []assistantStreamModelOp {
	if m.failed {
		return nil
	}
	ops := make([]assistantStreamModelOp, 0, 5)
	if m.sourceLineCount < assistantStreamModelMaxSourceLines {
		ops = append(ops, assistantStreamGrowSource)
	}
	if m.lastStableTarget > m.ledger.AssistantStreamState().ScheduledStableLines && len(m.lastStableLines) > 0 {
		ops = append(ops, assistantStreamScheduleStable)
	}
	if m.hasUnacceptedFlush() {
		ops = append(ops, assistantStreamAcceptNext)
	}
	if m.inFlight != nil {
		ops = append(ops, assistantStreamAckInFlight, assistantStreamFailInFlight, assistantStreamWrongInFlightAck)
	}
	return ops
}

func (m assistantStreamModel) hasUnacceptedFlush() bool {
	for _, flush := range m.flushes {
		if !flush.accepted {
			return true
		}
	}
	return false
}

func (m *assistantStreamModel) apply(t *testing.T, op assistantStreamModelOp, path []assistantStreamModelOp) {
	t.Helper()
	switch op {
	case assistantStreamGrowSource:
		m.applyGrowSource(t, path)
	case assistantStreamScheduleStable:
		m.applyScheduleStable(t, path)
	case assistantStreamAcceptNext:
		m.applyAcceptNext(t, path)
	case assistantStreamAckInFlight:
		m.applyAckInFlight(t, path)
	case assistantStreamFailInFlight:
		m.applyFailInFlight(t, path)
	case assistantStreamWrongInFlightAck:
		m.applyWrongInFlightAck(t, path)
	default:
		t.Fatalf("unknown assistant stream model op %d on path %s", op, formatAssistantStreamModelPath(path))
	}
}

func (m *assistantStreamModel) applyGrowSource(t *testing.T, path []assistantStreamModelOp) {
	t.Helper()
	if m.sourceLineCount >= assistantStreamModelMaxSourceLines {
		t.Fatalf("source grew past model bound on path %s", formatAssistantStreamModelPath(path))
	}
	m.sourceLineCount++
	update := m.ledger.ApplyAssistantStreamSource(AssistantStreamInput{
		Source: assistantStreamModelSource(m.sourceLineCount),
		Theme:  assistantStreamModelTheme,
		Width:  assistantStreamModelWidth,
	})
	if update.NeedsReplay {
		t.Fatalf("append-only source requested replay on path %s", formatAssistantStreamModelPath(path))
	}
	m.lastStableTarget = update.StableLineCount
	m.lastStableLines = assistantStreamModelLineTexts(update.Stable)
}

func (m *assistantStreamModel) applyScheduleStable(t *testing.T, path []assistantStreamModelOp) {
	t.Helper()
	state := m.ledger.AssistantStreamState()
	if m.lastStableTarget <= state.ScheduledStableLines || len(m.lastStableLines) == 0 {
		t.Fatalf("schedule without unscheduled stable lines on path %s", formatAssistantStreamModelPath(path))
	}
	lines := append([]string(nil), m.lastStableLines...)
	payload := strings.Join(lines, "\n")
	flush, ok := m.ledger.Enqueue(payload, FlushOptions{})
	if !ok {
		t.Fatalf("stable flush enqueue rejected on path %s", formatAssistantStreamModelPath(path))
	}
	m.ledger.BindAssistantStableFlush(flush.Sequence, m.lastStableTarget)
	m.flushes = append(m.flushes, assistantStreamModelFlush{
		flush:           flush,
		stableLineCount: m.lastStableTarget,
		lines:           lines,
	})
}

func (m *assistantStreamModel) applyAcceptNext(t *testing.T, path []assistantStreamModelOp) {
	t.Helper()
	index := m.firstUnacceptedFlushIndex()
	if index < 0 {
		t.Fatalf("accept without unaccepted flush on path %s", formatAssistantStreamModelPath(path))
	}
	write, ready, err := m.ledger.AcceptFlush(m.flushes[index].flush)
	if err != nil {
		t.Fatalf("accept stable flush failed on path %s: %v", formatAssistantStreamModelPath(path), err)
	}
	m.flushes[index].accepted = true
	if ready {
		m.observeWrite(t, path, write)
	}
}

func (m assistantStreamModel) firstUnacceptedFlushIndex() int {
	for idx, flush := range m.flushes {
		if !flush.accepted {
			return idx
		}
	}
	return -1
}

func (m *assistantStreamModel) applyAckInFlight(t *testing.T, path []assistantStreamModelOp) {
	t.Helper()
	if m.inFlight == nil {
		t.Fatalf("ack without in-flight write on path %s", formatAssistantStreamModelPath(path))
	}
	sequence := m.inFlight.Sequence
	update, err := m.ledger.Ack(TerminalWriteResult{Sequence: sequence})
	if err != nil {
		t.Fatalf("ack stable flush failed on path %s: %v", formatAssistantStreamModelPath(path), err)
	}
	flush := m.flushForSequence(t, path, sequence)
	m.permanentLines = append(m.permanentLines, flush.lines...)
	m.inFlight = nil
	if update.HasNext {
		m.observeWrite(t, path, update.Next)
	}
}

func (m *assistantStreamModel) applyFailInFlight(t *testing.T, path []assistantStreamModelOp) {
	t.Helper()
	if m.inFlight == nil {
		t.Fatalf("failure without in-flight write on path %s", formatAssistantStreamModelPath(path))
	}
	_, err := m.ledger.Ack(TerminalWriteResult{Sequence: m.inFlight.Sequence, Err: "assistant model write failure"})
	if !errors.Is(err, ErrTerminalWrite) {
		t.Fatalf("write failure err = %v, want ErrTerminalWrite on path %s", err, formatAssistantStreamModelPath(path))
	}
	m.failed = true
	m.expectedFailed = true
	m.inFlight = nil
}

func (m *assistantStreamModel) applyWrongInFlightAck(t *testing.T, path []assistantStreamModelOp) {
	t.Helper()
	if m.inFlight == nil {
		t.Fatalf("wrong ack without in-flight write on path %s", formatAssistantStreamModelPath(path))
	}
	sequence := m.inFlight.Sequence + 1
	if _, err := m.ledger.Ack(TerminalWriteResult{Sequence: sequence}); !errors.Is(err, ErrUnexpectedWriteAck) {
		t.Fatalf("wrong in-flight ack err = %v, want ErrUnexpectedWriteAck on path %s", err, formatAssistantStreamModelPath(path))
	}
	m.failed = true
	m.expectedFailed = true
}

func (m *assistantStreamModel) observeWrite(t *testing.T, path []assistantStreamModelOp, write TerminalWrite) {
	t.Helper()
	if m.inFlight != nil {
		t.Fatalf("second terminal write scheduled while %d is in flight on path %s", m.inFlight.Sequence, formatAssistantStreamModelPath(path))
	}
	expectedSequence := m.ledger.AckedSequence() + 1
	if write.Sequence != expectedSequence {
		t.Fatalf("write sequence = %d, want next ack frontier %d on path %s", write.Sequence, expectedSequence, formatAssistantStreamModelPath(path))
	}
	flush := m.flushForSequence(t, path, write.Sequence)
	if write.Text != flush.flush.Text {
		t.Fatalf("write text = %q, want %q on path %s", write.Text, flush.flush.Text, formatAssistantStreamModelPath(path))
	}
	m.writeSeen[write.Sequence]++
	if m.writeSeen[write.Sequence] > 1 {
		t.Fatalf("write %d scheduled more than once on path %s", write.Sequence, formatAssistantStreamModelPath(path))
	}
	writeCopy := write
	m.inFlight = &writeCopy
}

func (m assistantStreamModel) flushForSequence(t *testing.T, path []assistantStreamModelOp, sequence Sequence) assistantStreamModelFlush {
	t.Helper()
	for _, flush := range m.flushes {
		if flush.flush.Sequence == sequence {
			return flush
		}
	}
	t.Fatalf("unknown stable flush sequence %d on path %s", sequence, formatAssistantStreamModelPath(path))
	return assistantStreamModelFlush{}
}

func (m assistantStreamModel) assertInvariants(t *testing.T, path []assistantStreamModelOp) {
	t.Helper()
	state := m.ledger.AssistantStreamState()
	if m.failed {
		if !m.expectedFailed {
			t.Fatalf("assistant model failed unexpectedly on path %s", formatAssistantStreamModelPath(path))
		}
		if !m.ledger.Failed() {
			t.Fatalf("assistant model failed but ledger did not on path %s", formatAssistantStreamModelPath(path))
		}
	}
	rendered := assistantStreamModelRenderedLines(m.sourceLineCount)
	if state.AckedStableLines < 0 || state.AckedStableLines > len(rendered) {
		t.Fatalf("acked stable lines = %d outside rendered len %d on path %s", state.AckedStableLines, len(rendered), formatAssistantStreamModelPath(path))
	}
	if state.ScheduledStableLines < state.AckedStableLines || state.ScheduledStableLines > len(rendered) {
		t.Fatalf("scheduled/acked stable lines = %d/%d outside rendered len %d on path %s", state.ScheduledStableLines, state.AckedStableLines, len(rendered), formatAssistantStreamModelPath(path))
	}
	if !m.failed {
		nextSequence := m.ledger.AckedSequence() + 1
		for _, flush := range m.flushes {
			if !flush.accepted || flush.flush.Sequence != nextSequence {
				continue
			}
			if m.inFlight == nil || m.inFlight.Sequence != nextSequence {
				t.Fatalf("accepted assistant frontier flush %d has no in-flight write on path %s", nextSequence, formatAssistantStreamModelPath(path))
			}
		}
	}
	lastScheduledTarget := 0
	for _, flush := range m.flushes {
		if flush.stableLineCount <= lastScheduledTarget {
			t.Fatalf("stable flush target regressed from %d to %d on path %s", lastScheduledTarget, flush.stableLineCount, formatAssistantStreamModelPath(path))
		}
		lastScheduledTarget = flush.stableLineCount
	}
	if lastScheduledTarget != state.ScheduledStableLines {
		t.Fatalf("last scheduled stable target = %d, want ledger scheduled %d on path %s", lastScheduledTarget, state.ScheduledStableLines, formatAssistantStreamModelPath(path))
	}
	if !equalAssistantStreamModelLines(m.permanentLines, rendered[:state.AckedStableLines]) {
		t.Fatalf("permanent assistant lines = %v, want acked rendered prefix %v on path %s", m.permanentLines, rendered[:state.AckedStableLines], formatAssistantStreamModelPath(path))
	}
	if len(m.permanentLines) != state.AckedStableLines {
		t.Fatalf("permanent line count = %d, want acked stable count %d on path %s", len(m.permanentLines), state.AckedStableLines, formatAssistantStreamModelPath(path))
	}
	live := assistantStreamModelLineTexts(m.ledger.AssistantStreamLiveLines())
	if !equalAssistantStreamModelLines(live, rendered[state.AckedStableLines:]) {
		t.Fatalf("live assistant lines = %v, want unacked tail %v on path %s", live, rendered[state.AckedStableLines:], formatAssistantStreamModelPath(path))
	}
	if state.ScheduledStableLines > state.AckedStableLines && !assistantStreamModelHasPrefix(live, rendered[state.AckedStableLines:state.ScheduledStableLines]) {
		t.Fatalf("scheduled-but-unacked assistant lines disappeared from live tail: live=%v scheduled=%v path=%s", live, rendered[state.AckedStableLines:state.ScheduledStableLines], formatAssistantStreamModelPath(path))
	}
	seenPermanent := make(map[string]struct{}, len(m.permanentLines))
	for _, line := range m.permanentLines {
		if _, ok := seenPermanent[line]; ok {
			t.Fatalf("assistant permanent line duplicated: %q on path %s", line, formatAssistantStreamModelPath(path))
		}
		seenPermanent[line] = struct{}{}
	}
	for sequence, count := range m.writeSeen {
		if count > 1 {
			t.Fatalf("assistant write %d scheduled %d times on path %s", sequence, count, formatAssistantStreamModelPath(path))
		}
	}
	if m.inFlight != nil && m.inFlight.Sequence <= m.ledger.AckedSequence() {
		t.Fatalf("in-flight assistant write %d is at/before acked frontier %d on path %s", m.inFlight.Sequence, m.ledger.AckedSequence(), formatAssistantStreamModelPath(path))
	}
}

func assistantStreamModelSource(lineCount int) string {
	if lineCount <= 0 {
		return ""
	}
	var builder strings.Builder
	for idx := 1; idx <= lineCount; idx++ {
		fmt.Fprintf(&builder, "assistant model stable line %02d\n", idx)
	}
	return builder.String()
}

func assistantStreamModelRenderedLines(lineCount int) []string {
	if lineCount <= 0 {
		return nil
	}
	return assistantStreamModelLineTexts(tui.RenderAssistantMarkdownProjection(
		assistantStreamModelSource(lineCount),
		assistantStreamModelTheme,
		assistantStreamModelWidth,
	))
}

func assistantStreamModelLineTexts(lines []tui.TranscriptProjectionLine) []string {
	text := testProjectionLinesText(lines)
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func equalAssistantStreamModelLines(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if left[idx] != right[idx] {
			return false
		}
	}
	return true
}

func assistantStreamModelHasPrefix(lines []string, prefix []string) bool {
	if len(prefix) > len(lines) {
		return false
	}
	for idx := range prefix {
		if lines[idx] != prefix[idx] {
			return false
		}
	}
	return true
}

func formatAssistantStreamModelPath(path []assistantStreamModelOp) string {
	if len(path) == 0 {
		return "<start>"
	}
	parts := make([]string, 0, len(path))
	for _, op := range path {
		parts = append(parts, op.String())
	}
	return strings.Join(parts, " -> ")
}

func walkLedgerProtocolModel(t *testing.T, state ledgerProtocolModel, path []ledgerProtocolOp, depth int, visited *int) {
	t.Helper()
	state.assertInvariants(t, path)
	*visited = *visited + 1
	if depth >= 9 || state.failed {
		return
	}
	for _, op := range state.availableOps() {
		next := state.clone()
		next.apply(t, op, append(path, op))
		walkLedgerProtocolModel(t, next, append(path, op), depth+1, visited)
	}
}

func (m ledgerProtocolModel) clone() ledgerProtocolModel {
	cloned := m
	cloned.flushes = append([]modelFlush(nil), m.flushes...)
	cloned.permanent = append([]string(nil), m.permanent...)
	cloned.writeSeen = make(map[Sequence]int, len(m.writeSeen))
	for sequence, count := range m.writeSeen {
		cloned.writeSeen[sequence] = count
	}
	if m.inFlight != nil {
		write := *m.inFlight
		cloned.inFlight = &write
	}
	if m.ledger.pending != nil {
		cloned.ledger.pending = make(map[Sequence]ScheduledFlush, len(m.ledger.pending))
		for sequence, flush := range m.ledger.pending {
			cloned.ledger.pending[sequence] = flush
		}
	}
	if m.ledger.discardedInFlightAcks != nil {
		cloned.ledger.discardedInFlightAcks = make(map[Sequence]struct{}, len(m.ledger.discardedInFlightAcks))
		for sequence := range m.ledger.discardedInFlightAcks {
			cloned.ledger.discardedInFlightAcks[sequence] = struct{}{}
		}
	}
	return cloned
}

func (m ledgerProtocolModel) availableOps() []ledgerProtocolOp {
	if m.failed {
		return nil
	}
	ops := make([]ledgerProtocolOp, 0, 7)
	if len(m.flushes) < 3 {
		ops = append(ops, ledgerProtocolEnqueue)
	}
	if m.hasUnacceptedFlush() {
		ops = append(ops, ledgerProtocolAcceptOldest)
		if m.hasDistinctNewestUnacceptedFlush() {
			ops = append(ops, ledgerProtocolAcceptNewest)
		}
	}
	if m.inFlight != nil {
		ops = append(ops, ledgerProtocolAckInFlight, ledgerProtocolFailInFlight, ledgerProtocolWrongInFlightAck)
	}
	if len(m.permanent) > 0 {
		ops = append(ops, ledgerProtocolDuplicateAck)
	}
	if m.inFlight == nil {
		ops = append(ops, ledgerProtocolFutureAck)
	}
	return ops
}

func (m ledgerProtocolModel) hasUnacceptedFlush() bool {
	for _, flush := range m.flushes {
		if !flush.accepted {
			return true
		}
	}
	return false
}

func (m ledgerProtocolModel) hasDistinctNewestUnacceptedFlush() bool {
	first := -1
	last := -1
	for idx, flush := range m.flushes {
		if !flush.accepted {
			if first < 0 {
				first = idx
			}
			last = idx
		}
	}
	return first >= 0 && last >= 0 && first != last
}

func (m *ledgerProtocolModel) apply(t *testing.T, op ledgerProtocolOp, path []ledgerProtocolOp) {
	t.Helper()
	switch op {
	case ledgerProtocolEnqueue:
		m.applyEnqueue(t, path)
	case ledgerProtocolAcceptOldest:
		m.applyAccept(t, path, false)
	case ledgerProtocolAcceptNewest:
		m.applyAccept(t, path, true)
	case ledgerProtocolAckInFlight:
		m.applyAck(t, path)
	case ledgerProtocolFailInFlight:
		m.applyFail(t, path)
	case ledgerProtocolDuplicateAck:
		m.applyDuplicateAck(t, path)
	case ledgerProtocolFutureAck:
		m.applyFutureAck(t, path)
	case ledgerProtocolWrongInFlightAck:
		m.applyWrongInFlightAck(t, path)
	default:
		t.Fatalf("unknown model op %d on path %s", op, formatLedgerProtocolPath(path))
	}
}

func (m *ledgerProtocolModel) applyEnqueue(t *testing.T, path []ledgerProtocolOp) {
	t.Helper()
	payload := fmt.Sprintf("payload-%d", len(m.flushes)+1)
	flush, ok := m.ledger.Enqueue(payload, FlushOptions{})
	if !ok {
		t.Fatalf("enqueue rejected before failure on path %s", formatLedgerProtocolPath(path))
	}
	if int(flush.Sequence) != len(m.flushes)+1 {
		t.Fatalf("sequence = %d, want %d on path %s", flush.Sequence, len(m.flushes)+1, formatLedgerProtocolPath(path))
	}
	m.flushes = append(m.flushes, modelFlush{flush: flush, payload: payload})
}

func (m *ledgerProtocolModel) applyAccept(t *testing.T, path []ledgerProtocolOp, newest bool) {
	t.Helper()
	index := m.unacceptedFlushIndex(newest)
	if index < 0 {
		t.Fatalf("accept without unaccepted flush on path %s", formatLedgerProtocolPath(path))
	}
	write, ready, err := m.ledger.AcceptFlush(m.flushes[index].flush)
	if err != nil {
		t.Fatalf("accept failed on path %s: %v", formatLedgerProtocolPath(path), err)
	}
	m.flushes[index].accepted = true
	if ready {
		m.observeWrite(t, path, write)
	}
}

func (m ledgerProtocolModel) unacceptedFlushIndex(newest bool) int {
	found := -1
	for idx, flush := range m.flushes {
		if flush.accepted {
			continue
		}
		found = idx
		if !newest {
			return found
		}
	}
	return found
}

func (m *ledgerProtocolModel) applyAck(t *testing.T, path []ledgerProtocolOp) {
	t.Helper()
	if m.inFlight == nil {
		t.Fatalf("ack without in-flight write on path %s", formatLedgerProtocolPath(path))
	}
	sequence := m.inFlight.Sequence
	update, err := m.ledger.Ack(TerminalWriteResult{Sequence: sequence})
	if err != nil {
		t.Fatalf("ack failed on path %s: %v", formatLedgerProtocolPath(path), err)
	}
	m.permanent = append(m.permanent, m.payloadForSequence(t, path, sequence))
	m.inFlight = nil
	if update.HasNext {
		m.observeWrite(t, path, update.Next)
	}
}

func (m *ledgerProtocolModel) applyFail(t *testing.T, path []ledgerProtocolOp) {
	t.Helper()
	if m.inFlight == nil {
		t.Fatalf("fail without in-flight write on path %s", formatLedgerProtocolPath(path))
	}
	_, err := m.ledger.Ack(TerminalWriteResult{Sequence: m.inFlight.Sequence, Err: "model write failure"})
	if !errors.Is(err, ErrTerminalWrite) {
		t.Fatalf("write failure err = %v, want ErrTerminalWrite on path %s", err, formatLedgerProtocolPath(path))
	}
	m.failed = true
	m.expectedFailed = true
	m.inFlight = nil
}

func (m *ledgerProtocolModel) applyDuplicateAck(t *testing.T, path []ledgerProtocolOp) {
	t.Helper()
	if len(m.permanent) == 0 {
		t.Fatalf("duplicate ack without permanent output on path %s", formatLedgerProtocolPath(path))
	}
	sequence := Sequence(len(m.permanent))
	if _, err := m.ledger.Ack(TerminalWriteResult{Sequence: sequence}); !errors.Is(err, ErrUnexpectedWriteAck) {
		t.Fatalf("duplicate ack err = %v, want ErrUnexpectedWriteAck on path %s", err, formatLedgerProtocolPath(path))
	}
	m.failed = true
	m.expectedFailed = true
}

func (m *ledgerProtocolModel) applyFutureAck(t *testing.T, path []ledgerProtocolOp) {
	t.Helper()
	sequence := Sequence(len(m.flushes) + 2)
	if _, err := m.ledger.Ack(TerminalWriteResult{Sequence: sequence}); !errors.Is(err, ErrUnexpectedWriteAck) {
		t.Fatalf("future ack err = %v, want ErrUnexpectedWriteAck on path %s", err, formatLedgerProtocolPath(path))
	}
	m.failed = true
	m.expectedFailed = true
}

func (m *ledgerProtocolModel) applyWrongInFlightAck(t *testing.T, path []ledgerProtocolOp) {
	t.Helper()
	if m.inFlight == nil {
		t.Fatalf("wrong ack without in-flight write on path %s", formatLedgerProtocolPath(path))
	}
	sequence := m.inFlight.Sequence + 1
	if _, err := m.ledger.Ack(TerminalWriteResult{Sequence: sequence}); !errors.Is(err, ErrUnexpectedWriteAck) {
		t.Fatalf("wrong in-flight ack err = %v, want ErrUnexpectedWriteAck on path %s", err, formatLedgerProtocolPath(path))
	}
	m.failed = true
	m.expectedFailed = true
}

func (m *ledgerProtocolModel) observeWrite(t *testing.T, path []ledgerProtocolOp, write TerminalWrite) {
	t.Helper()
	if m.inFlight != nil {
		t.Fatalf("second terminal write scheduled while %d is in flight on path %s", m.inFlight.Sequence, formatLedgerProtocolPath(path))
	}
	expectedSequence := Sequence(len(m.permanent) + 1)
	if write.Sequence != expectedSequence {
		t.Fatalf("write sequence = %d, want next permanent frontier %d on path %s", write.Sequence, expectedSequence, formatLedgerProtocolPath(path))
	}
	payload := m.payloadForSequence(t, path, write.Sequence)
	if write.Text != payload {
		t.Fatalf("write text = %q, want %q on path %s", write.Text, payload, formatLedgerProtocolPath(path))
	}
	m.writeSeen[write.Sequence]++
	if m.writeSeen[write.Sequence] > 1 {
		t.Fatalf("write %d scheduled more than once on path %s", write.Sequence, formatLedgerProtocolPath(path))
	}
	writeCopy := write
	m.inFlight = &writeCopy
}

func (m ledgerProtocolModel) payloadForSequence(t *testing.T, path []ledgerProtocolOp, sequence Sequence) string {
	t.Helper()
	index := int(sequence) - 1
	if index < 0 || index >= len(m.flushes) {
		t.Fatalf("unknown sequence %d on path %s", sequence, formatLedgerProtocolPath(path))
	}
	return m.flushes[index].payload
}

func (m ledgerProtocolModel) assertInvariants(t *testing.T, path []ledgerProtocolOp) {
	t.Helper()
	if m.failed {
		if !m.expectedFailed {
			t.Fatalf("model failed unexpectedly on path %s", formatLedgerProtocolPath(path))
		}
		if !m.ledger.Failed() {
			t.Fatalf("model failed but ledger did not on path %s", formatLedgerProtocolPath(path))
		}
	}
	expectedPermanent := make([]string, 0, len(m.permanent))
	for idx := 0; idx < len(m.permanent); idx++ {
		expectedPermanent = append(expectedPermanent, m.flushes[idx].payload)
	}
	if !equalLedgerProtocolPayloads(m.permanent, expectedPermanent) {
		t.Fatalf("permanent output = %v, want acked prefix %v on path %s", m.permanent, expectedPermanent, formatLedgerProtocolPath(path))
	}
	if got, want := m.ledger.AckedSequence(), Sequence(len(m.permanent)); got != want {
		t.Fatalf("acked frontier = %d, want %d on path %s", got, want, formatLedgerProtocolPath(path))
	}
	if !m.failed {
		nextSequence := m.ledger.AckedSequence() + 1
		for _, flush := range m.flushes {
			if !flush.accepted || flush.flush.Sequence != nextSequence {
				continue
			}
			if m.inFlight == nil || m.inFlight.Sequence != nextSequence {
				t.Fatalf("accepted frontier flush %d has no in-flight write on path %s", nextSequence, formatLedgerProtocolPath(path))
			}
		}
	}
	for sequence, count := range m.writeSeen {
		if count > 1 {
			t.Fatalf("write %d scheduled %d times on path %s", sequence, count, formatLedgerProtocolPath(path))
		}
	}
	if m.inFlight != nil {
		if int(m.inFlight.Sequence) <= len(m.permanent) {
			t.Fatalf("in-flight write %d already permanent on path %s", m.inFlight.Sequence, formatLedgerProtocolPath(path))
		}
		if !strings.Contains(m.inFlight.Text, m.payloadForSequence(t, path, m.inFlight.Sequence)) {
			t.Fatalf("in-flight write text = %q on path %s", m.inFlight.Text, formatLedgerProtocolPath(path))
		}
	}
}

func equalLedgerProtocolPayloads(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if left[idx] != right[idx] {
			return false
		}
	}
	return true
}

func formatLedgerProtocolPath(path []ledgerProtocolOp) string {
	if len(path) == 0 {
		return "<start>"
	}
	parts := make([]string, 0, len(path))
	for _, op := range path {
		parts = append(parts, op.String())
	}
	return strings.Join(parts, " -> ")
}
