package app

import (
	"errors"

	"core/shared/clientui"
)

type ongoingCommittedRange struct {
	startEntryCount int
	endEntryCount   int
	revision        int64
}

type ongoingCommittedDeliveryCursor struct {
	initialized                    bool
	lastEmittedCommittedEntryCount int
	lastAppliedCommittedEntryCount int
	lastEmittedTranscriptRevision  int64
	nativeFlushInFlight            bool
	emissionEnabledByMode          bool
	pendingCommittedRanges         []ongoingCommittedRange

	flushSequence       uint64
	flushNextEntryCount int
	flushRevision       int64
}

func newOngoingCommittedDeliveryCursor(lastCommittedEntryCount int, revision int64) ongoingCommittedDeliveryCursor {
	if lastCommittedEntryCount < 0 {
		lastCommittedEntryCount = 0
	}
	return ongoingCommittedDeliveryCursor{
		initialized:                    true,
		lastEmittedCommittedEntryCount: lastCommittedEntryCount,
		lastAppliedCommittedEntryCount: lastCommittedEntryCount,
		lastEmittedTranscriptRevision:  revision,
		emissionEnabledByMode:          true,
	}
}

func (c *ongoingCommittedDeliveryCursor) setEmissionEnabled(enabled bool) {
	if c == nil {
		return
	}
	c.emissionEnabledByMode = enabled
}

func (c *ongoingCommittedDeliveryCursor) recordCommittedAdvance(committedEntryCount int, revision int64) {
	if c == nil || committedEntryCount <= c.lastEmittedCommittedEntryCount {
		return
	}
	c.markApplied(committedEntryCount, revision)
	if c.emissionEnabledByMode && !c.nativeFlushInFlight {
		return
	}
	c.recordPendingRange(c.lastEmittedCommittedEntryCount, committedEntryCount, revision)
}

func (c *ongoingCommittedDeliveryCursor) markApplied(committedEntryCount int, _ int64) {
	if c == nil {
		return
	}
	if committedEntryCount > c.lastAppliedCommittedEntryCount {
		c.lastAppliedCommittedEntryCount = committedEntryCount
	}
}

func (c *ongoingCommittedDeliveryCursor) beginNativeFlush(suffix clientui.CommittedTranscriptSuffix, sequence uint64) error {
	if c == nil {
		return errors.New("ongoing delivery cursor is required")
	}
	if suffix.StartEntryCount != c.lastEmittedCommittedEntryCount {
		if !c.nativeFlushInFlight || suffix.StartEntryCount < c.lastEmittedCommittedEntryCount || suffix.StartEntryCount > c.flushNextEntryCount {
			return errors.New("suffix start does not match delivery cursor")
		}
	}
	if suffix.NextEntryCount < suffix.StartEntryCount {
		return errors.New("suffix next cursor is before suffix start")
	}
	if suffix.NextEntryCount == suffix.StartEntryCount {
		return nil
	}
	c.markApplied(suffix.NextEntryCount, suffix.Revision)
	if c.nativeFlushInFlight {
		if suffix.NextEntryCount <= c.flushNextEntryCount {
			return nil
		}
		c.flushSequence = sequence
		c.flushNextEntryCount = suffix.NextEntryCount
		c.flushRevision = max(c.flushRevision, suffix.Revision)
		return nil
	}
	c.nativeFlushInFlight = true
	c.flushSequence = sequence
	c.flushNextEntryCount = suffix.NextEntryCount
	c.flushRevision = suffix.Revision
	return nil
}

func (c *ongoingCommittedDeliveryCursor) ackNativeFlush(sequence uint64) bool {
	if c == nil || !c.nativeFlushInFlight || sequence != c.flushSequence {
		return false
	}
	c.lastEmittedCommittedEntryCount = c.flushNextEntryCount
	c.lastEmittedTranscriptRevision = c.flushRevision
	if c.lastAppliedCommittedEntryCount < c.lastEmittedCommittedEntryCount {
		c.lastAppliedCommittedEntryCount = c.lastEmittedCommittedEntryCount
	}
	c.nativeFlushInFlight = false
	c.flushSequence = 0
	c.flushNextEntryCount = 0
	c.flushRevision = 0
	c.discardPendingThrough(c.lastEmittedCommittedEntryCount)
	return true
}

func (c *ongoingCommittedDeliveryCursor) failNativeFlush(sequence uint64) {
	if c == nil || !c.nativeFlushInFlight || sequence != c.flushSequence {
		return
	}
	failedNextEntryCount := c.flushNextEntryCount
	failedRevision := c.flushRevision
	c.nativeFlushInFlight = false
	c.flushSequence = 0
	c.flushNextEntryCount = 0
	c.flushRevision = 0
	if failedNextEntryCount > c.lastEmittedCommittedEntryCount {
		c.recordPendingRange(c.lastEmittedCommittedEntryCount, failedNextEntryCount, failedRevision)
	}
}

func (c *ongoingCommittedDeliveryCursor) nextSuffixRequest() (clientui.CommittedTranscriptSuffixRequest, bool) {
	if c == nil || c.nativeFlushInFlight {
		return clientui.CommittedTranscriptSuffixRequest{}, false
	}
	if len(c.pendingCommittedRanges) == 0 {
		return clientui.CommittedTranscriptSuffixRequest{
			AfterEntryCount: c.lastEmittedCommittedEntryCount,
			Limit:           clientui.DefaultCommittedTranscriptSuffixLimit,
		}, true
	}
	next := c.pendingCommittedRanges[0]
	limit := next.endEntryCount - c.lastEmittedCommittedEntryCount
	if limit <= 0 {
		limit = clientui.DefaultCommittedTranscriptSuffixLimit
	}
	return clientui.CommittedTranscriptSuffixRequest{
		AfterEntryCount: c.lastEmittedCommittedEntryCount,
		Limit:           limit,
	}, true
}

func (c *ongoingCommittedDeliveryCursor) recordPendingRange(startEntryCount int, endEntryCount int, revision int64) {
	if c == nil || endEntryCount <= startEntryCount {
		return
	}
	if len(c.pendingCommittedRanges) == 0 {
		c.pendingCommittedRanges = append(c.pendingCommittedRanges, ongoingCommittedRange{startEntryCount: startEntryCount, endEntryCount: endEntryCount, revision: revision})
		return
	}
	last := &c.pendingCommittedRanges[len(c.pendingCommittedRanges)-1]
	if startEntryCount <= last.endEntryCount {
		if endEntryCount > last.endEntryCount {
			last.endEntryCount = endEntryCount
		}
		if revision > last.revision {
			last.revision = revision
		}
		return
	}
	c.pendingCommittedRanges = append(c.pendingCommittedRanges, ongoingCommittedRange{startEntryCount: startEntryCount, endEntryCount: endEntryCount, revision: revision})
}

func (c *ongoingCommittedDeliveryCursor) discardPendingThrough(entryCount int) {
	if c == nil || len(c.pendingCommittedRanges) == 0 {
		return
	}
	ranges := c.pendingCommittedRanges[:0]
	for _, pending := range c.pendingCommittedRanges {
		if pending.endEntryCount <= entryCount {
			continue
		}
		if pending.startEntryCount < entryCount {
			pending.startEntryCount = entryCount
		}
		ranges = append(ranges, pending)
	}
	c.pendingCommittedRanges = ranges
}
