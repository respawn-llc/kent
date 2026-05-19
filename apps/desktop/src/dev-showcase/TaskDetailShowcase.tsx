import { Badge, Button, MarkdownText, SelectField, TextArea, TextInput } from "../ui";

export function TaskDetailBoard({
  cancelExpanded,
  onCancelExpandedChange,
}: Readonly<{ cancelExpanded: boolean; onCancelExpandedChange: (expanded: boolean) => void }>) {
  return (
    <div className="grid gap-[var(--space-3)] 2xl:grid-cols-[minmax(420px,0.9fr)_minmax(0,1.1fr)]">
      <Panel title="Task Header, Body, Inbox">
        <header className="flex flex-wrap items-start justify-between gap-[var(--space-3)]">
          <div className="min-w-0">
            <p className="m-0 font-mono text-sm text-[var(--color-muted)]">Task BLDR-104</p>
            <h3 className="m-0 mt-[var(--space-1)]">Add UI showcase board</h3>
            <p className="m-0 text-[var(--color-muted)]">Builder Desktop · MVP workflow</p>
          </div>
          <div className="flex flex-wrap gap-[var(--space-2)]">
            <Badge tone="warning">Waiting approval</Badge>
            <Badge>builder-cli</Badge>
          </div>
        </header>
        <article className="rounded-[var(--radius-l)] border border-[var(--color-outline)] bg-[var(--color-island-1)] p-[var(--space-3)]">
          <MarkdownText value={"Review all high-value UI states with **static mock data**.\n\n- Pin hover menu\n- Confirm cancel\n- Edit comments"} />
        </article>
        <TaskEditPreview />
        <TaskInboxPreview cancelExpanded={cancelExpanded} onCancelExpandedChange={onCancelExpandedChange} />
      </Panel>
      <Panel title="Tabs, Comments, Activity, Runs">
        <div className="grid gap-[var(--space-3)] lg:grid-cols-3">
          <CommentsPreview />
          <ActivityPreview />
          <RunsPreview />
        </div>
      </Panel>
    </div>
  );
}

function Panel({ children, title }: React.PropsWithChildren<Readonly<{ title: string }>>) {
  return (
    <section className="island-glass grid gap-[var(--space-3)] rounded-[var(--radius-xl)] p-[var(--space-4)]">
      <h3 className="m-0">{title}</h3>
      {children}
    </section>
  );
}

function TaskEditPreview() {
  return (
    <form className="grid gap-[var(--space-3)] rounded-[var(--radius-l)] border border-[var(--color-outline)] bg-[var(--color-island-2)] p-[var(--space-3)]">
      <TextInput label="Title" value="Add UI showcase board" readOnly />
      <TextArea label="Details" rows={3} value="Editable backlog preview with Markdown body." readOnly />
      <SelectField
        disabled
        label="Source workspace"
        onValueChange={() => undefined}
        options={[{ label: "builder-cli", value: "workspace-api" }]}
        value="workspace-api"
      />
      <Button variant="primary">Save changes</Button>
    </form>
  );
}

function TaskInboxPreview({
  cancelExpanded,
  onCancelExpandedChange,
}: Readonly<{ cancelExpanded: boolean; onCancelExpandedChange: (expanded: boolean) => void }>) {
  return (
    <section className="grid gap-[var(--space-3)] rounded-[var(--radius-l)] border border-[var(--color-outline)] bg-[var(--color-island-1)] p-[var(--space-3)]">
      <div className="flex flex-wrap items-center justify-between gap-[var(--space-2)]">
        <h4 className="m-0">Inbox</h4>
        <Badge tone="warning">2</Badge>
      </div>
      <div className="flex flex-wrap gap-[var(--space-2)]">
        <Button variant="primary">Resume</Button>
        <Button>Interrupt run-a7f2</Button>
        <Button
          onClick={() => {
            onCancelExpandedChange(!cancelExpanded);
          }}
          variant="danger"
        >
          Cancel task
        </Button>
      </div>
      {cancelExpanded ? (
        <article className="grid gap-[var(--space-2)] rounded-[var(--radius-l)] border border-[var(--color-outline)] bg-[var(--color-island-2)] p-[var(--space-3)]">
          <strong>Cancel task?</strong>
          <p className="m-0">This stops the task without a reason field.</p>
          <Button variant="danger">Confirm</Button>
        </article>
      ) : null}
      <QuestionPreview />
      <ApprovalPreview />
    </section>
  );
}

