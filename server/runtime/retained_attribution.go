package runtime

import (
	"core/server/llm"
	"core/server/session"
	"core/shared/modelcontract"
	"core/shared/textutil"
)

type producingStepKey struct {
	stepID  string
	purpose modelcontract.ProviderOperationPurpose
}

type producingStepEvidence map[producingStepKey]*modelcontract.ReasoningType

func restoredProducingStepEvidence(records []session.EventRecord, sessionID string) (producingStepEvidence, error) {
	evidence := make(producingStepEvidence)
	for _, record := range records {
		payload, err := record.Payload()
		if err != nil {
			return nil, err
		}
		observation, ok := payload.(session.CacheResponseObservationRecord)
		if !ok || record.StepID() == nil || observation.SessionID == nil || *observation.SessionID != sessionID || observation.Purpose == nil {
			continue
		}
		if *observation.Purpose != modelcontract.ProviderOperationPurposeGeneration && *observation.Purpose != modelcontract.ProviderOperationPurposeCompaction {
			continue
		}
		key := producingStepKey{stepID: *record.StepID(), purpose: *observation.Purpose}
		var typ *modelcontract.ReasoningType
		if observation.ProviderUsage != nil && observation.ProviderUsage.ProviderID != nil {
			typ = llm.LookupProviderReasoningType(*observation.ProviderUsage.ProviderID)
		}
		previous, exists := evidence[key]
		if exists && (previous == nil || typ == nil || *previous != *typ) {
			evidence[key] = nil
		} else {
			evidence[key] = typ
		}
	}
	return evidence, nil
}

func (e producingStepEvidence) infer(attribution *modelcontract.ReasoningAttribution, stepID *string, purpose modelcontract.ProviderOperationPurpose) *modelcontract.ReasoningAttribution {
	if attribution != nil || stepID == nil {
		return attribution
	}
	typ := e[producingStepKey{stepID: *stepID, purpose: purpose}]
	if typ == nil {
		return nil
	}
	return &modelcontract.ReasoningAttribution{Type: textutil.Pointer(typ)}
}

func producedReasoningAttribution(providerID *string, encrypted bool) *modelcontract.ReasoningAttribution {
	var typ *modelcontract.ReasoningType
	if providerID != nil {
		typ = llm.LookupProviderReasoningType(*providerID)
	}
	if !encrypted {
		typ = textutil.Value(modelcontract.ReasoningTypeUnencrypted)
	}
	return &modelcontract.ReasoningAttribution{Type: typ}
}

func stampProducedReasoning(response *llm.Response, providerID *string) error {
	if err := stampProducedItems(response.OutputItems, providerID); err != nil {
		return err
	}
	for _, items := range [][]llm.ReasoningItem{response.ReasoningItems, response.Assistant.ReasoningItems} {
		for index := range items {
			items[index].Attribution = producedReasoningAttribution(providerID, items[index].EncryptedContent != "")
		}
	}
	return nil
}

func stampProducedItems(items []llm.ResponseItem, providerID *string) error {
	for index := range items {
		item := &items[index]
		if item.Type == llm.ResponseItemTypeReasoning || item.Type == llm.ResponseItemTypeCompaction {
			if err := llm.RestoreRetainedItemFacts(item); err != nil {
				return err
			}
			item.Attribution = producedReasoningAttribution(providerID, item.EncryptedContent != nil)
		}
	}
	return nil
}
