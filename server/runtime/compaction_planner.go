package runtime

import (
	"core/server/llm"
	"core/shared/compaction"
)

type compactionPlanningSnapshot struct {
	autoCompactionEnabled         bool
	compactionMode                string
	autoCompactTokenLimit         int
	preSubmitCompactionLeadTokens int
	contextWindowTokens           int
	effectiveContextWindowPercent int
	maxOutputTokens               int
	lockedMaxOutputTokens         int
	lastUsage                     llm.Usage
}

type compactionEngineKind string

const (
	compactionEngineNone   compactionEngineKind = "none"
	compactionEngineLocal  compactionEngineKind = "local"
	compactionEngineRemote compactionEngineKind = "remote"
)

type compactionEnginePlan struct {
	engineKind                     compactionEngineKind
	fallbackToLocalOnBadCheckpoint bool
}

type compactionPlanner struct{}

func newCompactionPlanner() *compactionPlanner {
	return &compactionPlanner{}
}

func (p *compactionPlanner) mode(raw string) string {
	normalized, ok := NormalizeCompactionMode(raw)
	if !ok {
		return "native"
	}
	return normalized
}

func (p *compactionPlanner) autoCompactionAvailable(snapshot compactionPlanningSnapshot) bool {
	return snapshot.autoCompactionEnabled && p.mode(snapshot.compactionMode) != "none"
}

func (p *compactionPlanner) enginePlan(snapshot compactionPlanningSnapshot, caps llm.ProviderCapabilities) compactionEnginePlan {
	switch p.mode(snapshot.compactionMode) {
	case "none":
		return compactionEnginePlan{engineKind: compactionEngineNone}
	case "native":
		if caps.SupportsResponsesCompact {
			return compactionEnginePlan{
				engineKind:                     compactionEngineRemote,
				fallbackToLocalOnBadCheckpoint: true,
			}
		}
		return compactionEnginePlan{engineKind: compactionEngineLocal}
	default:
		return compactionEnginePlan{engineKind: compactionEngineLocal}
	}
}

func (p *compactionPlanner) contextWindowTokens(snapshot compactionPlanningSnapshot) int {
	if snapshot.contextWindowTokens > 0 {
		return snapshot.contextWindowTokens
	}
	if snapshot.lastUsage.WindowTokens > 0 {
		return snapshot.lastUsage.WindowTokens
	}
	return defaultContextWindowTokens
}

func (p *compactionPlanner) effectiveContextTokenLimit(snapshot compactionPlanningSnapshot) int {
	percent := snapshot.effectiveContextWindowPercent
	if percent <= 0 || percent > 100 {
		percent = 95
	}
	return (p.contextWindowTokens(snapshot) * percent) / 100
}

func (p *compactionPlanner) autoCompactTokenLimit(snapshot compactionPlanningSnapshot) int {
	if snapshot.autoCompactTokenLimit > 0 {
		return snapshot.autoCompactTokenLimit
	}
	limit := p.effectiveContextTokenLimit(snapshot)
	if limit < 1 {
		return 1
	}
	return limit
}

func (p *compactionPlanner) preSubmitTokenLimit(snapshot compactionPlanningSnapshot) int {
	limit := p.autoCompactTokenLimit(snapshot)
	if limit <= 0 {
		return 0
	}
	runwayTokens := compaction.DefaultPreSubmitRunwayTokens
	if snapshot.preSubmitCompactionLeadTokens > 0 {
		runwayTokens = snapshot.preSubmitCompactionLeadTokens
	}
	return compaction.EffectivePreSubmitThresholdTokens(limit, runwayTokens)
}

func (p *compactionPlanner) soonReminderLimit(snapshot compactionPlanningSnapshot) int {
	limit := p.autoCompactTokenLimit(snapshot)
	if limit <= 0 {
		return 0
	}
	reminderLimit := (limit * compactionSoonReminderPercent) / 100
	if reminderLimit < 1 {
		return 1
	}
	return reminderLimit
}

func (p *compactionPlanner) reservedOutputTokens(snapshot compactionPlanningSnapshot) int {
	if snapshot.lockedMaxOutputTokens > 0 {
		return snapshot.lockedMaxOutputTokens
	}
	if snapshot.maxOutputTokens > 0 {
		return snapshot.maxOutputTokens
	}
	return 0
}
