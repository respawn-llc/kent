package runprompt

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

func TestObservationDeadlineSurvivesObserverCompletion(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		scope := newObservationScope(t.Context())
		defer scope.close()
		started := time.Now()
		scope.armTimeout(2 * time.Second)
		scope.stopObserver()
		if scope.ctx.Err() != nil {
			t.Fatal("observer completion canceled queued work")
		}
		<-scope.ctx.Done()
		if !errors.Is(context.Cause(scope.ctx), context.DeadlineExceeded) {
			t.Fatalf("completion cause = %v, want deadline exceeded", context.Cause(scope.ctx))
		}
		if elapsed := time.Since(started); elapsed != 2*time.Second {
			t.Fatalf("unexpected virtual elapsed time: %v", elapsed)
		}
	})
}
