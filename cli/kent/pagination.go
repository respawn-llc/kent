package main

import (
	"fmt"
	"io"
)

const nextOffsetLineFormat = "Next offset: `%d`"

func nextOffsetLine(offset int) string {
	return fmt.Sprintf(nextOffsetLineFormat, offset)
}

func writeNextOffset(stderr io.Writer, offset int) error {
	return writePaginationContinuation(stderr, nextOffsetLine(offset))
}

func writePaginationContinuation(stderr io.Writer, line string) error {
	_, err := fmt.Fprintln(stderr, line)
	return err
}
