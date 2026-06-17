package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const eventLogScanChunkSize = int64(4096)

type parsedEvents struct {
	events             []Event
	totalBytes         int64
	lastSequence       int64
	droppedTrailingEOF bool
}

func (s *Store) bootstrapEventLogStateLocked() error {
	if !s.persisted {
		return nil
	}
	freshness := ConversationFreshnessFresh
	parsed, err := walkEventsFile(s.eventsFP, func(evt Event) error {
		freshness = advanceConversationFreshness(freshness, evt)
		return nil
	})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if writeErr := os.WriteFile(s.eventsFP, nil, 0o644); writeErr != nil {
				return fmt.Errorf("initialize missing events file: %w", writeErr)
			}
			s.eventsFileSizeBytes = 0
			s.pendingFsyncWrites = 0
			s.writesSinceCompaction = 0
			s.conversationFreshness = ConversationFreshnessFresh
			if s.meta.LastSequence != 0 {
				s.meta.LastSequence = 0
				s.meta.UpdatedAt = time.Now().UTC()
				if _, persistErr := s.persistMetaLocked(); persistErr != nil {
					return persistErr
				}
			}
			return nil
		}
		return err
	}
	s.eventsFileSizeBytes = parsed.totalBytes
	s.pendingFsyncWrites = 0
	s.writesSinceCompaction = 0
	s.conversationFreshness = freshness
	if parsed.lastSequence != s.meta.LastSequence {
		s.meta.LastSequence = parsed.lastSequence
		s.meta.UpdatedAt = time.Now().UTC()
		if _, err := s.persistMetaLocked(); err != nil {
			return err
		}
	}
	if parsed.droppedTrailingEOF {
		if err := s.compactEventsLocked(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) compactEventsIfNeededLocked() error {
	options := s.options.eventLog
	if options.compactionEveryWrites == 0 {
		return nil
	}
	if s.writesSinceCompaction < options.compactionEveryWrites {
		return nil
	}
	if s.eventsFileSizeBytes < options.compactionMinBytes {
		return nil
	}
	return s.compactEventsLocked()
}

func (s *Store) compactEventsLocked() error {
	parsed, err := readEventsFile(s.eventsFP)
	if err != nil {
		return err
	}
	if err := writeEventsFile(s.eventsFP, parsed.events); err != nil {
		return err
	}
	s.eventsFileSizeBytes = computeEventsJSONLSize(parsed.events)
	s.writesSinceCompaction = 0
	s.pendingFsyncWrites = 0
	return nil
}

func (s *Store) appendEventsLogLocked(events []Event) (int64, error) {
	fp, err := os.OpenFile(s.eventsFP, os.O_APPEND|os.O_RDWR, 0o644)
	if err != nil {
		return 0, fmt.Errorf("open events file for append: %w", err)
	}
	defer fp.Close()

	fileInfo, err := fp.Stat()
	if err != nil {
		return 0, fmt.Errorf("stat events file: %w", err)
	}
	currentSize := fileInfo.Size()
	s.eventsFileSizeBytes = currentSize

	needsSeparator, err := s.repairTrailingLineLocked(fp, currentSize)
	if err != nil {
		return 0, err
	}

	fileInfo, err = fp.Stat()
	if err != nil {
		return 0, fmt.Errorf("stat events file after repair: %w", err)
	}
	currentSize = fileInfo.Size()
	s.eventsFileSizeBytes = currentSize

	payload, err := encodeEventLines(events, needsSeparator)
	if err != nil {
		return 0, err
	}
	writtenBytes, err := writeAll(fp, payload)
	if err != nil {
		return 0, err
	}
	if err := s.maybeSyncEventsFileLocked(fp); err != nil {
		return 0, err
	}

	written := int64(writtenBytes)
	s.eventsFileSizeBytes += written
	return written, nil
}

func (s *Store) maybeSyncEventsFileLocked(fp *os.File) error {
	switch s.options.eventLog.fsyncPolicy {
	case EventLogFSyncNever:
		return nil
	case EventLogFSyncAlways:
		if err := fp.Sync(); err != nil {
			return fmt.Errorf("fsync events file: %w", err)
		}
		s.pendingFsyncWrites = 0
		return nil
	case EventLogFSyncPeriodic:
		s.pendingFsyncWrites++
		if s.pendingFsyncWrites < s.options.eventLog.fsyncIntervalWrites {
			return nil
		}
		if err := fp.Sync(); err != nil {
			return fmt.Errorf("fsync events file: %w", err)
		}
		s.pendingFsyncWrites = 0
		return nil
	default:
		return nil
	}
}

func (s *Store) repairTrailingLineLocked(fp *os.File, fileSize int64) (bool, error) {
	if fileSize == 0 {
		return false, nil
	}
	lastByte := [1]byte{}
	if _, err := fp.ReadAt(lastByte[:], fileSize-1); err != nil {
		return false, fmt.Errorf("read events file tail: %w", err)
	}
	if lastByte[0] == '\n' {
		return false, nil
	}

	lastNewlineOffset, err := lastNewlineOffset(fp, fileSize)
	if err != nil {
		return false, err
	}
	tailStart := lastNewlineOffset + 1
	tailLength := fileSize - tailStart
	if tailLength <= 0 {
		return false, nil
	}
	tail := make([]byte, tailLength)
	if _, err := fp.ReadAt(tail, tailStart); err != nil {
		return false, fmt.Errorf("read events tail line: %w", err)
	}

	trimmedTail := bytes.TrimSpace(tail)
	if len(trimmedTail) > 0 {
		var parsed Event
		if err := json.Unmarshal(trimmedTail, &parsed); err == nil {
			return true, nil
		}
	}
	if err := fp.Truncate(tailStart); err != nil {
		return false, fmt.Errorf("truncate events file tail: %w", err)
	}
	if _, err := fp.Seek(0, io.SeekEnd); err != nil {
		return false, fmt.Errorf("seek events file end: %w", err)
	}
	s.eventsFileSizeBytes = tailStart
	return false, nil
}

func readEventsFile(path string) (parsedEvents, error) {
	fp, err := openRegularSessionFile(path, "events file")
	if err != nil {
		return parsedEvents{}, fmt.Errorf("open events file: %w", err)
	}
	defer fp.Close()
	return parseEventsFromReader(bufio.NewReader(fp))
}

func walkEventsFile(path string, visit func(Event) error) (parsedEvents, error) {
	fp, err := openRegularSessionFile(path, "events file")
	if err != nil {
		return parsedEvents{}, fmt.Errorf("open events file: %w", err)
	}
	defer fp.Close()
	return walkEventsFromReader(bufio.NewReader(fp), visit)
}

func parseEventsFromReader(reader *bufio.Reader) (parsedEvents, error) {
	out := make([]Event, 0)
	totalBytes := int64(0)
	droppedTrailingEOF := false
	for {
		line, readErr := reader.ReadString('\n')
		totalBytes += int64(len(line))
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			var evt Event
			if err := json.Unmarshal([]byte(trimmed), &evt); err != nil {
				if errors.Is(readErr, io.EOF) && !strings.HasSuffix(line, "\n") {
					droppedTrailingEOF = true
					break
				}
				return parsedEvents{}, fmt.Errorf("parse event line: %w", err)
			}
			out = append(out, evt)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return parsedEvents{}, fmt.Errorf("read events line: %w", readErr)
		}
	}
	lastSequence := int64(0)
	if len(out) > 0 {
		lastSequence = out[len(out)-1].Seq
	}
	return parsedEvents{events: out, totalBytes: totalBytes, lastSequence: lastSequence, droppedTrailingEOF: droppedTrailingEOF}, nil
}

