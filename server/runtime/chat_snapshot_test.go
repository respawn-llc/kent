package runtime

import "core/server/session"

func (e *Engine) scanPersistedTranscript(req PersistedTranscriptScanRequest) *PersistedTranscriptScan {
	scan := NewPersistedTranscriptScan(req)
	if e == nil || e.store == nil {
		return scan
	}
	if err := e.store.WalkEvents(func(evt session.Event) error {
		return scan.ApplyPersistedEvent(evt)
	}); err != nil {
		return NewPersistedTranscriptScan(req)
	}
	return scan
}

func (e *Engine) ChatSnapshot() ChatSnapshot {
	if e == nil {
		return ChatSnapshot{}
	}
	snapshot := e.scanPersistedTranscript(PersistedTranscriptScanRequest{CacheWarningMode: e.cfg.CacheWarningMode}).CollectedPageSnapshot()
	e.overlayLiveStreaming(&snapshot)
	return snapshot
}
