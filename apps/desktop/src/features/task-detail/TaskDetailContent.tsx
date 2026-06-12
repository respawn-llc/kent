import { useId, useMemo, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Save } from "lucide-react";

import type { TaskDetail, TaskRun } from "../../api";
import { errorMessage } from "../../api/errors";
import { useConnectionSnapshot } from "../../app/useConnectionSnapshot";
import { useAppServices } from "../../app/useAppServices";
import {
  Button,
  Island,
  Popover,
  PopoverContent,
  PopoverTrigger,
  showStatusToast,
} from "../../ui";
import { fieldIslandInputClassName } from "../../ui/fieldInputStyles";
import { cx } from "../../ui/classes";
import { useUpdateTask } from "../tasks/useTaskMutations";
import { ActivityFeed, Comments } from "./TaskDetailActivity";
import { TaskInbox } from "./TaskDetailInbox";
import { TaskTabs, type DetailTab } from "./TaskDetailTabs";
import { taskStatusTone } from "./taskStatusTone";
import { useTaskMutations } from "./useTaskDetailData";
import type { useTaskActivity } from "./useTaskDetailData";

type TaskDraft = Readonly<{
  title: string;
  body: string;
}>;

type TaskDraftState = Readonly<{
  sourceKey: string;
  draft: TaskDraft;
}>;

export function TaskDetailContent({
  activity,
  detail,
  initialFocus,
  onMutated,
  openLink,
  resumeRunId,
}: Readonly<{
  activity: ReturnType<typeof useTaskActivity>;
  detail: TaskDetail;
  initialFocus?: "firstQuestion" | undefined;
  onMutated?: (() => void) | undefined;
  openLink: (url: string) => void;
  resumeRunId: string;
}>) {
  const { t } = useTranslation();
  const [tab, setTab] = useState<DetailTab>("comments");
  const draftSourceKey = taskDraftSourceKey(detail);
  const [draftState, setDraftState] = useState<TaskDraftState>(() => ({
    sourceKey: draftSourceKey,
    draft: taskDraft(detail),
  }));
  const draft = draftState.sourceKey === draftSourceKey ? draftState.draft : taskDraft(detail);
  const setDraft = (nextDraft: TaskDraft): void => {
    setDraftState({ sourceKey: draftSourceKey, draft: nextDraft });
  };
  const update = useUpdateTask(detail.id);
  const mutations = useTaskMutations(detail.id, onMutated);
  const connection = useConnectionSnapshot();
  const disabled = connection.phase !== "connected";
  const activityItems = activity.data?.pages.flatMap((page) => page.items) ?? [];

  async function saveDraft(nextDraft: TaskDraft = draft): Promise<void> {
    await update.mutateAsync({
      taskID: detail.id,
      title: nextDraft.title,
      body: nextDraft.body,
    });
    onMutated?.();
  }

  return (
    <div
      className="task-detail-island-stack grid min-h-full content-start gap-[var(--space-2)] pb-[var(--space-2)]"
      data-testid="task-detail-island-stack"
    >
      <TaskHeaderIsland
        detail={detail}
        disabled={disabled || update.isPending}
        draft={draft}
        onDraftChange={setDraft}
        onSave={saveDraft}
      />
      <div
        className="task-detail-body-split grid items-stretch gap-[var(--space-2)]"
        data-testid="task-detail-body-split"
      >
        <DescriptionIsland
          disabled={disabled || update.isPending}
          draft={draft}
          error={update.error}
          onDraftChange={setDraft}
        />
        <PropertiesIsland
          detail={detail}
          disabled={disabled}
          mutations={mutations}
          resumeRunId={resumeRunId}
        />
      </div>
      {detail.attention.length > 0 ? (
        <TaskInbox
          currentVersion={detail.workflowVersion}
          detail={detail}
          disabled={disabled}
          focusFirstQuestion={initialFocus === "firstQuestion"}
          mutations={mutations}
        />
      ) : null}
      <Island
        aria-label={tab === "comments" ? t("task.comments") : t("task.activity")}
        className="grid gap-[var(--space-3)]"
      >
        <TaskTabs
          activityCount={activityItems.length}
          commentCount={detail.comments.length}
          selected={tab}
          onSelect={setTab}
        />
        {tab === "comments" ? (
          <Comments
            comments={detail.comments}
            disabled={disabled}
            mutations={mutations}
            openLink={openLink}
          />
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
      </Island>
    </div>
  );
}

function taskDraft(detail: TaskDetail): TaskDraft {
  return { title: detail.title, body: detail.body };
}

function taskDraftSourceKey(detail: TaskDetail): string {
  return `${detail.id}:${detail.updatedAt.toString()}`;
}

function TaskHeaderIsland({
  detail,
  disabled,
  draft,
  onDraftChange,
  onSave,
}: Readonly<{
  detail: TaskDetail;
  disabled: boolean;
  draft: TaskDraft;
  onDraftChange: (draft: TaskDraft) => void;
  onSave: (draft?: TaskDraft) => Promise<void>;
}>) {
  const { t } = useTranslation();
  const title = draft.title;
  const dirty = draft.title !== detail.title || draft.body !== detail.body;

  function nextTitle(value: string): TaskDraft {
    return { ...draft, title: value.replaceAll("\n", " ") };
  }

  return (
    <Island
      className="grid gap-[var(--space-2)] py-[var(--space-2)] pr-[var(--space-2)] pl-[var(--space-4)]"
      data-testid="task-detail-title-island"
      unpadded
    >
      <form
        className="flex min-w-0 items-center gap-[var(--space-3)]"
        onSubmit={(event) => {
          event.preventDefault();
          void onSave();
        }}
      >
        <input
          aria-label={t("task.name")}
          className="app-region-no-drag min-w-0 flex-1 rounded-[var(--radius-m)] border border-transparent bg-transparent px-0 py-[var(--space-1)] text-[1.125rem] font-bold text-[var(--color-on-island)] outline-none focus:border-[var(--color-outline)] focus:bg-[var(--color-island-1)] focus:px-[var(--space-3)]"
          disabled={disabled}
          onChange={(event) => {
            onDraftChange(nextTitle(event.target.value));
          }}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault();
              event.currentTarget.form?.requestSubmit();
            }
          }}
          type="text"
          value={title}
        />
        {dirty ? (
          <Button
            aria-label={t("task.save")}
            className="shrink-0"
            data-testid="task-detail-save"
            disabled={disabled || title.trim().length === 0}
            size="icon"
            type="submit"
            variant="primary"
          >
            <Save aria-hidden="true" size={16} strokeWidth={1.75} />
          </Button>
        ) : null}
      </form>
    </Island>
  );
}

