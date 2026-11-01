package session

import (
	"errors"
	"fmt"

	"core/shared/invariant"
)

type currentEventLogReconciliationObservation struct {
	lastSequence             int64
	conversationEstablished  bool
	latestCompactionSequence *int64
}

func (s *Store) reconcileCurrentEventLog() (resultErr error) {
	if s == nil {
		return errors.New("session store is required")
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	s.mu.Lock()
	sessionDir := s.sessionDir
	s.mu.Unlock()
	lock, lockPath, err := acquireEventLogPersistenceLock(sessionDir)
	if err != nil {
		return err
	}
	defer joinEventLogPersistenceLockRelease(&resultErr, lock, lockPath)
	return s.reconcileCurrentEventLogWithStableLockHeld()
}

func (s *Store) reconcileCurrentEventLogWithStableLockHeld() error {
	s.mu.Lock()
	if s.eventLogMaterialization == nil || s.eventLogMaterialization.state != eventLogCurrentReconciliationPending {
		s.mu.Unlock()
		return errors.New("event-log validation requires pending materialization")
	}
	path := s.eventsFP
	sequence := s.meta.LastSequence
	s.mu.Unlock()
	if _, err := openCommittedEventLog(path, currentEventLogReadOnly, sequence); err != nil {
		return s.wrapCurrentEventLogReconciliationError(err)
	}
	s.mu.Lock()
	s.eventLogMaterialization.state = eventLogCurrent
	s.mu.Unlock()
	return nil
}

// The canonical v0 file remains intact until its deterministic sequence
// conversion is committed. If publication fails, its format still identifies
// the work that an explicit subsequent materialization needs to perform.
func (s *Store) commitLegacyEventLogConversion(log *currentEventLog) error {
	current, err := s.currentEventLogReconciliationObservation(log)
	if err != nil {
		return s.wrapCurrentEventLogReconciliationError(err)
	}
	s.mu.Lock()
	sequence := s.meta.LastSequence
	usageState := UsageStateReconciliationPreserve
	if s.meta.UsageState != nil && current.latestCompactionSequence != nil && *current.latestCompactionSequence > sequence {
		usageState = UsageStateReconciliationInvalidate
	}
	observation := &eventLogReconciliationObservation{
		reconciliation: PersistedEventLogReconciliation{
			SessionID:               s.meta.SessionID,
			ObservedLastSequence:    sequence,
			LastSequence:            current.lastSequence,
			ConversationEstablished: current.conversationEstablished,
			UpdatedAt:               s.options.now(),
			UsageState:              usageState,
		},
		version: s.metadataVersion + 1,
	}
	s.mu.Unlock()
	if err := s.observeEventLogReconciliation(observation); err != nil {
		return s.wrapCurrentEventLogReconciliationError(err)
	}
	s.mu.Lock()
	s.meta.LastSequence = current.lastSequence
	s.meta.ConversationEstablished = current.conversationEstablished
	s.meta.UpdatedAt = observation.reconciliation.UpdatedAt
	if usageState == UsageStateReconciliationInvalidate {
		s.meta.UsageState = nil
	}
	s.conversationFreshness = ConversationFreshnessFresh
	if current.conversationEstablished {
		s.conversationFreshness = ConversationFreshnessEstablished
	}
	s.metadataVersion = observation.version
	s.persistedMetaVersion = observation.version
	s.mu.Unlock()
	return nil
}

func (s *Store) currentEventLogReconciliationObservation(log *currentEventLog) (currentEventLogReconciliationObservation, error) {
	if log == nil {
		err := errors.New("event-log conversion invariant violated: current log is missing")
		invariant.NewPolicy().Check(false, invariant.FailureDiagnostic(invariant.ScopeSessionPersistence, "observe_current_event_log", err))
		return currentEventLogReconciliationObservation{}, err
	}
	window, err := log.readActiveSegment()
	if err != nil {
		return currentEventLogReconciliationObservation{}, err
	}
	current := currentEventLogReconciliationObservation{lastSequence: log.lastSequence}
	for _, record := range window.Records {
		kind, err := record.Kind()
		if err != nil {
			return currentEventLogReconciliationObservation{}, err
		}
		if IsContextBoundary(kind) {
			sequence := record.Seq()
			current.latestCompactionSequence = &sequence
			current.conversationEstablished = true
		}
		visible, err := hasVisibleUserMessageRecord(record)
		if err != nil {
			return currentEventLogReconciliationObservation{}, err
		}
		current.conversationEstablished = current.conversationEstablished || visible
	}
	return current, nil
}

func (s *Store) wrapCurrentEventLogReconciliationError(err error) error {
	return wrapEventLogMaterializationError(EventLogMaterializationStageReconciliation, true, true, fmt.Errorf("validate Session event log: %w", err))
}
