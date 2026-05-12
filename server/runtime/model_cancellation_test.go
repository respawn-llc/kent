package runtime

import (
	"context"
	"errors"
	"testing"

	"builder/server/llm"
	"builder/server/session"
	"builder/server/tools"
)

type cancelAwareModelClient struct {
	started chan struct{}
}

func (c cancelAwareModelClient) Generate(ctx context.Context, _ llm.Request) (llm.Response, error) {
	close(c.started)
	<-ctx.Done()
	return llm.Response{}, ctx.Err()
}

func TestGenerateWithRetryPropagatesContextCancellation(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	started := make(chan struct{})
	client := cancelAwareModelClient{started: started}
	eng, err := New(store, client, tools.NewRegistry(), Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := eng.generateWithRetry(ctx, "step-1", llm.Request{Model: "gpt-5"}, nil, nil, nil)
		done <- err
	}()

	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("generateWithRetry error=%v, want context.Canceled", err)
	}
}
