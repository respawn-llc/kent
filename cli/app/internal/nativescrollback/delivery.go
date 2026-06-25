package nativescrollback

import (
	"errors"

	"core/shared/clientui"
)

type CommittedDeliveryRange struct {
	StartEntryCount int
	EndEntryCount   int
	Revision        int64
}

type CommittedDeliveryState struct {
	Initialized                    bool
	LastEmittedCommittedEntryCount int
	LastAppliedCommittedEntryCount int
	LastEmittedTranscriptRevision  int64
	NativeFlushInFlight            bool
	EmissionEnabledByMode          bool
	PendingCommittedRanges         []CommittedDeliveryRange
	FlushSequence                  Sequence
	FlushNextEntryCount            int
	FlushRevision                  int64
}

type committedDeliveryCursor struct {
	initialized                    bool
	lastEmittedCommittedEntryCount int
	lastAppliedCommittedEntryCount int
	lastEmittedTranscriptRevision  int64
	nativeFlushInFlight            bool
	emissionEnabledByMode          bool
	pendingCommittedRanges         []CommittedDeliveryRange

	flushSequence       Sequence
	flushNextEntryCount int
	flushRevision       int64
}

func newCommittedDeliveryCursor(lastCommittedEntryCount int, revision int64) committedDeliveryCursor {
	if lastCommittedEntryCount < 0 {
		lastCommittedEntryCount = 0
	}
	return committedDeliveryCursor{
		initialized:                    true,
		lastEmittedCommittedEntryCount: lastCommittedEntryCount,
		lastAppliedCommittedEntryCount: lastCommittedEntryCount,
		lastEmittedTranscriptRevision:  revision,
		emissionEnabledByMode:          true,
	}
}

func (l *Ledger) CommittedDeliveryState() CommittedDeliveryState {
	if l == nil {
		return CommittedDeliveryState{}
	}
	cursor := l.delivery
	return CommittedDeliveryState{
		Initialized:                    cursor.initialized,
		LastEmittedCommittedEntryCount: cursor.lastEmittedCommittedEntryCount,
		LastAppliedCommittedEntryCount: cursor.lastAppliedCommittedEntryCount,
		LastEmittedTranscriptRevision:  cursor.lastEmittedTranscriptRevision,
		NativeFlushInFlight:            cursor.nativeFlushInFlight,
		EmissionEnabledByMode:          cursor.emissionEnabledByMode,
		PendingCommittedRanges:         append([]CommittedDeliveryRange(nil), cursor.pendingCommittedRanges...),
		FlushSequence:                  cursor.flushSequence,
		FlushNextEntryCount:            cursor.flushNextEntryCount,
		FlushRevision:                  cursor.flushRevision,
	}
}

func (l *Ledger) CommittedDeliveryInitialized() bool {
	return l != nil && l.delivery.initialized
}

func (l *Ledger) EnsureCommittedDelivery(lastCommittedEntryCount int, revision int64) {
	if l == nil || l.delivery.initialized {
		return
	}
	l.delivery = newCommittedDeliveryCursor(lastCommittedEntryCount, revision)
}

func (l *Ledger) ResetCommittedDelivery(lastCommittedEntryCount int, revision int64) {
	if l == nil {
		return
	}
	l.delivery = newCommittedDeliveryCursor(lastCommittedEntryCount, revision)
}

func (l *Ledger) ResetCommittedDeliveryAppliedRange(lastEmittedEntryCount int, lastAppliedEntryCount int, revision int64) {
	if l == nil {
		return
	}
	if lastEmittedEntryCount < 0 {
		lastEmittedEntryCount = 0
	}
	if lastAppliedEntryCount < lastEmittedEntryCount {
		lastAppliedEntryCount = lastEmittedEntryCount
	}
	l.delivery = newCommittedDeliveryCursor(lastEmittedEntryCount, revision)
	l.delivery.lastAppliedCommittedEntryCount = lastAppliedEntryCount
}

func (l *Ledger) SetCommittedDeliveryEmissionEnabled(enabled bool) {
	if l == nil {
		return
	}
	l.delivery.emissionEnabledByMode = enabled
}

func (l *Ledger) MarkCommittedApplied(committedEntryCount int, _ int64) {
	if l == nil {
		return
	}
	if committedEntryCount > l.delivery.lastAppliedCommittedEntryCount {
		l.delivery.lastAppliedCommittedEntryCount = committedEntryCount
	}
}

func (l *Ledger) MarkCommittedDelivered(committedEntryCount int, revision int64) {
	if l == nil {
		return
	}
	l.EnsureCommittedDelivery(committedEntryCount, revision)
	l.MarkCommittedApplied(committedEntryCount, revision)
	if committedEntryCount > l.delivery.lastEmittedCommittedEntryCount {
		l.delivery.lastEmittedCommittedEntryCount = committedEntryCount
	}
	if revision > l.delivery.lastEmittedTranscriptRevision {
		l.delivery.lastEmittedTranscriptRevision = revision
	}
}

func (l *Ledger) RecordCommittedAdvance(committedEntryCount int, revision int64) {
	if l == nil || committedEntryCount <= l.delivery.lastEmittedCommittedEntryCount {
		return
	}
	l.MarkCommittedApplied(committedEntryCount, revision)
	if l.delivery.emissionEnabledByMode && !l.delivery.nativeFlushInFlight {
		return
	}
	l.RecordPendingCommittedRange(l.delivery.lastEmittedCommittedEntryCount, committedEntryCount, revision)
}

