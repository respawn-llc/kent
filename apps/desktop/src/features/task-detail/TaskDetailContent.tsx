import { useState } from "react";
import { useTranslation } from "react-i18next";

import type { TaskDetail } from "../../api";
import { errorMessage } from "../../api/errors";
import { useConnectionSnapshot } from "../../app/useConnectionSnapshot";
import { Badge, Button, MarkdownText, SelectField, TextArea, TextInput } from "../../ui";
import { useUpdateTask, useWorkspaces } from "../tasks/useTaskMutations";
import { ActivityFeed, Comments } from "./TaskDetailActivity";
import { TaskInbox } from "./TaskDetailInbox";
import { RunsTab } from "./TaskDetailRuns";
import { TaskTabs, type DetailTab } from "./TaskDetailTabs";
import { useTaskMutations } from "./useTaskDetailData";
import type { useTaskActivity } from "./useTaskDetailData";

export function TaskDetailContent({
  activity,
  detail,
  onMutated,
  openLink,
  resumeRunId,
}: Readonly<{
  activity: ReturnType<typeof useTaskActivity>;
  detail: TaskDetail;
  onMutated?: (() => void) | undefined;
  openLink: (url: string) => void;
  resumeRunId: string;
}>) {
  const { t } = useTranslation();
  const [tab, setTab] = useState<DetailTab>("comments");
  const mutations = useTaskMutations(detail.id, onMutated);
  const connection = useConnectionSnapshot();
  const disabled = connection.phase !== "connected";
  const activeRuns = detail.runs.filter((run) => run.completedAt === 0 && run.interruptedAt === 0);
  const activityItems = activity.data?.pages.flatMap((page) => page.items) ?? [];

  return (
    <div className="grid h-full min-h-0 grid-rows-[auto_auto_minmax(0,1fr)] gap-[var(--space-4)]">
      <TaskDetailHeader detail={detail} />
      <section
        aria-label={t("task.description")}
        className="grid max-h-[28vh] min-h-[120px] gap-[var(--space-3)] overflow-auto rounded-[var(--radius-l)] border border-[var(--color-outline)] bg-[var(--color-island-1)] p-[var(--space-3)] hide-scrollbar"
      >
        <MarkdownText onOpenLink={openLink} value={detail.body || t("task.noDescription")} />
        <TaskEditForm
          detail={detail}
          disabled={disabled}
          key={detail.id}
          onMutated={onMutated}
        />
      </section>
      <div className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-[var(--space-4)] overflow-hidden">
        <TaskInbox
          activeRuns={activeRuns}
          currentGraphRevision={detail.workflowGraphRevision}
          detail={detail}
          disabled={disabled}
          mutations={mutations}
          resumeRunId={resumeRunId}
        />
        <section className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-[var(--space-3)] overflow-hidden">
          <TaskTabs
            activityCount={activityItems.length}
            commentCount={detail.comments.length}
            runCount={detail.runs.length}
            selected={tab}
            onSelect={setTab}
          />
          <div className="min-h-0 overflow-auto hide-scrollbar">
            {tab === "comments" ? (
              <Comments comments={detail.comments} disabled={disabled} mutations={mutations} openLink={openLink} />
            ) : null}
            {tab === "activity" ? (
              <ActivityFeed
                hasNextPage={activity.hasNextPage}
                isFetchingNextPage={activity.isFetchingNextPage}
                items={activityItems}
                onLoadMore={() => {
                  void activity.fetchNextPage();
                }}
              />
            ) : null}
            {tab === "runs" ? <RunsTab detail={detail} disabled={disabled} /> : null}
          </div>
        </section>
      </div>
    </div>
  );
}

function TaskDetailHeader({ detail }: Readonly<{ detail: TaskDetail }>) {
  const { t } = useTranslation();
  return (
    <header className="flex flex-wrap items-start justify-between gap-[var(--space-4)]">
      <div className="min-w-0">
        <p className="m-0 font-mono text-sm text-[var(--color-muted)]">
          {t("task.shortId", { id: detail.shortID })}
        </p>
        <h2 className="my-[var(--space-1)] min-w-0 truncate">{detail.title}</h2>
        <p className="m-0 min-w-0 truncate text-[var(--color-muted)]">
          {detail.projectName} · {detail.workflowName}
        </p>
      </div>
      <div className="flex flex-wrap items-center gap-[var(--space-2)]">
        <Badge tone="info">{detail.status.label}</Badge>
        <Badge tone="neutral">{detail.sourceWorkspace.name}</Badge>
      </div>
    </header>
  );
}

function TaskEditForm({
  detail,
  disabled,
  onMutated,
}: Readonly<{ detail: TaskDetail; disabled: boolean; onMutated?: (() => void) | undefined }>) {
  const { t } = useTranslation();
  const editable =
    detail.status.kind === "backlog" &&
    detail.runs.length === 0 &&
    detail.worktreePath.length === 0 &&
    !detail.done &&
    detail.canceledAt === 0;
  const workspaces = useWorkspaces(detail.projectID);
  const update = useUpdateTask(detail.id);
  const [title, setTitle] = useState(detail.title);
  const [body, setBody] = useState(detail.body);
  const [workspaceID, setWorkspaceID] = useState(detail.sourceWorkspace.id);
  const workspaceOptions = (workspaces.data?.workspaces ?? []).map((workspace) => ({
    label: workspace.name,
    value: workspace.id,
  }));

  if (!editable) {
    return <p className="m-0 text-sm text-[var(--color-muted)]">{t("task.sourceWorkspaceLocked")}</p>;
  }

  async function save(): Promise<void> {
    await update.mutateAsync({ taskID: detail.id, title, body, sourceWorkspaceID: workspaceID });
    onMutated?.();
  }

  return (
    <form
      className="grid gap-[var(--space-3)] rounded-[var(--radius-l)] border border-[var(--color-outline)] bg-[var(--color-island-2)] p-[var(--space-3)]"
      onSubmit={(event) => {
        event.preventDefault();
        void save();
      }}
    >
      <TextInput
        label={t("task.name")}
        onChange={(event) => {
          setTitle(event.target.value);
        }}
        value={title}
      />
      <TextArea
        label={t("task.body")}
        onChange={(event) => {
          setBody(event.target.value);
        }}
        rows={4}
        value={body}
      />
      <SelectField
        label={t("task.sourceWorkspace")}
        onValueChange={setWorkspaceID}
        options={workspaceOptions}
        value={workspaceID}
      />
      {update.error !== null ? (
        <p className="m-0 text-[var(--color-error)]">{errorMessage(update.error)}</p>
      ) : null}
      <Button disabled={disabled || update.isPending} type="submit" variant="primary">
        {t("task.save")}
      </Button>
    </form>
  );
}
