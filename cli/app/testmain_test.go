package app

import (
	"os"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	previousDuration := transientStatusDuration
	previousStrictMode := defaultTUIStrictIOMode
	transientStatusDuration = 30 * time.Millisecond
	defaultTUIStrictIOMode = tuiStrictIOModePanic
	code := m.Run()
	transientStatusDuration = previousDuration
	defaultTUIStrictIOMode = previousStrictMode
	os.Exit(code)
}
