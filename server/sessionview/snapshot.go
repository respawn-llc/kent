package sessionview

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"builder/server/runtime"
	"builder/server/runtimeview"
	"builder/server/session"
	"builder/shared/clientui"
	"builder/shared/config"
)

type SessionSnapshotSource interface {
	ResolveSessionSnapshot(ctx context.Context, sessionID string) (SessionSnapshot, error)
}

type SessionSnapshot interface {
	Capabilities() SessionSnapshotCapabilities
	MainView(ctx context.Context) (clientui.RuntimeMainView, error)
	TranscriptPage(ctx context.Context, req clientui.TranscriptPageRequest) (clientui.TranscriptPage, error)
	CommittedTranscriptSuffix(ctx context.Context, req clientui.CommittedTranscriptSuffixRequest) (clientui.CommittedTranscriptSuffix, error)
	Run(ctx context.Context, runID string) (*clientui.RunView, error)
}

type SessionSnapshotCapabilities struct {
	TranscriptMetadata      bool
	MainViewState           bool
	RunView                 bool
	TranscriptPages         bool
	CommittedSuffixes       bool
	Freshness               bool
	ExecutionTarget         bool
	UpdateStatus            bool
	CacheWarningVisibility  bool
	CompactionProjections   bool
	OffsetLimitPagination   bool
	DefaultTranscriptWindow bool
}

func requiredSessionSnapshotCapabilities() SessionSnapshotCapabilities {
	return SessionSnapshotCapabilities{
		TranscriptMetadata:      true,
		MainViewState:           true,
		RunView:                 true,
		TranscriptPages:         true,
		CommittedSuffixes:       true,
		Freshness:               true,
		ExecutionTarget:         true,
		UpdateStatus:            true,
		CacheWarningVisibility:  true,
		CompactionProjections:   true,
		OffsetLimitPagination:   true,
		DefaultTranscriptWindow: true,
	}
}

func coreSessionSnapshotCapabilities() SessionSnapshotCapabilities {
	capabilities := requiredSessionSnapshotCapabilities()
	capabilities.ExecutionTarget = false
	capabilities.UpdateStatus = false
	return capabilities
}

type enrichedSessionSnapshotSource struct {
	base     SessionSnapshotSource
	targets  ExecutionTargetResolver
	updates  func() UpdateStatusProvider
	clearers []interface{ ClearCaches() }
}

func newEnrichedSessionSnapshotSource(base SessionSnapshotSource, targets ExecutionTargetResolver, updates func() UpdateStatusProvider) SessionSnapshotSource {
	source := &enrichedSessionSnapshotSource{base: base, targets: targets, updates: updates}
	if clearer, ok := base.(interface{ ClearCaches() }); ok {
		source.clearers = append(source.clearers, clearer)
	}
	return source
}

