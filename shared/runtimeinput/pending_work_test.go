package runtimeinput

import "testing"

func TestNormalizePendingWorkArgumentTreatsFEFFAsWhitespace(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  string
	}{
		{"\uFEFFkeep\uFEFF\uFEFF details\uFEFF", "keep details"},
		{" \t\uFEFF\n", ""},
		{"\u0085keep\u00a0details\u0085", "keep details"},
	} {
		if got := NormalizePendingWorkArgument(tc.input); got != tc.want {
			t.Fatalf("normalized %q = %q, want %q", tc.input, got, tc.want)
		}
	}
}
