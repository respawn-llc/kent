package nativescrollback

import (
	"errors"
	"strings"
)

type Sequence uint64

type FlushOptions struct {
	AllowBlank       bool
	ClearBelowBefore bool
}

type ScheduledFlush struct {
	Sequence         Sequence
	Text             string
	AllowBlank       bool
	ClearBelowBefore bool
}

type TerminalWrite struct {
	Sequence   Sequence
	Text       string
	AllowBlank bool
}

type TerminalWriteResult struct {
	Sequence Sequence
	Err      string
}

type AckUpdate struct {
	Sequence Sequence
	Frontier Sequence
	Next     TerminalWrite
	HasNext  bool
}

type Checkpoint struct {
	Sequence Sequence
}

type Ledger struct {
	nextSequence          Sequence
	ackedSequence         Sequence
	inFlight              Sequence
	pending               map[Sequence]ScheduledFlush
	discardedInFlightAcks map[Sequence]struct{}
	projection            projectionLedger
	delivery              committedDeliveryCursor
	assistant             assistantStreamLedger
	failed                bool
}

var (
	ErrLedgerFailed       = errors.New("native scrollback ledger is failed")
	ErrUnexpectedWriteAck = errors.New("native scrollback terminal write ack does not match in-flight write")
	ErrTerminalWrite      = errors.New("native scrollback terminal write failed")
)

func (l *Ledger) Enqueue(text string, opts FlushOptions) (ScheduledFlush, bool) {
	if l == nil || l.failed || text == "" {
		return ScheduledFlush{}, false
	}
	if !opts.AllowBlank && strings.TrimSpace(text) == "" {
		return ScheduledFlush{}, false
	}
	l.nextSequence++
	return ScheduledFlush{
		Sequence:         l.nextSequence,
		Text:             text,
		AllowBlank:       opts.AllowBlank,
		ClearBelowBefore: opts.ClearBelowBefore,
	}, true
}

func (l *Ledger) LastScheduledSequence() Sequence {
	if l == nil {
		return 0
	}
	return l.nextSequence
}

func (l *Ledger) AckedSequence() Sequence {
	if l == nil {
		return 0
	}
	return l.ackedSequence
}

func (l *Ledger) Checkpoint() Checkpoint {
	if l == nil {
		return Checkpoint{}
	}
	return Checkpoint{Sequence: l.nextSequence}
}

func (l *Ledger) CheckpointReached(checkpoint Checkpoint) bool {
	if l == nil {
		return checkpoint.Sequence == 0
	}
	return checkpoint.Sequence <= l.ackedSequence
}

func (l *Ledger) PendingCount() int {
	if l == nil {
		return 0
	}
	count := len(l.pending)
	if l.inFlight != 0 {
		count++
	}
	return count
}

func (l *Ledger) Failed() bool {
	return l != nil && l.failed
}

func (l *Ledger) DiscardScheduled() {
	if l == nil {
		return
	}
	if l.inFlight != 0 {
		if l.discardedInFlightAcks == nil {
			l.discardedInFlightAcks = make(map[Sequence]struct{})
		}
		l.discardedInFlightAcks[l.inFlight] = struct{}{}
	}
	l.cancelCommittedNativeFlush()
	l.ackedSequence = l.nextSequence
	l.inFlight = 0
	clear(l.pending)
}

func (l *Ledger) AcceptFlush(flush ScheduledFlush) (TerminalWrite, bool, error) {
	if l == nil {
		return TerminalWrite{}, false, nil
	}
	if l.failed {
		return TerminalWrite{}, false, ErrLedgerFailed
	}
	if flush.Sequence == 0 || flush.Sequence <= l.ackedSequence {
		return TerminalWrite{}, false, nil
	}
	if flush.Sequence > l.nextSequence {
		l.failed = true
		return TerminalWrite{}, false, ErrUnexpectedWriteAck
	}
	if flush.Sequence == l.inFlight {
		return TerminalWrite{}, false, nil
	}
	if l.pending == nil {
		l.pending = make(map[Sequence]ScheduledFlush)
	}
	if _, exists := l.pending[flush.Sequence]; exists {
		return TerminalWrite{}, false, nil
	}
	l.pending[flush.Sequence] = flush
	return l.nextReadyWrite()
}

func (l *Ledger) Ack(result TerminalWriteResult) (AckUpdate, error) {
	if l == nil || result.Sequence == 0 {
		return AckUpdate{}, nil
	}
	if l.failed {
		return AckUpdate{}, ErrLedgerFailed
	}
	if result.Sequence <= l.ackedSequence {
		if _, discarded := l.discardedInFlightAcks[result.Sequence]; discarded {
			delete(l.discardedInFlightAcks, result.Sequence)
			return AckUpdate{Sequence: result.Sequence, Frontier: l.ackedSequence}, nil
		}
		l.failed = true
		return AckUpdate{}, ErrUnexpectedWriteAck
	}
	if result.Sequence != l.inFlight {
		l.failed = true
		return AckUpdate{}, ErrUnexpectedWriteAck
	}
	if strings.TrimSpace(result.Err) != "" {
		l.failed = true
		return AckUpdate{}, errors.Join(ErrTerminalWrite, errors.New(result.Err))
	}
	l.ackedSequence = result.Sequence
	l.assistant.ackStableFlush(result.Sequence)
	l.inFlight = 0
	next, ok, err := l.nextReadyWrite()
	return AckUpdate{
		Sequence: result.Sequence,
		Frontier: l.ackedSequence,
		Next:     next,
		HasNext:  ok,
	}, err
}

func (l *Ledger) nextReadyWrite() (TerminalWrite, bool, error) {
	if l == nil || l.inFlight != 0 || l.failed {
		return TerminalWrite{}, false, nil
	}
	nextSequence := l.ackedSequence + 1
	flush, ok := l.pending[nextSequence]
	if !ok {
		return TerminalWrite{}, false, nil
	}
	delete(l.pending, nextSequence)
	l.inFlight = nextSequence
	text := flush.Text
	if flush.ClearBelowBefore {
		text = "\x1b[J" + text
	}
	if !flush.AllowBlank && strings.TrimSpace(text) == "" {
		return TerminalWrite{}, false, nil
	}
	return TerminalWrite{Sequence: nextSequence, Text: text, AllowBlank: flush.AllowBlank}, true, nil
}
