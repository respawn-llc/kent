package app

import (
	"core/shared/config"
	"os"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	_ = os.Unsetenv(config.PersistenceRootEnvName)
	previousDuration := transientStatusDuration
	transientStatusDuration = 30 * time.Millisecond
	code := m.Run()
	transientStatusDuration = previousDuration
	os.Exit(code)
}
