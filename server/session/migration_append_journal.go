package session

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	appendRecoveryRecordVersion = 1
	maxAppendRecoveryRecordSize = int64(16 << 20)
	maxAppendRecoverySuffixSize = int64(16 << 20)
	appendRecoveryPrepared      = appendRecoveryPhase("prepared")
	appendRecoveryCommitted     = appendRecoveryPhase("committed")
)

type appendRecoveryPhase string

type appendRecoveryEvents struct {
	StartOffset   int64  `json:"start_offset"`
	EndOffset     int64  `json:"end_offset"`
	EventCount    int    `json:"event_count"`
	FirstSequence int64  `json:"first_sequence"`
	LastSequence  int64  `json:"last_sequence"`
	SHA256        string `json:"sha256"`
}

type appendRecoverySuffixConflictError struct {
	message string
}

func (e *appendRecoverySuffixConflictError) Error() string {
	return e.message
}

type appendRecoveryMeta struct {
	Meta   Meta   `json:"meta"`
	SHA256 string `json:"sha256"`
}

type appendRecoveryRecord struct {
	Version int                   `json:"version"`
	Phase   appendRecoveryPhase   `json:"phase"`
	Pre     appendRecoveryMeta    `json:"pre"`
	Post    appendRecoveryMeta    `json:"post"`
	Events  *appendRecoveryEvents `json:"events,omitempty"`
}

func appendRecoveryMetaOf(meta Meta) (appendRecoveryMeta, error) {
	meta.CreatedAt = meta.CreatedAt.UTC().Truncate(time.Millisecond)
	meta.UpdatedAt = meta.UpdatedAt.UTC().Truncate(time.Millisecond)
	encoded, err := json.Marshal(meta)
	return appendRecoveryMeta{Meta: meta, SHA256: digestBytes(encoded)}, err
}

func digestMeta(meta Meta) (string, error) {
	snapshot, err := appendRecoveryMetaOf(meta)
	return snapshot.SHA256, err
}

