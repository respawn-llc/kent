package workflow

type ThinkingMutationKind uint8

const (
	ThinkingMutationUnchanged ThinkingMutationKind = iota
	ThinkingMutationSet
	ThinkingMutationClear
)

// ThinkingMutation carries the accepted setting action through preparation and
// assignment without encoding Clear as an invalid empty effort.
type ThinkingMutation struct {
	kind  ThinkingMutationKind
	value ThinkingValue
}

func KeepThinking() ThinkingMutation {
	return ThinkingMutation{kind: ThinkingMutationUnchanged}
}

func SetThinking(value ThinkingValue) ThinkingMutation {
	return ThinkingMutation{kind: ThinkingMutationSet, value: value}
}

func ClearThinking() ThinkingMutation {
	return ThinkingMutation{kind: ThinkingMutationClear}
}

func (mutation ThinkingMutation) Kind() ThinkingMutationKind {
	return mutation.kind
}

func (mutation ThinkingMutation) Value() ThinkingValue {
	return mutation.value
}
