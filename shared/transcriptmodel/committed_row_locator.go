package transcript

// CommittedRowLocator identifies one committed transcript row within a Session.
// EventSequence is the durable event-record sequence and RowOrdinal is the
// one-based projection ordinal within that event.
type CommittedRowLocator struct {
	EventSequence int64 `json:"event_sequence"`
	RowOrdinal    int64 `json:"row_ordinal"`
}
