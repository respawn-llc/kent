/* eslint-disable max-lines -- Task detail content keeps the editable header, properties, and feed panes colocated. */
import { useId, useMemo, useState } from "react";
import { Check, Save } from "lucide-react";
import { useTranslation } from "react-i18next";

import type { TaskDetail, TeleportTarget } from "../../api";
import { errorMessage } from "../../api/errors";
import { useConnectionSnapshot } from "../../app/useConnectionSnapshot";
import { useAppServices } from "../../app/useAppServices";
import { Button, Island } from "../../ui";
import { fieldInputClassName } from "../../ui/Field";
import { cx } from "../../ui/classes";
import { fieldLabelClassName } from "../../ui/fieldStyles";
import { useUpdateTask } from "../tasks/useTaskMutations";
import { ActivityFeed, Comments } from "./TaskDetailActivity";
import { TaskInbox } from "./TaskDetailInbox";
import { TaskTabs, type DetailTab } from "./TaskDetailTabs";
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
      className="grid min-h-full content-start gap-[var(--space-2)] pb-[var(--space-2)]"
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
        className="grid gap-[var(--space-2)] min-[512px]:grid-cols-[minmax(0,7fr)_minmax(260px,3fr)]"
        data-testid="task-detail-body-split"
      >
        <DescriptionIsland
          detail={detail}
          disabled={disabled || update.isPending}
          draft={draft}
          error={update.error}
          onDraftChange={setDraft}
          onSave={saveDraft}
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
      className="grid gap-[var(--space-2)] px-[var(--space-4)] py-[var(--space-2)]"
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
          <button
            aria-label={t("task.saveTitle")}
            className="grid h-9 w-9 shrink-0 place-items-center rounded-full border border-[var(--color-outline)] bg-[var(--color-island-1)] text-[var(--color-on-island)] disabled:opacity-55"
            disabled={disabled || title.trim().length === 0}
            type="submit"
          >
            <Check aria-hidden="true" size={18} strokeWidth={1.8} />
          </button>
        ) : null}
        <span className="shrink-0 font-mono text-sm uppercase text-[var(--color-muted)]">
          {detail.shortID}
        </span>
      </form>
    </Island>
  );
}

