package launch

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"core/server/llm"
	"core/server/metadata"
	"core/server/session"
	"core/shared/config"
)

type ConnectionRotation struct {
	store *metadata.Store
	mu    sync.Mutex
	// Immutable published order lets data-only previews avoid waiting for allocations.
	assignments atomic.Pointer[connectionAssignments]
}

func NewConnectionRotation(store *metadata.Store) *ConnectionRotation {
	if store == nil {
		panic("connection consumption store is required")
	}
	return &ConnectionRotation{store: store}
}

func (r *ConnectionRotation) RecordUsage(ctx context.Context, connection config.ConnectionID, model string, usage llm.Usage) {
	score, err := llm.EstimateConsumption(model, usage)
	if err == nil && score == 0 {
		return
	}
	if err == nil {
		err = r.store.RecordConnectionConsumption(ctx, connection, time.Now(), score)
	}
	if err != nil {
		log.Printf("record connection consumption for connection %q model %q: %v", connection, model, err)
	}
}

type connectionAssignments struct {
	lastAssigned map[config.ConnectionID]uint64
	sequence     uint64
}

func ConnectionMembership(settings config.Settings) ([]config.ConnectionID, string, error) {
	members, err := settings.ConnectionMembers()
	if err != nil {
		return nil, "", err
	}
	canonical := slices.Clone(members)
	slices.Sort(canonical)
	key, err := json.Marshal(canonical)
	if err != nil {
		return nil, "", err
	}
	return members, string(key), nil
}

func (r *ConnectionRotation) Prepare(ctx context.Context, settings config.Settings, allocate bool) (config.ConnectionID, error) {
	members, err := settings.ConnectionMembers()
	if err != nil {
		return "", err
	}
	if r == nil {
		if !allocate || len(members) == 1 {
			return members[0], nil
		}
		return "", fmt.Errorf("server connection rotation is required for allocating a connection set")
	}
	scores, err := r.store.ConnectionConsumption(ctx, members, time.Now())
	if err != nil {
		log.Printf("read connection consumption for %v: %v; using assignment order", members, err)
	}
	if !allocate {
		return r.assignments.Load().leastConsumed(members, scores), nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	next := &connectionAssignments{lastAssigned: make(map[config.ConnectionID]uint64)}
	if current := r.assignments.Load(); current != nil {
		next.lastAssigned = maps.Clone(current.lastAssigned)
		next.sequence = current.sequence
	}
	maps.DeleteFunc(next.lastAssigned, func(id config.ConnectionID, _ uint64) bool {
		_, configured := settings.Connections[id]
		return !configured
	})
	selected := next.leastConsumed(members, scores)
	next.sequence++
	next.lastAssigned[selected] = next.sequence
	r.assignments.Store(next)
	return selected, nil
}

func (r *connectionAssignments) leastConsumed(members []config.ConnectionID, scores map[config.ConnectionID]int64) config.ConnectionID {
	selected := members[0]
	for _, member := range members[1:] {
		if scores[member] < scores[selected] {
			selected = member
		} else if scores[member] == scores[selected] && r != nil && r.lastAssigned[member] < r.lastAssigned[selected] {
			selected = member
		}
	}
	return selected
}

func (p RunPromptPreparationContext) prepareConnection(ctx context.Context, settings *config.Settings, source config.SourceReport) error {
	saved := p.ConnectionID
	if retained := p.RetainedConnectionSelection; retained != nil {
		if saved != nil || p.AllocateConnection {
			return fmt.Errorf("retained Connection selection cannot be combined with resume or allocation")
		}
		members, err := settings.ConnectionMembers()
		if err != nil {
			return err
		}
		if !slices.Contains(members, *retained) {
			return fmt.Errorf("retained Connection %q is no longer a member of the selected Agent's connection set", *retained)
		}
		saved = retained
	}
	id, _, err := p.Rotation.ResolveSessionConnection(ctx, *settings, saved, p.AllocateConnection)
	if err != nil {
		return err
	}
	settings.Connection = config.SingleConnection(id)
	return projectSessionConnection(settings, source, session.Meta{ConnectionID: &id, Locked: p.ModelLock})
}

// ResolveSessionConnection projects a binding without changing Session metadata.
// Only a removed definition permits ordinary resume to select a replacement.
func ResolveSessionConnection(settings config.Settings, saved *config.ConnectionID) (config.ConnectionID, *config.ConnectionReplacement, error) {
	return (*ConnectionRotation)(nil).ResolveSessionConnection(context.Background(), settings, saved, false)
}

func (r *ConnectionRotation) ResolveSessionConnection(ctx context.Context, settings config.Settings, saved *config.ConnectionID, allocate bool) (config.ConnectionID, *config.ConnectionReplacement, error) {
	if saved != nil {
		if _, err := config.ParseConnectionID(string(*saved)); err != nil {
			return "", nil, err
		}
		if _, present := settings.Connections[*saved]; present {
			return *saved, nil, nil
		}
	}
	selected, err := r.Prepare(ctx, settings, allocate)
	if err != nil {
		return "", nil, err
	}
	if saved == nil {
		return selected, nil, nil
	}
	return selected, &config.ConnectionReplacement{Previous: *saved, Current: selected}, nil
}

func BindSessionConnection(store *session.Store, settings *config.Settings, sources map[string]config.Origin) (*config.ConnectionReplacement, error) {
	return (*ConnectionRotation)(nil).BindSessionConnection(context.Background(), store, settings, sources)
}

func (r *ConnectionRotation) BindSessionConnection(ctx context.Context, store *session.Store, settings *config.Settings, sources map[string]config.Origin) (*config.ConnectionReplacement, error) {
	saved := store.Meta().ConnectionID
	selected, replacement, err := r.ResolveSessionConnection(ctx, *settings, saved, true)
	if err != nil {
		return nil, err
	}
	if saved == nil || *saved != selected {
		if err := store.SetConnectionID(selected); err != nil {
			return nil, err
		}
	}
	if err := projectSessionConnection(settings, config.SourceReport{Sources: sources}, store.Meta()); err != nil {
		return nil, err
	}
	return replacement, nil
}
func projectSessionConnection(settings *config.Settings, source config.SourceReport, meta session.Meta) error {
	selected, _, err := ResolveSessionConnection(*settings, meta.ConnectionID)
	if err != nil {
		return err
	}
	settings.Connection = config.SingleConnection(selected)
	defaultSources := maps.Clone(source.Sources)
	if meta.Locked != nil {
		if defaultSources == nil {
			defaultSources = map[string]config.Origin{}
		}
		defaultSources["model"] = config.Origin{Kind: config.SourceSession, Property: config.PropertyAddress{Key: "model"}}
	}
	if err := llm.ApplyConnectionModelDefaults(settings, defaultSources); err != nil {
		return err
	}
	config.InheritReviewerSettings(settings, source.Sources)
	return nil
}
