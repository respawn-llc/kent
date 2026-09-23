package runtime

import (
	"fmt"

	"core/server/session"
	"core/shared/transcript"
)

type TranscriptCommittedRowProvenance struct {
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
	if _, err := record.Payload(); err != nil {
		return TranscriptCommittedRowProvenance{}, err
	}
	return TranscriptCommittedRowProvenance{
		EventSequence:     record.Seq(),
		CommittedAtUnixMs: record.CommittedAtUnixMs(),
	}, nil
}

func cloneTranscriptCommittedRowProvenance(value *TranscriptCommittedRowProvenance) *TranscriptCommittedRowProvenance {
	if value == nil {
		return nil
	}
	copyValue := *value
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
