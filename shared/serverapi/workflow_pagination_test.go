package serverapi

import "testing"

func TestTrimOffsetLookaheadCalculatesNextOffset(t *testing.T) {
	nextOffset := 2
	items, next := TrimOffsetLookahead(OffsetWindow{Offset: 0, Limit: 2}, []string{"a", "b", "c"})
	if len(items) != 2 || items[0] != "a" || items[1] != "b" ||
		next == nil || *next != nextOffset {
		t.Fatalf("items = %v, next = %v", items, next)
	}

	items, next = TrimOffsetLookahead(OffsetWindow{Offset: 2, Limit: 2}, []string{"c"})
	if len(items) != 1 || next != nil {
		t.Fatalf("terminal items = %v, next = %v", items, next)
	}
}
