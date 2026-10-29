package runtime

import (
	"fmt"
	"strings"

	"core/server/llm"
	"core/server/sessionstore"
	"core/shared/textutil"
	"core/shared/transcriptmodel"
)

type resolvedReasoningEntry struct {
	entry      llm.ReasoningEntry
	identity   *TranscriptReasoningTraceIdentity
	durationMs *int64
}

func (s *defaultStepExecutor) resolveReasoningEntries(stepID string, entries []llm.ReasoningEntry, requireAllTraces bool) ([]resolvedReasoningEntry, error) {
	_, provisional := s.engine.transcriptRuntimeState().ReasoningSnapshot()
	type provisionalTrace struct {
		trace   TranscriptReasoningTraceState
		ordinal int
	}
	byCoordinate := make(map[transcriptReasoningCoordinate]provisionalTrace, len(provisional))
	for ordinal, trace := range provisional {
		if trace.Source.OutputIndex == nil || trace.Source.PartIndex == nil {
			return nil, fmt.Errorf("provisional reasoning trace has no source coordinate")
		}
		key := transcriptReasoningCoordinate{
			output: *trace.Source.OutputIndex,
			part:   *trace.Source.PartIndex,
		}
		byCoordinate[key] = provisionalTrace{trace: trace, ordinal: ordinal}
	}
	seenCoordinates := make(map[transcriptReasoningCoordinate]struct{}, len(entries))
	seenAliases := make(map[string]transcriptReasoningCoordinate, len(entries))
	var lastMatchedOrdinal *int
	var resolved []resolvedReasoningEntry
	matched := 0
	for _, entry := range entries {
		if strings.TrimSpace(entry.Text) == "" {
			continue
		}
		coordinate, correlated, coordinateErr := reasoningCoordinateFromEntry(entry)
		if coordinateErr != nil {
			return nil, coordinateErr
		}
		if entry.ItemIdentity != nil {
			if err := entry.ItemIdentity.Validate(); err != nil {
				return nil, err
			}
		}
		if correlated {
			if _, duplicate := seenCoordinates[coordinate]; duplicate {
				return nil, fmt.Errorf("completed reasoning response repeats source coordinate output=%d part=%d", coordinate.output, coordinate.part)
			}
			seenCoordinates[coordinate] = struct{}{}
			trace, ok := byCoordinate[coordinate]
			if ok {
				if lastMatchedOrdinal != nil && trace.ordinal < *lastMatchedOrdinal {
					return nil, fmt.Errorf("completed reasoning response reorders provisional source coordinates")
				}
				lastMatchedOrdinal = textutil.Value(trace.ordinal)
			}
			if entry.ItemIdentity != nil {
				alias, err := llm.ReasoningItemIdentityAlias(*entry.ItemIdentity)
				if err != nil {
					return nil, err
				}
				if existing, ok := seenAliases[alias]; ok && existing != coordinate {
					return nil, fmt.Errorf("completed reasoning response aliases one provider item to multiple source coordinates")
				}
				seenAliases[alias] = coordinate
			}
			if !ok {
				resolved = append(resolved, resolvedReasoningEntry{entry: entry})
				continue
			}
			if entry.ItemIdentity != nil {
				if err := s.engine.transcriptRuntimeState().ObserveReasoningItemIdentity(stepID, entry.SourceCoordinate, entry.ItemIdentity); err != nil {
					return nil, err
				}
			}
			identity := cloneTranscriptReasoningTraceIdentity(&trace.trace.Identity)
			durationMs, durationErr := s.engine.transcriptRuntimeState().ReasoningDurationMs(stepID, entry.SourceCoordinate)
			if durationErr != nil {
				return nil, durationErr
			}
			resolved = append(resolved, resolvedReasoningEntry{entry: entry, identity: identity, durationMs: durationMs})
			matched++
			continue
		}
		resolved = append(resolved, resolvedReasoningEntry{entry: entry})
	}
	if requireAllTraces && matched != len(provisional) {
		return nil, fmt.Errorf("accepted response left provisional reasoning traces unresolved")
	}
	return resolved, nil
}

