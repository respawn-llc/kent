package sessionruntime

import (
	"context"
	goruntime "runtime"
	"testing"

	"core/shared/runtimeids"
)

func TestSessionAdmissionMemoryDoesNotGrowWithHistoricalSessions(t *testing.T) {
	authority := NewAuthority(AuthorityOptions{})
	t.Cleanup(func() {
		if err := authority.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	goruntime.GC()
	var before goruntime.MemStats
	goruntime.ReadMemStats(&before)
	for range 400 {
		ids := make([]runtimeids.SessionID, 50)
		for index := range ids {
			ids[index] = runtimeids.NewSessionID()
		}
		block, err := authority.TryBlockSessionStarts(context.Background(), ids, SessionStartBlockMaintenance)
		if err != nil {
			t.Fatal(err)
		}
		if err := block.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	goruntime.GC()
	var after goruntime.MemStats
	goruntime.ReadMemStats(&after)
	goruntime.KeepAlive(authority)
	// Allow allocator/runtime noise, but not bookkeeping for all 20,000 Sessions.
	if after.HeapAlloc > before.HeapAlloc+(2<<20) {
		t.Fatalf("released Session admission retained %d bytes", after.HeapAlloc-before.HeapAlloc)
	}
}
