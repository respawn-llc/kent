package sessionnaming

import (
	"context"
	"errors"
	"testing"

	"core/server/runtime"
	"core/shared/config"
	"core/shared/runtimeids"
)

func TestNamingFailureDeliveryMode(t *testing.T) {
	for _, headless := range []bool{false, true} {
		t.Run(map[bool]string{false: "interactive", true: "headless"}[headless], func(t *testing.T) {
			delivered := false
			service := NewService(nil, nil, func(string) (config.App, error) {
				return config.App{}, errors.New("configuration unavailable")
			}, nil, func(context.Context, string, error) { delivered = true })
			service.Submit(runtimeids.NewSessionID(), t.TempDir(), runtime.SessionNamingInput{
				Task: "Fix totals", Headless: headless,
			})
			if delivered == headless {
				t.Fatalf("headless=%t, attention delivered=%t", headless, delivered)
			}
		})
	}
}