func walkEventsFromReader(reader *bufio.Reader, visit func(Event) error) (parsedEvents, error) {
	totalBytes := int64(0)
	droppedTrailingEOF := false
	lastSequence := int64(0)
	for {
		line, readErr := reader.ReadString('\n')
		totalBytes += int64(len(line))
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			var evt Event
			if err := json.Unmarshal([]byte(trimmed), &evt); err != nil {
				if errors.Is(readErr, io.EOF) && !strings.HasSuffix(line, "\n") {
					droppedTrailingEOF = true
					break
				}
				return parsedEvents{}, fmt.Errorf("parse event line: %w", err)
			}
			lastSequence = evt.Seq
			if visit != nil {
				if err := visit(evt); err != nil {
					return parsedEvents{}, err
				}
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return parsedEvents{}, fmt.Errorf("read events line: %w", readErr)
		}
	}
	return parsedEvents{totalBytes: totalBytes, lastSequence: lastSequence, droppedTrailingEOF: droppedTrailingEOF}, nil
}

func encodeEventLines(events []Event, hasExistingContent bool) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	if hasExistingContent {
		buf.WriteByte('\n')
	}
	for _, event := range events {
		line, err := json.Marshal(event)
		if err != nil {
			return nil, fmt.Errorf("marshal event line: %w", err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}

func writeAll(fp *os.File, payload []byte) (int, error) {
	offset := 0
	for offset < len(payload) {
		written, err := fp.Write(payload[offset:])
		if err != nil {
			return offset, fmt.Errorf("append events file: %w", err)
		}
		if written == 0 {
			return offset, fmt.Errorf("append events file: wrote 0 bytes")
		}
		offset += written
	}
	return offset, nil
}

func writeEventsFile(path string, events []Event) error {
	tmp := path + ".tmp"
	fp, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open events tmp file: %w", err)
	}
	for _, event := range events {
		line, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			_ = fp.Close()
			return fmt.Errorf("marshal event line: %w", marshalErr)
		}
		line = append(line, '\n')
		_, writeErr := writeAll(fp, line)
		if writeErr != nil {
			_ = fp.Close()
			return writeErr
		}
	}
	if err := fp.Close(); err != nil {
		return fmt.Errorf("close events tmp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace events file: %w", err)
	}
	return nil
}

func computeEventsJSONLSize(events []Event) int64 {
	total := int64(0)
	for _, event := range events {
		line, err := json.Marshal(event)
		if err != nil {
			continue
		}
		total += int64(len(line) + 1)
	}
	return total
}

func lastNewlineOffset(fp *os.File, fileSize int64) (int64, error) {
	if fileSize == 0 {
		return -1, nil
	}
	position := fileSize
	for position > 0 {
		chunkSize := eventLogScanChunkSize
		if position < chunkSize {
			chunkSize = position
		}
		start := position - chunkSize
		chunk := make([]byte, chunkSize)
		if _, err := fp.ReadAt(chunk, start); err != nil {
			return -1, fmt.Errorf("scan events file for newline: %w", err)
		}
		if idx := bytes.LastIndexByte(chunk, '\n'); idx >= 0 {
			return start + int64(idx), nil
		}
		position = start
	}
	return -1, nil
}
