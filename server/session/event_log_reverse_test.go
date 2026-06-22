package session

import (
	"os"
	"path/filepath"
	"reflect"
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
