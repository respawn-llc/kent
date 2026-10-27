package runtime

import (
	"core/server/session"
	"core/shared/config"
)

func (e *Engine) DangerouslyWalkEntireHugeEventLog(cacheWarningMode config.CacheWarningMode) *streamingTranscriptScan {
	scan := newStreamingTranscriptScan(cacheWarningMode)
	if e == nil || e.store == nil {
		return scan
	}
	if err := e.eventLog.WalkRecords(func(evt session.EventRecord) error {
		return scan.ApplyPersistedEvent(evt)
	}); err != nil {
		return newStreamingTranscriptScan(cacheWarningMode)
	}
	return scan
}

func (e *Engine) ChatSnapshot() ChatSnapshot {
	if e == nil {
		return ChatSnapshot{}
	}
	snapshot := e.DangerouslyWalkEntireHugeEventLog(e.cfg.CacheWarningMode).PageSnapshot()
	e.overlayLiveStreaming(&snapshot)
	return snapshot
}
