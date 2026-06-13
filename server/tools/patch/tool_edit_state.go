package patch

import (
	"regexp"

	patchformat "core/shared/transcript/patchformat"
)

const hunkMaxFuzz = 8

var unifiedHunkHeaderPattern = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@(?: .*)?$`)

type editHunk struct {
	header    hunkHeader
	changes   []patchformat.ChangeLine
	endOfFile bool
}

type hunkHeader struct {
	hasPosition bool
	context     string
	oldStart    int
	oldCount    int
	newStart    int
	newCount    int
}