func (s *enrichedSessionSnapshotSource) ResolveSessionSnapshot(ctx context.Context, sessionID string) (SessionSnapshot, error) {
	if s == nil || s.base == nil {
		return nil, errors.New("session store resolver is required")
	}
	snapshot, err := s.base.ResolveSessionSnapshot(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return enrichedSessionSnapshot{base: snapshot, targets: s.targets, updates: s.updates}, nil
}

func (s *enrichedSessionSnapshotSource) ClearCaches() {
	if s == nil {
		return
	}
	for _, clearer := range s.clearers {
		clearer.ClearCaches()
	}
}

type enrichedSessionSnapshot struct {
	base    SessionSnapshot
	targets ExecutionTargetResolver
	updates func() UpdateStatusProvider
}

func (s enrichedSessionSnapshot) Capabilities() SessionSnapshotCapabilities {
	capabilities := s.base.Capabilities()
	capabilities.ExecutionTarget = true
	capabilities.UpdateStatus = true
	return capabilities
}

func (s enrichedSessionSnapshot) MainView(ctx context.Context) (clientui.RuntimeMainView, error) {
	view, err := s.base.MainView(ctx)
	if err != nil {
		return clientui.RuntimeMainView{}, err
	}
	if s.targets != nil && strings.TrimSpace(view.Session.SessionID) != "" {
		target, err := s.targets.ResolveSessionExecutionTarget(ctx, view.Session.SessionID)
		if err != nil {
			return clientui.RuntimeMainView{}, err
		}
		view.Session.ExecutionTarget = target
	}
	if s.updates != nil {
		if provider := s.updates(); provider != nil {
			view.Status.Update = provider.Status(ctx)
		}
	}
	return view, nil
}

func (s enrichedSessionSnapshot) TranscriptPage(ctx context.Context, req clientui.TranscriptPageRequest) (clientui.TranscriptPage, error) {
	return s.base.TranscriptPage(ctx, req)
}

func (s enrichedSessionSnapshot) CommittedTranscriptSuffix(ctx context.Context, req clientui.CommittedTranscriptSuffixRequest) (clientui.CommittedTranscriptSuffix, error) {
	return s.base.CommittedTranscriptSuffix(ctx, req)
}

func (s enrichedSessionSnapshot) Run(ctx context.Context, runID string) (*clientui.RunView, error) {
	return s.base.Run(ctx, runID)
}

type resolvedSessionSnapshotSource struct {
	sessions SessionStoreResolver
	runtimes RuntimeResolver
	dormant  *dormantSessionSnapshotSource
}

func newResolvedSessionSnapshotSource(sessions SessionStoreResolver, runtimes RuntimeResolver, cacheWarningMode func() config.CacheWarningMode) *resolvedSessionSnapshotSource {
	return &resolvedSessionSnapshotSource{
		sessions: sessions,
		runtimes: runtimes,
		dormant:  newDormantSessionSnapshotSource(cacheWarningMode),
	}
}

func (s *resolvedSessionSnapshotSource) ResolveSessionSnapshot(ctx context.Context, sessionID string) (SessionSnapshot, error) {
	if s == nil {
		return nil, errors.New("session store resolver is required")
	}
	if s.runtimes != nil {
		engine, err := s.runtimes.ResolveRuntime(ctx, sessionID)
		if err != nil {
			return nil, err
		}
		if engine != nil {
			return liveRuntimeSessionSnapshot{engine: engine, sessions: s.sessions}, nil
		}
	}
	if s.sessions == nil {
		return nil, errors.New("session store resolver is required")
	}
	store, err := s.sessions.ResolveSessionStore(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if store == nil {
		return nil, errors.New("session store resolver is required")
	}
	return s.dormant.snapshot(store), nil
}

func (s *resolvedSessionSnapshotSource) ClearCaches() {
	if s != nil && s.dormant != nil {
		s.dormant.clear()
	}
}

type liveRuntimeSessionSnapshot struct {
	engine   *runtime.Engine
	sessions SessionStoreResolver
}

func (s liveRuntimeSessionSnapshot) Capabilities() SessionSnapshotCapabilities {
	return coreSessionSnapshotCapabilities()
}

func (s liveRuntimeSessionSnapshot) MainView(context.Context) (clientui.RuntimeMainView, error) {
	return runtimeview.MainViewFromRuntime(s.engine), nil
}

func (s liveRuntimeSessionSnapshot) TranscriptPage(_ context.Context, req clientui.TranscriptPageRequest) (clientui.TranscriptPage, error) {
	return runtimeview.TranscriptPageFromRuntime(s.engine, req), nil
}

func (s liveRuntimeSessionSnapshot) CommittedTranscriptSuffix(_ context.Context, req clientui.CommittedTranscriptSuffixRequest) (clientui.CommittedTranscriptSuffix, error) {
	return runtimeview.CommittedTranscriptSuffixFromRuntime(s.engine, req), nil
}

func (s liveRuntimeSessionSnapshot) Run(ctx context.Context, runID string) (*clientui.RunView, error) {
	want := strings.TrimSpace(runID)
	if active := runtimeview.RunViewFromRuntime(s.engine.SessionID(), s.engine.ActiveRun()); active != nil && strings.TrimSpace(active.RunID) == want {
		return active, nil
	}
	var store *session.Store
	var err error
	if s.sessions != nil {
		store, err = s.sessions.ResolveSessionStore(ctx, s.engine.SessionID())
	}
	if err != nil {
		return nil, err
	}
	if store == nil {
		return nil, errors.New("session store resolver is required")
	}
	return runViewFromStore(store, want)
}

type dormantSessionSnapshotSource struct {
	cacheWarningMode func() config.CacheWarningMode
	dormant          *dormantTranscriptCache
	dormantPages     *dormantTranscriptPageCache
}

func newDormantSessionSnapshotSource(cacheWarningMode func() config.CacheWarningMode) *dormantSessionSnapshotSource {
	source := &dormantSessionSnapshotSource{cacheWarningMode: cacheWarningMode}
	source.dormant = newDormantTranscriptCacheWithLimit(dormantTranscriptCacheMaxEntries, func(ctx context.Context, store *session.Store) (dormantTranscriptCacheEntry, error) {
		return source.buildCacheEntry(ctx, store)
	})
	source.dormantPages = newDormantTranscriptPageCacheWithLimit(dormantTranscriptPageCacheMaxEntries)
	return source
}

func (s *dormantSessionSnapshotSource) snapshot(store *session.Store) dormantSessionSnapshot {
	return dormantSessionSnapshot{source: s, store: store}
}

func (s *dormantSessionSnapshotSource) clear() {
	if s == nil {
		return
	}
	if s.dormant != nil {
		s.dormant.clear()
	}
	if s.dormantPages != nil {
		s.dormantPages.clear()
	}
}

func (s *dormantSessionSnapshotSource) buildCacheEntry(ctx context.Context, store *session.Store) (dormantTranscriptCacheEntry, error) {
	cacheWarningMode := config.CacheWarningModeDefault
	if s != nil && s.cacheWarningMode != nil {
		cacheWarningMode = normalizeServiceCacheWarningMode(s.cacheWarningMode())
	}
	meta := store.Meta()
	scan, err := scanDormantTranscript(ctx, store, runtime.PersistedTranscriptScanRequest{
		TrackOngoingTail: true,
		TailLimit:        runtimeview.OngoingTailEntryLimit,
		CacheWarningMode: cacheWarningMode,
	})
	if err != nil {
		return dormantTranscriptCacheEntry{}, err
	}
	var activeRun *clientui.RunView
	latestRun, err := store.LatestRun()
	if err != nil {
		return dormantTranscriptCacheEntry{}, err
	}
	if latestRun != nil && latestRun.Status == session.RunStatusRunning {
		activeRun = runtimeview.RunViewFromSessionRecord(meta.SessionID, latestRun)
	}
	return dormantTranscriptCacheEntry{
		sessionDir:                   store.Dir(),
		sessionID:                    meta.SessionID,
		revision:                     meta.LastSequence,
		totalEntries:                 scan.TotalEntries(),
		lastCommittedAssistantAnswer: scan.LastCommittedAssistantFinalAnswer(),
		ongoingTail:                  scan.OngoingTailSnapshot(),
		activeRun:                    activeRun,
	}, nil
}

type dormantSessionSnapshot struct {
	source *dormantSessionSnapshotSource
	store  *session.Store
}

func (s dormantSessionSnapshot) Capabilities() SessionSnapshotCapabilities {
	return coreSessionSnapshotCapabilities()
}

func (s dormantSessionSnapshot) MainView(ctx context.Context) (clientui.RuntimeMainView, error) {
	if s.store == nil {
		return clientui.RuntimeMainView{}, errors.New("session store is required")
	}
	entry, err := s.source.dormant.get(ctx, s.store)
	if err != nil {
		return clientui.RuntimeMainView{}, err
	}
	meta := s.store.Meta()
	freshness := runtimeview.ConversationFreshnessFromSession(s.store.ConversationFreshness())
	return entry.mainView(meta, freshness), nil
}

func (s dormantSessionSnapshot) TranscriptPage(ctx context.Context, req clientui.TranscriptPageRequest) (clientui.TranscriptPage, error) {
	if s.store == nil {
		return clientui.TranscriptPage{}, errors.New("session store is required")
	}
	req = runtimeview.NormalizeDefaultTranscriptRequest(req)
	meta := s.store.Meta()
	freshness := runtimeview.ConversationFreshnessFromSession(s.store.ConversationFreshness())
	entry, err := s.source.dormant.get(ctx, s.store)
	if err != nil {
		return clientui.TranscriptPage{}, err
	}
	if req.Window == clientui.TranscriptWindowOngoingTail {
		return runtimeview.TranscriptPageFromOngoingTailWindow(meta.SessionID, meta.Name, freshness, meta.LastSequence, entry.ongoingTail, req), nil
	}
	offset := req.Offset
	limit := req.Limit
	if req.PageSize > 0 {
		offset = req.Page * req.PageSize
		limit = req.PageSize
	}
	if page, ok := entry.transcriptPageCoveredByTail(meta, freshness, clientui.TranscriptPageRequest{Offset: offset, Limit: limit}); ok {
		return page, nil
	}
	cacheWarningMode := config.CacheWarningModeDefault
	if s.source != nil && s.source.cacheWarningMode != nil {
		cacheWarningMode = normalizeServiceCacheWarningMode(s.source.cacheWarningMode())
	}
	cacheKey := dormantTranscriptPageCacheKeyForStore(s.store, meta, freshness, cacheWarningMode, offset, limit)
	return s.source.dormantPages.getOrBuild(cacheKey, func() (clientui.TranscriptPage, error) {
		scan, err := scanDormantTranscript(ctx, s.store, runtime.PersistedTranscriptScanRequest{Offset: offset, Limit: limit, CacheWarningMode: cacheWarningMode})
		if err != nil {
			return clientui.TranscriptPage{}, err
		}
		pageOffset := offset
		if pageOffset > scan.TotalEntries() {
			pageOffset = scan.TotalEntries()
		}
		return runtimeview.TranscriptPageFromCollectedChat(
			meta.SessionID,
			meta.Name,
			freshness,
			meta.LastSequence,
			runtimeview.ChatSnapshotFromRuntime(scan.CollectedPageSnapshot()),
			scan.TotalEntries(),
			pageOffset,
			clientui.TranscriptPageRequest{Offset: pageOffset, Limit: limit},
		), nil
	})
}

func (s dormantSessionSnapshot) CommittedTranscriptSuffix(ctx context.Context, req clientui.CommittedTranscriptSuffixRequest) (clientui.CommittedTranscriptSuffix, error) {
	if s.store == nil {
		return clientui.CommittedTranscriptSuffix{}, errors.New("session store is required")
	}
	req = clientui.NormalizeCommittedTranscriptSuffixRequest(req)
	meta := s.store.Meta()
	freshness := runtimeview.ConversationFreshnessFromSession(s.store.ConversationFreshness())
	cacheWarningMode := config.CacheWarningModeDefault
	if s.source != nil && s.source.cacheWarningMode != nil {
		cacheWarningMode = normalizeServiceCacheWarningMode(s.source.cacheWarningMode())
	}
	scan, err := scanDormantTranscript(ctx, s.store, runtime.PersistedTranscriptScanRequest{
		Offset:           req.AfterEntryCount,
		Limit:            req.Limit,
		CacheWarningMode: cacheWarningMode,
	})
	if err != nil {
		return clientui.CommittedTranscriptSuffix{}, err
	}
	startEntryCount := req.AfterEntryCount
	if total := scan.TotalEntries(); startEntryCount > total {
		startEntryCount = total
	}
	return runtimeview.CommittedTranscriptSuffixFromCollectedChat(
		meta.SessionID,
		meta.Name,
		freshness,
		meta.LastSequence,
		runtimeview.ChatSnapshotFromRuntime(scan.CollectedPageSnapshot()),
		scan.TotalEntries(),
		startEntryCount,
		req,
	), nil
}

func (s dormantSessionSnapshot) Run(_ context.Context, runID string) (*clientui.RunView, error) {
	if s.store == nil {
		return nil, errors.New("session store is required")
	}
	return runViewFromStore(s.store, strings.TrimSpace(runID))
}

func scanDormantTranscript(ctx context.Context, store *session.Store, req runtime.PersistedTranscriptScanRequest) (*runtime.PersistedTranscriptScan, error) {
	scan := runtime.NewPersistedTranscriptScan(req)
	if err := store.WalkEvents(func(evt session.Event) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		return scan.ApplyPersistedEvent(evt)
	}); err != nil {
		return nil, err
	}
	return scan, nil
}

func runViewFromStore(store *session.Store, runID string) (*clientui.RunView, error) {
	runs, err := store.ReadRuns()
	if err != nil {
		return nil, err
	}
	for _, run := range runs {
		if run.RunID == runID {
			copyRun := run
			return runtimeview.RunViewFromSessionRecord(store.Meta().SessionID, &copyRun), nil
		}
	}
	return nil, fmt.Errorf("run %q not found", runID)
}
