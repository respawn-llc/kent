package session

import (
	"errors"
	"fmt"
)

// GenerationContextRecord contains only context captured when live model work
// starts. The preceding compaction owns its output; intervening events retain
// their own identities and are not copied into this record.
type GenerationContextRecord struct {
	BeforeSummary []MessageRecord `json:"before_summary"`
	AfterSummary  []MessageRecord `json:"after_summary"`
}

func (GenerationContextRecord) eventKind() EventKind { return EventKindGenerationContext }

func (r GenerationContextRecord) validate() error {
	_, err := normalizeGenerationContextRecord(r)
	return err
}

func normalizeGenerationContextRecord(record GenerationContextRecord) (GenerationContextRecord, error) {
	if len(record.BeforeSummary)+len(record.AfterSummary) == 0 {
		return GenerationContextRecord{}, errors.New("generation context requires messages")
	}
	for _, segment := range []*[]MessageRecord{&record.BeforeSummary, &record.AfterSummary} {
		messages := make([]MessageRecord, len(*segment))
		for i, message := range *segment {
			if message.Role != MessageRoleDeveloper {
				return GenerationContextRecord{}, errors.New("generation context requires developer messages")
			}
			normalized, err := normalizeMessageRecord(message)
			if err != nil {
				return GenerationContextRecord{}, fmt.Errorf("generation context message: %w", err)
			}
			messages[i] = normalized
		}
		*segment = messages
	}
	return record, nil
}
