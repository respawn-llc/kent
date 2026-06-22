package session

// collectEvents accumulates the full event history via the streaming reader for
// in-package tests that assert against persisted events. Production code must
// not materialize the full event log; see sessiontest.CollectEvents for the
// cross-package equivalent.
func collectEvents(s *Store) ([]Event, error) {
	events := make([]Event, 0)
	if err := s.WalkEvents(func(evt Event) error {
		events = append(events, evt)
		return nil
	}); err != nil {
		return nil, err
	}
	return events, nil
}