function DescriptionIsland({
  detail,
  disabled,
  draft,
  error,
  onDraftChange,
  onSave,
}: Readonly<{
  detail: TaskDetail;
  disabled: boolean;
  draft: TaskDraft;
  error: unknown;
  onDraftChange: (draft: TaskDraft) => void;
  onSave: (draft?: TaskDraft) => Promise<void>;
}>) {
  const { t } = useTranslation();
  const descriptionId = useId();
  const descriptionErrorId = `${descriptionId}-error`;
  const descriptionError = error == null ? "" : errorMessage(error);
  const descriptionDirty = draft.body !== detail.body;
  const saveDisabled = disabled || draft.title.trim().length === 0 || !descriptionDirty;
  return (
    <Island
      aria-label={t("task.description")}
      className="grid gap-[var(--space-3)]"
      data-testid="task-detail-description-island"
    >
      <label className={fieldLabelClassName} htmlFor={descriptionId}>
        {t("task.description")}
      </label>
      <div className="grid" data-testid="task-description-input-frame">
        <textarea
          aria-describedby={descriptionError.length > 0 ? descriptionErrorId : undefined}
          aria-invalid={descriptionError.length > 0 ? true : undefined}
          className={cx(fieldInputClassName, "col-start-1 row-start-1 block min-h-[220px] resize-y pb-0")}
          disabled={disabled}
          id={descriptionId}
          onChange={(event) => {
            onDraftChange({ ...draft, body: event.target.value });
          }}
          placeholder={t("task.bodyPlaceholder")}
          value={draft.body}
        />
        <Button
          aria-hidden={!descriptionDirty}
          aria-label={t("task.save")}
          className={cx(
            "col-start-1 row-start-1 grid h-9 w-9 place-items-center self-end justify-self-end rounded-full !p-0 transition-opacity duration-[var(--motion-fast)]",
            descriptionDirty ? "opacity-100" : "pointer-events-none opacity-0",
          )}
          data-testid="task-description-save"
          disabled={saveDisabled}
          onClick={() => {
            void onSave();
          }}
          style={{ marginBottom: "var(--space-2)", marginRight: "var(--space-2)" }}
          tabIndex={descriptionDirty ? undefined : -1}
          variant="primary"
        >
          <Save aria-hidden="true" size={18} strokeWidth={1.8} />
        </Button>
      </div>
      {descriptionError.length > 0 ? (
        <span className="text-[var(--color-error)]" id={descriptionErrorId}>
          {descriptionError}
        </span>
      ) : null}
    </Island>
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
  const { api, nativeBridge } = useAppServices();
  const [confirmCancel, setConfirmCancel] = useState(false);
  const [openCliError, setOpenCliError] = useState("");
  const cliSessionExists = useMemo(
    () => detail.runs.some((run) => run.sessionID.trim().length > 0),
    [detail.runs],
  );
  const activeRuns = useMemo(
    () => detail.runs.filter((run) => run.completedAt === 0 && run.interruptedAt === 0),
    [detail.runs],
  );
  const resumeID = resumeRunId.length > 0 ? resumeRunId : detail.actions.resumeRunID;
  const terminalAvailable = nativeBridge.capabilities.terminal.launchBuilderSession;

  async function openInCli(): Promise<void> {
    const target = await api.getTeleportTarget(detail.id, "");
    if (!target.available) {
      setOpenCliError(target.failureReason || t("task.teleportUnavailable"));
      return;
    }
    await nativeBridge.terminal.launchBuilderSession({
      sessionId: target.sessionID,
      cwd: teleportCwd(teleportRoot(detail, target), target.cwdRelpath),
    });
  }

  return (
    <Island aria-label={t("task.properties")} className="grid content-start gap-[var(--space-3)]">
      <PropertyLine label={t("task.project")} value={detail.projectName} />
      <PropertyLine label={t("task.status")} value={detail.status.label} />
      <PropertyLine label={t("task.workspace")} value={detail.sourceWorkspace.name} />
      <PropertyLine label={t("task.workflow")} value={detail.workflowName} />
      <PropertyLine label={t("task.sessions")} value={detail.runs.length.toString()} />
      <div className="grid gap-[var(--space-2)] pt-[var(--space-1)]">
        {cliSessionExists ? (
          <Button
            disabled={disabled || !terminalAvailable}
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
          <Button
            disabled={disabled}
            onClick={() => {
              setConfirmCancel(true);
            }}
            variant="danger"
          >
            {t("task.cancel")}
          </Button>
        ) : null}
      </div>
      {!terminalAvailable && cliSessionExists ? (
        <p className="m-0 text-sm text-[var(--color-muted)]">{t("task.teleportUnavailable")}</p>
      ) : null}
      {openCliError.length > 0 ? (
        <p className="m-0 text-sm text-[var(--color-error)]">{openCliError}</p>
      ) : null}
      {confirmCancel ? (
        <div className="grid gap-[var(--space-2)] rounded-[var(--radius-l)] border border-[var(--color-outline)] bg-[var(--color-island-1)] p-[var(--space-3)]">
          <strong>{t("task.cancelConfirmTitle")}</strong>
          <p className="m-0">{t("task.cancelConfirmBody")}</p>
          <Button
            disabled={disabled}
            onClick={() => {
              void mutations.cancel.mutateAsync();
            }}
            variant="danger"
          >
            {t("app.confirm")}
          </Button>
        </div>
      ) : null}
    </Island>
  );
}

function PropertyLine({ label, value }: Readonly<{ label: string; value: string }>) {
  return (
    <p className="m-0 min-w-0 text-sm">
      {label}: <span className="text-[var(--color-muted)]">{value}</span>
    </p>
  );
}

function teleportCwd(worktreePath: string, cwdRelpath: string): string {
  if (worktreePath.length === 0) {
    return "";
  }
  if (cwdRelpath.length === 0) {
    return worktreePath;
  }
  return `${worktreePath}/${cwdRelpath}`;
}

function teleportRoot(detail: TaskDetail, target: TeleportTarget): string {
  return target.worktreeID.length > 0 ? detail.worktreePath : detail.sourceWorkspace.rootPath;
}
