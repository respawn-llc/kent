package session

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
)

// The database sequence is the commit decision. Tail bytes are only candidates
// until that decision includes them, even when they form valid event records.
func openCommittedEventLog(path string, mode currentEventLogMode, sequence int64) (*currentEventLog, error) {
	return openCurrentEventLogAtSequence(path, mode, &sequence)
}

func committedEventLogEnd(fp *os.File, size, firstOffset, sequence int64, version int) (int64, error) {
	if sequence < 0 {
		return 0, errors.New("committed event sequence must be non-negative")
	}
	if sequence == 0 {
		return firstOffset, nil
	}
	// A missing commit boundary must not turn reopening into a full-history
	// scan. The previous writer also bounded its entire outstanding suffix.
	const maxUncommittedTailBytes = int64(32 << 20)
	limit := max(firstOffset, size-maxUncommittedTailBytes)
	tail := io.NewSectionReader(fp, limit, size-limit)
	end := size - limit
	for end > 0 {
		line, start, terminated, err := readPreviousCurrentEventLine(tail, end, 0)
		if err != nil {
			return 0, err
		}
		record, err := decodeEventRecordForVersion(version, bytes.TrimSpace(line))
		if err == nil {
			if record.Seq() == sequence && terminated {
				return limit + end, nil
			}
			if record.Seq() < sequence {
				break
			}
		}
		end = start
	}
	return 0, fmt.Errorf("committed event sequence %d is missing from the bounded event-log tail", sequence)
}
