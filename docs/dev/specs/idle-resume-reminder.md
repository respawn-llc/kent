## Idle Resume Reminder

- Kent must check for an idle reminder lazily when actual model work starts, not when an input or answer arrives.
- The check must cover human Send, Steer, and Queue, Session and Workflow Task Resume, Workflow Task Approval, new Workflow Tasks or Nodes in an existing Session, Goal continuation, and tool, background-result, and developer-reminder continuations.
- Opening or reconnecting a Session, or changing settings alone, must not trigger a reminder.
- Kent must persist a separate last-model/tool-activity time across Session and server reopening and compaction.
- Successfully completed model requests and completed ordinary tool executions must refresh that time, including tool-call-only or reasoning-only responses and unsuccessful tool executions. Failed or canceled model requests must not refresh it.
- Incoming input, human answers, Approval decisions, drafts, settings, client activity, and reminder insertion must not refresh that time.
- Question and Session access-Approval waits must count as waits rather than ordinary tool work. A completed long-running ordinary tool must count as work rather than sleep.
- Kent must skip the reminder when the activity time is absent. Existing Sessions and rollback forks must start without backfilled activity time.
- Kent must skip the reminder for a fresh Session or the first message after compaction without a subsequent conversation message. This exclusion must use actual conversation state and must not clear the activity time.
- The global setting `idle_resume_reminder_seconds` must default to `3600`. Zero must disable the reminder. Negative values must be rejected. Only elapsed time strictly greater than the configured threshold must qualify.
- Elapsed time must use absolute timestamps. A backward clock must not qualify.
- Kent must emit at most one reminder when model work resumes after a batch of Question answers, never one per answer.
- Kent must persist the reminder lazily at the ordinary input-commit boundary, immediately before accompanying new human input, or before resumed model work when there is no human input.
- Kent must not rewrite content already sent to the provider or rewrite an existing reminder.
- Kent may inspect at most one previous conversation step to avoid a duplicate reminder on Retry or Resume. Rare duplicates beyond that bound are acceptable. Kent must not add crash recovery or separate reminder tracking for this feature.
- The reminder must say exactly: `Session has been resumed after you've been asleep for <duration>. The workspace, VCS, or environment state may have changed. Current time <timestamp>.`
- The duration must use whole minutes with days, hours, and minutes, omitting seconds and zero units, such as `17d8h24m`, `1h`, or `24m`. A qualifying sub-minute gap must display `0m`.
- The current time must use server-local time in the Environment format `YYYY-MM-DDTHH:mm:ss`.
