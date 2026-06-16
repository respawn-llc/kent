package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Snapshot struct {
	Meta                  Meta
	Events                []Event
	Runs                  []RunRecord
	ConversationFreshness ConversationFreshness
}

func SnapshotFromDir(sessionDir string) (Snapshot, error) {
	meta, err := ReadMetaFromDir(sessionDir)
	if err != nil {
		return Snapshot{}, err
	}
	parsed, err := readEventsFile(filepath.Join(sessionDir, eventsFile))
	if err != nil {
		if os.IsNotExist(err) {
			return Snapshot{Meta: meta, ConversationFreshness: ConversationFreshnessFresh}, nil
		}
		return Snapshot{}, err
	}
	return Snapshot{
		Meta:                  meta,
		Events:                parsed.events,
		Runs:                  runsFromEvents(parsed.events),
		ConversationFreshness: conversationFreshnessFromEvents(parsed.events),
	}, nil
}

func ReadMetaFromDir(sessionDir string) (Meta, error) {
	return readMetaFile(filepath.Join(sessionDir, sessionFile))
}

func readMetaFile(path string) (Meta, error) {
	data, err := readRegularSessionFile(path, "session meta")
	if err != nil {
		return Meta{}, fmt.Errorf("%w: %w", ErrReadSessionMeta, err)
	}
	var meta Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		return Meta{}, fmt.Errorf("parse session meta: %w", err)
	}
	return meta, nil
}
