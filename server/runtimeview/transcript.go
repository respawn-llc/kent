package runtimeview

import (
	"core/server/runtime"
	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/textutil"
)

const RecentTailEntryLimit = 500

func TranscriptPageFromRuntime(engine *runtime.Engine, req *transcriptpb.PageRequest) (*transcriptpb.Page, error) {
	if engine == nil {
		return nil, nil
	}
	var segment runtime.TranscriptSegmentPage
	var err error
	switch direction := req.Direction.(type) {
	case *transcriptpb.PageRequest_NewerCursor:
		segment, err = engine.TranscriptSegmentPageForward(direction.NewerCursor)
	case *transcriptpb.PageRequest_Cursor:
		segment, err = engine.TranscriptSegmentPage(direction.Cursor)
	default:
		segment, err = engine.TranscriptNewestSegmentPage()
	}
	if err != nil {
		return nil, err
	}
	freshness, err := engine.ConversationFreshness()
	if err != nil {
		return nil, err
	}
	return TranscriptPageFromSegment(
		engine.SessionID(),
		engine.SessionName(),
		ConversationFreshnessFromSession(freshness),
		segment,
	)
}

func TranscriptPageFromSegment(sessionID, sessionName string, freshness runtimepb.ConversationFreshness, page runtime.TranscriptSegmentPage) (*transcriptpb.Page, error) {
	segment, err := TranscriptTailSegmentFromSegment(page)
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
		SessionName:             textutil.OptionalExactString(sessionName),
		ConversationFreshness:   freshness,
		OlderCursor:             segment.OlderCursor,
		HasMoreAbove:            segment.HasMoreAbove,
		NewerCursor:             transcriptCursor(page.HasMoreBelow, page.NewerCursor),
		HasMoreBelow:            page.HasMoreBelow,
		LatestRollbackCandidate: candidate,
		Entries:                 segment.Entries,
	}, nil
}

func TranscriptTailSegmentFromSegment(page runtime.TranscriptSegmentPage) (*transcriptpb.TailSegment, error) {
	return transcriptTailSegmentFromFactsChecked(
		runtime.TranscriptCommittedRowFactsFromSnapshot(page.Snapshot),
		transcriptCursor(page.HasMoreAbove, page.OlderCursor),
		page.HasMoreAbove,
	)
}

func transcriptTailSegmentFromFactsChecked(
	facts []runtime.TranscriptCommittedRowFact,
	olderCursor *int64,
	hasMoreAbove bool,
) (*transcriptpb.TailSegment, error) {
	entries, err := transcriptRowsFromFactsChecked(facts)
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

func transcriptCursor(hasMore bool, cursor int64) *int64 {
	if !hasMore {
		return nil
	}
	return &cursor
}
