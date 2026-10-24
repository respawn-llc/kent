package registry

func RecommendedOptionIndexFromSnapshot(index int) *int {
	if index == 0 {
		return nil
	}
	return &index
}