func digestBytes(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func validateRecoveryTarget(path string) error {
	fp, err := openRegularSessionFile(path, "append recovery record")
	if err == nil {
		return fp.Close()
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func syncRecoveryDirectory(path string) error {
	return syncSessionDirectory(filepath.Dir(path))
}

func clearLegacyAppendJournal(sessionDir string) error {
	path := filepath.Join(sessionDir, appendRecoveryFile)
	if err := validateRecoveryTarget(path); err != nil {
		return err
	}
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return syncRecoveryDirectory(path)
}

func readLegacyAppendJournal(sessionDir, sessionID string) (*appendRecoveryRecord, error) {
	fp, err := openRegularSessionFile(filepath.Join(sessionDir, appendRecoveryFile), "append recovery record")
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	encoded, readErr := io.ReadAll(io.LimitReader(fp, maxAppendRecoveryRecordSize+1))
	if err := errors.Join(readErr, fp.Close()); err != nil {
		return nil, err
	}
	if int64(len(encoded)) > maxAppendRecoveryRecordSize {
		return nil, errors.New("append recovery record exceeds size limit")
	}
	record := new(appendRecoveryRecord)
	if err := json.Unmarshal(encoded, record); err != nil {
		return nil, err
	}
	if err := record.validate(sessionID); err != nil {
		return nil, err
	}
	return record, nil
}

func (record appendRecoveryRecord) validate(sessionID string) error {
	preDigest, preErr := digestMeta(record.Pre.Meta)
	postDigest, postErr := digestMeta(record.Post.Meta)
	events := record.Events
	sessionID = strings.TrimSpace(sessionID)
	switch {
	case record.Version != appendRecoveryRecordVersion:
		return fmt.Errorf("append recovery version %d is unsupported", record.Version)
	case record.Pre.Meta.SessionID != sessionID || record.Post.Meta.SessionID != sessionID:
		return fmt.Errorf("append recovery metadata does not match session %q", sessionID)
	case record.Phase != appendRecoveryPrepared && record.Phase != appendRecoveryCommitted:
		return fmt.Errorf("append recovery phase %q is invalid", record.Phase)
	case preErr != nil || postErr != nil || preDigest != record.Pre.SHA256 || postDigest != record.Post.SHA256:
		return errors.Join(preErr, postErr, errors.New("append recovery metadata digest is invalid"))
	case events == nil && record.Phase != appendRecoveryCommitted:
		return errors.New("metadata-only recovery record must be committed")
	case events == nil && record.Pre.Meta.LastSequence != record.Post.Meta.LastSequence:
		return errors.New("metadata-only append journal changes transcript sequence")
	case events != nil && (events.StartOffset < 0 || events.EndOffset <= events.StartOffset ||
		events.EndOffset-events.StartOffset > maxAppendRecoverySuffixSize ||
		events.EventCount <= 0 || events.FirstSequence <= 0 ||
		events.LastSequence < events.FirstSequence || strings.TrimSpace(events.SHA256) == ""):
		return errors.New("append recovery event identity is invalid")
	case events != nil && (events.FirstSequence != record.Pre.Meta.LastSequence+1 ||
		events.LastSequence != record.Post.Meta.LastSequence ||
		int64(events.EventCount) != events.LastSequence-events.FirstSequence+1):
		return errors.New("append journal sequence range disagrees with metadata")
	default:
		return nil
	}
}

func inspectAppendRecoverySuffix(path string, events appendRecoveryEvents, phase appendRecoveryPhase) (exact bool, err error) {
	fp, err := openRegularSessionFile(path, "events file")
	if err != nil {
		return false, err
	}
	defer func() { err = errors.Join(err, fp.Close()) }()
	info, err := fp.Stat()
	if err != nil {
		return false, err
	}
	size := info.Size()
	if size < events.StartOffset || size > events.EndOffset {
		return false, &appendRecoverySuffixConflictError{
			message: fmt.Sprintf(
				"events file size %d is outside recovery range [%d,%d]",
				size,
				events.StartOffset,
				events.EndOffset,
			),
		}
	}
	if size != events.EndOffset || phase == appendRecoveryPrepared {
		return size == events.EndOffset, nil
	}
	hash := sha256.New()
	classification, err := classifyEventLogSource(path)
	if err != nil {
		return false, fmt.Errorf("classify append recovery event log: %w", err)
	}
	decode := decodeEventRecordV1
	if classification.source == eventLogSourceCurrent {
		if classification.foundVersion == nil {
			return false, errors.New("current append recovery event log version is required")
		}
		version := *classification.foundVersion
		decode = func(encoded []byte) (EventRecord, error) {
			return decodeEventRecordForVersion(version, encoded)
		}
	} else if classification.source != eventLogSourceLegacy {
		return false, fmt.Errorf(
			"append recovery event log source %d is unsupported",
			classification.source,
		)
	}
	decoder := json.NewDecoder(io.TeeReader(
		io.NewSectionReader(fp, events.StartOffset, events.EndOffset-events.StartOffset),
		hash,
	))
	count := 0
	firstSequence := int64(0)
	lastSequence := int64(0)
	for {
		var encoded json.RawMessage
		err := decoder.Decode(&encoded)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return false, fmt.Errorf("parse committed event suffix: %w", err)
		}
		event, err := decode(encoded)
		if err != nil {
			return false, fmt.Errorf("parse committed typed event suffix: %w", err)
		}
		count++
		if event.Seq() != events.FirstSequence+int64(count)-1 {
			return false, errors.New("committed append-journal suffix has a sequence gap")
		}
		if count == 1 {
			firstSequence = event.Seq()
		}
		lastSequence = event.Seq()
	}
	if count != events.EventCount || firstSequence != events.FirstSequence || lastSequence != events.LastSequence ||
		fmt.Sprintf("%x", hash.Sum(nil)) != events.SHA256 {
		return false, &appendRecoverySuffixConflictError{
			message: "committed event suffix identity is invalid",
		}
	}
	return true, nil
}

func truncateAppendRecoverySuffix(path string, offset int64) error {
	if err := validateRecoveryTarget(path); err != nil {
		return err
	}
	fp, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	return syncAndClose(fp, fp.Truncate(offset))
}

// MigrateAppendJournal preserves the former journal commit decision once,
// before the database records completion of the format cutover. Ordinary
// Session opening never calls it.
func MigrateAppendJournal(persisted PersistedSessionRecord, commit func(PersistedStoreSnapshot) error) (resultErr error) {
	if persisted.Meta == nil || commit == nil {
		return errors.New("append-journal migration requires persisted metadata and commit authority")
	}
	if _, err := os.Lstat(filepath.Join(persisted.SessionDir, appendRecoveryFile)); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	lock, lockPath, err := acquireEventLogPersistenceLock(persisted.SessionDir)
	if err != nil {
		return err
	}
	defer joinEventLogPersistenceLockRelease(&resultErr, lock, lockPath)
	record, err := readLegacyAppendJournal(persisted.SessionDir, persisted.Meta.SessionID)
	if err != nil {
		return err
	}
	if record == nil {
		return nil
	}
	if persisted.Meta.LastSequence != record.Pre.Meta.LastSequence && persisted.Meta.LastSequence != record.Post.Meta.LastSequence {
		return errors.New("append-journal migration conflicts with committed database sequence")
	}
	eventsPath := filepath.Join(persisted.SessionDir, eventsFile)
	if record.Events != nil {
		exact, err := inspectAppendRecoverySuffix(eventsPath, *record.Events, record.Phase)
		if err != nil {
			return err
		}
		if record.Phase == appendRecoveryCommitted && !exact {
			return errors.New("committed append-journal suffix is incomplete")
		}
		if record.Phase == appendRecoveryPrepared {
			if persisted.Meta.LastSequence != record.Pre.Meta.LastSequence {
				return errors.New("prepared append-journal suffix overlaps committed history")
			}
			prefix, err := openCommittedEventLog(eventsPath, currentEventLogReadOnly, persisted.Meta.LastSequence)
			if err != nil {
				return err
			}
			if prefix.lastCompleteOffset != record.Events.StartOffset {
				return errors.New("prepared append-journal suffix does not start at the committed boundary")
			}
			if err := truncateAppendRecoverySuffix(eventsPath, record.Events.StartOffset); err != nil {
				return err
			}
		}
	}
	if record.Phase == appendRecoveryCommitted {
		if err := commit(PersistedStoreSnapshot{
			SessionDir:   persisted.SessionDir,
			Meta:         record.Post.Meta,
			ContextFacts: persisted.ContextFacts,
		}); err != nil {
			return err
		}
	}
	return clearLegacyAppendJournal(persisted.SessionDir)
}
