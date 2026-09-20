package sessionview

import (
	"context"
	"errors"
	"strings"

	"core/server/goalview"
	"core/server/runtime"
	"core/server/runtimeview"
	"core/server/session"
	"core/shared/config"
	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/textutil"
)

type runtimeMainViewSnapshotProvider interface {
	RuntimeMainViewSnapshot(sessionID string) (*runtimepb.MainView, bool)
}

func readWithContext[T any](ctx context.Context, read func() (T, error)) (T, error) {
	var zero T
	if err := context.Cause(ctx); err != nil {
		return zero, err
	}
	value, err := read()
	if err != nil {
		return zero, err
	}
	if err := context.Cause(ctx); err != nil {
		return zero, err
	}
	return value, nil
}

func resultWithContext[T any](ctx context.Context, value T) (T, error) {
	if err := context.Cause(ctx); err != nil {
		var zero T
		return zero, err
	}
	return value, nil
}

func (s *Service) resolveMainView(ctx context.Context, sessionID string) (*runtimepb.MainView, error) {
	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	if s.mainViews != nil {
		if view, ok := s.mainViews.RuntimeMainViewSnapshot(sessionID); ok {
			if s.targets != nil && strings.TrimSpace(view.Session.SessionId) != "" {
				target, err := s.targets.ResolveSessionExecutionTarget(ctx, view.Session.SessionId)
				if err != nil {
					return nil, err
				}
				view.Session.ExecutionTarget = target
			}
			return resultWithContext(ctx, view)
		}
	}
	view, err := session.ResolvePersistedSessionView(ctx, s.persisted, sessionID)
	if err != nil {
		return nil, err
	}
	projection, err := s.resolveDormantChatProjection(ctx, session.ContextSnapshot{
		Meta:  view.Meta(),
		Facts: view.ContextFacts(),
	})
	if err != nil {
		return nil, err
	}
	return (dormantSessionSnapshot{
		view:             view,
		cacheWarningMode: s.cacheWarningMode,
		projection:       projection,
	}).MainView(ctx)
}

type dormantSessionSnapshot struct {
	view             *session.PersistedSessionView
	cacheWarningMode config.CacheWarningMode
	projection       dormantChatProjection
}

func (s dormantSessionSnapshot) MainView(ctx context.Context) (*runtimepb.MainView, error) {
	if s.view == nil {
		return nil, errors.New("persisted Session view is required")
	}
	meta := s.view.Meta()
	sessionFreshness := s.view.ConversationFreshness()
	version, err := protoapi.NewReadModelVersion(
		"persisted-session-"+meta.SessionID,
		1,
		uint64(meta.LastSequence)+1,
	)
	if err != nil {
		return nil, err
	}
	freshness := runtimeview.ConversationFreshnessFromSession(sessionFreshness)
	goalAvailability, err := session.GoalAvailabilityFromMeta(meta)
	if err != nil {
		return nil, err
	}
	goal, err := goalview.FromSessionState(meta.Goal, goalAvailability, false)
	if err != nil {
		return nil, err
	}
	usedTokens, err := protoapi.Int32(int(s.projection.context.UsedTokens), "used tokens")
	if err != nil {
		return nil, err
	}
	windowTokens, err := protoapi.Int32(int(s.projection.context.ContextWindowTokens), "context window tokens")
	if err != nil {
		return nil, err
	}
	compactionCount, err := protoapi.Int32(int(s.projection.context.CompletedCompactionCount), "compaction count")
	if err != nil {
		return nil, err
	}
	status := &runtimepb.Status{
		ReviewerFrequency:         strings.TrimSpace(s.projection.settings.Reviewer.Frequency),
		ReviewerEnabled:           strings.TrimSpace(s.projection.settings.Reviewer.Frequency) != "off",
		AutoCompactionEnabled:     s.projection.autoCompactionEnabled,
		QuestionsEnabled:          s.projection.questionsEnabled,
		FastModeAvailable:         s.projection.fastModeAvailable,
		FastModeEnabled:           s.projection.fastModeAvailable && s.projection.settings.PriorityRequestMode,
		ConversationFreshness:     freshness,
		PreviousSessionId:         protoapi.OptionalSessionIDToProto(meta.PreviousSessionID),
		ParentAgentSessionId:      protoapi.OptionalSessionIDToProto(meta.ParentAgentSessionID),
		NavigationTargetSessionId: protoapi.OptionalSessionIDToProto(session.NavigationTargetSessionID(meta)),
		ThinkingLevel:             strings.TrimSpace(s.projection.settings.ThinkingLevel),
		CompactionMode:            string(s.projection.settings.CompactionMode),
		ContextUsage: &runtimepb.ContextUsage{
			UsedTokens:   usedTokens,
			WindowTokens: windowTokens,
		},
		CompactionCount: compactionCount,
		Goal:            goal,
		WorkflowSession: s.projection.workflow,
	}
	view := &runtimepb.MainView{
		Version: version,
		Status:  status,
		Session: &runtimepb.SessionView{
			SessionId:             meta.SessionID,
			SessionName:           textutil.OptionalExactString(meta.Name),
			AgentRole:             session.ContinuationAgentRole(meta),
			ConversationFreshness: freshness,
			ExecutionTarget:       s.projection.target,
		},
		Activity: &runtimepb.Activity{
			State:    runtimepb.ActivityState_RUNTIME_ACTIVITY_UNAVAILABLE,
			Reviewer: runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_INACTIVE,
		},
	}
	return resultWithContext(ctx, view)
}

