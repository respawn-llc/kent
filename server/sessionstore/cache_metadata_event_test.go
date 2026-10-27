package session

import (
	"strings"
	"testing"
)

func TestCacheMetadataObservationValidationAndRoundTrip(t *testing.T) {
	t.Parallel()
	for _, hash := range []*string{nil, stringPointer(strings.Repeat("a", 64)), stringPointer(""), stringPointer("not-a-digest"), stringPointer(strings.Repeat("a", 62))} {
		request := CacheRequestObservationRecord{
			DigestVersion: CacheDigestV1,
			CacheKey:      "cache-key",
			Scope:         CacheScopeConversation,
			ChunkCount:    2,
			TerminalHash:  strings.Repeat("b", 64),
			MetadataHash:  hash,
		}
		for _, payload := range []EventRecordPayload{request, CacheResponseObservationRecord{
			DigestVersion: &request.DigestVersion,
			CacheKey:      &request.CacheKey,
			Scope:         &request.Scope,
			ChunkCount:    &request.ChunkCount,
			TerminalHash:  &request.TerminalHash,
			MetadataHash:  hash,
		}} {
			record, err := NewEventRecord(1, nil, payload)
			valid := hash == nil || len(*hash) == 64
			if !valid {
				if err == nil {
					t.Fatalf("accepted invalid metadata digest for %T", payload)
				}
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			line, err := encodeEventRecordV1(record)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := decodeEventRecordV1(line)
			if err != nil {
				t.Fatal(err)
			}
			var restored *string
			switch observation := mustEventRecordPayload(decoded).(type) {
			case CacheRequestObservationRecord:
				restored = observation.MetadataHash
			case CacheResponseObservationRecord:
				restored = observation.MetadataHash
			default:
				t.Fatalf("unexpected restored payload %T", observation)
			}
			if hash == nil {
				if restored != nil {
					t.Fatal("absent metadata evidence became present")
				}
			} else if restored == nil || *restored != *hash {
				t.Fatal("metadata evidence did not survive round trip")
			}
		}
	}
}

func TestCacheResponseMetadataRequiresCompleteCacheFacts(t *testing.T) {
	t.Parallel()
	if _, err := NewEventRecord(1, nil, CacheResponseObservationRecord{
		MetadataHash: stringPointer(strings.Repeat("a", 64)),
	}); err == nil {
		t.Fatal("accepted metadata evidence without cache response facts")
	}
}

func TestHistoryReplacementCacheMetadataValidationAndRoundTrip(t *testing.T) {
	t.Parallel()
	conversation := CacheMetadataObservation{
		DigestVersion: CacheDigestV1, CacheKey: "conversation",
		Scope: CacheScopeConversation, MetadataHash: strings.Repeat("a", 64),
	}
	reviewer := conversation
	reviewer.CacheKey = "reviewer"
	reviewer.Scope = CacheScopeReviewer
	invalidDigest := conversation
	invalidDigest.MetadataHash = ""
	for _, tt := range []struct {
		name         string
		observations []CacheMetadataObservation
		valid        bool
	}{
		{name: "absent evidence", valid: true},
		{name: "conversation", observations: []CacheMetadataObservation{conversation}, valid: true},
		{name: "both scopes", observations: []CacheMetadataObservation{conversation, reviewer}, valid: true},
		{name: "duplicate scope", observations: []CacheMetadataObservation{conversation, conversation}},
		{name: "unbounded scopes", observations: []CacheMetadataObservation{conversation, reviewer, conversation}},
		{name: "invalid digest", observations: []CacheMetadataObservation{invalidDigest}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			record, err := NewEventRecord(1, nil, HistoryReplacementRecord{
				Engine: "local", Mode: CompactionModeManual, CacheMetadata: tt.observations,
			})
			if !tt.valid {
				if err == nil {
					t.Fatal("accepted invalid boundary cache evidence")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			line, err := encodeEventRecordV1(record)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := decodeEventRecordV1(line)
			if err != nil {
				t.Fatal(err)
			}
			restored := mustEventRecordPayload(decoded).(HistoryReplacementRecord).CacheMetadata
			if len(restored) != len(tt.observations) {
				t.Fatal("boundary evidence count changed")
			}
			for index, observation := range restored {
				if observation != tt.observations[index] {
					t.Fatal("boundary evidence changed")
				}
			}
		})
	}
}
