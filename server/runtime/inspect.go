package runtime

import (
	"context"
	"errors"

	"core/server/llm"
	"github.com/google/uuid"
)

// PrepareInspectionRequest builds the provider-agnostic llm.Request for a session
// using the production request-assembly path, WITHOUT running a model turn or
// performing any network I/O. It is an operator-only diagnostic seam used by
// offline inspection tooling to capture request shape only. It does not
// reproduce provider token accounting or compaction/context decisions.
//
// The engine must already be constructed (which auto-hydrates the active transcript
// segment via restoreMessages). It prepares the same meta context that a live turn
// prepares before running the same pre-dispatch preparation as a live turn. If
// that preparation requires a model-backed compaction, inspection returns the
// preparation error rather than emitting a stale post-preparation payload.
// allowTools mirrors the production tool-exposure behavior; pass false to
// produce a tool-less payload. A saved compaction summary awaiting its first
// live request cannot be inspected as a prepared request.
func PrepareInspectionRequest(ctx context.Context, eng *Engine, allowTools bool) (llm.Request, error) {
	if _, pending := eng.generationContextSnapshot().(pendingGenerationContext); pending {
		return llm.Request{}, errors.New("compacted output awaits its first live request; inspection cannot prepare generation context")
	}
	eng.ensureOrchestrationCollaborators()
	stepID := uuid.NewString()
	if err := eng.ensureMetaContextForRequest(ctx, stepID); err != nil {
		return llm.Request{}, err
	}
	if err := (&defaultStepExecutor{engine: eng}).prepareModelTurn(ctx, stepID); err != nil {
		return llm.Request{}, err
	}
	return eng.buildContextFreeRequest(ctx, stepID, nil, allowTools)
}
