package llm

import (
	"core/shared/textutil"
	"strings"
	"testing"
)

func TestNormalizeReasoningSummaryTextPreservesBoldMarkers(t *testing.T) {
	text := normalizeReasoningSummaryLines(strings.Split(strings.ReplaceAll("**Preparing patch**\n\nI am exploring options.\n**Running checks**", "\r\n", "\n"), "\n"))
	if text != "**Preparing patch**\n\nI am exploring options.\n**Running checks**" {
		t.Fatalf("unexpected normalized text: %q", text)
	}
}

func TestReasoningSummaryDeltaFromTextCarriesCurrentStatus(t *testing.T) {
	delta := reasoningSummaryDeltaFromText(nil, nil, "reasoning", "**Checking tests**")
	if delta.Text != "" {
		t.Fatalf("unexpected delta text: %q", delta.Text)
	}
	if delta.CurrentStatus == nil || delta.CurrentStatus.Text != "Checking tests" {
		t.Fatalf("unexpected current status: %+v", delta.CurrentStatus)
	}
}

func TestReasoningSummaryDeltaFromTextPreservesRawTraceBody(t *testing.T) {
	text := "\r\n\r\n**Checking tests**\r\n\r\n\r\nDetails\r\n"
	delta := reasoningSummaryDeltaFromText(nil, nil, "reasoning", text)
	want := "\r\n\r\n\r\n\r\n\r\nDetails\r\n"
	if delta.Text != want {
		t.Fatalf("delta text = %q, want raw body %q", delta.Text, want)
	}
}

func TestReasoningSummaryDeltaFromTextRejectsIncompleteStatus(t *testing.T) {
	delta := reasoningSummaryDeltaFromText(nil, nil, "reasoning", "**Checking tests")
	if delta.CurrentStatus != nil {
		t.Fatalf("unexpected current status: %+v", delta.CurrentStatus)
	}
}

func TestReasoningSummaryDeltaFromTextRejectsEmptyStatus(t *testing.T) {
	for _, text := range []string{"****", "**   **"} {
		delta := reasoningSummaryDeltaFromText(nil, nil, "reasoning", text)
		if delta.CurrentStatus != nil {
			t.Fatalf("text %q produced unexpected current status: %+v", text, delta.CurrentStatus)
		}
	}
}

func TestReasoningSummaryDeltaFromTextRejectsNonStrongMarkdown(t *testing.T) {
	for _, text := range []string{"Checking tests", "`**Checking tests**`"} {
		delta := reasoningSummaryDeltaFromText(nil, nil, "reasoning", text)
		if delta.CurrentStatus != nil {
			t.Fatalf("text %q produced unexpected current status: %+v", text, delta.CurrentStatus)
		}
	}
}

func TestReasoningSummaryDeltaFromTextRejectsLinkedStatus(t *testing.T) {
	for _, text := range []string{
		"[**Checking tests**](https://example.com)",
		"**[Checking tests](https://example.com)**",
	} {
		delta := reasoningSummaryDeltaFromText(nil, nil, "reasoning", text)
		if delta.CurrentStatus != nil {
			t.Fatalf("text %q produced unexpected current status: %+v", text, delta.CurrentStatus)
		}
	}
}

func TestReasoningSummaryDeltaFromTextRejectsStatusContainingNestedMarkup(t *testing.T) {
	for _, text := range []string{
		"**Checking _tests_**",
		"**`Checking tests`**",
		"**Checking ~~tests~~**",
	} {
		delta := reasoningSummaryDeltaFromText(nil, nil, "reasoning", text)
		if delta.CurrentStatus != nil {
			t.Fatalf("text %q produced unexpected current status: %+v", text, delta.CurrentStatus)
		}
	}
}

func TestReasoningSummaryDeltaFromTextUsesFirstValidStatus(t *testing.T) {
	delta := reasoningSummaryDeltaFromText(
		nil,
		nil,
		"reasoning",
		"**[ignored](https://example.com)** then **Checking tests** then **Writing summary**",
	)
	if delta.CurrentStatus == nil || delta.CurrentStatus.Text != "Checking tests" {
		t.Fatalf("unexpected current status: %+v", delta.CurrentStatus)
	}
	if delta.Text != "**[ignored](https://example.com)** then  then **Writing summary**" {
		t.Fatalf("unrecognized markup or later trace emphasis changed: %q", delta.Text)
	}
}

func TestReasoningSummaryDeltaFromTextRemovesOnlyRecognizedSourceSpan(t *testing.T) {
	for _, tc := range []struct {
		text string
		want string
	}{
		{"`**Checking tests**` then **Checking tests** tail", "`**Checking tests**` then  tail"},
		{"prefix __Проверка 🧪__ suffix", "prefix  suffix"},
		{"**Checking tests**\n\n`code` and _trace_", "\n\n`code` and _trace_"},
		{"**Checking tests", "**Checking tests"},
		{"**Checking _tests_**", "**Checking _tests_**"},
	} {
		t.Run(tc.text, func(t *testing.T) {
			delta := reasoningSummaryDeltaFromText(nil, nil, "reasoning", tc.text)
			if delta.Text != tc.want {
				t.Fatalf("trace = %q, want %q", delta.Text, tc.want)
			}
		})
	}
}

func TestReasoningSummaryDeltaFromTextTrimsStatusWhitespace(t *testing.T) {
	delta := reasoningSummaryDeltaFromText(nil, nil, "reasoning", "\n  **Checking tests**  \n")
	if delta.CurrentStatus == nil || delta.CurrentStatus.Text != "Checking tests" {
		t.Fatalf("unexpected current status: %+v", delta.CurrentStatus)
	}
}

func TestNormalizeReasoningEntriesOmitsStatusOnlyReasoningEntries(t *testing.T) {
	got := normalizeReasoningEntries([]ReasoningEntry{{Role: textutil.Value("reasoning"), Text: "**Preparing patch**"}})
	if len(got) != 0 {
		t.Fatalf("status became a reasoning trace: %+v", got)
	}
}
