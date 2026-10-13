package runtimeview

import (
	"core/server/runtime"
	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
	"core/shared/textutil"
)

const RecentTailEntryLimit = 500

func TranscriptPageFromSegment(sessionID string, sessionName *string, freshness runtimepb.ConversationFreshness, page runtime.TranscriptSegmentPage, identities map[runtimeids.SessionID]runtimeids.SessionIdentity) (*transcriptpb.Page, error) {
	segment, err := TranscriptTailSegmentFromSegment(page, identities)
	if err != nil {
		return nil, err
	}
	var candidate *transcriptpb.RollbackCandidate
	if page.LatestRollbackCandidate != nil {
		candidate = &transcriptpb.RollbackCandidate{
			UserMessageSeq:       page.LatestRollbackCandidate.UserMessageSeq,
			CandidatePageEndByte: page.LatestRollbackCandidate.CandidatePageEndByte,
		}
	}
	return &transcriptpb.Page{
		SessionId:               sessionID,
		SessionName:             textutil.Pointer(sessionName),
		ConversationFreshness:   freshness,
		OlderCursor:             segment.OlderCursor,
		HasMoreAbove:            segment.HasMoreAbove,
		NewerCursor:             transcriptCursor(page.HasMoreBelow, page.NewerCursor),
		HasMoreBelow:            page.HasMoreBelow,
		LatestRollbackCandidate: candidate,
		Entries:                 segment.Entries,
	}, nil
}

func TranscriptTailSegmentFromSegment(page runtime.TranscriptSegmentPage, identities map[runtimeids.SessionID]runtimeids.SessionIdentity) (*transcriptpb.TailSegment, error) {
	return transcriptTailSegmentFromFactsChecked(
		runtime.TranscriptCommittedRowFactsFromSnapshot(page.Snapshot),
		transcriptCursor(page.HasMoreAbove, page.OlderCursor),
		page.HasMoreAbove,
		identities,
	)
}

func transcriptTailSegmentFromFactsChecked(
	facts []runtime.TranscriptCommittedRowFact,
	olderCursor *int64,
	hasMoreAbove bool,
	identities map[runtimeids.SessionID]runtimeids.SessionIdentity,
) (*transcriptpb.TailSegment, error) {
	entries, err := transcriptRowsFromFactsChecked(facts, identities)
	if err != nil {
		return nil, err
	}
	segment := &transcriptpb.TailSegment{
		OlderCursor:  olderCursor,
		HasMoreAbove: hasMoreAbove,
		Entries:      entries,
	}
	if err := protoapi.Validate(segment); err != nil {
		return nil, err
	}
	return segment, nil
}

func TranscriptReferencedSessions(facts []runtime.TranscriptCommittedRowFact) []runtimeids.SessionID {
	var ids []runtimeids.SessionID
	for _, fact := range facts {
		if fact.Tool != nil && fact.Tool.AnsweredBySessionID != nil {
			ids = append(ids, *fact.Tool.AnsweredBySessionID)
		}
	}
	return ids
}

func transcriptCursor(hasMore bool, cursor int64) *int64 {
	if !hasMore {
		return nil
	}
	return &cursor
}
