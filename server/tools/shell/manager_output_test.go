package shell

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestManagerCollectUntilTreatsExitedProcessAsIncompleteUntilOutputFinalized(t *testing.T) {
	entry := &processEntry{
		notify:          make(chan struct{}, 1),
		outputFinalized: make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := (&Manager{}).collectUntil(ctx, entry, time.Now().Add(time.Second))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("collectUntil error = %v, want context canceled while output remains unfinalized", err)
	}
}