function QuestionPreview() {
  return (
    <form className="grid gap-[var(--space-3)] rounded-[var(--radius-l)] border border-[var(--color-warning)] bg-[color-mix(in_srgb,var(--color-warning)_12%,transparent)] p-[var(--space-3)]">
      <h4 className="m-0">Question</h4>
      <p className="m-0">Which preview should open first when dev server launches?</p>
      <label className="rounded-[var(--radius-m)] border border-[var(--color-primary)] bg-[color-mix(in_srgb,var(--color-primary)_14%,transparent)] p-[var(--space-2)]">
        <input className="mr-[var(--space-2)]" defaultChecked name="question-preview" type="radio" />
        UI showcase Recommended
      </label>
      <label className="rounded-[var(--radius-m)] border border-[var(--color-outline)] bg-[var(--color-island-1)] p-[var(--space-2)]">
        <input className="mr-[var(--space-2)]" name="question-preview" type="radio" />
        Home route
      </label>
      <TextArea label="Answer" rows={3} value="Use dev showcase for visual review." readOnly />
      <Button variant="primary">Submit answer</Button>
    </form>
  );
}

function ApprovalPreview() {
  return (
    <section className="grid gap-[var(--space-3)] rounded-[var(--radius-l)] border border-[var(--color-warning)] bg-[color-mix(in_srgb,var(--color-warning)_12%,transparent)] p-[var(--space-3)]">
      <h4 className="m-0">Approval</h4>
      <p className="m-0">Agent wants to move task from Design to Implementation.</p>
      <dl className="grid grid-cols-[max-content_minmax(0,1fr)] gap-x-[var(--space-3)] gap-y-[var(--space-2)]">
        <dt>Approval snapshot</dt><dd>Design · Ready for implementation</dd>
        <dt>Target nodes</dt><dd>Implementation, Review</dd>
        <dt>Output values</dt><dd>scope: browser preview</dd>
        <dt>Version</dt><dd>7</dd>
      </dl>
      <Button variant="primary">Approve</Button>
    </section>
  );
}

function CommentsPreview() {
  return (
    <section className="grid gap-[var(--space-3)]">
      <h4 className="m-0">Comments</h4>
      <TextArea label="Add comment" rows={3} value="Capture light/dark screenshots after style changes." readOnly />
      <Button variant="primary">Add comment</Button>
      {["Looks good in compact board width.", "Need check collapsed hover menu contrast."].map((comment) => (
        <article className="grid gap-[var(--space-2)] rounded-[var(--radius-l)] border border-[var(--color-outline)] bg-[var(--color-island-1)] p-[var(--space-3)]" key={comment}>
          <MarkdownText value={comment} />
          <div className="flex flex-wrap gap-[var(--space-2)]">
            <Button variant="ghost">Edit comment</Button>
            <Button variant="danger">Delete comment</Button>
          </div>
        </article>
      ))}
    </section>
  );
}

function ActivityPreview() {
  return (
    <section className="grid gap-[var(--space-3)]">
      <h4 className="m-0">Activity</h4>
      {["Task created", "Run started", "Question asked", "Approval prepared"].map((summary) => (
        <article className="rounded-[var(--radius-m)] border border-[var(--color-outline)] bg-[var(--color-island-1)] p-[var(--space-3)]" key={summary}>
          <span>{summary}</span>
          <time className="block text-sm text-[var(--color-muted)]">2m ago</time>
        </article>
      ))}
    </section>
  );
}

function RunsPreview() {
  return (
    <section className="grid gap-[var(--space-3)]">
      <h4 className="m-0">Runs</h4>
      <article className="grid gap-[var(--space-2)] rounded-[var(--radius-l)] border border-[var(--color-outline)] bg-[var(--color-island-1)] p-[var(--space-3)]">
        <h5 className="m-0">Telemetry</h5>
        <p className="m-0"><strong>Status</strong> Waiting approval</p>
        <p className="m-0 break-all"><strong>Worktree</strong> /tmp/builder-ui-showcase</p>
      </article>
      <article className="grid gap-[var(--space-1)] rounded-[var(--radius-l)] border border-[var(--color-outline)] bg-[var(--color-island-1)] p-[var(--space-3)]">
        <span className="font-mono">run-a7f2</span>
        <span>running</span>
        <span className="font-mono text-sm text-[var(--color-muted)]">session-ui-showcase</span>
        <span className="text-sm text-[var(--color-muted)]">builder</span>
      </article>
      <Button disabled>Teleport unavailable</Button>
      <Button>Teleport available</Button>
    </section>
  );
}
