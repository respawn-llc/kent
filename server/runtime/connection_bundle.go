package runtime

import (
	"context"
	"errors"
	"maps"

	"core/server/llm"
	"core/shared/config"
	"core/shared/llmerrors"
	"core/shared/textutil"
	"core/shared/transcript"
)

type ConnectionSelectionReason uint8

const (
	ConnectionBeforeDispatch ConnectionSelectionReason = iota + 1
	ConnectionUsageExhausted
)

type ConnectionSelectionRequest struct {
	Current config.ConnectionID
	Failed  *config.ConnectionID
	Reason  ConnectionSelectionReason
	Visited map[config.ConnectionID]struct{}
}

type PreparedConnection struct {
	ID                     config.ConnectionID
	Client                 llm.Client
	TokenEstimator         llm.TokenEstimator
	RequestCapabilities    llm.ProviderCapabilities
	TransportCapabilities  llm.ProviderCapabilities
	RawImageOriginalDetail bool
}

type connectionBundle struct {
	id                     *config.ConnectionID
	client                 *observedModelClient
	estimator              llm.TokenEstimator
	requestCapabilities    llm.ProviderCapabilities
	transportCapabilities  llm.ProviderCapabilities
	rawImageOriginalDetail bool
}

func (e *Engine) connectionSnapshot() *connectionBundle {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.connection
}

func (e *Engine) EstimateText(text string) int {
	return e.connectionSnapshot().estimator.EstimateText(text)
}

func (e *Engine) RawImageOriginalDetail() bool {
	return e.connectionSnapshot().rawImageOriginalDetail
}

type ConnectionResolver func(context.Context, ConnectionSelectionRequest) (*PreparedConnection, error)

func (e *Engine) selectConnection(ctx context.Context, stepID string, request ConnectionSelectionRequest) (*connectionBundle, error) {
	return e.selectConnectionWithPreparation(ctx, stepID, request, nil)
}

func (e *Engine) selectConnectionWithPreparation(ctx context.Context, stepID string, request ConnectionSelectionRequest, prepare func(*connectionBundle) error) (*connectionBundle, error) {
	current := e.connectionSnapshot()
	if e.cfg.ResolveConnection == nil || current.id == nil {
		return current, nil
	}
	request.Current = *current.id
	replacement, err := e.cfg.ResolveConnection(ctx, request)
	if err != nil {
		return nil, err
	}
	if replacement == nil {
		return current, nil
	}
	if err := ValidateSessionProviderChange(e.eventLog, replacement.TransportCapabilities); err != nil {
		return nil, err
	}
	next := &connectionBundle{
		id: &replacement.ID, client: newObservedModelClient(replacement.Client),
		estimator:              replacement.TokenEstimator,
		requestCapabilities:    replacement.RequestCapabilities,
		transportCapabilities:  replacement.TransportCapabilities,
		rawImageOriginalDetail: replacement.RawImageOriginalDetail,
	}
	if prepare != nil {
		if err := prepare(next); err != nil {
			return nil, err
		}
	}
	if err := e.store.SetConnectionID(replacement.ID); err != nil {
		return nil, err
	}
	e.mu.Lock()
	e.connection = next
	e.transcriptState.chatProjection().bindTokenEstimator(next.estimator)
	e.mu.Unlock()
	notice := config.ConnectionReplacement{Previous: *current.id, Current: replacement.ID}
	if err := e.steer(stepID, steerLocalEntryIntent(storedLocalEntry{
		Visibility: transcript.EntryVisibilityOngoing, Role: string(transcript.EntryRoleSystem),
		ConnectionFallback: &notice,
	})); err != nil {
		return nil, err
	}
	event := Event{Kind: EventConnectionReplaced, ConnectionReplacement: &notice}
	if err := e.steer(stepID, steeringIntent{
		priority: steeringPriorityRuntimeEvent,
		items:    []steeringItem{{connectionReplacement: &notice}, {event: &event}},
	}); err != nil {
		return nil, err
	}
	return next, nil
}

// Each operation owns its accepted input and re-prepares it for the captured
// bundle. Transient retries remain within that attempt's existing retry policy.
func withConnectionFallback[T any](
	ctx context.Context,
	selectConnection func(context.Context, ConnectionSelectionRequest) (*connectionBundle, error),
	operation func(*connectionBundle) (T, error),
) (T, error) {
	var zero T
	visited := make(map[config.ConnectionID]struct{})
	var causes []error
	current, err := selectConnection(ctx, ConnectionSelectionRequest{Reason: ConnectionBeforeDispatch, Visited: maps.Clone(visited)})
	if err != nil {
		return zero, err
	}
	for {
		if current.id != nil {
			visited[*current.id] = struct{}{}
		}
		result, err := operation(current)
		if err == nil || !llm.IsUsageExhaustionError(err) {
			return result, err
		}
		causes = append(causes, err)
		next, selectionErr := selectConnection(ctx, ConnectionSelectionRequest{
			Reason: ConnectionUsageExhausted, Failed: textutil.Pointer(current.id), Visited: maps.Clone(visited),
		})
		if selectionErr != nil {
			var unavailable *llmerrors.ConnectionsUnavailableError
			if errors.As(selectionErr, &unavailable) {
				return zero, &llmerrors.ConnectionsUnavailableError{Members: unavailable.Members, Causes: causes}
			}
			return zero, selectionErr
		}
		if next == current {
			return zero, err
		}
		current = next
	}
}
