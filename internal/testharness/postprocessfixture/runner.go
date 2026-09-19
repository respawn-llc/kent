package postprocessfixture

import (
	"testing"

	"core/server/tools/shell/postprocess"
)

func NewRunner(t testing.TB, settings postprocess.Settings) *postprocess.Runner {
	t.Helper()
	runner, err := postprocess.NewRunner(settings)
	if err != nil {
		t.Fatalf("new shell postprocessor: %v", err)
	}
	return runner
}
