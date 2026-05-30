package actions

import (
	"context"
	"encoding/json"
	"fmt"
)

type Handler func(ctx context.Context, payload json.RawMessage) error

type Registry struct {
	handlers map[string]Handler
}

func NewRegistry() *Registry {
	return &Registry{handlers: map[string]Handler{}}
}

func (r *Registry) Resolve(id string) (Handler, bool) {
	h, ok := r.handlers[id]
	return h, ok
}

func (r *Registry) Execute(ctx context.Context, id string, payload json.RawMessage) error {
	h, ok := r.Resolve(id)
	if !ok {
		return fmt.Errorf("fatal: unknown action id %q", id)
	}
	return h(ctx, payload)
}
