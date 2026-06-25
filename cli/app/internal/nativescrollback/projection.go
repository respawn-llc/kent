package nativescrollback

import "core/cli/tui"

type ProjectionState struct {
	Projection          tui.TranscriptProjection
	BaseOffset          int
	CommittedEntryCount int
	Replayed            bool
}

type RenderedProjectionState struct {
	Projection tui.TranscriptProjection
	BaseOffset int
	Snapshot   string
}

type RenderedProjectionCommitUpdate struct {
	ResetStreaming bool
}

type projectionLedger struct {
	committedEntryCount int
	historyReplayed     bool
	currentProjection   tui.TranscriptProjection
	currentBaseOffset   int
	renderedProjection  tui.TranscriptProjection
	renderedBaseOffset  int
	renderedSnapshot    string
	renderedCommit      renderedProjectionCommit
}

type renderedProjectionCommit struct {
	pending        bool
	checkpoint     Checkpoint
	projection     tui.TranscriptProjection
	baseOffset     int
	snapshot       string
	resetStreaming bool
}

func (l *Ledger) ResetHistoryState() {
	if l == nil {
		return
	}
	l.projection = projectionLedger{}
	l.assistant = assistantStreamLedger{}
	l.DiscardScheduled()
}

func (l *Ledger) MarkHistoryReplayed() {
	if l == nil {
		return
	}
	l.projection.historyReplayed = true
}

func (l *Ledger) HistoryReplayed() bool {
	if l == nil {
		return false
	}
	return l.projection.historyReplayed
}

func (l *Ledger) SetCurrentProjection(projection tui.TranscriptProjection, baseOffset int, committedEntryCount int) {
	if l == nil {
		return
	}
	l.projection.currentProjection = projection.Clone()
	l.projection.currentBaseOffset = baseOffset
	l.projection.committedEntryCount = committedEntryCount
	l.projection.historyReplayed = true
}

func (l *Ledger) CurrentProjection() ProjectionState {
	if l == nil {
		return ProjectionState{}
	}
	return ProjectionState{
		Projection:          l.projection.currentProjection.Clone(),
		BaseOffset:          l.projection.currentBaseOffset,
		CommittedEntryCount: l.projection.committedEntryCount,
		Replayed:            l.projection.historyReplayed,
	}
}

func (l *Ledger) CurrentCommittedEntryCount() int {
	if l == nil {
		return 0
	}
	return l.projection.committedEntryCount
}

func (l *Ledger) CurrentProjectionEmpty() bool {
	return l == nil || l.projection.currentProjection.Empty()
}

func (l *Ledger) RenderedProjection() RenderedProjectionState {
	if l == nil {
		return RenderedProjectionState{}
	}
	return RenderedProjectionState{
		Projection: l.projection.renderedProjection.Clone(),
		BaseOffset: l.projection.renderedBaseOffset,
		Snapshot:   l.projection.renderedSnapshot,
	}
}

func (l *Ledger) ScheduledRenderedProjection() RenderedProjectionState {
	if l == nil {
		return RenderedProjectionState{}
	}
	if !l.projection.renderedCommit.pending {
		return l.RenderedProjection()
	}
	commit := l.projection.renderedCommit
	return RenderedProjectionState{
		Projection: commit.projection.Clone(),
		BaseOffset: commit.baseOffset,
		Snapshot:   commit.snapshot,
	}
}

func (l *Ledger) RenderedProjectionEmpty() bool {
	return l == nil || l.projection.renderedProjection.Empty()
}

func (l *Ledger) ScheduleRenderedProjectionCommit(projection tui.TranscriptProjection, baseOffset int, resetStreaming bool) {
	if l == nil {
		return
	}
	l.projection.renderedCommit = renderedProjectionCommit{
		pending:        true,
		checkpoint:     l.Checkpoint(),
		projection:     projection.Clone(),
		baseOffset:     baseOffset,
		snapshot:       projection.Render(tui.TranscriptDivider),
		resetStreaming: resetStreaming,
	}
}

func (l *Ledger) ApplyRenderedProjectionCommitIfReady() (RenderedProjectionCommitUpdate, bool) {
	if l == nil || !l.projection.renderedCommit.pending {
		return RenderedProjectionCommitUpdate{}, false
	}
	commit := l.projection.renderedCommit
	if !l.CheckpointReached(commit.checkpoint) {
		return RenderedProjectionCommitUpdate{}, false
	}
	l.projection.renderedProjection = commit.projection.Clone()
	l.projection.renderedBaseOffset = commit.baseOffset
	l.projection.renderedSnapshot = commit.snapshot
	l.projection.renderedCommit = renderedProjectionCommit{}
	return RenderedProjectionCommitUpdate{ResetStreaming: commit.resetStreaming}, true
}

func (l *Ledger) RenderedProjectionCommitPending() bool {
	return l != nil && l.projection.renderedCommit.pending
}
