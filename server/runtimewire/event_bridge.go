package runtimewire

import (
	"sync/atomic"

	"core/server/runtime"
)

type EventBridge struct {
	Events    <-chan runtime.Event
	GapEvents <-chan struct{}
	Dropped   atomic.Uint64
	events    chan runtime.Event
	gapEvents chan struct{}
	onDrop    func(total uint64, evt runtime.Event)
}

func NewEventBridge(buffer int, onDrop func(total uint64, evt runtime.Event)) *EventBridge {
	if buffer <= 0 {
		buffer = 1
	}
	events := make(chan runtime.Event, buffer)
	gapEvents := make(chan struct{}, 1)
	return &EventBridge{
		Events:    events,
		GapEvents: gapEvents,
		events:    events,
		gapEvents: gapEvents,
		onDrop:    onDrop,
	}
}

func (b *EventBridge) Publish(evt runtime.Event) {
	select {
	case b.events <- evt:
	default:
		total := b.Dropped.Add(1)
		select {
		case b.gapEvents <- struct{}{}:
		default:
		}
		if b.onDrop != nil {
			b.onDrop(total, evt)
		}
	}
}
