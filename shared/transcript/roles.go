package transcript

type EntryRole string

// EntryRoleCompactionSummary marks a persisted compaction or handoff summary.
const EntryRoleCompactionSummary EntryRole = "compaction_summary"

// EntryRoleManualCompactionCarryover marks the synthetic message that preserves
// the last user prompt across a manual compaction boundary.
const EntryRoleManualCompactionCarryover EntryRole = "manual_compaction_carryover"

// EntryRoleDeveloperContext marks developer/meta context that should only
// appear in verbose transcript views (AGENTS, skills, environment, headless prompts).
const EntryRoleDeveloperContext EntryRole = "developer_context"

// EntryRoleDeveloperFeedback marks developer feedback that should remain
// visible in compact transcript views.
const EntryRoleDeveloperFeedback EntryRole = "developer_feedback"

// EntryRoleDeveloperErrorFeedback marks operator-facing error feedback that
// should remain visible in compact transcript views.
const EntryRoleDeveloperErrorFeedback EntryRole = "developer_error_feedback"

// EntryRoleInterruption marks persisted interruption notices.
const EntryRoleInterruption EntryRole = "interruption"

// EntryRoleGoalFeedback marks user-facing goal lifecycle notices.
const EntryRoleGoalFeedback EntryRole = "goal_feedback"
