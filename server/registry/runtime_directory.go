package registry

import (
	"strings"
	"sync"

	"core/server/runtime"
)

type runtimeDirectory struct {
	mu      sync.RWMutex
	entries map[string]*runtimeEntry
}

type runtimeEntry struct {
	engine          *runtime.Engine
	sessionActivity *sessionActivityBroker
	promptActivity  *promptActivityBroker
	pendingPrompts  *pendingPromptStore
}

func newRuntimeDirectory() *runtimeDirectory {
	return &runtimeDirectory{entries: make(map[string]*runtimeEntry)}
}

func newRuntimeEntry(engine *runtime.Engine) *runtimeEntry {
	return &runtimeEntry{
		engine:          engine,
		sessionActivity: newSessionActivityBroker(),
		promptActivity:  newPromptActivityBroker(),
		pendingPrompts:  newPendingPromptStore(),
	}
}

func (d *runtimeDirectory) Register(sessionID string, engine *runtime.Engine) *runtimeEntry {
	if d == nil || engine == nil {
		return nil
	}
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return nil
	}
	entry := newRuntimeEntry(engine)
	d.mu.Lock()
	previous := d.entries[id]
	d.entries[id] = entry
	d.mu.Unlock()
	return previous
}

func (d *runtimeDirectory) Unregister(sessionID string, engine *runtime.Engine) (string, *runtimeEntry) {
	if d == nil {
		return "", nil
	}
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return "", nil
	}
	d.mu.Lock()
	entry := d.entries[id]
	if entry == nil || (engine != nil && entry.engine != engine) {
		d.mu.Unlock()
		return "", nil
	}
	delete(d.entries, id)
	d.mu.Unlock()
	return id, entry
}

func (d *runtimeDirectory) Resolve(sessionID string) *runtime.Engine {
	entry := d.Entry(sessionID)
	if entry == nil {
		return nil
	}
	return entry.engine
}

func (d *runtimeDirectory) Active(sessionID string) bool {
	return d.Entry(sessionID) != nil
}

func (d *runtimeDirectory) IDs() []string {
	if d == nil {
		return nil
	}
	d.mu.RLock()
	ids := make([]string, 0, len(d.entries))
	for id := range d.entries {
		ids = append(ids, id)
	}
	d.mu.RUnlock()
	return ids
}

func (d *runtimeDirectory) Entry(sessionID string) *runtimeEntry {
	if d == nil {
		return nil
	}
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return nil
	}
	d.mu.RLock()
	entry := d.entries[id]
	d.mu.RUnlock()
	return entry
}

func closeRuntimeEntry(entry *runtimeEntry, err error) {
	if entry == nil {
		return
	}
	entry.pendingPrompts.Close(err)
	entry.promptActivity.Close(err)
	entry.sessionActivity.Close(err)
}
