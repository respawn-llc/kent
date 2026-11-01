package metadata_test

import (
	"context"
	"testing"
	"time"

	"core/internal/testharness/testsetup"
	"core/shared/config"
)

func TestConnectionConsumptionAccumulates(t *testing.T) {
	store := testsetup.OpenStore(t, t.TempDir())
	ctx := context.Background()
	now := time.Now()
	for _, score := range []int64{12, 30} {
		if err := store.RecordConnectionConsumption(ctx, config.ConnectionID("a"), now, score); err != nil {
			t.Fatal(err)
		}
	}
	totals, err := store.ConnectionConsumption(ctx, []config.ConnectionID{"a", "b"}, now)
	if err != nil || totals["a"] != 42 || totals["b"] != 0 {
		t.Fatalf("totals = %v, error = %v", totals, err)
	}
}

func TestConnectionConsumptionExpiresWithoutWrites(t *testing.T) {
	store := testsetup.OpenStore(t, t.TempDir())
	ctx := context.Background()
	now := time.Date(2026, 10, 15, 18, 35, 0, 0, time.FixedZone("offset", 2*60*60))
	cutoff := now.UTC().Add(-7 * 24 * time.Hour).Truncate(time.Hour)
	for _, sample := range []struct {
		at    time.Time
		score int64
	}{
		{cutoff.Add(-time.Hour), 99},
		{cutoff, 12},
		{now, 30},
	} {
		if err := store.RecordConnectionConsumption(ctx, "a", sample.at, sample.score); err != nil {
			t.Fatal(err)
		}
	}
	for _, sample := range []struct {
		at   time.Time
		want int64
	}{
		{now, 42},
		{now.Add(time.Hour), 30},
		{now.Add(8 * 24 * time.Hour), 0},
	} {
		totals, err := store.ConnectionConsumption(ctx, []config.ConnectionID{"a"}, sample.at)
		if err != nil || totals["a"] != sample.want {
			t.Fatalf("at %v: totals = %v, want %d, error = %v", sample.at, totals, sample.want, err)
		}
	}
}

func TestConnectionConsumptionReloads(t *testing.T) {
	root := t.TempDir()
	open := testsetup.StoreOpener(t, root)
	store := open()
	ctx := context.Background()
	now := time.Now()
	totals, err := store.ConnectionConsumption(ctx, []config.ConnectionID{"a"}, now)
	if err != nil || len(totals) != 0 {
		t.Fatalf("fresh history = %v, error = %v", totals, err)
	}
	if err := store.RecordConnectionConsumption(ctx, "a", now, 42); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = open()
	totals, err = store.ConnectionConsumption(ctx, []config.ConnectionID{"a"}, now)
	if err != nil || totals["a"] != 42 {
		t.Fatalf("reloaded totals = %v, error = %v", totals, err)
	}
}
