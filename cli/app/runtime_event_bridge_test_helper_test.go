package app

import (
	"builder/server/runtime"
	"builder/server/runtimewire"
)

type runtimeEventBridge = runtimewire.EventBridge

func newRuntimeEventBridge(buffer int, onDrop func(total uint64, evt runtime.Event)) *runtimeEventBridge {
	return runtimewire.NewEventBridge(buffer, onDrop)
}