func (s dormantSessionSnapshot) TranscriptPage(ctx context.Context, req *transcriptpb.PageRequest) (*transcriptpb.Page, error) {
	if s.view == nil {
		return nil, errors.New("persisted Session view is required")
	}
	if req.Direction == nil {
		segment, err := s.newestSegment(ctx)
		if err != nil {
			return nil, err
		}
		page, err := s.transcriptPage(segment)
		if err != nil {
			return nil, err
		}
		return resultWithContext(ctx, page)
	}
	var (
		segment runtime.TranscriptSegmentPage
		err     error
	)
	switch direction := req.Direction.(type) {
	case *transcriptpb.PageRequest_NewerCursor:
		segment, err = readWithContext(ctx, func() (runtime.TranscriptSegmentPage, error) {
			return runtime.TranscriptSegmentPageForwardFromEventLog(s.view, direction.NewerCursor, s.cacheWarningModeOrDefault())
		})
	case *transcriptpb.PageRequest_Cursor:
		segment, err = readWithContext(ctx, func() (runtime.TranscriptSegmentPage, error) {
			return runtime.TranscriptSegmentPageFromEventLog(s.view, direction.Cursor, s.cacheWarningModeOrDefault())
		})
	default:
		return nil, errors.New("unsupported transcript page direction")
	}
	if err != nil {
		return nil, err
	}
	newest, err := s.newestSegment(ctx)
	if err != nil {
		return nil, err
	}
	segment.LatestRollbackCandidate = newest.LatestRollbackCandidate
	page, err := s.transcriptPage(segment)
	if err != nil {
		return nil, err
	}
	return resultWithContext(ctx, page)
}

func (s dormantSessionSnapshot) TranscriptTailEntries(ctx context.Context) ([]runtime.ChatEntry, error) {
	if s.view == nil {
		return nil, errors.New("persisted Session view is required")
	}
	segment, err := s.newestSegment(ctx)
	if err != nil {
		return nil, err
	}
	return resultWithContext(ctx, append([]runtime.ChatEntry(nil), segment.Snapshot.Entries...))
}

func (s dormantSessionSnapshot) newestSegment(ctx context.Context) (runtime.TranscriptSegmentPage, error) {
	return readWithContext(ctx, func() (runtime.TranscriptSegmentPage, error) {
		return runtime.TranscriptNewestSegmentPageFromEventLog(s.view, s.cacheWarningModeOrDefault())
	})
}

func (s dormantSessionSnapshot) cacheWarningModeOrDefault() config.CacheWarningMode {
	return normalizeServiceCacheWarningMode(s.cacheWarningMode)
}

func (s dormantSessionSnapshot) transcriptPage(segment runtime.TranscriptSegmentPage) (*transcriptpb.Page, error) {
	meta := s.view.Meta()
	return runtimeview.TranscriptPageFromSegment(
		meta.SessionID,
		meta.Name,
		runtimeview.ConversationFreshnessFromSession(s.view.ConversationFreshness()),
		segment,
	)
}
