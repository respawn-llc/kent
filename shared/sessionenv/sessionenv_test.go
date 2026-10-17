package sessionenv

import "testing"

func TestLookupSessionID(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
		ok   bool
	}{
		{name: "missing"},
		{name: "blank", env: map[string]string{SessionIDEnv: " \t\n"}},
		{name: "trims", env: map[string]string{SessionIDEnv: " session-1 \n"}, want: "session-1", ok: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := LookupSessionID(func(key string) (string, bool) {
				value, exists := tt.env[key]
				return value, exists
			})
			if got != tt.want || ok != tt.ok {
				t.Fatalf("LookupSessionID = (%q, %t), want (%q, %t)", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestLookupSessionIDNilLookup(t *testing.T) {
	if got, ok := LookupSessionID(nil); got != "" || ok {
		t.Fatalf("LookupSessionID(nil) = (%q, %t), want empty false", got, ok)
	}
}

func TestLookupStepID(t *testing.T) {
	stepID, ok := LookupStepID(func(key string) (string, bool) {
		if key == StepIDEnv {
			return " step-1 ", true
		}
		return "", false
	})
	if !ok || stepID != "step-1" {
		t.Fatalf("LookupStepID = (%q, %t), want step-1 true", stepID, ok)
	}
}
