package runtime

import (
	"fmt"

	"core/server/sessionstore"
	"core/shared/transcriptmodel"
)

type TranscriptCommittedRowProvenance struct {
	QueueItemIDs      []string
	EventSequence     int64
	CommittedAtUnixMs *transcript.CommittedAtUnixMs
	ProjectedOrdinal  *int64
}

func transcriptCommittedProvenanceBefore(
	left *TranscriptCommittedRowProvenance,
	right *TranscriptCommittedRowProvenance,
) bool {
	if left == nil || right == nil {
		return false
	}
	if left.EventSequence != right.EventSequence {
		return left.EventSequence < right.EventSequence
	}
	if left.ProjectedOrdinal != nil && right.ProjectedOrdinal != nil {
		return *left.ProjectedOrdinal < *right.ProjectedOrdinal
	}
	return false
}

func transcriptProvenanceFromRecord(record session.EventRecord) (TranscriptCommittedRowProvenance, error) {
	if record.Seq() <= 0 {
		return TranscriptCommittedRowProvenance{}, fmt.Errorf("committed transcript provenance has invalid event sequence %d", record.Seq())
	}
	payload, err := record.Payload()
	if err != nil {
		return TranscriptCommittedRowProvenance{}, err
	}
	provenance := TranscriptCommittedRowProvenance{
		EventSequence:     record.Seq(),
		CommittedAtUnixMs: record.CommittedAtUnixMs(),
	}
	if message, ok := payload.(session.MessageRecord); ok {
		provenance.QueueItemIDs = append([]string(nil), message.QueueItemIDs...)
	}
	return provenance, nil
}

func cloneTranscriptCommittedRowProvenance(value *TranscriptCommittedRowProvenance) *TranscriptCommittedRowProvenance {
	if value == nil {
		return nil
	}
	copyValue := *value
	copyValue.QueueItemIDs = append([]string(nil), value.QueueItemIDs...)
	if value.CommittedAtUnixMs != nil {
		committedAt := *value.CommittedAtUnixMs
		copyValue.CommittedAtUnixMs = &committedAt
	}
	if value.ProjectedOrdinal != nil {
		ordinal := *value.ProjectedOrdinal
		copyValue.ProjectedOrdinal = &ordinal
	}
	return &copyValue
}
