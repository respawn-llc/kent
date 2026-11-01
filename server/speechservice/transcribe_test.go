package speech

import "testing"

func TestTranscriptionNoOp(t *testing.T) {
	for _, text := range []string{"", " \n\t", "？！—…", "🎙️✨", "$ + ="} {
		if result := Normalize(text); result.GetNoOp() == nil {
			t.Errorf("expected successful no-op for %q", text)
		}
	}
	for _, text := range []string{"  Привет!\n", "你好", "مرحبا", "१२३", "42 🎙️"} {
		if result := Normalize(text); result.GetTranscript() != text {
			t.Errorf("provider text changed: %q", text)
		}
	}
}