function DescriptionIsland({
  disabled,
  draft,
  error,
  onDraftChange,
}: Readonly<{
  disabled: boolean;
  draft: TaskDraft;
  error: unknown;
  onDraftChange: (draft: TaskDraft) => void;
}>) {
  const { t } = useTranslation();
  const descriptionId = useId();
  const descriptionErrorId = `${descriptionId}-error`;
  const descriptionError = error == null ? "" : errorMessage(error);
  return (
    <div className="grid min-h-0 min-w-0 gap-[var(--space-2)]" data-testid="task-description-input-frame">
      <textarea
        aria-describedby={descriptionError.length > 0 ? descriptionErrorId : undefined}
        aria-invalid={descriptionError.length > 0 ? true : undefined}
        aria-label={t("task.description")}
        className={cx(fieldIslandInputClassName(0), "block min-h-[220px] resize-none p-[var(--space-4)]")}
        disabled={disabled}
        id={descriptionId}
        onChange={(event) => {
          onDraftChange({ ...draft, body: event.target.value });
        }}
        placeholder={t("task.bodyPlaceholder")}
        value={draft.body}
      />
      {descriptionError.length > 0 ? (
        <span className="text-[var(--color-error)]" id={descriptionErrorId}>
          {descriptionError}
        </span>
      ) : null}
    </div>
  );
}

