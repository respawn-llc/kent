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
	"core/server/sessionstore"
	"core/shared/config"
	"core/shared/llmerrors"
)

type ConnectionRotation struct {
	mu       sync.Mutex
	cooldown time.Duration
	store    *metadata.Store
	// Immutable published order lets data-only previews avoid waiting for allocations.
	assignments atomic.Pointer[connectionAssignments]
}

func NewConnectionRotation(store *metadata.Store, cooldown time.Duration) *ConnectionRotation {
	if store == nil {
		panic("connection consumption store is required")
	}
	return &ConnectionRotation{store: store, cooldown: cooldown}
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
	cooldowns    map[config.ConnectionID]time.Time
	sequence     uint64
}

func (r *ConnectionRotation) FallbackEnabled() bool { return r != nil && r.cooldown > 0 }

func (r *ConnectionRotation) CoolingDown(id config.ConnectionID) bool {
	if !r.FallbackEnabled() {
		return false
	}
	snapshot := r.assignments.Load()
	return snapshot != nil && snapshot.cooldowns[id].After(time.Now())
}

func (r *ConnectionRotation) SelectReplacement(ctx context.Context, settings config.Settings, scope config.ConnectionScope, visited map[config.ConnectionID]struct{}) (config.ConnectionID, error) {
	if scope.UnavailableRole != nil {
		return "", fmt.Errorf("configured Agent role %q is unavailable; restore it before replacing its connection", *scope.UnavailableRole)
	}
	settings.Connection = scope.Selection
	return r.prepare(ctx, settings, true, visited)
}

func (r *ConnectionRotation) RecordExhaustion(id config.ConnectionID) {
	if r == nil || r.cooldown == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	next := r.assignments.Load().clone()
	next.cooldowns[id] = time.Now().Add(r.cooldown)
	r.assignments.Store(next)
}

func (r *connectionAssignments) clone() *connectionAssignments {
	if r == nil {
		return &connectionAssignments{
			lastAssigned: make(map[config.ConnectionID]uint64),
			cooldowns:    make(map[config.ConnectionID]time.Time),
		}
	}
	return &connectionAssignments{
		lastAssigned: maps.Clone(r.lastAssigned),
		cooldowns:    maps.Clone(r.cooldowns),
		sequence:     r.sequence,
	}
}

type UnavailableConnection = llmerrors.UnavailableConnection
type ConnectionsUnavailableError = llmerrors.ConnectionsUnavailableError

func (r *connectionAssignments) eligible(members []config.ConnectionID, visited map[config.ConnectionID]struct{}) ([]config.ConnectionID, error) {
	now := time.Now()
	eligible := make([]config.ConnectionID, 0, len(members))
	unavailable := &ConnectionsUnavailableError{}
	for _, id := range members {
		var deadline *time.Time
		if r != nil {
			if until, ok := r.cooldowns[id]; ok && until.After(now) {
				deadline = &until
			}
		}
		_, seen := visited[id]
		if deadline == nil && !seen {
			eligible = append(eligible, id)
		}
		unavailable.Members = append(unavailable.Members, UnavailableConnection{ID: id, AvailableAt: deadline})
	}
	if len(eligible) == 0 {
		return nil, unavailable
	}
	return eligible, nil
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
	return r.prepare(ctx, settings, allocate, nil)
}

