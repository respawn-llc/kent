package textutil

import "math"

func ApproxTokenCount(chars int) int {
	if chars <= 0 {
		return 0
	}
	return int(math.Ceil(float64(chars) / 4.0))
}

func ApproxTextTokenCount(text string) int {
	if text == "" {
		return 0
	}
	return ApproxTokenCount(len(text))
}
