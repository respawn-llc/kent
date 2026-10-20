package serverapi

const OffsetPaginationMaxLimit = 100

type OffsetWindow struct {
	Offset int
	Limit  int
}

func TrimOffsetLookahead[T any](window OffsetWindow, items []T) ([]T, *int) {
	if len(items) <= window.Limit {
		return items, nil
	}
	nextOffset := window.Offset + window.Limit
	return items[:window.Limit], &nextOffset
}
