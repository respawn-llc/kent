package runtime

import (
	"testing"
	"time"
)

func TestIdleResumeEligibility(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	threshold := 3600
	for _, test := range []struct {
		name string
		age  time.Duration
		want bool
	}{
		{"recent", time.Minute, false},
		{"equal", time.Hour, false},
		{"older", time.Hour + time.Nanosecond, true},
		{"backward", -time.Hour, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			last := now.Add(-test.age)
			if got := idleResumeEligible(&threshold, &last, now); got != test.want {
				t.Fatalf("eligible = %v, want %v", got, test.want)
			}
		})
	}
	if idleResumeEligible(&threshold, nil, now) || idleResumeEligible(nil, &now, now.Add(2*time.Hour)) {
		t.Fatal("absent activity or disabled policy qualified")
	}
}

func TestIdleResumeDuration(t *testing.T) {
	for _, test := range []struct {
		elapsed time.Duration
		want    string
	}{
		{59 * time.Second, "0m"},
		{24*time.Minute + 59*time.Second, "24m"},
		{time.Hour, "1h"},
		{(17*24+8)*time.Hour + 24*time.Minute + 59*time.Second, "17d8h24m"},
		{48 * time.Hour, "2d"},
	} {
		if got := formatIdleResumeDuration(test.elapsed); got != test.want {
			t.Fatalf("%v formatted as %q, want %q", test.elapsed, got, test.want)
		}
	}
}
