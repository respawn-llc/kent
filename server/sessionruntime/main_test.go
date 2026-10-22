package sessionruntime

import (
	"os"
	"testing"

	"core/shared/imagefileio"
)

func TestMain(m *testing.M) {
	imagefileio.ExitIfWorker(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	os.Exit(m.Run())
}