func (l *Ledger) BeginCommittedNativeFlush(suffix clientui.CommittedTranscriptSuffix, sequence Sequence) error {
	if l == nil || !l.delivery.initialized {
		return errors.New("ongoing delivery cursor is required")
	}
	if suffix.StartEntryCount != l.delivery.lastEmittedCommittedEntryCount {
		if !l.delivery.nativeFlushInFlight || suffix.StartEntryCount < l.delivery.lastEmittedCommittedEntryCount || suffix.StartEntryCount > l.delivery.flushNextEntryCount {
			return errors.New("suffix start does not match delivery cursor")
		}
	}
	if suffix.NextEntryCount < suffix.StartEntryCount {
		return errors.New("suffix next cursor is before suffix start")
	}
	if suffix.NextEntryCount == suffix.StartEntryCount {
		return nil
	}
	l.MarkCommittedApplied(suffix.NextEntryCount, suffix.Revision)
	if l.delivery.nativeFlushInFlight {
		if suffix.NextEntryCount <= l.delivery.flushNextEntryCount {
			return nil
		}
		l.delivery.flushSequence = sequence
		l.delivery.flushNextEntryCount = suffix.NextEntryCount
		l.delivery.flushRevision = max(l.delivery.flushRevision, suffix.Revision)
		return nil
	}
	l.delivery.nativeFlushInFlight = true
	l.delivery.flushSequence = sequence
	l.delivery.flushNextEntryCount = suffix.NextEntryCount
	l.delivery.flushRevision = suffix.Revision
	return nil
}

func (l *Ledger) AckCommittedNativeFlush(sequence Sequence) bool {
	if l == nil || !l.delivery.nativeFlushInFlight || sequence != l.delivery.flushSequence {
		return false
	}
	l.delivery.lastEmittedCommittedEntryCount = l.delivery.flushNextEntryCount
	l.delivery.lastEmittedTranscriptRevision = l.delivery.flushRevision
	if l.delivery.lastAppliedCommittedEntryCount < l.delivery.lastEmittedCommittedEntryCount {
		l.delivery.lastAppliedCommittedEntryCount = l.delivery.lastEmittedCommittedEntryCount
	}
	l.delivery.nativeFlushInFlight = false
	l.delivery.flushSequence = 0
	l.delivery.flushNextEntryCount = 0
	l.delivery.flushRevision = 0
	l.discardPendingCommittedThrough(l.delivery.lastEmittedCommittedEntryCount)
	return true
}

func (l *Ledger) FailCommittedNativeFlush(sequence Sequence) {
	if l == nil || !l.delivery.nativeFlushInFlight || sequence != l.delivery.flushSequence {
		return
	}
	failedNextEntryCount := l.delivery.flushNextEntryCount
	failedRevision := l.delivery.flushRevision
	l.delivery.nativeFlushInFlight = false
	l.delivery.flushSequence = 0
	l.delivery.flushNextEntryCount = 0
	l.delivery.flushRevision = 0
	if failedNextEntryCount > l.delivery.lastEmittedCommittedEntryCount {
		l.RecordPendingCommittedRange(l.delivery.lastEmittedCommittedEntryCount, failedNextEntryCount, failedRevision)
	}
}

func (l *Ledger) CommittedDeliveryNextSuffixRequest() (clientui.CommittedTranscriptSuffixRequest, bool) {
	if l == nil || l.delivery.nativeFlushInFlight {
		return clientui.CommittedTranscriptSuffixRequest{}, false
	}
	return clientui.CommittedTranscriptSuffixRequest{}, true
}

func (l *Ledger) RecordPendingCommittedRange(startEntryCount int, endEntryCount int, revision int64) {
	if l == nil || endEntryCount <= startEntryCount {
		return
	}
	if len(l.delivery.pendingCommittedRanges) == 0 {
		l.delivery.pendingCommittedRanges = append(l.delivery.pendingCommittedRanges, CommittedDeliveryRange{StartEntryCount: startEntryCount, EndEntryCount: endEntryCount, Revision: revision})
		return
	}
	last := &l.delivery.pendingCommittedRanges[len(l.delivery.pendingCommittedRanges)-1]
	if startEntryCount <= last.EndEntryCount {
		if endEntryCount > last.EndEntryCount {
			last.EndEntryCount = endEntryCount
		}
		if revision > last.Revision {
			last.Revision = revision
		}
		return
	}
	l.delivery.pendingCommittedRanges = append(l.delivery.pendingCommittedRanges, CommittedDeliveryRange{StartEntryCount: startEntryCount, EndEntryCount: endEntryCount, Revision: revision})
}

func (l *Ledger) discardPendingCommittedThrough(entryCount int) {
	if l == nil || len(l.delivery.pendingCommittedRanges) == 0 {
		return
	}
	ranges := l.delivery.pendingCommittedRanges[:0]
	for _, pending := range l.delivery.pendingCommittedRanges {
		if pending.EndEntryCount <= entryCount {
			continue
		}
		if pending.StartEntryCount < entryCount {
			pending.StartEntryCount = entryCount
		}
		ranges = append(ranges, pending)
	}
	l.delivery.pendingCommittedRanges = ranges
}