function PropertiesIsland({
  detail,
  disabled,
  mutations,
  resumeRunId,
}: Readonly<{
  detail: TaskDetail;
  disabled: boolean;
  mutations: ReturnType<typeof useTaskMutations>;
  resumeRunId: string;
}>) {
  const { t } = useTranslation();
  const { nativeBridge } = useAppServices();
  const [openCliError, setOpenCliError] = useState("");
  const cliSessionExists = useMemo(
    () => detail.runs.some((run) => run.sessionID.trim().length > 0),
    [detail.runs],
  );
  const cliCommand = useMemo(() => builderSessionCommand(detail.runs), [detail.runs]);
  const activeRuns = useMemo(
    () => detail.runs.filter((run) => run.completedAt === 0 && run.interruptedAt === 0),
    [detail.runs],
  );
  const resumeID = resumeRunId.length > 0 ? resumeRunId : detail.actions.resumeRunID;

  async function openInCli(): Promise<void> {
    if (cliCommand.length === 0) {
      setOpenCliError(t("task.cliCommandUnavailable"));
      return;
    }
    await copyText(cliCommand, nativeBridge);
    showStatusToast({
      id: "task-cli-command-copied",
      title: t("task.cliCommandCopied"),
      tone: "success",
    });
  }

  return (
    <Island aria-label={t("task.properties")} className="grid min-w-0 content-start gap-[var(--space-3)]">
      <PropertyLine label={t("task.identifier", { defaultValue: "ID" })} value={<span className="font-mono">{detail.shortID}</span>} />
      <PropertyLine label={t("task.project")} value={detail.projectName} />
      <PropertyLine
        label={t("task.status")}
        value={<TaskStatusText label={detail.status.label} tone={taskStatusTone(detail.status)} />}
      />
      <PropertyLine label={t("task.workspace")} value={detail.sourceWorkspace.name} />
      <PropertyLine label={t("task.workflow")} value={detail.workflowName} />
      <PropertyLine label={t("task.sessions")} value={detail.runs.length.toString()} />
      <div className="grid gap-[var(--space-2)] pt-[var(--space-1)]">
        {cliSessionExists ? (
          <Button
            disabled={disabled || cliCommand.length === 0}
            onClick={() => {
              setOpenCliError("");
              void openInCli().catch((cause: unknown) => {
                setOpenCliError(errorMessage(cause));
              });
            }}
            variant="secondary"
          >
            {t("task.openInCli")}
          </Button>
        ) : null}
        {detail.actions.canResume ? (
          <Button
            disabled={disabled}
            onClick={() => {
              void mutations.resume.mutateAsync(resumeID);
            }}
            variant="primary"
          >
            {t("board.resume")}
          </Button>
        ) : null}
        {activeRuns.map((run) => (
          <Button
            disabled={disabled}
            key={run.id}
            onClick={() => {
              void mutations.interrupt.mutateAsync(run.id);
            }}
            variant="secondary"
          >
            {t("board.interrupt")} <span className="font-mono">{run.id}</span>
          </Button>
        ))}
        {detail.actions.canCancel ? (
          <Popover>
            <PopoverTrigger asChild>
              <Button disabled={disabled} variant="danger">
                {t("task.cancel")}
              </Button>
            </PopoverTrigger>
            <PopoverContent align="end" className="w-56" side="top">
              <strong>{t("task.cancelConfirmTitle")}</strong>
              <Button
                disabled={disabled}
                onClick={() => {
                  void mutations.cancel.mutateAsync();
                }}
                variant="danger"
              >
                {t("app.confirm")}
              </Button>
            </PopoverContent>
          </Popover>
        ) : null}
      </div>
      {openCliError.length > 0 ? (
        <p className="m-0 text-sm text-[var(--color-error)]">{openCliError}</p>
      ) : null}
    </Island>
  );
}

function TaskStatusText({ label, tone }: Readonly<{ label: string; tone: ReturnType<typeof taskStatusTone> }>) {
  return <span className={cx("font-bold", taskStatusTextClassName(tone))}>{label}</span>;
}

function taskStatusTextClassName(tone: ReturnType<typeof taskStatusTone>): string {
  if (tone === "info") {
    return "text-[var(--color-primary)]";
  }
  if (tone === "success") {
    return "text-[var(--color-success)]";
  }
  if (tone === "warning") {
    return "text-[var(--color-warning)]";
  }
  if (tone === "danger") {
    return "text-[var(--color-error)]";
  }
  return "text-[var(--color-muted)]";
}

function PropertyLine({ label, value }: Readonly<{ label: string; value: ReactNode }>) {
  return (
    <p className="m-0 flex min-w-0 flex-wrap items-center gap-[var(--space-1)] text-sm">
      {label}: <span className="text-[var(--color-muted)]">{value}</span>
    </p>
  );
}

async function copyText(
  value: string,
  nativeBridge: ReturnType<typeof useAppServices>["nativeBridge"],
): Promise<void> {
  if (nativeBridge.capabilities.clipboard.writeText) {
    await nativeBridge.clipboard.writeText(value);
    return;
  }
  await navigator.clipboard.writeText(value);
}

function builderSessionCommand(runs: readonly TaskRun[]): string {
  const run = preferredSessionRun(runs);
  return run === null ? "" : `builder --session=${run.sessionID}`;
}

function preferredSessionRun(runs: readonly TaskRun[]): TaskRun | null {
  const sessionRuns = runs.filter((run) => run.sessionID.trim().length > 0);
  return (
    [...sessionRuns].reverse().find((run) => run.completedAt === 0 && run.interruptedAt === 0) ??
    sessionRuns.at(-1) ??
    null
  );
}