func (s *defaultStepExecutor) reconcileReasoning(stepID string, entries []llm.ReasoningEntry) error {
	resolved, err := s.resolveReasoningEntries(stepID, entries, true)
	if err != nil {
		return err
	}
	for _, entry := range resolved {
		if entry.identity == nil {
			if err := s.persistCompletedOnlyReasoning(stepID, entry.entry.Text); err != nil {
				return err
			}
			continue
		}
		receipt, err := s.engine.steerWithCommitReceipt(stepID, steerReasoningLocalEntryIntent(storedLocalEntry{
			Visibility: transcript.EntryVisibilityDetail, Role: string(transcript.EntryRoleReasoning),
			Text: entry.entry.Text, DurationMs: entry.durationMs,
		}, *entry.identity))
		if receipt.Committed {
			if consumeErr := s.engine.transcriptRuntimeState().ConsumeReasoningTrace(stepID, entry.entry.SourceCoordinate); consumeErr != nil {
				return consumeErr
			}
		}
		if err != nil {
			return err
		}
		if !receipt.Committed {
			return fmt.Errorf("reasoning trace persistence returned without a durable commit")
		}
	}
	return nil
}

func (s *defaultStepExecutor) prepareNativeReasoning(stepID string, native *llm.NativeResponse) (*llm.NativeResponse, error) {
	var entries []llm.ReasoningEntry
	for _, item := range llm.ItemsFromMessages([]llm.Message{{NativeResponse: native}}) {
		entries = append(entries, item.ReasoningSummary...)
	}
	// Stream-only summaries finish through the provisional display lifecycle.
	// Only terminal-backed coordinates can carry durable presentation facts.
	resolved, err := s.resolveReasoningEntries(stepID, entries, false)
	if err != nil {
		return nil, err
	}
	var presentation []session.NativeReasoningPresentation
	for _, entry := range resolved {
		if entry.entry.SourceCoordinate == nil || entry.entry.SourceCoordinate.OutputIndex == nil || entry.entry.SourceCoordinate.PartIndex == nil {
			return nil, fmt.Errorf("native reasoning has no source coordinate")
		}
		fact := session.NativeReasoningPresentation{
			OutputIndex: *entry.entry.SourceCoordinate.OutputIndex,
			PartIndex:   *entry.entry.SourceCoordinate.PartIndex,
			DurationMs:  entry.durationMs,
		}
		if entry.identity != nil {
			fact.KentTraceID = entry.identity.Kent
		}
		presentation = append(presentation, fact)
	}
	prepared := native.WithReasoningPresentation(presentation)
	if _, err := llm.MessageFromNativeResponse(*prepared.Record()); err != nil {
		return nil, err
	}
	return prepared, nil
}

func (s *defaultStepExecutor) resolveReasoningDisposition(
	stepID string,
	next completedResponseNext,
	entries []llm.ReasoningEntry,
) error {
	switch next {
	case completedResponseNextExternalWorkflowTerminal,
		completedResponseNextWorkflowPreflightRejected:
		return s.resetProvisionalReasoning(stepID)
	case completedResponseNextAccepted,
		completedResponseNextFinalAnswerToolsTerminal:
		return s.reconcileReasoning(stepID, entries)
	default:
		return fmt.Errorf("completed response produced invalid reasoning disposition")
	}
}

func (s *defaultStepExecutor) resetProvisionalReasoning(stepID string) error {
	_, traces := s.engine.transcriptRuntimeState().ReasoningSnapshot()
	if len(traces) == 0 {
		return nil
	}
	return s.engine.steer(stepID, steerResetReasoningStateIntent())
}

func (s *defaultStepExecutor) persistCompletedOnlyReasoning(stepID, text string) error {
	receipt, err := s.engine.steerWithCommitReceipt(
		stepID,
		steerLocalEntryIntent(storedLocalEntry{
			Visibility: transcript.EntryVisibilityDetail,
			Role:       string(transcript.EntryRoleReasoning),
			Text:       text,
		}),
	)
	if err != nil {
		return err
	}
	if !receipt.Committed {
		return fmt.Errorf("completed-only reasoning persistence returned without a durable commit")
	}
	return nil
}

func reasoningCoordinateFromEntry(entry llm.ReasoningEntry) (transcriptReasoningCoordinate, bool, error) {
	if entry.SourceCoordinate == nil {
		return transcriptReasoningCoordinate{}, false, nil
	}
	if err := entry.SourceCoordinate.Validate(); err != nil {
		return transcriptReasoningCoordinate{}, false, fmt.Errorf("completed reasoning entry has invalid source coordinate: %w", err)
	}
	return transcriptReasoningCoordinate{
		output: *entry.SourceCoordinate.OutputIndex,
		part:   *entry.SourceCoordinate.PartIndex,
	}, true, nil
}