func (r *ConnectionRotation) prepare(ctx context.Context, settings config.Settings, allocate bool, visited map[config.ConnectionID]struct{}) (config.ConnectionID, error) {
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
		snapshot := r.assignments.Load()
		members, err = snapshot.eligible(members, visited)
		if err != nil {
			return "", err
		}
		return snapshot.leastConsumed(members, scores), nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	next := r.assignments.Load().clone()
	maps.DeleteFunc(next.lastAssigned, func(id config.ConnectionID, _ uint64) bool {
		_, configured := settings.Connections[id]
		return !configured
	})
	maps.DeleteFunc(next.cooldowns, func(id config.ConnectionID, deadline time.Time) bool {
		_, configured := settings.Connections[id]
		return !configured || !deadline.After(time.Now())
	})
	members, err = next.eligible(members, visited)
	if err != nil {
		return "", err
	}
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

func (p RunPromptPreparationContext) prepareConnection(ctx context.Context, settings *config.Settings, source config.SourceReport, scope config.ConnectionScope) error {
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
	id, _, err := p.Rotation.ResolveSessionConnection(ctx, *settings, source, scope, saved, p.AllocateConnection)
	if err != nil {
		return err
	}
	settings.Connection = config.SingleConnection(id)
	return projectSessionConnection(settings, source, session.Meta{ConnectionID: &id, Locked: p.ModelLock})
}

// ResolveSessionConnection projects a binding without changing Session metadata.
// Only a removed definition permits ordinary resume to select a replacement.
func ResolveSessionConnection(settings config.Settings, source config.SourceReport, scope config.ConnectionScope, saved *config.ConnectionID) (config.ConnectionID, *config.ConnectionReplacement, error) {
	return (*ConnectionRotation)(nil).ResolveSessionConnection(context.Background(), settings, source, scope, saved, false)
}

func (r *ConnectionRotation) ResolveSessionConnection(
	ctx context.Context,
	settings config.Settings,
	source config.SourceReport,
	scope config.ConnectionScope,
	saved *config.ConnectionID,
	allocate bool,
) (config.ConnectionID, *config.ConnectionReplacement, error) {
	if saved != nil {
		if _, err := config.ParseConnectionID(string(*saved)); err != nil {
			return "", nil, err
		}
		if _, present := settings.Connections[*saved]; present {
			return *saved, nil, nil
		}
	}
	candidates := settings
	if allocate || saved != nil {
		candidates, _, _ = sessionConnectionCandidates(settings, source, scope)
	}
	selected, err := r.Prepare(ctx, candidates, allocate)
	if err != nil {
		return "", nil, err
	}
	if saved == nil {
		return selected, nil, nil
	}
	return selected, &config.ConnectionReplacement{Previous: *saved, Current: selected}, nil
}

func BindSessionConnection(store *session.Store, settings *config.Settings, sources map[string]config.Origin, scope config.ConnectionScope) (*config.ConnectionReplacement, error) {
	return (*ConnectionRotation)(nil).BindSessionConnection(context.Background(), store, settings, sources, scope)
}

func (r *ConnectionRotation) BindSessionConnection(ctx context.Context, store *session.Store, settings *config.Settings, sources map[string]config.Origin, scope config.ConnectionScope) (*config.ConnectionReplacement, error) {
	saved := store.Meta().ConnectionID
	source := config.SourceReport{Sources: sources}
	_, _, fallback := sessionConnectionCandidates(*settings, source, scope)
	var selected config.ConnectionID
	var replacement *config.ConnectionReplacement
	selectedResolved := false
	if fallback {
		if planned, err := settings.Connection.ConcreteID(); err == nil {
			_, plannedDefined := settings.Connections[*planned]
			savedDefined := false
			if saved != nil {
				_, savedDefined = settings.Connections[*saved]
			}
			if plannedDefined && !savedDefined {
				// Session planning already selected this connection. Bind that
				// result rather than allocating from the fallback candidates again.
				selected = *planned
				if saved != nil {
					replacement = &config.ConnectionReplacement{Previous: *saved, Current: selected}
				}
				selectedResolved = true
			}
		}
	}
	if !selectedResolved {
		var err error
		selected, replacement, err = r.ResolveSessionConnection(ctx, *settings, source, scope, saved, true)
		if err != nil {
			return nil, err
		}
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
	selected, _, err := ResolveSessionConnection(
		*settings, source, config.ConnectionScope{Selection: settings.Connection}, meta.ConnectionID,
	)
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

func sessionConnectionCandidates(
	settings config.Settings,
	source config.SourceReport,
	scope config.ConnectionScope,
) (config.Settings, config.ConnectionID, bool) {
	if scope.UnavailableRole != nil || scope.Selection == nil || len(*scope.Selection) != 1 {
		return settings, "", false
	}
	origin, present := source.Sources["connection"]
	if !present || origin.Property.Role != nil {
		return settings, "", false
	}
	defaultID := (*scope.Selection)[0]
	if _, present := settings.Connections[defaultID]; present {
		return settings, "", false
	}
	members := make(config.ConnectionSelection, 0, len(settings.Connections))
	for _, id := range settings.ConnectionOrder {
		if _, present := settings.Connections[id]; present {
			members = append(members, id)
		}
	}
	if len(members) == 0 {
		return settings, "", false
	}
	candidates := settings
	candidates.Connection = &members
	return candidates, defaultID, true
}

func subagentConnectionSource(source config.SourceReport, role config.SubagentRole) config.SourceReport {
	origin, present := role.Sources["connection"]
	if !present || source.Sources["connection"].OverridesRole("connection") {
		return source
	}
	source.Sources = maps.Clone(source.Sources)
	if source.Sources == nil {
		source.Sources = make(map[string]config.Origin)
	}
	source.Sources["connection"] = origin
	return source
}
