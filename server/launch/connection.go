package launch

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"core/server/llm"
	"core/server/session"
	"core/shared/config"
)

type ConnectionRotation struct {
	mu       sync.Mutex
	cooldown time.Duration
	// Immutable published order lets data-only previews avoid waiting for allocations.
	assignments atomic.Pointer[connectionAssignments]
}

type connectionAssignments struct {
	lastAssigned map[config.ConnectionID]uint64
	cooldowns    map[config.ConnectionID]time.Time
	sequence     uint64
}

func NewConnectionRotation(cooldown time.Duration) *ConnectionRotation {
	return &ConnectionRotation{cooldown: cooldown}
}

func (r *ConnectionRotation) FallbackEnabled() bool { return r != nil && r.cooldown > 0 }

func (r *ConnectionRotation) CoolingDown(id config.ConnectionID) bool {
	if !r.FallbackEnabled() {
		return false
	}
	snapshot := r.assignments.Load()
	return snapshot != nil && snapshot.cooldowns[id].After(time.Now())
}

func (r *ConnectionRotation) SelectReplacement(settings config.Settings, visited map[config.ConnectionID]struct{}) (config.ConnectionID, error) {
	return r.prepare(settings, true, visited)
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

type UnavailableConnection struct {
	ID          config.ConnectionID
	AvailableAt *time.Time
}

type ConnectionsUnavailableError struct {
	Members []UnavailableConnection
	Causes  []error
}

func (e *ConnectionsUnavailableError) Error() string {
	return fmt.Sprintf("configured connections unavailable: %v; retry after cooldown or update the configured connections", e.Members)
}

func (e *ConnectionsUnavailableError) Unwrap() []error { return e.Causes }

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

func (r *ConnectionRotation) Prepare(settings config.Settings, allocate bool) (config.ConnectionID, error) {
	return r.prepare(settings, allocate, nil)
}

func (r *ConnectionRotation) prepare(settings config.Settings, allocate bool, visited map[config.ConnectionID]struct{}) (config.ConnectionID, error) {
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
	if !allocate {
		snapshot := r.assignments.Load()
		members, err = snapshot.eligible(members, visited)
		if err != nil {
			return "", err
		}
		return snapshot.leastRecentlyAssigned(members), nil
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
	selected := next.leastRecentlyAssigned(members)
	next.sequence++
	next.lastAssigned[selected] = next.sequence
	r.assignments.Store(next)
	return selected, nil
}

func (r *connectionAssignments) leastRecentlyAssigned(members []config.ConnectionID) config.ConnectionID {
	selected := members[0]
	if r == nil {
		return selected
	}
	for _, member := range members {
		sequence, assigned := r.lastAssigned[member]
		if !assigned {
			return member
		}
		if sequence < r.lastAssigned[selected] {
			selected = member
		}
	}
	return selected
}

func (p RunPromptPreparationContext) prepareConnection(settings *config.Settings, source config.SourceReport) error {
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
	id, _, err := p.Rotation.ResolveSessionConnection(*settings, saved, p.AllocateConnection)
	if err != nil {
		return err
	}
	settings.Connection = config.SingleConnection(id)
	return projectSessionConnection(settings, source, session.Meta{ConnectionID: &id, Locked: p.ModelLock})
}

// ResolveSessionConnection projects a binding without changing Session metadata.
// Only a removed definition permits ordinary resume to select a replacement.
func ResolveSessionConnection(settings config.Settings, saved *config.ConnectionID) (config.ConnectionID, *config.ConnectionReplacement, error) {
	return (*ConnectionRotation)(nil).ResolveSessionConnection(settings, saved, false)
}

func (r *ConnectionRotation) ResolveSessionConnection(settings config.Settings, saved *config.ConnectionID, allocate bool) (config.ConnectionID, *config.ConnectionReplacement, error) {
	if saved != nil {
		if _, err := config.ParseConnectionID(string(*saved)); err != nil {
			return "", nil, err
		}
		if _, present := settings.Connections[*saved]; present {
			return *saved, nil, nil
		}
	}
	selected, err := r.Prepare(settings, allocate)
	if err != nil {
		return "", nil, err
	}
	if saved == nil {
		return selected, nil, nil
	}
	return selected, &config.ConnectionReplacement{Previous: *saved, Current: selected}, nil
}

func BindSessionConnection(store *session.Store, settings *config.Settings, sources map[string]config.Origin) (*config.ConnectionReplacement, error) {
	return (*ConnectionRotation)(nil).BindSessionConnection(store, settings, sources)
}

func (r *ConnectionRotation) BindSessionConnection(store *session.Store, settings *config.Settings, sources map[string]config.Origin) (*config.ConnectionReplacement, error) {
	saved := store.Meta().ConnectionID
	selected, replacement, err := r.ResolveSessionConnection(*settings, saved, true)
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
