package session

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func matchEventKind(kind string) func(Event) bool {
	return func(evt Event) bool { return evt.Kind == kind }
}

func eventKinds(events []Event) []string {
	kinds := make([]string, 0, len(events))
	for _, evt := range events {
		kinds = append(kinds, evt.Kind)
	}
	return kinds
}

func TestReadEventsBackwardUntilReturnsActiveTail(t *testing.T) {
	store := newSessionTestStore(t)
	for _, e := range []struct{ kind, body string }{
		{"message", "a"},
		{"message", "b"},
		{"history_replaced", "hr"},
		{"message", "c"},
		{"message", "d"},
	} {
		if _, _, err := store.AppendEvent("step", e.kind, map[string]string{"v": e.body}); err != nil {
			t.Fatalf("append %s: %v", e.kind, err)
		}
	}

	events, err := store.ReadEventsBackwardUntil(matchEventKind("history_replaced"))
	if err != nil {
		t.Fatalf("read backward: %v", err)
	}
	if got, want := eventKinds(events), []string{"history_replaced", "message", "message"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("active tail kinds = %v, want %v", got, want)
	}
}

func TestReadEventsBackwardUntilNoMatchReturnsAllEvents(t *testing.T) {
	store := newSessionTestStore(t)
	for i := 0; i < 3; i++ {
		if _, _, err := store.AppendEvent("step", "message", map[string]int{"v": i}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	events, err := store.ReadEventsBackwardUntil(matchEventKind("history_replaced"))
	if err != nil {
		t.Fatalf("read backward: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("no-match read returned %d events, want all 3", len(events))
	}
}

func TestReadEventsBackwardWindowPaginatesWholeFileViaCursor(t *testing.T) {
	const n = 25
	var sb strings.Builder
	for i := 1; i <= n; i++ {
		sb.WriteString(fmt.Sprintf(`{"seq":%d,"kind":"message","payload":{"v":%d}}`+"\n", i, i))
	}
	sb.WriteString(`{"seq":99,"kind":"message","pa`)
	path := filepath.Join(t.TempDir(), eventsFile)
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("write events file: %v", err)
	}

	for _, chunk := range []int64{1, 5, 13, 1 << 20} {
		for _, pageSize := range []int{1, 4, 7, n, n + 5} {
			var collected []Event
			endOffset := int64(0)
			for pages := 0; ; pages++ {
				if pages > n+2 {
					t.Fatalf("chunk=%d page=%d: pagination did not terminate", chunk, pageSize)
				}
				window, err := readEventsBackwardWindowFile(path, endOffset, pageSize, chunk)
				if err != nil {
					t.Fatalf("chunk=%d page=%d: read window: %v", chunk, pageSize, err)
				}
				if len(window.Events) > pageSize {
					t.Fatalf("chunk=%d page=%d: window returned %d entries, exceeds page", chunk, pageSize, len(window.Events))
				}
				collected = append(append([]Event(nil), window.Events...), collected...)
				if window.ReachedStart {
					break
				}
				if window.StartOffset <= 0 || window.StartOffset >= endOffsetOrSize(t, path, endOffset) {
					t.Fatalf("chunk=%d page=%d: cursor did not advance (start=%d end=%d)", chunk, pageSize, window.StartOffset, endOffset)
				}
				endOffset = window.StartOffset
			}
			if len(collected) != n {
				t.Fatalf("chunk=%d page=%d: reconstructed %d events, want %d (torn tail must be dropped)", chunk, pageSize, len(collected), n)
			}
			for i := range collected {
				if collected[i].Seq != int64(i+1) {
					t.Fatalf("chunk=%d page=%d: event %d seq = %d, want %d", chunk, pageSize, i, collected[i].Seq, i+1)
				}
			}
		}
	}
}

func endOffsetOrSize(t *testing.T, path string, endOffset int64) int64 {
	t.Helper()
	if endOffset > 0 {
		return endOffset
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	return info.Size()
}

func TestReadEventsBackwardUntilFileReassemblesAcrossChunksAndToleratesTornTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), eventsFile)
	content := `{"seq":1,"kind":"message","payload":{"v":"a"}}
{"seq":2,"kind":"history_replaced","payload":{"v":"hr"}}
{"seq":3,"kind":"message","payload":{"v":"c"}}
{"seq":4,"kind":"message","payload":{"v":"d"}}
{"seq":5,"kind":"message","pa`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write events file: %v", err)
	}

	for _, chunk := range []int64{1, 3, 7, 64, 1 << 20} {
		events, err := readEventsBackwardUntilFile(path, chunk, matchEventKind("history_replaced"))
		if err != nil {
			t.Fatalf("read backward (chunk=%d): %v", chunk, err)
		}
		if got, want := eventKinds(events), []string{"history_replaced", "message", "message"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("chunk=%d active tail kinds = %v, want %v (torn trailing line must be dropped)", chunk, got, want)
		}
	}
}
