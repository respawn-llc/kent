package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrUnknownAction is returned when an action id has no registered handler.
// It marks an unrecoverable (fatal) dispatch failure; match it with errors.Is.
var ErrUnknownAction = errors.New("unknown action id")

type Handler func(ctx context.Context, payload json.RawMessage) error

type Registry struct {
	handlers map[string]Handler
}

func NewRegistry() *Registry {
	return &Registry{handlers: map[string]Handler{}}
}

func (r *Registry) Execute(ctx context.Context, id string, payload json.RawMessage) error {
	h, ok := r.handlers[id]
	if !ok {
		return fmt.Errorf("fatal: %w %q", ErrUnknownAction, id)
	}
	return h(ctx, payload)
}
