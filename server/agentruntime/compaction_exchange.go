package runtime

import (
	"core/server/llm"
	"core/shared/textutil"
)

// acceptedAssistantSpan owns the complete output of one accepted response.
// Identity is in-memory only and is rebuilt by ordinary bounded hydration.
type acceptedAssistantSpan struct {
	protected bool
	position  int
}

func joinCompletionOwners(items []llm.ResponseItem, owners []*acceptedAssistantSpan) {
	calls := make(map[string]*acceptedAssistantSpan)
	for index, item := range items {
		if !isToolCallItem(item.Type) {
			continue
		}
		if id, ok := textutil.FirstOptionalTrimmed(item.CallID, item.ID); ok {
			if _, exists := calls[id]; exists {
				calls[id] = nil
			} else {
				calls[id] = owners[index]
			}
		}
	}
	for index, item := range items {
		if owners[index] != nil {
			continue
		}
		if isToolOutputItem(item.Type) && item.CallID != nil {
			owners[index] = calls[*item.CallID]
		} else if isOutputAttachment(item) {
			owners[index] = calls[*item.LinkedCallID]
		}
	}
}

type compactionOmission struct {
	owners  []*acceptedAssistantSpan
	omitted map[*acceptedAssistantSpan]struct{}
}

func (s *transcriptRuntimeState) snapshotCompactionInput() ([]llm.ResponseItem, *int, *compactionOmission) {
	chat := s.chatProjection()
	chat.mu.RLock()
	defer chat.mu.RUnlock()
	items, end, owners := chat.snapshotOwnedProviderItemsLocked()
	return items, end, &compactionOmission{owners: owners, omitted: make(map[*acceptedAssistantSpan]struct{})}
}

func isOutputAttachment(item llm.ResponseItem) bool {
	return item.LinkKind != nil && *item.LinkKind == llm.ResponseItemLinkToolOutputAttachment && item.LinkedCallID != nil
}

// omitNewest removes exactly one complete accepted response and its owned
// results. The items are request copies; the active context is never changed.
func (p *compactionOmission) omitNewest(items []llm.ResponseItem) ([]llm.ResponseItem, bool) {
	if p == nil {
		return items, false
	}
	blocked := make(map[*acceptedAssistantSpan]bool)
	calls := make(map[string][]*acceptedAssistantSpan)
	results := make(map[string]int)
	for index, item := range items {
		owner := p.owners[index]
		if owner != nil && (owner.protected ||
			item.Type == llm.ResponseItemTypeCompaction ||
			item.Type == llm.ResponseItemTypeConfigurationUpdate ||
			(item.Type == llm.ResponseItemTypeOther && !isOutputAttachment(item)) ||
			(!isOutputAttachment(item) && ((item.Role != nil && *item.Role != llm.RoleAssistant) || item.MessageType != nil))) {
			blocked[owner] = true
		}
		if isToolCallItem(item.Type) {
			if id, ok := textutil.FirstOptionalTrimmed(item.CallID, item.ID); ok {
				calls[id] = append(calls[id], owner)
			} else {
				blocked[owner] = true
			}
		}
		if isToolOutputItem(item.Type) && item.CallID != nil {
			results[*item.CallID]++
		}
	}
	for id, owners := range calls {
		if len(owners) != 1 || results[id] != 1 {
			for _, owner := range owners {
				blocked[owner] = true
			}
		}
	}
	for index, item := range items {
		var reference *string
		if isToolOutputItem(item.Type) {
			reference = item.CallID
		} else if item.LinkedCallID != nil {
			reference = item.LinkedCallID
		}
		if reference == nil {
			continue
		}
		owners := calls[*reference]
		if len(owners) != 1 || owners[0] != p.owners[index] {
			blocked[p.owners[index]] = true
			for _, owner := range owners {
				blocked[owner] = true
			}
		}
	}
	var newest *acceptedAssistantSpan
	for _, owner := range p.owners {
		if owner != nil && !blocked[owner] && (newest == nil || owner.position > newest.position) {
			newest = owner
		}
	}
	if newest == nil {
		return items, false
	}
	p.omitted[newest] = struct{}{}
	return p.filter(items, p.owners), true
}

func (p *compactionOmission) filter(items []llm.ResponseItem, owners []*acceptedAssistantSpan) []llm.ResponseItem {
	out := make([]llm.ResponseItem, 0, len(items))
	keptOwners := make([]*acceptedAssistantSpan, 0, len(items))
	for index, item := range items {
		if _, omitted := p.omitted[owners[index]]; omitted {
			continue
		}
		out = append(out, item)
		keptOwners = append(keptOwners, owners[index])
	}
	p.owners = keptOwners
	return out
}

func (p *compactionOmission) repairedInput(state *transcriptRuntimeState) []llm.ResponseItem {
	items, _, fresh := state.snapshotCompactionInput()
	if p == nil {
		return items
	}
	return p.filter(items, fresh.owners)
}
