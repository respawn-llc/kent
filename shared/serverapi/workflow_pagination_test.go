package serverapi

import (
	"errors"
	"testing"
)

func TestResolveOffsetWindowDefaultsAndValidatesBounds(t *testing.T) {
	window, err := ResolveOffsetWindow(nil, nil)
	if err != nil {
		t.Fatalf("ResolveOffsetWindow defaults: %v", err)
	}
	if window.Offset != 0 || window.Limit != OffsetPaginationMaxLimit {
		t.Fatalf("default window = %+v, want offset 0 and limit %d", window, OffsetPaginationMaxLimit)
	}

	zero := 0
	limit := 1
	window, err = ResolveOffsetWindow(&zero, &limit)
	if err != nil {
		t.Fatalf("ResolveOffsetWindow explicit values: %v", err)
	}
	if window.Offset != zero || window.Limit != limit {
		t.Fatalf("explicit window = %+v, want offset %d and limit %d", window, zero, limit)
	}

	negativeOffset := -1
	zeroLimit := 0
	overLimit := OffsetPaginationMaxLimit + 1
	for _, test := range []struct {
		name   string
		offset *int
		limit  *int
		field  string
	}{
		{name: "negative offset", offset: &negativeOffset, field: "offset"},
		{name: "zero limit", limit: &zeroLimit, field: "limit"},
		{name: "limit above maximum", limit: &overLimit, field: "limit"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := ResolveOffsetWindow(test.offset, test.limit)
			var windowError *offsetWindowError
			if !errors.As(err, &windowError) || windowError.Field != test.field {
				t.Fatalf("ResolveOffsetWindow error = %v, want %s OffsetWindowError", err, test.field)
			}
		})
	}
}

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
